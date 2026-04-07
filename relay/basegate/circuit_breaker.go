package basegate

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Circuit breaker states.
const (
	CircuitClosed   = "closed"
	CircuitOpen     = "open"
	CircuitHalfOpen = "half_open"
)

// CircuitBreakerConfig controls the circuit breaker behaviour globally.
type CircuitBreakerConfig struct {
	FailureThreshold  int   // consecutive failures before tripping to OPEN (default 5)
	CooldownSec       int64 // seconds in OPEN before allowing a probe (default 60)
	HalfOpenMaxProbes int   // max concurrent probes in HALF_OPEN (default 1)
}

// AdapterCircuitInfo is the JSON-serialisable snapshot returned by admin APIs.
type AdapterCircuitInfo struct {
	Name                 string `json:"name"`
	State                string `json:"state"`
	FailureCount         int    `json:"failure_count"`
	CooldownRemainingSec int64  `json:"cooldown_remaining_sec,omitempty"`
}

// adapterCircuit holds the per-adapter circuit breaker state.
type adapterCircuit struct {
	state         string
	failureCount  int
	lastFailureAt int64
	probeInFlight int32 // managed via sync/atomic
}

var (
	circuitMu     sync.RWMutex
	circuits      = make(map[string]*adapterCircuit)
	circuitConfig = CircuitBreakerConfig{
		FailureThreshold:  5,
		CooldownSec:       60,
		HalfOpenMaxProbes: 1,
	}
)

// SetCircuitBreakerConfig replaces the global circuit breaker configuration.
func SetCircuitBreakerConfig(cfg CircuitBreakerConfig) {
	circuitMu.Lock()
	defer circuitMu.Unlock()
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.CooldownSec <= 0 {
		cfg.CooldownSec = 60
	}
	if cfg.HalfOpenMaxProbes <= 0 {
		cfg.HalfOpenMaxProbes = 1
	}
	circuitConfig = cfg
}

// getOrCreate returns the circuit for the given adapter, creating a CLOSED circuit if absent.
// Caller must hold at least a read lock; if creating, caller must hold a write lock.
func getOrCreateLocked(adapterName string) *adapterCircuit {
	c, ok := circuits[adapterName]
	if !ok {
		c = &adapterCircuit{state: CircuitClosed}
		circuits[adapterName] = c
	}
	return c
}

// CanAttempt checks whether an adapter is available for an attempt.
//
// CLOSED  → always true
// OPEN    → check if cooldown has expired; if so, transition to HALF_OPEN and allow a probe
// HALF_OPEN → allow up to HalfOpenMaxProbes concurrent probes (atomic CAS)
func CanAttempt(adapterName string) bool {
	circuitMu.Lock()
	defer circuitMu.Unlock()

	c := getOrCreateLocked(adapterName)

	switch c.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		now := time.Now().Unix()
		if now-c.lastFailureAt >= circuitConfig.CooldownSec {
			// Cooldown expired — transition to HALF_OPEN
			c.state = CircuitHalfOpen
			atomic.StoreInt32(&c.probeInFlight, 0)
			// Allow this caller as the first probe
			atomic.AddInt32(&c.probeInFlight, 1)
			return true
		}
		return false

	case CircuitHalfOpen:
		current := atomic.LoadInt32(&c.probeInFlight)
		if int(current) < circuitConfig.HalfOpenMaxProbes {
			atomic.AddInt32(&c.probeInFlight, 1)
			return true
		}
		return false

	default:
		return true
	}
}

// RecordSuccess resets the failure count and transitions HALF_OPEN → CLOSED.
func RecordSuccess(adapterName string) {
	circuitMu.Lock()
	defer circuitMu.Unlock()

	c := getOrCreateLocked(adapterName)
	c.failureCount = 0
	if c.state == CircuitHalfOpen {
		c.state = CircuitClosed
		atomic.StoreInt32(&c.probeInFlight, 0)
	}
}

// RecordFailure increments the consecutive failure count.
// If the threshold is reached (CLOSED) or the probe fails (HALF_OPEN), the circuit trips to OPEN.
func RecordFailure(adapterName string) {
	circuitMu.Lock()
	defer circuitMu.Unlock()

	c := getOrCreateLocked(adapterName)
	c.failureCount++
	c.lastFailureAt = time.Now().Unix()

	switch c.state {
	case CircuitClosed:
		if c.failureCount >= circuitConfig.FailureThreshold {
			c.state = CircuitOpen
		}
	case CircuitHalfOpen:
		// Probe failed — re-trip to OPEN, reset cooldown timer
		c.state = CircuitOpen
		atomic.StoreInt32(&c.probeInFlight, 0)
	}
}

// GetCircuitState returns the current state snapshot for a single adapter.
func GetCircuitState(adapterName string) (state string, failureCount int, cooldownRemainingSec int64) {
	circuitMu.RLock()
	defer circuitMu.RUnlock()

	c, ok := circuits[adapterName]
	if !ok {
		return CircuitClosed, 0, 0
	}

	state = c.state
	failureCount = c.failureCount

	if c.state == CircuitOpen {
		elapsed := time.Now().Unix() - c.lastFailureAt
		remaining := circuitConfig.CooldownSec - elapsed
		if remaining > 0 {
			cooldownRemainingSec = remaining
		}
	}
	return
}

// ListCircuitStates returns a snapshot of all tracked adapter circuits,
// sorted by adapter name for deterministic output.
func ListCircuitStates() []AdapterCircuitInfo {
	circuitMu.RLock()
	defer circuitMu.RUnlock()

	result := make([]AdapterCircuitInfo, 0, len(circuits))
	now := time.Now().Unix()

	for name, c := range circuits {
		info := AdapterCircuitInfo{
			Name:         name,
			State:        c.state,
			FailureCount: c.failureCount,
		}
		if c.state == CircuitOpen {
			remaining := circuitConfig.CooldownSec - (now - c.lastFailureAt)
			if remaining > 0 {
				info.CooldownRemainingSec = remaining
			}
		}
		result = append(result, info)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ResetCircuit manually resets an adapter circuit to CLOSED (admin operation).
func ResetCircuit(adapterName string) {
	circuitMu.Lock()
	defer circuitMu.Unlock()

	c, ok := circuits[adapterName]
	if !ok {
		return
	}
	c.state = CircuitClosed
	c.failureCount = 0
	c.lastFailureAt = 0
	atomic.StoreInt32(&c.probeInFlight, 0)
}

// resetCircuitsForTest clears all circuit state. Only for use in tests.
func resetCircuitsForTest() {
	circuitMu.Lock()
	defer circuitMu.Unlock()
	circuits = make(map[string]*adapterCircuit)
}
