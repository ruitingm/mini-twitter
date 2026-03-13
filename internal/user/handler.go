package user

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/yourorg/mini-twitter/pkg/auth"
)

// Handler holds the user service and logger for HTTP endpoints.
type Handler struct {
	svc *Service
	log zerolog.Logger
}

// NewHandler constructs a Handler wrapping the given service.
func NewHandler(svc *Service, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Routes registers all user endpoints on a new chi router.
// Routes that mutate state or read private data are protected by authMiddleware.
func (h *Handler) Routes(authMiddleware func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/auth/register", h.Register)                           // create account (no auth)
	r.Post("/auth/login", h.Login)                                 // exchange credentials for JWT
	r.Get("/users/{username}", h.GetUser)                          // public profile
	r.With(authMiddleware).Put("/users/me", h.UpdateMe)            // update own profile
	r.With(authMiddleware).Post("/users/{id}/follow", h.Follow)    // follow a user
	r.With(authMiddleware).Delete("/users/{id}/follow", h.Unfollow) // unfollow a user
	return r
}

// Register handles POST /auth/register.
// Creates a new user account and returns the user object + a signed JWT.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// All three fields are mandatory
	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username, email, and password are required")
		return
	}
	user, token, err := h.svc.Register(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		h.log.Error().Err(err).Msg("register failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 201 Created with both the user object and a usable JWT
	writeJSON(w, http.StatusCreated, map[string]any{"user": user, "token": token})
}

// Login handles POST /auth/login.
// Validates credentials and returns a fresh JWT on success.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, token, err := h.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		// Return a generic message to avoid leaking whether the username exists
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "token": token})
}

// GetUser handles GET /users/{username} — returns the public profile for any user.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username") // extract the {username} path segment
	user, err := h.svc.GetByUsername(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// UpdateMe handles PUT /users/me — updates the authenticated user's display name and bio.
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	// Get the caller's user ID from the JWT middleware context
	userID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := h.svc.UpdateMe(r.Context(), userID, req.DisplayName, req.Bio)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// Follow handles POST /users/{id}/follow — the authenticated user starts following {id}.
func (h *Handler) Follow(w http.ResponseWriter, r *http.Request) {
	// The follower is the authenticated caller
	followerID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// The followee is identified by the {id} URL param
	followeeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.svc.Follow(r.Context(), followerID, followeeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent) // 204 on success
}

// Unfollow handles DELETE /users/{id}/follow — the authenticated user stops following {id}.
func (h *Handler) Unfollow(w http.ResponseWriter, r *http.Request) {
	followerID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	followeeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.svc.Unfollow(r.Context(), followerID, followeeID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeJSON sets Content-Type, writes the status, and JSON-encodes the value.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError sends a standard JSON error object with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
