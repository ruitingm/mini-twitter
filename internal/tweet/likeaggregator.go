package tweet

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/yourorg/mini-twitter/pkg/cache"
	"github.com/yourorg/mini-twitter/pkg/metrics"
)

// LikeAggregator batches like/unlike operations that arrive in Redis and
// periodically flushes the accumulated deltas to Postgres.
// This avoids a DB write on every single like, dramatically reducing write load.
type LikeAggregator struct {
	repo     *Repository
	redis    *cache.Client
	interval time.Duration // how often to flush Redis deltas to Postgres
	log      zerolog.Logger
	tracked  chan uuid.UUID // IDs of tweets with pending like changes
}

// NewLikeAggregator creates a LikeAggregator with a channel large enough to hold
// up to 100,000 pending tweet IDs before dropping (non-blocking TrackTweet).
func NewLikeAggregator(repo *Repository, rdb *cache.Client, interval time.Duration, log zerolog.Logger) *LikeAggregator {
	return &LikeAggregator{
		repo:     repo,
		redis:    rdb,
		interval: interval,
		log:      log,
		tracked:  make(chan uuid.UUID, 100000),
	}
}

// TrackTweet records that a tweet's like count may have changed.
// It is non-blocking: if the channel is full the tweet will be handled by the DB fallback.
func (la *LikeAggregator) TrackTweet(tweetID uuid.UUID) {
	select {
	case la.tracked <- tweetID:
	default:
		// channel full; tweet will be picked up by DB fallback
	}
}

// Start launches the background flush loop in a goroutine.
func (la *LikeAggregator) Start(ctx context.Context) {
	go la.run(ctx)
}

// run is the main event loop: it collects tweet IDs and flushes them on a ticker.
func (la *LikeAggregator) run(ctx context.Context) {
	ticker := time.NewTicker(la.interval)
	defer ticker.Stop()

	// pending holds the set of tweet IDs we've seen since the last flush
	pending := make(map[uuid.UUID]struct{})

	for {
		select {
		case <-ctx.Done():
			return // service is shutting down
		case id := <-la.tracked:
			pending[id] = struct{}{} // dedup: only flush each tweet once per interval
		case <-ticker.C:
			la.flush(ctx, pending)
			pending = make(map[uuid.UUID]struct{}) // reset for the next interval
		}
	}
}

// flush applies all pending like deltas to the tweets table.
// For each tweet it first tries to read the delta from Redis (GETDEL removes the key atomically).
// If Redis is empty it falls back to the like_count_pending DB table for durability.
func (la *LikeAggregator) flush(ctx context.Context, pending map[uuid.UUID]struct{}) {
	if len(pending) == 0 {
		return
	}

	flushTime := time.Now()
	metrics.LikeAggregatorFlushTotal.Inc() // increment Prometheus counter

	for tweetID := range pending {
		key := cache.LikeCountKey(tweetID.String())
		// GETDEL atomically reads and deletes the Redis key containing the accumulated delta
		val, err := la.redis.GetDel(ctx, key).Int()
		if err == redis.Nil || val == 0 {
			// Redis delta is gone (e.g. key expired or was never written).
			// Fall back to the durable like_count_pending table to avoid losing counts.
			sums, err := la.repo.FlushPendingLikes(ctx, flushTime)
			if err != nil {
				la.log.Error().Err(err).Msg("flush pending likes from DB failed")
				return
			}
			// Apply each tweet's sum from the DB fallback
			for id, delta := range sums {
				if err := la.repo.IncrLikeCount(ctx, id, delta); err != nil {
					la.log.Error().Err(err).Str("tweet_id", id.String()).Msg("incr like count failed")
				}
			}
			return
		}
		if err != nil {
			la.log.Warn().Err(err).Str("tweet_id", tweetID.String()).Msg("getdel redis error")
			continue
		}
		// Apply the Redis delta directly to the tweets.like_count column
		if err := la.repo.IncrLikeCount(ctx, tweetID, val); err != nil {
			la.log.Error().Err(err).Str("tweet_id", tweetID.String()).Msg("incr like count failed")
		}
		// Clean up any matching rows in the DB pending table
		la.repo.FlushPendingLikes(ctx, flushTime)
	}
}
