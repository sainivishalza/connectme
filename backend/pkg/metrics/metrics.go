// Package metrics exposes Prometheus metrics. Labels are deliberately
// low-cardinality (route *patterns*, fixed enums), never user ids, refs or
// raw paths, so the series count stays bounded and no personal data leaks
// into the monitoring system.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	Registry = prometheus.NewRegistry()

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_http_requests_total", Help: "HTTP requests by method, route pattern and status."},
		[]string{"method", "route", "status"})
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "connectme_http_request_duration_seconds", Help: "HTTP request latency by method and route pattern.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}},
		[]string{"method", "route"})
	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "connectme_http_in_flight_requests", Help: "Requests currently being served."})

	rateLimited = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_rate_limited_total", Help: "Requests rejected by a rate limiter."}, []string{"limiter"})

	jobLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "connectme_job_last_success_timestamp_seconds", Help: "Unix time of the last successful run of a background job."},
		[]string{"job"})
	jobRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_job_runs_total", Help: "Background job runs by result."}, []string{"job", "result"})
	jobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "connectme_job_duration_seconds", Help: "Background job run time.", Buckets: prometheus.DefBuckets},
		[]string{"job"})
	jobItems = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_job_items_total", Help: "Items processed by background jobs."}, []string{"job", "kind"})

	aiRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_ai_requests_total", Help: "Calls to the AI service by kind and outcome."},
		[]string{"kind", "outcome"})
	aiBreakerOpen = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "connectme_ai_breaker_open", Help: "1 while the AI-service circuit breaker is open."})

	moderation = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_moderation_decisions_total", Help: "Content moderation outcomes."},
		[]string{"kind", "action"})
	webhooks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_stripe_webhooks_total", Help: "Stripe webhook deliveries by result."}, []string{"result"})
	business = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "connectme_events_total", Help: "Product events (signups, logins, matches...)."}, []string{"event"})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests, httpDuration, httpInFlight, rateLimited,
		jobLastSuccess, jobRuns, jobDuration, jobItems,
		aiRequests, aiBreakerOpen, moderation, webhooks, business,
	)
}

// Handler serves the Prometheus text format. Serve it on a private listener
// (see METRICS_ADDR); it must never be reachable from the internet.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}

// ---- HTTP -------------------------------------------------------------

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) {
	w.code = c
	w.ResponseWriter.WriteHeader(c)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTP records request count and latency per route pattern. Place it
// outermost so panics recovered further in are still counted as 500.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		start := time.Now()
		httpInFlight.Inc()
		defer func() {
			httpInFlight.Dec()
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(sw.code)).Inc()
			httpDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		}()
		next.ServeHTTP(sw, r)
	})
}

// ---- recorders --------------------------------------------------------

func RateLimited(limiter string) { rateLimited.WithLabelValues(limiter).Inc() }

// Job records one run of a background job.
func Job(name string, d time.Duration, err error) {
	jobDuration.WithLabelValues(name).Observe(d.Seconds())
	if err != nil {
		jobRuns.WithLabelValues(name, "error").Inc()
		return
	}
	jobRuns.WithLabelValues(name, "ok").Inc()
	jobLastSuccess.WithLabelValues(name).SetToCurrentTime()
}

func JobItems(name, kind string, n int) {
	if n > 0 {
		jobItems.WithLabelValues(name, kind).Add(float64(n))
	}
}

// AI records a call outcome: ok | error | breaker_open.
func AI(kind, outcome string) { aiRequests.WithLabelValues(kind, outcome).Inc() }

func AIBreaker(open bool) {
	if open {
		aiBreakerOpen.Set(1)
	} else {
		aiBreakerOpen.Set(0)
	}
}

func Moderation(kind, action string) { moderation.WithLabelValues(kind, action).Inc() }
func Webhook(result string)          { webhooks.WithLabelValues(result).Inc() }
func Event(name string)              { business.WithLabelValues(name).Inc() }
