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
	"mini-twitter/internal/tweet"
	"mini-twitter/internal/user"
	"mini-twitter/pkg/auth"
	"mini-twitter/pkg/cache"
	"mini-twitter/pkg/config"
	"mini-twitter/pkg/db"
	"mini-twitter/pkg/logger"
)

func main() {
	log := logger.New("tweet-service")

	cfg, err := config.Load("")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Cancellable root context so background workers (fanout, aggregator) stop cleanly
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to Postgres with primary/replica topology
	database, err := db.New(ctx, cfg.PostgresPrimaryURL, cfg.PostgresReplicaURLs, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close()

	// Connect to Redis for timeline fanout and like count caching
	rdb, err := cache.New(cfg.RedisAddr, cfg.RedisPassword)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to redis")
	}

	authMgr := auth.NewManager(cfg.JWTSecret, cfg.JWTExpiry)
	// userRepo is used by the fanout worker to look up follower IDs
	userRepo := user.NewRepository(database)
	tweetRepo := tweet.NewRepository(database)
	// LikeAggregator batches Redis like-count increments and flushes them to Postgres periodically
	aggregator := tweet.NewLikeAggregator(tweetRepo, rdb, cfg.AggregatorInterval, log)
	// FanoutWorker pushes new tweet IDs into each follower's Redis timeline list
	fanoutWorker := tweet.NewFanoutWorker(cfg.FanoutChanBuffer, cfg.FanoutWorkerCount, rdb, userRepo, log)
	// Service orchestrates tweet CRUD, likes, and the fanout/consistency strategy
	svc := tweet.NewService(tweetRepo, rdb, fanoutWorker, aggregator, cfg.FanoutStrategy, cfg.ConsistencyMode, log)
	h := tweet.NewHandler(svc, log)

	// Start background goroutines before accepting HTTP traffic
	fanoutWorker.Start(ctx)
	aggregator.Start(ctx)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Mount("/v1", h.Routes(auth.Middleware(authMgr)))

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		log.Info().Str("addr", addr).Msg("tweet service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	// Block until shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	cancel() // stop fanout worker and aggregator goroutines

	// Give in-flight HTTP requests 15 seconds to finish
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	log.Info().Msg("tweet service stopped")
}
