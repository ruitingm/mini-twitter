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
	"github.com/yourorg/mini-twitter/internal/timeline"
	"github.com/yourorg/mini-twitter/pkg/auth"
	"github.com/yourorg/mini-twitter/pkg/cache"
	"github.com/yourorg/mini-twitter/pkg/config"
	"github.com/yourorg/mini-twitter/pkg/db"
	"github.com/yourorg/mini-twitter/pkg/logger"
	"github.com/yourorg/mini-twitter/pkg/metrics"
)

func main() {
	// Create a structured logger tagged with the service name
	log := logger.New("timeline-service")

	// Load configuration from environment variables
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx := context.Background()
	// Connect to Postgres (primary + optional replicas for read scaling)
	database, err := db.New(ctx, cfg.PostgresPrimaryURL, cfg.PostgresReplicaURLs, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close() // release all connection pools on exit

	// Connect to Redis, used for caching pre-built timelines
	rdb, err := cache.New(cfg.RedisAddr, cfg.RedisPassword)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to redis")
	}

	// Build the JWT manager (shared secret + expiry from config)
	authMgr := auth.NewManager(cfg.JWTSecret, cfg.JWTExpiry)
	// Repository handles all DB and Redis data access for timelines
	repo := timeline.NewRepository(database, rdb, log)
	// Service implements the core timeline logic (fan-out-on-write vs fan-out-on-read)
	svc := timeline.NewService(repo, cfg.TweetServiceURL, cfg.FanoutStrategy, log)
	// Handler wires HTTP routes to service methods
	h := timeline.NewHandler(svc, log)

	r := chi.NewRouter()
	r.Use(middleware.RequestID) // attach a unique request ID header
	r.Use(middleware.Recoverer) // recover from panics
	// Mount routes under /v1; home timeline requires auth, user timeline is public
	r.Mount("/v1", h.Routes(auth.Middleware(authMgr)))
	r.Handle("/metrics", metrics.Handler()) // Prometheus metrics endpoint

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	// Start serving in a background goroutine
	go func() {
		log.Info().Str("addr", addr).Msg("timeline service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	// Wait for SIGINT or SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Gracefully drain requests with a 15-second deadline
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	log.Info().Msg("timeline service stopped")
}
