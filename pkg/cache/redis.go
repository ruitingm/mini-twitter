package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Client wraps the go-redis Client to allow adding helper methods.
type Client struct {
	*redis.Client
}

// New creates a Redis client, pings the server to verify connectivity, and returns the client.
func New(addr, password string) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,     // e.g. "localhost:6379"
		Password: password, // empty string means no auth
	})
	// Ping ensures the server is reachable before the service starts accepting traffic
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb}, nil
}

// TimelineKey returns the Redis list key that stores a user's pre-built home timeline.
// Format: "timeline:<userID>"
func TimelineKey(userID string) string {
	return "timeline:" + userID
}

// LikeCountKey returns the Redis string key that holds the accumulated (unflush) like delta
// for a tweet. The LikeAggregator reads this with GETDEL and writes it to Postgres.
// Format: "like_count:<tweetID>"
func LikeCountKey(tweetID string) string {
	return "like_count:" + tweetID
}

// RateKey returns the Redis counter key used for per-IP rate limiting.
// Each key covers a single minute window identified by the Unix minute number.
// Format: "rate:<ip>:<unixMinute>"
func RateKey(ip string, unixMinute int64) string {
	return fmt.Sprintf("rate:%s:%d", ip, unixMinute)
}
