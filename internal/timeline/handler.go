package timeline

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/yourorg/mini-twitter/pkg/auth"
)

// Handler holds the service layer and a logger for the timeline HTTP endpoints.
type Handler struct {
	svc *Service
	log zerolog.Logger
}

// NewHandler constructs a Handler wrapping the given service.
func NewHandler(svc *Service, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Routes registers timeline endpoints on a new chi router.
// The home timeline requires authentication; the user timeline is public.
func (h *Handler) Routes(authMiddleware func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	r.With(authMiddleware).Get("/timeline/home", h.HomeTimeline) // requires valid JWT
	r.Get("/timeline/user/{id}", h.UserTimeline)                  // public
	return r
}

// HomeTimeline returns the authenticated user's personalised feed.
func (h *Handler) HomeTimeline(w http.ResponseWriter, r *http.Request) {
	// Extract the caller's user ID that the auth middleware stored in context
	userID, ok := auth.UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	limit, before := parsePaginationParams(r)
	tweets, err := h.svc.GetHomeTimeline(r.Context(), userID, limit, before)
	if err != nil {
		h.log.Error().Err(err).Msg("home timeline failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tweets": tweets})
}

// UserTimeline returns the public tweet history for any user ID in the URL.
func (h *Handler) UserTimeline(w http.ResponseWriter, r *http.Request) {
	// Parse the {id} URL segment as a UUID
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	limit, before := parsePaginationParams(r)
	tweets, err := h.svc.GetUserTimeline(r.Context(), userID, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tweets": tweets})
}

// parsePaginationParams reads the optional "limit" and "before" query params.
// "limit" is the max number of tweets to return (default 50).
// "before" is a Unix nanosecond timestamp used as a cursor for the next page.
func parsePaginationParams(r *http.Request) (int, *time.Time) {
	limit := 50 // default page size
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	var before *time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		// Expect Unix nanoseconds so we can page through tweets without gaps
		if ns, err := strconv.ParseInt(b, 10, 64); err == nil {
			t := time.Unix(0, ns)
			before = &t
		}
	}
	return limit, before
}

// writeJSON sets the Content-Type header, writes the status code, and JSON-encodes the value.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError is a convenience wrapper that sends a JSON error body.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
