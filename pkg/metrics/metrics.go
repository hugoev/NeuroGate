// Package metrics provides Prometheus instrumentation for NeuroGate services
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the service
type Metrics struct {
	// Gateway metrics
	RequestsTotal       *prometheus.CounterVec
	RequestDuration     *prometheus.HistogramVec
	ActiveRequests      prometheus.Gauge
	CircuitBreakerState *prometheus.GaugeVec

	// Worker metrics
	InferenceDuration   *prometheus.HistogramVec
	TokensGenerated     *prometheus.CounterVec
	TokensPerSecond     *prometheus.GaugeVec
	OllamaRequestsTotal *prometheus.CounterVec
	OllamaRequestErrors *prometheus.CounterVec
	OllamaConnected     prometheus.Gauge
	WorkerLoad          prometheus.Gauge
	ActiveInferences    prometheus.Gauge
}

// NewGatewayMetrics creates metrics for the Gateway service
func NewGatewayMetrics(namespace string) *Metrics {
	return &Metrics{
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "requests_total",
				Help:      "Total number of requests received by the gateway",
			},
			[]string{"method", "path", "status"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "request_duration_seconds",
				Help:      "Request duration in seconds",
				Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"method", "path"},
		),
		ActiveRequests: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "active_requests",
				Help:      "Number of requests currently being processed",
			},
		),
		CircuitBreakerState: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "circuit_breaker_state",
				Help:      "Circuit breaker state (0=closed, 1=open, 2=half-open)",
			},
			[]string{"worker"},
		),
	}
}

// NewWorkerMetrics creates metrics for the Worker service
func NewWorkerMetrics(namespace string) *Metrics {
	return &Metrics{
		InferenceDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "inference_duration_seconds",
				Help:      "LLM inference duration in seconds",
				Buckets:   []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300},
			},
			[]string{"model"},
		),
		TokensGenerated: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tokens_generated_total",
				Help:      "Total number of tokens generated",
			},
			[]string{"model"},
		),
		TokensPerSecond: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "tokens_per_second",
				Help:      "Current tokens per second generation rate",
			},
			[]string{"model"},
		),
		OllamaRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "ollama_requests_total",
				Help:      "Total number of requests made to Ollama",
			},
			[]string{"model", "status"},
		),
		OllamaRequestErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "ollama_request_errors_total",
				Help:      "Total number of Ollama request errors",
			},
			[]string{"model", "error_type"},
		),
		OllamaConnected: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "ollama_connected",
				Help:      "Whether the worker is connected to Ollama (1=yes, 0=no)",
			},
		),
		WorkerLoad: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "worker_load",
				Help:      "Current load on the worker (0.0 to 1.0)",
			},
		),
		ActiveInferences: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "active_inferences",
				Help:      "Number of inferences currently in progress",
			},
		),
	}
}

// Handler returns the Prometheus HTTP handler for metrics endpoint
func Handler() http.Handler {
	return promhttp.Handler()
}

// RecordRequest records a completed request with its status
func (m *Metrics) RecordRequest(method, path, status string, durationSeconds float64) {
	m.RequestsTotal.WithLabelValues(method, path, status).Inc()
	m.RequestDuration.WithLabelValues(method, path).Observe(durationSeconds)
}

// RecordInference records a completed inference
func (m *Metrics) RecordInference(model string, durationSeconds float64, tokensGenerated int) {
	m.InferenceDuration.WithLabelValues(model).Observe(durationSeconds)
	m.TokensGenerated.WithLabelValues(model).Add(float64(tokensGenerated))

	if durationSeconds > 0 {
		tps := float64(tokensGenerated) / durationSeconds
		m.TokensPerSecond.WithLabelValues(model).Set(tps)
	}
}

// SetCircuitBreakerState sets the circuit breaker state for a worker
func (m *Metrics) SetCircuitBreakerState(worker string, state int) {
	m.CircuitBreakerState.WithLabelValues(worker).Set(float64(state))
}

// SetOllamaConnected sets the Ollama connection status
func (m *Metrics) SetOllamaConnected(connected bool) {
	if connected {
		m.OllamaConnected.Set(1)
	} else {
		m.OllamaConnected.Set(0)
	}
}
