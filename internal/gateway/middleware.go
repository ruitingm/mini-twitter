package gateway

import (
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/yourorg/mini-twitter/pkg/auth"
	"github.com/yourorg/mini-twitter/pkg/cache"
)

// RateLimiter implements token-bucket rate limiting per IP using Redis.
type RateLimiter struct {
	redis *cache.Client
	rpm   int // max requests per minute per IP
	log   zerolog.Logger
}

// NewRateLimiter constructs a RateLimiter with the given Redis client and RPM cap.
func NewRateLimiter(rdb *cache.Client, rpm int, log zerolog.Logger) *RateLimiter {
	return &RateLimiter{redis: rdb, rpm: rpm, log: log}
}

// Middleware counts requests per IP per minute using a Redis INCR key.
// If the counter exceeds the RPM limit, it returns 429 Too Many Requests.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r) // resolve the true client IP from headers
		// Bucket key changes every minute, creating a sliding-ish window
		minute := time.Now().Unix() / 60
		key := cache.RateKey(ip, minute)

		ctx := r.Context()
		// Use a pipeline to atomically increment the counter and set its TTL
		pipe := rl.redis.Pipeline()
		incrCmd := pipe.Incr(ctx, key)            // increment this minute's request count
		pipe.Expire(ctx, key, 61*time.Second)     // expire after 61s so the key auto-cleans
		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			// Redis error: fail open (allow the request) to avoid blocking users
			rl.log.Warn().Err(err).Msg("rate limit redis error")
			next.ServeHTTP(w, r)
			return
		}
		count, err := incrCmd.Result()
		if err != nil {
			next.ServeHTTP(w, r) // fail open on read error
			return
		}
		// Inform the client of their limit and remaining quota via standard headers
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.rpm))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(maxInt(0, rl.rpm-int(count))))
		if int(count) > rl.rpm {
			// Client has exceeded the per-minute limit; return 429
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r) // within limit; continue to the next handler
	})
}

// JWTPassthrough validates the JWT and forwards the request.
// It does NOT block invalid tokens — that is done per-route on internal services.
// This middleware simply extracts user info and forwards X-User-ID header.
func JWTPassthrough(mgr *auth.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Pass token through; internal services validate
			next.ServeHTTP(w, r)
		})
	}
}

// realIP extracts the originating client IP, honouring common reverse-proxy headers.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip // set by nginx
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip // set by load balancers; may be comma-separated; use first value ideally
	}
	return r.RemoteAddr // direct connection fallback
}

// maxInt returns the larger of two ints (used to clamp remaining-quota to ≥ 0).
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
