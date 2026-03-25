package tweet

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"mini-twitter/pkg/auth"
)

// Handler holds the service layer and logger for tweet HTTP endpoints.
type Handler struct {
	svc *Service
	log zerolog.Logger
}

// NewHandler constructs a Handler wrapping the given service.
func NewHandler(svc *Service, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Routes registers all tweet endpoints on a new chi router.
// Write operations (create, delete, like) require a valid JWT via authMiddleware.
func (h *Handler) Routes(authMiddleware func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	r.With(authMiddleware).Post("/tweets", h.CreateTweet)           // POST  /v1/tweets
	r.With(authMiddleware).Delete("/tweets/{id}", h.DeleteTweet)    // DELETE /v1/tweets/{id}
	r.With(authMiddleware).Post("/tweets/{id}/like", h.LikeTweet)   // POST  /v1/tweets/{id}/like
	r.With(authMiddleware).Delete("/tweets/{id}/like", h.UnlikeTweet) // DELETE /v1/tweets/{id}/like
	r.Get("/tweets/{id}", h.GetTweet)                               // GET   /v1/tweets/{id} (public)
	// Internal endpoint called by the timeline service to bulk-fetch tweet details
	r.Post("/tweets/batch", h.GetBatch)
	return r
}

// CreateTweet handles POST /v1/tweets.
// Reads content and optional reply_to_id from the request body and calls the service.
func (h *Handler) CreateTweet(w http.ResponseWriter, r *http.Request) {
	// Retrieve the authenticated user's ID injected by the JWT middleware
	userID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Content   string     `json:"content"`
		ReplyToID *uuid.UUID `json:"reply_to_id,omitempty"` // nil for top-level tweets
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tweet, err := h.svc.CreateTweet(r.Context(), userID, req.Content, req.ReplyToID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tweet) // 201 Created with the new tweet JSON
}

// DeleteTweet handles DELETE /v1/tweets/{id}.
// Only the tweet author can delete; the service enforces the ownership check.
func (h *Handler) DeleteTweet(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// Parse the {id} URL segment as a UUID
	tweetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tweet id")
		return
	}
	if err := h.svc.DeleteTweet(r.Context(), tweetID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent) // 204 No Content on success
}

// LikeTweet handles POST /v1/tweets/{id}/like.
func (h *Handler) LikeTweet(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tweetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tweet id")
		return
	}
	if err := h.svc.LikeTweet(r.Context(), userID, tweetID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UnlikeTweet handles DELETE /v1/tweets/{id}/like.
func (h *Handler) UnlikeTweet(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tweetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tweet id")
		return
	}
	if err := h.svc.UnlikeTweet(r.Context(), userID, tweetID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetTweet handles GET /v1/tweets/{id} — public, no auth required.
func (h *Handler) GetTweet(w http.ResponseWriter, r *http.Request) {
	tweetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tweet id")
		return
	}
	tweet, err := h.svc.GetTweet(r.Context(), tweetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "tweet not found")
		return
	}
	writeJSON(w, http.StatusOK, tweet)
}

// GetBatch handles POST /v1/tweets/batch — internal endpoint for the timeline service.
// Accepts {"ids": [...]} and returns a JSON array of full tweet objects.
func (h *Handler) GetBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tweets, err := h.svc.GetTweetsByIDs(r.Context(), req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tweets)
}

// writeJSON sets Content-Type, writes the status code, and JSON-encodes v.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError is a convenience helper that sends a JSON error object.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
