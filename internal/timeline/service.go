package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	defaultLimit     = 50             // used when the caller passes an out-of-range limit
	readModeCacheTTL = 60 * time.Second // how long to cache timelines in fan-out-on-read mode
)

// Tweet is a minimal copy of the tweet model for timeline responses.
// The timeline service fetches these from the tweet service rather than querying tweets directly.
type Tweet struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Content   string     `json:"content"`
	LikeCount int        `json:"like_count"`
	ReplyToID *uuid.UUID `json:"reply_to_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Service implements timeline retrieval supporting Redis caching vs direct PostgreSQL:
//   - useRedis=true:  tweet IDs are cached in Redis (fan-out-on-write)
//   - useRedis=false: tweet IDs are fetched from PostgreSQL at read time
type Service struct {
	repo            *Repository
	tweetServiceURL string
	useRedis        bool   // true = Redis caching, false = direct PostgreSQL
	log             zerolog.Logger
	httpClient      *http.Client
}

// NewService creates a Service. The httpClient has a 5-second timeout for tweet service calls.
func NewService(repo *Repository, tweetServiceURL string, useRedis bool, log zerolog.Logger) *Service {
	return &Service{
		repo:            repo,
		tweetServiceURL: tweetServiceURL,
		useRedis:        useRedis,
		log:             log,
		httpClient:      &http.Client{Timeout: 5 * time.Second},
	}
}

// GetHomeTimeline returns the home feed for userID with cursor-based pagination.
// It first tries Redis (fast), then falls back to Postgres on a cache miss.
func (s *Service) GetHomeTimeline(ctx context.Context, userID uuid.UUID, limit int, before *time.Time) ([]*Tweet, error) {
	// Clamp limit to a safe range
	if limit <= 0 || limit > 100 {
		limit = defaultLimit
	}

	if s.useRedis {
		// fan-out-on-write: the tweet service pre-populated our Redis list
		ids, hit, err := s.repo.GetHomeTimelineIDs(ctx, userID, limit)
		if err != nil {
			s.log.Warn().Err(err).Msg("redis timeline read failed, falling back to DB")
		}
		if hit && len(ids) > 0 {
			// Cache hit: enrich the IDs into full tweet objects via the tweet service
			return s.enrichTweets(ctx, ids)
		}
		// Redis miss: reconstruct the timeline from Postgres
		s.log.Info().Str("user_id", userID.String()).Msg("timeline cache miss, reading from DB")
		followeeIDs, err := s.repo.GetFolloweeIDs(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("get followees: %w", err)
		}
		ids, err = s.repo.GetHomeTimelineFromDB(ctx, followeeIDs, limit, before)
		if err != nil {
			return nil, fmt.Errorf("db timeline: %w", err)
		}
		return s.enrichTweets(ctx, ids)
	}

	// Direct PostgreSQL strategy: always query who the user follows, then read their tweets
	followeeIDs, err := s.repo.GetFolloweeIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get followees: %w", err)
	}
	// Still check Redis for a short-lived cache to reduce repeat DB hits
	cachedIDs, hit, _ := s.repo.GetHomeTimelineIDs(ctx, userID, limit)
	if hit && len(cachedIDs) > 0 {
		return s.enrichTweets(ctx, cachedIDs)
	}
	ids, err := s.repo.GetHomeTimelineFromDB(ctx, followeeIDs, limit, before)
	if err != nil {
		return nil, fmt.Errorf("db timeline: %w", err)
	}
	// Store the result in Redis briefly in a background goroutine (non-blocking)
	go s.repo.CacheHomeTimeline(context.Background(), userID, ids, readModeCacheTTL)
	return s.enrichTweets(ctx, ids)
}

// GetUserTimeline returns the public tweet history for a specific user.
// It always reads from Postgres because user timelines are not pre-cached.
func (s *Service) GetUserTimeline(ctx context.Context, userID uuid.UUID, limit int, before *time.Time) ([]*Tweet, error) {
	if limit <= 0 || limit > 100 {
		limit = defaultLimit
	}
	ids, err := s.repo.GetUserTimelineFromDB(ctx, userID, limit, before)
	if err != nil {
		return nil, err
	}
	return s.enrichTweets(ctx, ids)
}

// enrichTweets calls the tweet service's /v1/tweets/batch endpoint to hydrate IDs
// into full Tweet objects (content, like count, etc.).
func (s *Service) enrichTweets(ctx context.Context, ids []uuid.UUID) ([]*Tweet, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build the JSON request body: {"ids": ["uuid1", "uuid2", ...]}
	type batchReq struct {
		IDs []uuid.UUID `json:"ids"`
	}
	body, _ := json.Marshal(batchReq{IDs: ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.tweetServiceURL+"/v1/tweets/batch", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request to the tweet service
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tweet service call: %w", err)
	}
	defer resp.Body.Close()

	// Read and decode the JSON array of Tweet objects
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tweets []*Tweet
	if err := json.Unmarshal(respBody, &tweets); err != nil {
		return nil, fmt.Errorf("decode tweet batch: %w", err)
	}
	return tweets, nil
}
