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
	"mini-twitter/internal/user"
	"mini-twitter/pkg/auth"
	"mini-twitter/pkg/config"
	"mini-twitter/pkg/db"
	"mini-twitter/pkg/logger"
)

func main() {
	log := logger.New("user-service")

	// Load all settings from environment variables
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx := context.Background()
	// Open connection pools to primary Postgres and any read replicas
	database, err := db.New(ctx, cfg.PostgresPrimaryURL, cfg.PostgresReplicaURLs, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close()

	// Wire up the three layers: JWT manager → repository → service → handler
	authMgr := auth.NewManager(cfg.JWTSecret, cfg.JWTExpiry)
	repo := user.NewRepository(database)       // handles raw SQL queries
	svc := user.NewService(repo, authMgr)      // business logic (register, login, follow, etc.)
	h := user.NewHandler(svc, log)             // HTTP request/response translation

	r := chi.NewRouter()
	r.Use(middleware.RequestID) // adds X-Request-ID to every request for tracing
	r.Use(middleware.Recoverer) // converts panics into 500 responses
	// Protected routes (UpdateMe, Follow, Unfollow) sit behind the JWT auth middleware
	r.Mount("/v1", h.Routes(auth.Middleware(authMgr)))

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	// Accept connections in the background
	go func() {
		log.Info().Str("addr", addr).Msg("user service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	// Wait for OS shutdown signal (Ctrl-C or `docker stop`)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Graceful shutdown: wait up to 15 seconds for active requests to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	log.Info().Msg("user service stopped")
}
