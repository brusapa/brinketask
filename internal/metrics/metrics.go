// Package metrics exposes Prometheus metrics (SPEC section 10, D-72) on a
// port of their own. Labels never carry user data: HTTP routes are the
// router's patterns ("GET /api/v1/tasks/{id}"), not request paths.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/brusapa/brinketask/internal/purge"
)

// namespace prefixes every metric of the application.
const namespace = "brinketask"

// Metrics holds the application's metrics and their registry.
type Metrics struct {
	registry *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec

	remindersFired prometheus.Counter
	fireDelay      prometheus.Histogram
	deliveries     *prometheus.CounterVec

	purged    *prometheus.CounterVec
	lastPurge prometheus.Gauge
}

// New creates the metrics, with the Go runtime, process and database pool
// collectors. pool may be nil in tests.
func New(pool *pgxpool.Pool) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "http_requests_total",
			Help: "HTTP requests by route pattern, method and status code.",
		}, []string{"route", "method", "code"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "http_request_duration_seconds",
			Help:    "Time to answer HTTP requests, by route pattern.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1, 2.5, 5},
		}, []string{"route", "method"}),
		remindersFired: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "reminders_fired_total",
			Help: "Reminders the scheduler fired.",
		}),
		fireDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Name: "reminder_fire_delay_seconds",
			Help:    "How late reminders fired; SPEC section 6 aims at under 60 s.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 900, 3600},
		}),
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "deliveries_total",
			Help: "Delivery outcomes: sent, retry, failed, gone (device disabled) and skipped (too late).",
		}, []string{"outcome"}),
		purged: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "purged_rows_total",
			Help: "Rows the purge removed, by kind.",
		}, []string{"kind"}),
		lastPurge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "last_purge_timestamp_seconds",
			Help: "When this process last ran the purge (Unix time).",
		}),
	}
	m.registry.MustRegister(
		m.httpRequests, m.httpDuration, m.remindersFired, m.fireDelay, m.deliveries, m.purged, m.lastPurge,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	if pool != nil {
		m.registry.MustRegister(newPoolCollector(pool))
	}
	return m
}

// Handler serves the metrics in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Middleware counts and times the requests next serves. It reads the
// route from the request after the ServeMux has matched it: the mux sets
// Request.Pattern on the request it is given.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		m.httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(recorder.status)).Inc()
		m.httpDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}

// statusRecorder remembers the status code a handler writes.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer (flushing,
// deadlines).
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// ReminderFired implements notify.Observer.
func (m *Metrics) ReminderFired(late time.Duration) {
	m.remindersFired.Inc()
	m.fireDelay.Observe(late.Seconds())
}

// Delivery implements notify.Observer.
func (m *Metrics) Delivery(outcome string) {
	m.deliveries.WithLabelValues(outcome).Inc()
}

// PurgeResult records what a purge removed.
func (m *Metrics) PurgeResult(r purge.Result, at time.Time) {
	if r.Skipped {
		return
	}
	for kind, n := range map[string]int{
		"lists": r.Lists, "tasks": r.Tasks, "checklist_items": r.ChecklistItems, "tags": r.Tags,
		"reminders": r.Reminders, "completions": r.Completions, "deliveries": r.Deliveries, "sessions": r.Sessions,
	} {
		m.purged.WithLabelValues(kind).Add(float64(n))
	}
	m.lastPurge.Set(float64(at.Unix()))
}

// poolCollector reports the database pool's state when scraped.
type poolCollector struct {
	pool                    *pgxpool.Pool
	total, idle, acquired   *prometheus.Desc
	acquireCount, waitedFor *prometheus.Desc
}

func newPoolCollector(pool *pgxpool.Pool) *poolCollector {
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "db_pool", name), help, nil, nil)
	}
	return &poolCollector{
		pool:         pool,
		total:        desc("connections", "Open database connections."),
		idle:         desc("idle_connections", "Idle database connections."),
		acquired:     desc("acquired_connections", "Database connections in use."),
		acquireCount: desc("acquires_total", "Connections taken from the pool."),
		waitedFor:    desc("acquire_wait_seconds_total", "Time spent waiting for a free connection."),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.total
	ch <- c.idle
	ch <- c.acquired
	ch <- c.acquireCount
	ch <- c.waitedFor
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(s.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.idle, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.acquired, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.acquireCount, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.waitedFor, prometheus.CounterValue, s.AcquireDuration().Seconds())
}
