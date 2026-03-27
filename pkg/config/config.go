package config

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all runtime configuration read from environment variables.
// Each field maps to an env var via the `envconfig` tag; `required:"true"` causes
// a fatal error at startup if the variable is missing.
type Config struct {
	// Service identity
	ServiceName string `envconfig:"SERVICE_NAME" default:"mini-twitter"`
	Port        int    `envconfig:"PORT" default:"8080"` // HTTP listen port

	// Database — primary handles writes; replicas handle reads
	PostgresPrimaryURL  string   `envconfig:"POSTGRES_PRIMARY_URL"`
	PostgresReplicaURLs []string `envconfig:"POSTGRES_REPLICA_URLS"` // comma-separated list; optional

	// Redis — single node; used for timelines, rate limiting, and like counts
	RedisAddr     string `envconfig:"REDIS_ADDR" default:"localhost:6379"`
	RedisPassword string `envconfig:"REDIS_PASSWORD"` // empty = no auth

	// Auth — symmetric HMAC secret for JWT signing
	JWTSecret string        `envconfig:"JWT_SECRET" required:"true"`
	JWTExpiry time.Duration `envconfig:"JWT_EXPIRY" default:"24h"` // token lifetime

	// Feature flags
	UseRedis        bool   `envconfig:"USE_REDIS" default:"true"`           // true = Redis caching, false = direct PostgreSQL
	ConsistencyMode string `envconfig:"CONSISTENCY_MODE" default:"eventual"` // "eventual" or "strong"

	// Fanout worker pool — controls throughput of timeline fan-out
	FanoutWorkerCount  int           `envconfig:"FANOUT_WORKER_COUNT" default:"10"`    // number of goroutines
	FanoutChanBuffer   int           `envconfig:"FANOUT_CHAN_BUFFER" default:"10000"`  // job queue size
	AggregatorInterval time.Duration `envconfig:"AGGREGATOR_INTERVAL" default:"30s"`  // like-flush cadence

	// Internal service URLs used by the gateway to proxy requests
	UserServiceURL     string `envconfig:"USER_SERVICE_URL" default:"http://localhost:8081"`
	TweetServiceURL    string `envconfig:"TWEET_SERVICE_URL" default:"http://localhost:8082"`
	TimelineServiceURL string `envconfig:"TIMELINE_SERVICE_URL" default:"http://localhost:8083"`

	// Rate limiting — maximum requests per minute per IP at the gateway
	RateLimitRPM int `envconfig:"RATE_LIMIT_RPM" default:"60"`
}

// Load reads all environment variables (with the optional prefix) into a Config struct.
func Load(prefix string) (*Config, error) {
	var cfg Config
	if err := envconfig.Process(prefix, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
