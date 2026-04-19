package db

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// replicaEntry pairs a connection pool with a health flag that is updated
// by a background goroutine. Reader() skips replicas whose healthy flag is false.
type replicaEntry struct {
	pool    *pgxpool.Pool
	healthy atomic.Bool
}

// DB holds a primary connection pool for writes and zero or more replica pools for reads.
// Replicas are selected in round-robin order using an atomic counter.
// A background health checker pings each replica every 3 seconds and marks unhealthy
// replicas so that Reader() can skip them and fall back to a healthy replica or the primary.
type DB struct {
	Primary  *pgxpool.Pool   // receives all INSERT/UPDATE/DELETE queries
	replicas []replicaEntry  // receive SELECT queries for read scaling
	counter  atomic.Uint64   // incremented each time Reader() is called for round-robin
	log      zerolog.Logger
	cancel   context.CancelFunc // stops the health-checker goroutine on Close()
}

// New opens a connection pool to the primary Postgres instance and (optionally) to each replica.
// Replica connection failures are non-fatal: the replica is skipped and a warning is logged.
// A background health checker is started to continuously monitor replica availability.
func New(ctx context.Context, primaryURL string, replicaURLs []string, log zerolog.Logger) (*DB, error) {
	// Primary pool config
	primaryConfig, err := pgxpool.ParseConfig(primaryURL)
	if err != nil {
		return nil, fmt.Errorf("parse primary config: %w", err)
	}

	// Tune primary pool for write traffic + fallback reads
	primaryConfig.MaxConns = 40
	primaryConfig.MinConns = 5
	primaryConfig.MaxConnLifetime = 30 * time.Minute

	// Connect to the primary; this is mandatory — fatal if it fails
	primary, err := pgxpool.NewWithConfig(ctx, primaryConfig)
	if err != nil {
		return nil, fmt.Errorf("primary pool: %w", err)
	}

	// Ping the primary to confirm the connection is live
	if err := primary.Ping(ctx); err != nil {
		return nil, fmt.Errorf("primary ping: %w", err)
	}

	// Attempt to connect to each replica; skip those that are unreachable
	replicas := make([]replicaEntry, 0, len(replicaURLs))
	for _, url := range replicaURLs {
		replicaConfig, err := pgxpool.ParseConfig(url)
		if err != nil {
			log.Warn().Err(err).Str("url", url).Msg("failed to parse replica config, skipping")
			continue
		}

		// Tune replica pools for read traffic and faster failure detection
		replicaConfig.MaxConns = 20
		replicaConfig.MinConns = 2
		replicaConfig.MaxConnLifetime = 30 * time.Minute
		replicaConfig.HealthCheckPeriod = 5 * time.Second
		replicaConfig.MaxConnIdleTime = 15 * time.Second

		pool, err := pgxpool.NewWithConfig(ctx, replicaConfig)
		if err != nil {
			log.Warn().Err(err).Str("url", url).Msg("failed to connect replica, skipping")
			continue
		}
		if err := pool.Ping(ctx); err != nil {
			log.Warn().Err(err).Str("url", url).Msg("replica ping failed, skipping")
			pool.Close()
			continue
		}

		entry := replicaEntry{pool: pool}
		entry.healthy.Store(true)
		replicas = append(replicas, entry)
	}

	hcCtx, cancel := context.WithCancel(context.Background())

	db := &DB{
		Primary:  primary,
		replicas: replicas,
		log:      log,
		cancel:   cancel,
	}

	if len(replicas) > 0 {
		db.startHealthChecker(hcCtx)
	}

	return db, nil
}

// startHealthChecker spawns a goroutine that pings every replica every 3 seconds.
// If a replica fails to respond within 2 seconds, it is marked unhealthy.
// When a previously-unhealthy replica responds again, it is marked healthy.
func (d *DB) startHealthChecker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for i := range d.replicas {
					pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
					err := d.replicas[i].pool.Ping(pingCtx)
					pingCancel()

					wasHealthy := d.replicas[i].healthy.Load()
					if err != nil {
						if wasHealthy {
							d.replicas[i].healthy.Store(false)
							d.log.Warn().Int("replica", i).Err(err).Msg("replica health check failed, marking unhealthy")
						}
					} else {
						if !wasHealthy {
							d.replicas[i].healthy.Store(true)
							d.log.Info().Int("replica", i).Msg("replica recovered, marking healthy")
						}
					}
				}
			}
		}
	}()
}

// Reader returns a replica pool for reads, falling back to primary if no healthy replicas are available.
// Replicas are chosen in round-robin order; unhealthy replicas are skipped.
func (d *DB) Reader() *pgxpool.Pool {
	n := uint64(len(d.replicas))
	if n == 0 {
		// No replicas configured: send reads to the primary
		return d.Primary
	}

	// Atomic increment gives lock-free round-robin starting position
	start := d.counter.Add(1)
	for i := uint64(0); i < n; i++ {
		idx := (start + i) % n
		if d.replicas[idx].healthy.Load() {
			return d.replicas[idx].pool
		}
	}

	// All replicas unhealthy: fall back to primary
	d.log.Warn().Msg("all replicas unhealthy, falling back to primary")
	return d.Primary
}

// Close releases all connection pools (primary and replicas) and stops the health checker.
func (d *DB) Close() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.Primary != nil {
		d.Primary.Close()
	}
	for i := range d.replicas {
		if d.replicas[i].pool != nil {
			d.replicas[i].pool.Close()
		}
	}
}
