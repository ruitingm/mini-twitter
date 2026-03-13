package tweet

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yourorg/mini-twitter/pkg/db"
)

// Tweet is the core domain model for a tweet stored in Postgres.
type Tweet struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Content   string     `json:"content"`
	LikeCount int        `json:"like_count"`
	ReplyToID *uuid.UUID `json:"reply_to_id,omitempty"` // nil for top-level tweets
	CreatedAt time.Time  `json:"created_at"`
}

// Repository provides typed SQL access for tweet data.
type Repository struct {
	db *db.DB
}

// NewRepository wraps the shared DB handle.
func NewRepository(d *db.DB) *Repository {
	return &Repository{db: d}
}

// Create inserts a new tweet and back-fills ID and CreatedAt from the RETURNING clause.
func (r *Repository) Create(ctx context.Context, t *Tweet) error {
	q := `INSERT INTO tweets (user_id, content, reply_to_id) VALUES ($1, $2, $3) RETURNING id, created_at`
	// QueryRow writes to the primary and scans the generated fields back into t
	return r.db.Primary.QueryRow(ctx, q, t.UserID, t.Content, t.ReplyToID).Scan(&t.ID, &t.CreatedAt)
}

// GetByID fetches a single tweet by its UUID from a read replica.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Tweet, error) {
	t := &Tweet{}
	err := r.db.Reader().QueryRow(ctx,
		`SELECT id, user_id, content, like_count, reply_to_id, created_at FROM tweets WHERE id = $1`, id).
		Scan(&t.ID, &t.UserID, &t.Content, &t.LikeCount, &t.ReplyToID, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Delete removes a tweet only if the caller is the author (user_id check prevents unauthorised deletion).
func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := r.db.Primary.Exec(ctx,
		`DELETE FROM tweets WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// No row was deleted — either the tweet doesn't exist or doesn't belong to this user
		return pgx.ErrNoRows
	}
	return nil
}

// IncrLikeCount adds delta (positive or negative) to the stored like_count for a tweet.
// Used by the LikeAggregator when flushing batched like events.
func (r *Repository) IncrLikeCount(ctx context.Context, tweetID uuid.UUID, delta int) error {
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE tweets SET like_count = like_count + $1 WHERE id = $2`, delta, tweetID)
	return err
}

// InsertLike records that userID liked tweetID in the likes table.
// ON CONFLICT DO NOTHING prevents duplicate-like errors.
func (r *Repository) InsertLike(ctx context.Context, userID, tweetID uuid.UUID) error {
	_, err := r.db.Primary.Exec(ctx,
		`INSERT INTO likes (user_id, tweet_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, tweetID)
	return err
}

// DeleteLike removes a like row (unlike operation).
func (r *Repository) DeleteLike(ctx context.Context, userID, tweetID uuid.UUID) error {
	_, err := r.db.Primary.Exec(ctx,
		`DELETE FROM likes WHERE user_id = $1 AND tweet_id = $2`, userID, tweetID)
	return err
}

// InsertLikePending writes a durable like-delta record used as a fallback when
// the Redis like-count key is unavailable. delta is +1 for like, -1 for unlike.
func (r *Repository) InsertLikePending(ctx context.Context, tweetID uuid.UUID, delta int) error {
	_, err := r.db.Primary.Exec(ctx,
		`INSERT INTO like_count_pending (tweet_id, delta) VALUES ($1, $2)`, tweetID, delta)
	return err
}

// FlushPendingLikes returns the sum of pending deltas per tweet created before `before`,
// then deletes those rows so they are only applied once.
func (r *Repository) FlushPendingLikes(ctx context.Context, before time.Time) (map[uuid.UUID]int, error) {
	// Aggregate all pending deltas grouped by tweet so we do one UPDATE per tweet
	rows, err := r.db.Primary.Query(ctx,
		`SELECT tweet_id, SUM(delta) FROM like_count_pending WHERE created_at < $1 GROUP BY tweet_id`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uuid.UUID]int)
	for rows.Next() {
		var id uuid.UUID
		var sum int
		if err := rows.Scan(&id, &sum); err != nil {
			return nil, err
		}
		result[id] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Delete flushed rows to prevent them from being applied a second time
	_, err = r.db.Primary.Exec(ctx, `DELETE FROM like_count_pending WHERE created_at < $1`, before)
	return result, err
}

// GetByUserID returns a page of tweets authored by userID, newest first.
// If before is non-nil, only tweets older than that timestamp are returned (cursor pagination).
func (r *Repository) GetByUserID(ctx context.Context, userID uuid.UUID, limit int, before *time.Time) ([]*Tweet, error) {
	var rows pgx.Rows
	var err error
	if before == nil {
		rows, err = r.db.Reader().Query(ctx,
			`SELECT id, user_id, content, like_count, reply_to_id, created_at FROM tweets WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
			userID, limit)
	} else {
		rows, err = r.db.Reader().Query(ctx,
			`SELECT id, user_id, content, like_count, reply_to_id, created_at FROM tweets WHERE user_id = $1 AND created_at < $2 ORDER BY created_at DESC LIMIT $3`,
			userID, *before, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTweets(rows)
}

// GetByIDs fetches multiple tweets by ID in a single query (for timeline enrichment).
// Results are ordered by created_at DESC so the timeline remains sorted.
func (r *Repository) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*Tweet, error) {
	rows, err := r.db.Reader().Query(ctx,
		`SELECT id, user_id, content, like_count, reply_to_id, created_at FROM tweets WHERE id = ANY($1) ORDER BY created_at DESC`,
		ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTweets(rows)
}

// GetTimelineFromDB fetches the home timeline directly from PostgreSQL (fan-out-on-read).
// It queries tweets from all followees at once using ANY($1).
func (r *Repository) GetTimelineFromDB(ctx context.Context, followeeIDs []uuid.UUID, limit int, before *time.Time) ([]*Tweet, error) {
	var rows pgx.Rows
	var err error
	if before == nil {
		rows, err = r.db.Reader().Query(ctx,
			`SELECT id, user_id, content, like_count, reply_to_id, created_at FROM tweets WHERE user_id = ANY($1) ORDER BY created_at DESC LIMIT $2`,
			followeeIDs, limit)
	} else {
		rows, err = r.db.Reader().Query(ctx,
			`SELECT id, user_id, content, like_count, reply_to_id, created_at FROM tweets WHERE user_id = ANY($1) AND created_at < $2 ORDER BY created_at DESC LIMIT $3`,
			followeeIDs, *before, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTweets(rows)
}

// scanTweets iterates a pgx.Rows result set and scans each row into a *Tweet.
func scanTweets(rows pgx.Rows) ([]*Tweet, error) {
	var tweets []*Tweet
	for rows.Next() {
		t := &Tweet{}
		if err := rows.Scan(&t.ID, &t.UserID, &t.Content, &t.LikeCount, &t.ReplyToID, &t.CreatedAt); err != nil {
			return nil, err
		}
		tweets = append(tweets, t)
	}
	return tweets, rows.Err()
}
