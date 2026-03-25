package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourorg/mini-twitter/internal/gateway"
	"github.com/yourorg/mini-twitter/pkg/auth"
	"github.com/yourorg/mini-twitter/pkg/cache"
	"github.com/yourorg/mini-twitter/pkg/config"
	"github.com/yourorg/mini-twitter/pkg/logger"
)

func main() {
	// Create a structured logger tagged with the service name "gateway"
	log := logger.New("gateway")

	// Load configuration from environment variables; fatal if any required var is missing
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Connect to Redis, used for rate limiting
	rdb, err := cache.New(cfg.RedisAddr, cfg.RedisPassword)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to redis")
	}

	// Build the JWT manager for signing/validating tokens
	authMgr := auth.NewManager(cfg.JWTSecret, cfg.JWTExpiry)
	// Build the per-IP rate limiter backed by Redis
	rateLimiter := gateway.NewRateLimiter(rdb, cfg.RateLimitRPM, log)
	// Build the reverse-proxy handler that routes requests to the three internal services
	h, err := gateway.NewHandler(gateway.Config{
		UserServiceURL:     cfg.UserServiceURL,
		TweetServiceURL:    cfg.TweetServiceURL,
		TimelineServiceURL: cfg.TimelineServiceURL,
	}, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create handler")
	}

	// Set up the chi router with shared middleware
	r := chi.NewRouter()
	r.Use(middleware.RequestID) // attach a unique X-Request-ID to every request
	r.Use(middleware.Recoverer) // recover from panics and return 500 instead of crashing
	r.Use(rateLimiter.Middleware) // enforce per-IP request rate limits
	r.Use(gateway.JWTPassthrough(authMgr)) // forward JWT headers; validation happens inside each service
	r.Mount("/", h.Routes())               // mount all proxy routes under "/"

	// Format the listen address, e.g. ":8080"
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	// Start the HTTP server in a goroutine so we can wait for a shutdown signal below
	go func() {
		log.Info().Str("addr", addr).Msg("gateway starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	// Block until the OS sends SIGINT (Ctrl-C) or SIGTERM (container stop)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Give in-flight requests up to 15 seconds to finish before shutting down
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	log.Info().Msg("gateway stopped")
}
