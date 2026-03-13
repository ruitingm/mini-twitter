package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// Config holds the base URLs of the three downstream microservices.
type Config struct {
	UserServiceURL     string
	TweetServiceURL    string
	TimelineServiceURL string
}

// Handler holds one reverse proxy per downstream service.
type Handler struct {
	userProxy     *httputil.ReverseProxy
	tweetProxy    *httputil.ReverseProxy
	timelineProxy *httputil.ReverseProxy
	log           zerolog.Logger
}

// NewHandler parses the three service URLs and creates a reverse proxy for each.
func NewHandler(cfg Config, log zerolog.Logger) (*Handler, error) {
	// Parse each URL string into a *url.URL so the proxy knows where to forward
	userURL, err := url.Parse(cfg.UserServiceURL)
	if err != nil {
		return nil, err
	}
	tweetURL, err := url.Parse(cfg.TweetServiceURL)
	if err != nil {
		return nil, err
	}
	timelineURL, err := url.Parse(cfg.TimelineServiceURL)
	if err != nil {
		return nil, err
	}

	return &Handler{
		// SingleHostReverseProxy rewrites every incoming request to target the given host
		userProxy:     httputil.NewSingleHostReverseProxy(userURL),
		tweetProxy:    httputil.NewSingleHostReverseProxy(tweetURL),
		timelineProxy: httputil.NewSingleHostReverseProxy(timelineURL),
		log:           log,
	}, nil
}

// Routes returns a chi router with every public API path mapped to the correct proxy.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Route("/v1", func(r chi.Router) {
		// User service routes
		r.Post("/auth/register", h.proxyTo(h.userProxy))          // create a new account
		r.Post("/auth/login", h.proxyTo(h.userProxy))             // exchange credentials for JWT
		r.Get("/users/{username}", h.proxyTo(h.userProxy))        // fetch public user profile
		r.Put("/users/me", h.proxyTo(h.userProxy))                // update own profile (auth required)
		r.Post("/users/{id}/follow", h.proxyTo(h.userProxy))      // follow a user (auth required)
		r.Delete("/users/{id}/follow", h.proxyTo(h.userProxy))    // unfollow a user (auth required)

		// Tweet service routes
		r.Post("/tweets", h.proxyTo(h.tweetProxy))                // create a tweet (auth required)
		r.Delete("/tweets/{id}", h.proxyTo(h.tweetProxy))         // delete own tweet (auth required)
		r.Get("/tweets/{id}", h.proxyTo(h.tweetProxy))            // fetch a single tweet
		r.Post("/tweets/{id}/like", h.proxyTo(h.tweetProxy))      // like a tweet (auth required)
		r.Delete("/tweets/{id}/like", h.proxyTo(h.tweetProxy))    // unlike a tweet (auth required)

		// Timeline service routes
		r.Get("/timeline/home", h.proxyTo(h.timelineProxy))       // home feed (auth required)
		r.Get("/timeline/user/{id}", h.proxyTo(h.timelineProxy))  // any user's public tweet history
	})

	return r
}

// proxyTo returns an http.HandlerFunc that delegates the request to the given proxy.
func (h *Handler) proxyTo(proxy *httputil.ReverseProxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r) // forward request and stream the upstream response back
	}
}
