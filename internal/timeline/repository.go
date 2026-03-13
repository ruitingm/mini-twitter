package timeline

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/yourorg/mini-twitter/pkg/cache"
	"github.com/yourorg/mini-twitter/pkg/db"
	"github.com/yourorg/mini-twitter/pkg/metrics"
)

// Repository handles all data access (Postgres + Redis) for timelines.
type Repository struct {
	db    *db.DB
	redis *cache.Client
	log   zerolog.Logger
}

// NewRepository constructs a Repository with the given DB and Redis clients.
func NewRepository(d *db.DB, rdb *cache.Client, log zerolog.Logger) *Repository {
	return &Repository{db: d, redis: rdb, log: log}
}

// GetHomeTimelineIDs tries to read the user's pre-built timeline from Redis.
// Returns (ids, true, nil) on a cache hit, or (nil, false, nil) on a miss/error.
func (r *Repository) GetHomeTimelineIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, bool, error) {
	key := cache.TimelineKey(userID.String())
	// LRANGE fetches elements 0..limit-1 from the Redis list (newest-first order)
	strs, err := r.redis.LRange(ctx, key, 0, int64(limit-1)).Result()
	if err == redis.Nil || len(strs) == 0 {
		// Key doesn't exist or list is empty → cache miss
		metrics.RedisMisses.WithLabelValues("timeline").Inc()
		return nil, false, nil
	}
	if err != nil {
		r.log.Warn().Err(err).Msg("redis lrange failed")
		metrics.RedisMisses.WithLabelValues("timeline").Inc()
		return nil, false, nil
	}
	metrics.RedisHits.WithLabelValues("timeline").Inc()
	// Convert the string UUID slice returned by Redis into []uuid.UUID
	ids := make([]uuid.UUID, 0, len(strs))
	for _, s := range strs {
		id, err := uuid.Parse(s)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids, true, nil
}

// GetHomeTimelineFromDB queries Postgres for tweets from the users this person follows.
// If before is nil, returns the most recent tweets; otherwise pages backward from that timestamp.
func (r *Repository) GetHomeTimelineFromDB(ctx context.Context, followeeIDs []uuid.UUID, limit int, before *time.Time) ([]uuid.UUID, error) {
	if len(followeeIDs) == 0 {
		return nil, nil // user follows nobody; return an empty feed immediately
	}
	var results []uuid.UUID
	var queryErr error
	if before == nil {
		// First page: no cursor — order by created_at descending, take the latest `limit` tweets
		rows, err := r.db.Reader().Query(ctx,
			`SELECT id FROM tweets WHERE user_id = ANY($1) ORDER BY created_at DESC LIMIT $2`,
			followeeIDs, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			results = append(results, id)
		}
		queryErr = rows.Err()
	} else {
		// Subsequent page: only return tweets older than the cursor timestamp
		rows, err := r.db.Reader().Query(ctx,
			`SELECT id FROM tweets WHERE user_id = ANY($1) AND created_at < $2 ORDER BY created_at DESC LIMIT $3`,
			followeeIDs, *before, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			results = append(results, id)
		}
		queryErr = rows.Err()
	}
	return results, queryErr
}

// GetUserTimelineFromDB queries Postgres for a single user's own tweets with optional cursor.
func (r *Repository) GetUserTimelineFromDB(ctx context.Context, userID uuid.UUID, limit int, before *time.Time) ([]uuid.UUID, error) {
	var results []uuid.UUID
	var queryErr error
	if before == nil {
		rows, err := r.db.Reader().Query(ctx,
			`SELECT id FROM tweets WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
			userID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			results = append(results, id)
		}
		queryErr = rows.Err()
	} else {
		rows, err := r.db.Reader().Query(ctx,
			`SELECT id FROM tweets WHERE user_id = $1 AND created_at < $2 ORDER BY created_at DESC LIMIT $3`,
			userID, *before, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			results = append(results, id)
		}
		queryErr = rows.Err()
	}
	return results, queryErr
}

// CacheHomeTimeline stores a list of tweet IDs in Redis for fast future reads.
// The list is appended to the right (RPUSH) so LRange returns them in insertion order.
func (r *Repository) CacheHomeTimeline(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, ttl time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	key := cache.TimelineKey(userID.String())
	// Convert UUIDs to []interface{} as required by the Redis client variadic args
	vals := make([]interface{}, len(ids))
	for i, id := range ids {
		vals[i] = id.String()
	}
	// Pipeline the RPUSH + EXPIRE into one round-trip
	pipe := r.redis.Pipeline()
	pipe.RPush(ctx, key, vals...)   // append all IDs to the list
	pipe.Expire(ctx, key, ttl)     // reset TTL so the list auto-expires
	_, err := pipe.Exec(ctx)
	return err
}

// GetFolloweeIDs returns the list of user IDs that userID is following (for fan-out-on-read).
func (r *Repository) GetFolloweeIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Reader().Query(ctx,
		`SELECT followee_id FROM follows WHERE follower_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
