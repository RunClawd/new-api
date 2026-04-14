package service

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ---------------------------------------------------------------------------
// BaseGate Prometheus metrics (Phase 16)
// ---------------------------------------------------------------------------

var (
	// BgRequestsTotal counts completed BaseGate requests by capability, status, and billing mode.
	BgRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bg_requests_total",
		Help: "Total BaseGate requests by capability, status, and billing mode.",
	}, []string{"capability", "status", "billing_mode"})

	// BgRequestDuration observes end-to-end dispatch latency in seconds.
	BgRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bg_request_duration_seconds",
		Help:    "End-to-end BaseGate request duration in seconds.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"capability", "mode"})

	// BgAdapterDuration observes per-adapter invocation latency in seconds.
	BgAdapterDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bg_adapter_duration_seconds",
		Help:    "Provider adapter invocation duration in seconds.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"adapter", "capability"})

	// BgCircuitBreakerState exposes current circuit breaker state per adapter (0=closed, 1=open, 2=half_open).
	BgCircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "bg_circuit_breaker_state",
		Help: "Circuit breaker state per adapter (0=closed, 1=open, 2=half_open).",
	}, []string{"adapter"})

	// BgActiveSessions tracks the number of active (non-terminal) sessions.
	BgActiveSessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bg_active_sessions",
		Help: "Number of active BaseGate sessions.",
	})

	// BgBillingAmountTotal accumulates total billed amount in quota units.
	BgBillingAmountTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bg_billing_amount_total",
		Help: "Cumulative billing amount in quota units.",
	}, []string{"capability", "billing_mode"})
)

// CircuitStateToFloat converts circuit breaker state string to a numeric gauge value.
func CircuitStateToFloat(state string) float64 {
	switch state {
	case "closed":
		return 0
	case "open":
		return 1
	case "half_open":
		return 2
	default:
		return -1
	}
}
