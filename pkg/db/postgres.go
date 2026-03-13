package db

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// DB holds a primary connection pool for writes and zero or more replica pools for reads.
// Replicas are selected in round-robin order using an atomic counter.
type DB struct {
	Primary  *pgxpool.Pool   // receives all INSERT/UPDATE/DELETE queries
	replicas []*pgxpool.Pool // receive SELECT queries for read scaling
	counter  atomic.Uint64   // incremented each time Reader() is called for round-robin
	log      zerolog.Logger
}

// New opens a connection pool to the primary Postgres instance and (optionally) to each replica.
// Replica connection failures are non-fatal: the replica is skipped and a warning is logged.
func New(ctx context.Context, primaryURL string, replicaURLs []string, log zerolog.Logger) (*DB, error) {
	// Connect to the primary; this is mandatory — fatal if it fails
	primary, err := pgxpool.New(ctx, primaryURL)
	if err != nil {
		return nil, fmt.Errorf("primary pool: %w", err)
	}
	// Ping the primary to confirm the connection is live
	if err := primary.Ping(ctx); err != nil {
		return nil, fmt.Errorf("primary ping: %w", err)
	}

	// Attempt to connect to each replica; skip those that are unreachable
	replicas := make([]*pgxpool.Pool, 0, len(replicaURLs))
	for _, url := range replicaURLs {
		pool, err := pgxpool.New(ctx, url)
		if err != nil {
			log.Warn().Err(err).Str("url", url).Msg("failed to connect replica, skipping")
			continue
		}
		if err := pool.Ping(ctx); err != nil {
			log.Warn().Err(err).Str("url", url).Msg("replica ping failed, skipping")
			continue
		}
		replicas = append(replicas, pool)
	}

	return &DB{Primary: primary, replicas: replicas, log: log}, nil
}

// Reader returns a replica pool for reads, falling back to primary if no replicas are available.
// Replicas are chosen in round-robin order to distribute read load evenly.
func (d *DB) Reader() *pgxpool.Pool {
	if len(d.replicas) == 0 {
		// No replicas configured: send reads to the primary
		return d.Primary
	}
	// Atomic increment + modulo gives lock-free round-robin selection
	idx := d.counter.Add(1) % uint64(len(d.replicas))
	pool := d.replicas[idx]
	// Quick liveness check via Stats (non-blocking) — fall back to primary if down
	if pool.Stat().TotalConns() == 0 {
		d.log.Warn().Msg("replica appears down, falling back to primary")
		return d.Primary
	}
	return pool
}

// Close releases all connection pools (primary and replicas) when the service shuts down.
func (d *DB) Close() {
	d.Primary.Close()
	for _, r := range d.replicas {
		r.Close()
	}
}
