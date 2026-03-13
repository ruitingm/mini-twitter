package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Package-level Prometheus metrics automatically registered with the default registry
// via promauto (no manual Register call required).
var (
	// HTTPRequestDuration tracks latency for every HTTP handler, broken down by service,
	// handler name, HTTP method, and response status code.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds",
		Buckets: prometheus.DefBuckets, // default buckets: .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10
	}, []string{"service", "handler", "method", "status_code"})

	// FanoutQueueDepth is a gauge showing how many fanout jobs are waiting in the channel.
	// A sustained high value indicates the workers can't keep up with write throughput.
	FanoutQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "fanout_queue_depth",
		Help: "Current depth of the fanout job queue",
	})

	// FanoutJobDuration measures how long each individual fanout job takes end-to-end.
	FanoutJobDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "fanout_job_duration_seconds",
		Help:    "Duration of individual fanout jobs",
		Buckets: prometheus.DefBuckets,
	})

	// RedisHits counts successful cache reads, labelled by operation (e.g. "timeline").
	RedisHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "redis_hits_total",
		Help: "Redis cache hits",
	}, []string{"operation"})

	// RedisMisses counts cache misses, triggering a fallback to Postgres.
	RedisMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "redis_misses_total",
		Help: "Redis cache misses",
	}, []string{"operation"})

	// DBQueryDuration tracks Postgres query latency, labelled by query name and whether
	// it hit a replica ("true") or the primary ("false").
	DBQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "Database query duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"query_name", "replica"})

	// LikeAggregatorFlushTotal counts how many times the like aggregator has flushed
	// its Redis buffer to Postgres.
	LikeAggregatorFlushTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "like_aggregator_flush_total",
		Help: "Total like aggregator flushes",
	})
)

// Handler returns the Prometheus HTTP handler for the /metrics scrape endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// responseWriter wraps http.ResponseWriter to capture the status code written by a handler.
// This is needed because the standard ResponseWriter doesn't expose the status after WriteHeader.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader intercepts the status code before forwarding it to the underlying writer.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// InstrumentHandler wraps an HTTP handler to record its duration and status code in Prometheus.
// The status defaults to 200 if the handler never calls WriteHeader explicitly.
func InstrumentHandler(service, handler string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK} // default 200
		next.ServeHTTP(rw, r)
		// Record the duration labelled with the resolved status code
		HTTPRequestDuration.WithLabelValues(service, handler, r.Method, strconv.Itoa(rw.statusCode)).
			Observe(time.Since(start).Seconds())
	})
}
