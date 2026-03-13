package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the custom JWT payload embedded in every token.
// It extends jwt.RegisteredClaims with application-specific fields.
type Claims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	jwt.RegisteredClaims // standard fields: ExpiresAt, IssuedAt, etc.
}

// Manager wraps the HMAC secret and token lifetime for signing and verifying JWTs.
type Manager struct {
	secret []byte        // HMAC-SHA256 signing key
	expiry time.Duration // how long a token is valid after issuance
}

// NewManager creates a Manager from the given secret string and expiry duration.
func NewManager(secret string, expiry time.Duration) *Manager {
	return &Manager{secret: []byte(secret), expiry: expiry}
}

// Sign creates a signed JWT for the given user and returns the compact token string.
func (m *Manager) Sign(userID uuid.UUID, username string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.expiry)), // token expires after `expiry`
			IssuedAt:  jwt.NewNumericDate(time.Now()),              // record when the token was created
		},
	}
	// Sign with HMAC-SHA256 using our shared secret
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse validates the token signature and expiry, then returns the decoded Claims.
// Returns an error if the token is invalid, expired, or uses an unexpected algorithm.
func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		// Reject tokens that were signed with anything other than HMAC (e.g. alg:none attacks)
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil // return the key so the library can verify the signature
	})
	if err != nil {
		return nil, err
	}
	// Type-assert Claims and confirm token.Valid (expiry check happened inside ParseWithClaims)
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
