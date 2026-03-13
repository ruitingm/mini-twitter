package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yourorg/mini-twitter/pkg/auth"
	"golang.org/x/crypto/bcrypt"
)

// Service implements the user domain business logic (registration, auth, social graph).
type Service struct {
	repo *Repository
	auth *auth.Manager
}

// NewService wires the repository and JWT manager into the service.
func NewService(repo *Repository, auth *auth.Manager) *Service {
	return &Service{repo: repo, auth: auth}
}

// Register creates a new account. It hashes the password with bcrypt, persists the user,
// then signs a JWT so the caller is immediately authenticated.
func (s *Service) Register(ctx context.Context, username, email, password string) (*User, string, error) {
	// bcrypt automatically generates a random salt; DefaultCost is a safe trade-off
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}
	u := &User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
	}
	// Persist; id and created_at are populated by the RETURNING clause in the repository
	if err := s.repo.Create(ctx, u); err != nil {
		return nil, "", fmt.Errorf("create user: %w", err)
	}
	// Sign a JWT containing the new user's ID and username
	token, err := s.auth.Sign(u.ID, u.Username)
	if err != nil {
		return nil, "", fmt.Errorf("sign token: %w", err)
	}
	return u, token, nil
}

// Login verifies username + password and returns the user with a fresh JWT on success.
func (s *Service) Login(ctx context.Context, username, password string) (*User, string, error) {
	// Load the user — both "not found" and "wrong password" return the same error
	// to avoid leaking whether the username exists (username enumeration prevention)
	u, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		return nil, "", errors.New("invalid credentials")
	}
	// Constant-time comparison between the provided password and the stored hash
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, "", errors.New("invalid credentials")
	}
	token, err := s.auth.Sign(u.ID, u.Username)
	if err != nil {
		return nil, "", fmt.Errorf("sign token: %w", err)
	}
	return u, token, nil
}

// GetByUsername fetches a user's public profile by their unique username.
func (s *Service) GetByUsername(ctx context.Context, username string) (*User, error) {
	return s.repo.GetByUsername(ctx, username)
}

// GetByID fetches a user by their UUID (used internally by other services).
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.repo.GetByID(ctx, id)
}

// UpdateMe loads the user, applies the new display name and bio, and persists the change.
func (s *Service) UpdateMe(ctx context.Context, userID uuid.UUID, displayName, bio string) (*User, error) {
	// Fetch the current record so we only change the two mutable fields
	u, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	u.DisplayName = displayName
	u.Bio = bio
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Follow creates a follow relationship from followerID to followeeID.
// Returns an error if a user tries to follow themselves.
func (s *Service) Follow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if followerID == followeeID {
		return errors.New("cannot follow yourself")
	}
	return s.repo.Follow(ctx, followerID, followeeID)
}

// Unfollow removes the follow relationship from followerID to followeeID.
func (s *Service) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	return s.repo.Unfollow(ctx, followerID, followeeID)
}

// GetFollowerIDs returns paginated follower IDs — called by the fanout worker.
func (s *Service) GetFollowerIDs(ctx context.Context, userID uuid.UUID, limit int, afterID *uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.GetFollowerIDs(ctx, userID, limit, afterID)
}

// GetFolloweeIDs returns all IDs that userID follows — called for fan-out-on-read timelines.
func (s *Service) GetFolloweeIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.GetFolloweeIDs(ctx, userID)
}
