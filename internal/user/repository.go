package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/mini-twitter/pkg/db"
)

// User is the domain model for a registered account.
// PasswordHash is excluded from JSON responses via the "-" tag.
type User struct {
	ID             uuid.UUID `json:"id"`
	Username       string    `json:"username"`
	Email          string    `json:"email,omitempty"`    // omitted when not the owner
	PasswordHash   string    `json:"-"`                  // never serialised to JSON
	DisplayName    string    `json:"display_name"`
	Bio            string    `json:"bio"`
	FollowerCount  int       `json:"follower_count"`
	FollowingCount int       `json:"following_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// Repository handles all SQL for user data.
type Repository struct {
	db *db.DB
}

// NewRepository wraps the shared DB handle.
func NewRepository(d *db.DB) *Repository {
	return &Repository{db: d}
}

// Create inserts a new user row and fills in the generated id and created_at.
func (r *Repository) Create(ctx context.Context, u *User) error {
	q := `INSERT INTO users (username, email, password_hash, display_name, bio)
          VALUES ($1, $2, $3, $4, $5)
          RETURNING id, created_at`
	return r.db.Primary.QueryRow(ctx, q, u.Username, u.Email, u.PasswordHash, u.DisplayName, u.Bio).
		Scan(&u.ID, &u.CreatedAt)
}

// GetByUsername looks up a user by their unique username (used during login).
func (r *Repository) GetByUsername(ctx context.Context, username string) (*User, error) {
	return r.queryOne(ctx, r.db.Reader(),
		`SELECT id, username, email, password_hash, display_name, bio, follower_count, following_count, created_at FROM users WHERE username = $1`,
		username)
}

// GetByID looks up a user by their UUID primary key.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return r.queryOne(ctx, r.db.Reader(),
		`SELECT id, username, email, password_hash, display_name, bio, follower_count, following_count, created_at FROM users WHERE id = $1`,
		id)
}

// Update persists changes to the user's display_name and bio fields.
func (r *Repository) Update(ctx context.Context, u *User) error {
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE users SET display_name=$1, bio=$2 WHERE id=$3`,
		u.DisplayName, u.Bio, u.ID)
	return err
}

// Follow inserts a follow relationship and atomically increments both counters in a transaction.
// ON CONFLICT DO NOTHING makes the operation idempotent if the follow already exists.
func (r *Repository) Follow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	tx, err := r.db.Primary.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op if Commit succeeds

	// Insert the follow row; silently succeeds if the relationship already exists
	_, err = tx.Exec(ctx,
		`INSERT INTO follows (follower_id, followee_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		followerID, followeeID)
	if err != nil {
		return err
	}
	// Bump the follower's "following" counter
	_, err = tx.Exec(ctx,
		`UPDATE users SET following_count = following_count + 1 WHERE id = $1`, followerID)
	if err != nil {
		return err
	}
	// Bump the followee's "followers" counter
	_, err = tx.Exec(ctx,
		`UPDATE users SET follower_count = follower_count + 1 WHERE id = $1`, followeeID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Unfollow removes the follow relationship and decrements both counters atomically.
// Uses GREATEST(... - 1, 0) to prevent the counter going negative if something got out of sync.
func (r *Repository) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	tx, err := r.db.Primary.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`DELETE FROM follows WHERE follower_id = $1 AND followee_id = $2`, followerID, followeeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // already not following; nothing to update
	}
	// Decrement following_count, floor at 0
	_, err = tx.Exec(ctx,
		`UPDATE users SET following_count = GREATEST(following_count - 1, 0) WHERE id = $1`, followerID)
	if err != nil {
		return err
	}
	// Decrement follower_count, floor at 0
	_, err = tx.Exec(ctx,
		`UPDATE users SET follower_count = GREATEST(follower_count - 1, 0) WHERE id = $1`, followeeID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetFollowerIDs returns follower IDs for a given user, paginated by cursor.
// afterID is used as a keyset cursor: only IDs > afterID are returned.
func (r *Repository) GetFollowerIDs(ctx context.Context, userID uuid.UUID, limit int, afterID *uuid.UUID) ([]uuid.UUID, error) {
	var rows pgx.Rows
	var err error
	if afterID == nil {
		// First page: no cursor, order by follower_id for stable pagination
		rows, err = r.db.Reader().Query(ctx,
			`SELECT follower_id FROM follows WHERE followee_id = $1 ORDER BY follower_id LIMIT $2`,
			userID, limit)
	} else {
		// Subsequent pages: only IDs greater than the cursor (keyset pagination)
		rows, err = r.db.Reader().Query(ctx,
			`SELECT follower_id FROM follows WHERE followee_id = $1 AND follower_id > $2 ORDER BY follower_id LIMIT $3`,
			userID, *afterID, limit)
	}
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

// GetFolloweeIDs returns IDs of users that userID follows (used by timeline fan-out-on-read).
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

// queryOne is a reusable helper that runs a single-row SELECT and scans all User fields.
func (r *Repository) queryOne(ctx context.Context, pool *pgxpool.Pool, q string, args ...any) (*User, error) {
	u := &User{}
	err := pool.QueryRow(ctx, q, args...).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash,
		&u.DisplayName, &u.Bio, &u.FollowerCount, &u.FollowingCount, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("queryOne: %w", err)
	}
	return u, nil
}
