package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// contextKey is a private type for context keys to avoid collisions with other packages.
type contextKey string

const (
	ContextKeyUserID   contextKey = "userID"   // key under which the parsed UUID is stored
	ContextKeyUsername contextKey = "username" // key under which the username string is stored
)

// Middleware returns an HTTP middleware that enforces JWT authentication.
// It reads the "Authorization: Bearer <token>" header, parses the token,
// and injects the user's ID and username into the request context.
// Requests with a missing or invalid token receive a 401 Unauthorized response.
func Middleware(mgr *Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			// Require the standard "Bearer <token>" format
			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"missing or invalid authorization header"}`, http.StatusUnauthorized)
				return
			}
			// Strip the "Bearer " prefix to get the raw JWT string
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := mgr.Parse(tokenStr)
			if err != nil {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			// Inject parsed values into the context so handlers can read them without re-parsing
			ctx := context.WithValue(r.Context(), ContextKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, ContextKeyUsername, claims.Username)
			next.ServeHTTP(w, r.WithContext(ctx)) // continue to the protected handler
		})
	}
}

// UserIDFromCtx retrieves the authenticated user's UUID from the request context.
// Returns (id, true) if present, or (zero, false) if the middleware didn't run.
func UserIDFromCtx(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ContextKeyUserID).(uuid.UUID)
	return id, ok
}

// UsernameFromCtx retrieves the authenticated user's username from the request context.
func UsernameFromCtx(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(ContextKeyUsername).(string)
	return name, ok
}
