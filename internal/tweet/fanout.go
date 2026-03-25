package tweet

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"mini-twitter/pkg/cache"
)

const (
	timelineMaxLen = 1000           // max tweet IDs kept per user's Redis timeline list
	timelineTTL    = 24 * time.Hour // Redis list expires after 24 hours of inactivity
	fanoutPageSize = 500            // number of followers fetched per DB page during fanout
)

// FanoutJob carries the information the fanout worker needs to update follower timelines.
type FanoutJob struct {
	TweetID  uuid.UUID
	AuthorID uuid.UUID
}

// FollowerFetcher abstracts the user service call to get follower IDs.
// Using an interface allows the fanout worker to be tested without a real DB.
type FollowerFetcher interface {
	GetFollowerIDs(ctx context.Context, userID uuid.UUID, limit int, afterID *uuid.UUID) ([]uuid.UUID, error)
}

// FanoutWorker runs a pool of goroutines that push new tweet IDs into each follower's
// Redis timeline list whenever a tweet is created (fan-out-on-write strategy).
type FanoutWorker struct {
	jobs    chan FanoutJob // buffered channel of pending fanout jobs
	redis   *cache.Client
	fetcher FollowerFetcher
	log     zerolog.Logger
	workers int // number of concurrent worker goroutines
}

// NewFanoutWorker creates a FanoutWorker with a job channel of size bufSize
// and workerCount concurrent goroutines ready to process jobs.
func NewFanoutWorker(bufSize, workerCount int, rdb *cache.Client, fetcher FollowerFetcher, log zerolog.Logger) *FanoutWorker {
	return &FanoutWorker{
		jobs:    make(chan FanoutJob, bufSize),
		redis:   rdb,
		fetcher: fetcher,
		log:     log,
		workers: workerCount,
	}
}

// Start launches workerCount goroutines that drain the jobs channel until ctx is cancelled.
func (fw *FanoutWorker) Start(ctx context.Context) {
	for i := 0; i < fw.workers; i++ {
		go fw.work(ctx)
	}
}

// Enqueue adds a fanout job to the channel.
// If the channel is full it drops the job (non-blocking) and logs a warning,
// rather than blocking the caller (tweet creation) indefinitely.
func (fw *FanoutWorker) Enqueue(job FanoutJob) {
	select {
	case fw.jobs <- job: // successfully queued
	default:
		// Channel full: shed load to protect the service
		fw.log.Warn().Str("tweet_id", job.TweetID.String()).Msg("fanout queue full, skipping fan-out")
	}
}

// work is the per-goroutine loop: it blocks on the jobs channel and processes each job.
func (fw *FanoutWorker) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return // context cancelled (service shutdown); exit the goroutine
		case job := <-fw.jobs:
			if err := fw.fanout(ctx, job); err != nil {
				fw.log.Error().Err(err).Str("tweet_id", job.TweetID.String()).Msg("fanout error")
			}
		}
	}
}

// fanout iterates through all followers of the author in pages of fanoutPageSize,
// and for each page pushes the tweet ID to the front of every follower's Redis timeline.
func (fw *FanoutWorker) fanout(ctx context.Context, job FanoutJob) error {
	var afterID *uuid.UUID      // cursor for paginating through followers
	tweetIDStr := job.TweetID.String()

	for {
		// Fetch the next page of follower IDs from the user service/repository
		ids, err := fw.fetcher.GetFollowerIDs(ctx, job.AuthorID, fanoutPageSize, afterID)
		if err != nil {
			return fmt.Errorf("get followers: %w", err)
		}
		if len(ids) == 0 {
			break // no more followers to update
		}

		// Build a Redis pipeline to update all followers in a single round-trip
		pipe := fw.redis.Pipeline()
		for _, followerID := range ids {
			key := cache.TimelineKey(followerID.String())
			pipe.LPush(ctx, key, tweetIDStr)              // prepend the new tweet ID (newest first)
			pipe.LTrim(ctx, key, 0, timelineMaxLen-1)     // keep at most 1000 entries
			pipe.Expire(ctx, key, timelineTTL)            // refresh TTL on every update
		}
		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			fw.log.Warn().Err(err).Msg("redis pipeline error during fanout")
		}

		// If we got fewer entries than the page size we've reached the last page
		if len(ids) < fanoutPageSize {
			break
		}
		// Advance the cursor to the last ID in this page for the next iteration
		last := ids[len(ids)-1]
		afterID = &last
	}
	return nil
}
