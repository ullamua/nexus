package diagnostics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus counters and histograms for Nexus.
type Metrics struct {
	Registry        *prometheus.Registry
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	PipelineSteps   *prometheus.CounterVec
	CacheHits       *prometheus.CounterVec
	ConnectorErrors *prometheus.CounterVec
}

// NewMetrics registers and returns all Prometheus metrics using a fresh registry.
// Each call creates an isolated registry, which is safe to use in tests.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_requests_total",
		Help: "Total number of requests processed by Nexus.",
	}, []string{"connector", "action", "status"})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nexus_request_duration_ms",
		Help:    "Request duration in milliseconds.",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
	}, []string{"connector", "action"})

	pipelineSteps := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_pipeline_steps_total",
		Help: "Total pipeline steps executed.",
	}, []string{"connector", "action", "status"})

	cacheHits := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_cache_hits_total",
		Help: "Total cache hits and misses.",
	}, []string{"hit"})

	connectorErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_connector_errors_total",
		Help: "Total connector errors by connector and error code.",
	}, []string{"connector", "code"})

	reg.MustRegister(requestsTotal, requestDuration, pipelineSteps, cacheHits, connectorErrors)

	return &Metrics{
		Registry:        reg,
		RequestsTotal:   requestsTotal,
		RequestDuration: requestDuration,
		PipelineSteps:   pipelineSteps,
		CacheHits:       cacheHits,
		ConnectorErrors: connectorErrors,
	}
}

// RecordRequest records metrics for a completed request.
func (m *Metrics) RecordRequest(connector, action, status string, latencyMS int64) {
	m.RequestsTotal.WithLabelValues(connector, action, status).Inc()
	m.RequestDuration.WithLabelValues(connector, action).Observe(float64(latencyMS))
}

// RecordPipelineStep records metrics for a completed pipeline step.
func (m *Metrics) RecordPipelineStep(connector, action, status string) {
	m.PipelineSteps.WithLabelValues(connector, action, status).Inc()
}

// RecordCacheHit records a cache hit or miss.
func (m *Metrics) RecordCacheHit(hit bool) {
	label := "false"
	if hit {
		label = "true"
	}
	m.CacheHits.WithLabelValues(label).Inc()
}

// RecordConnectorError records a connector error.
func (m *Metrics) RecordConnectorError(connector, code string) {
	m.ConnectorErrors.WithLabelValues(connector, code).Inc()
}
