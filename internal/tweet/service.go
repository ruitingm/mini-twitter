package tweet

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/yourorg/mini-twitter/pkg/cache"
)

// Service implements the tweet domain business logic.
// It supports two fanout strategies and two consistency modes:
//   - fanoutStrategy "write": push tweet IDs to follower timelines at creation time
//   - fanoutStrategy "read":  timeline service fetches tweet IDs on demand
//   - consistencyMode "strong":   like counts are updated synchronously in Postgres
//   - consistencyMode "eventual": like counts go through Redis + background aggregator
type Service struct {
	repo            *Repository
	redis           *cache.Client
	fanout          *FanoutWorker
	aggregator      *LikeAggregator
	fanoutStrategy  string // "write" or "read"
	consistencyMode string // "eventual" or "strong"
	log             zerolog.Logger
}

// NewService wires together all dependencies needed by the tweet service layer.
func NewService(
	repo *Repository,
	rdb *cache.Client,
	fanout *FanoutWorker,
	aggregator *LikeAggregator,
	fanoutStrategy, consistencyMode string,
	log zerolog.Logger,
) *Service {
	return &Service{
		repo:            repo,
		redis:           rdb,
		fanout:          fanout,
		aggregator:      aggregator,
		fanoutStrategy:  fanoutStrategy,
		consistencyMode: consistencyMode,
		log:             log,
	}
}

// CreateTweet validates the content, persists the tweet, and (if configured)
// enqueues a fanout job to push the tweet ID into all follower timelines.
func (s *Service) CreateTweet(ctx context.Context, userID uuid.UUID, content string, replyToID *uuid.UUID) (*Tweet, error) {
	// Enforce Twitter-like character limit
	if len(content) == 0 || len(content) > 280 {
		return nil, errors.New("content must be 1-280 characters")
	}
	t := &Tweet{UserID: userID, Content: content, ReplyToID: replyToID}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("create tweet: %w", err)
	}
	// Trigger fan-out-on-write: push the tweet into every follower's Redis timeline
	if s.fanoutStrategy == "write" {
		s.fanout.Enqueue(FanoutJob{TweetID: t.ID, AuthorID: userID})
	}
	return t, nil
}

// DeleteTweet removes a tweet; the repository ensures only the owner can delete it.
func (s *Service) DeleteTweet(ctx context.Context, tweetID, userID uuid.UUID) error {
	return s.repo.Delete(ctx, tweetID, userID)
}

// LikeTweet records a like in the likes table and updates the like counter.
// In "strong" mode the count is updated synchronously; in "eventual" mode it goes
// through Redis first and is flushed to Postgres by the LikeAggregator later.
func (s *Service) LikeTweet(ctx context.Context, userID, tweetID uuid.UUID) error {
	// Always insert the like record for deduplication and audit purposes
	if err := s.repo.InsertLike(ctx, userID, tweetID); err != nil {
		return fmt.Errorf("insert like: %w", err)
	}
	if s.consistencyMode == "strong" {
		// Synchronous Postgres increment — consistent but higher write latency
		return s.repo.IncrLikeCount(ctx, tweetID, 1)
	}
	// Eventual consistency path: increment the Redis counter for fast reads
	key := cache.LikeCountKey(tweetID.String())
	if err := s.redis.Incr(ctx, key).Err(); err != nil {
		s.log.Warn().Err(err).Msg("redis incr like_count failed")
	}
	// Also write to like_count_pending as a durable fallback if Redis is lost
	if err := s.repo.InsertLikePending(ctx, tweetID, 1); err != nil {
		s.log.Warn().Err(err).Msg("insert like_count_pending failed")
	}
	// Tell the aggregator to include this tweet in the next flush cycle
	s.aggregator.TrackTweet(tweetID)
	return nil
}

// UnlikeTweet is the inverse of LikeTweet: removes the like record and decrements the count.
func (s *Service) UnlikeTweet(ctx context.Context, userID, tweetID uuid.UUID) error {
	if err := s.repo.DeleteLike(ctx, userID, tweetID); err != nil {
		return fmt.Errorf("delete like: %w", err)
	}
	if s.consistencyMode == "strong" {
		return s.repo.IncrLikeCount(ctx, tweetID, -1) // subtract 1 synchronously
	}
	// Eventual path: decrement Redis counter and record the -1 delta in the pending table
	key := cache.LikeCountKey(tweetID.String())
	if err := s.redis.Decr(ctx, key).Err(); err != nil {
		s.log.Warn().Err(err).Msg("redis decr like_count failed")
	}
	if err := s.repo.InsertLikePending(ctx, tweetID, -1); err != nil {
		s.log.Warn().Err(err).Msg("insert like_count_pending -1 failed")
	}
	s.aggregator.TrackTweet(tweetID)
	return nil
}

// GetTweet fetches a single tweet by ID (used by the public GET /tweets/{id} endpoint).
func (s *Service) GetTweet(ctx context.Context, tweetID uuid.UUID) (*Tweet, error) {
	return s.repo.GetByID(ctx, tweetID)
}

// GetTweetsByIDs fetches multiple tweets by ID (used by the timeline enrichment batch endpoint).
func (s *Service) GetTweetsByIDs(ctx context.Context, ids []uuid.UUID) ([]*Tweet, error) {
	return s.repo.GetByIDs(ctx, ids)
}
