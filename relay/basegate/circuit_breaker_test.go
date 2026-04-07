package basegate

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  5,
		CooldownSec:       60,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-1"

	// First 4 failures should keep circuit CLOSED
	for i := 0; i < 4; i++ {
		RecordFailure(adapter)
		state, count, _ := GetCircuitState(adapter)
		assert.Equal(t, CircuitClosed, state, "should still be closed after %d failures", i+1)
		assert.Equal(t, i+1, count)
	}

	// 5th failure trips to OPEN
	RecordFailure(adapter)
	state, count, cooldown := GetCircuitState(adapter)
	assert.Equal(t, CircuitOpen, state)
	assert.Equal(t, 5, count)
	assert.Greater(t, cooldown, int64(0))
}

func TestCircuitBreaker_OpenRejectsAttempts(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  3,
		CooldownSec:       60,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-2"

	// Trip the circuit
	for i := 0; i < 3; i++ {
		RecordFailure(adapter)
	}

	state, _, _ := GetCircuitState(adapter)
	require.Equal(t, CircuitOpen, state)

	// CanAttempt should return false while OPEN (cooldown not expired)
	assert.False(t, CanAttempt(adapter))
	assert.False(t, CanAttempt(adapter))
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  3,
		CooldownSec:       1, // 1-second cooldown for fast test
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-3"

	// Trip the circuit
	for i := 0; i < 3; i++ {
		RecordFailure(adapter)
	}

	state, _, _ := GetCircuitState(adapter)
	require.Equal(t, CircuitOpen, state)

	// Wait for cooldown to expire
	time.Sleep(1100 * time.Millisecond)

	// Now CanAttempt should return true and transition to HALF_OPEN
	assert.True(t, CanAttempt(adapter))

	state, _, _ = GetCircuitState(adapter)
	assert.Equal(t, CircuitHalfOpen, state)
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  3,
		CooldownSec:       1,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-4"

	// Trip to OPEN
	for i := 0; i < 3; i++ {
		RecordFailure(adapter)
	}

	// Wait for cooldown
	time.Sleep(1100 * time.Millisecond)

	// Transition to HALF_OPEN via probe
	require.True(t, CanAttempt(adapter))
	state, _, _ := GetCircuitState(adapter)
	require.Equal(t, CircuitHalfOpen, state)

	// Record success — should transition to CLOSED
	RecordSuccess(adapter)

	state, count, _ := GetCircuitState(adapter)
	assert.Equal(t, CircuitClosed, state)
	assert.Equal(t, 0, count)
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  3,
		CooldownSec:       1,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-5"

	// Trip to OPEN
	for i := 0; i < 3; i++ {
		RecordFailure(adapter)
	}

	// Wait for cooldown
	time.Sleep(1100 * time.Millisecond)

	// Transition to HALF_OPEN
	require.True(t, CanAttempt(adapter))
	state, _, _ := GetCircuitState(adapter)
	require.Equal(t, CircuitHalfOpen, state)

	// Record failure — should trip back to OPEN with fresh cooldown
	RecordFailure(adapter)

	state, _, cooldown := GetCircuitState(adapter)
	assert.Equal(t, CircuitOpen, state)
	assert.Greater(t, cooldown, int64(0))
}

func TestCircuitBreaker_SuccessResetsCount(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  5,
		CooldownSec:       60,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-6"

	// Accumulate some failures (but below threshold)
	RecordFailure(adapter)
	RecordFailure(adapter)
	RecordFailure(adapter)

	_, count, _ := GetCircuitState(adapter)
	require.Equal(t, 3, count)

	// Success resets the counter
	RecordSuccess(adapter)

	state, count, _ := GetCircuitState(adapter)
	assert.Equal(t, CircuitClosed, state)
	assert.Equal(t, 0, count)

	// Now need full threshold again to trip
	for i := 0; i < 4; i++ {
		RecordFailure(adapter)
	}
	state, _, _ = GetCircuitState(adapter)
	assert.Equal(t, CircuitClosed, state, "should still be closed after 4 failures (needs 5)")
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  100, // high threshold so we don't trip during the test
		CooldownSec:       60,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-concurrent"
	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				CanAttempt(adapter)
				if i%3 == 0 {
					RecordFailure(adapter)
				} else {
					RecordSuccess(adapter)
				}
				GetCircuitState(adapter)
			}
		}(g)
	}

	wg.Wait()

	// Should not panic or deadlock. State should be consistent.
	state, _, _ := GetCircuitState(adapter)
	assert.Contains(t, []string{CircuitClosed, CircuitOpen, CircuitHalfOpen}, state)
}

func TestCircuitBreaker_ResetCircuit(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  3,
		CooldownSec:       60,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-reset"

	// Trip the circuit
	for i := 0; i < 3; i++ {
		RecordFailure(adapter)
	}

	state, _, _ := GetCircuitState(adapter)
	require.Equal(t, CircuitOpen, state)

	// Manual reset
	ResetCircuit(adapter)

	state, count, cooldown := GetCircuitState(adapter)
	assert.Equal(t, CircuitClosed, state)
	assert.Equal(t, 0, count)
	assert.Equal(t, int64(0), cooldown)

	// Should accept attempts again
	assert.True(t, CanAttempt(adapter))
}

func TestCircuitBreaker_ListCircuitStates(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  3,
		CooldownSec:       60,
		HalfOpenMaxProbes: 1,
	})

	// Create several circuits in different states
	// adapter-a: CLOSED with 1 failure
	RecordFailure("adapter-a")

	// adapter-b: OPEN (tripped)
	for i := 0; i < 3; i++ {
		RecordFailure("adapter-b")
	}

	// adapter-c: CLOSED (no activity, just a probe)
	CanAttempt("adapter-c")

	states := ListCircuitStates()

	// Should be sorted by name
	require.Len(t, states, 3)
	assert.Equal(t, "adapter-a", states[0].Name)
	assert.Equal(t, "adapter-b", states[1].Name)
	assert.Equal(t, "adapter-c", states[2].Name)

	// Verify states
	assert.Equal(t, CircuitClosed, states[0].State)
	assert.Equal(t, 1, states[0].FailureCount)
	assert.Equal(t, int64(0), states[0].CooldownRemainingSec)

	assert.Equal(t, CircuitOpen, states[1].State)
	assert.Equal(t, 3, states[1].FailureCount)
	assert.Greater(t, states[1].CooldownRemainingSec, int64(0))

	assert.Equal(t, CircuitClosed, states[2].State)
	assert.Equal(t, 0, states[2].FailureCount)
}

func TestCircuitBreaker_HalfOpenProbeLimit(t *testing.T) {
	resetCircuitsForTest()
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  3,
		CooldownSec:       1,
		HalfOpenMaxProbes: 1,
	})

	adapter := "test-adapter-probe-limit"

	// Trip to OPEN
	for i := 0; i < 3; i++ {
		RecordFailure(adapter)
	}

	// Wait for cooldown
	time.Sleep(1100 * time.Millisecond)

	// First probe allowed
	assert.True(t, CanAttempt(adapter))

	state, _, _ := GetCircuitState(adapter)
	require.Equal(t, CircuitHalfOpen, state)

	// Second probe should be rejected (max 1)
	assert.False(t, CanAttempt(adapter))
}

func TestCircuitBreaker_ResetNonexistent(t *testing.T) {
	resetCircuitsForTest()

	// Resetting a nonexistent adapter should not panic
	ResetCircuit("nonexistent-adapter")

	// State should be the default
	state, count, cooldown := GetCircuitState("nonexistent-adapter")
	assert.Equal(t, CircuitClosed, state)
	assert.Equal(t, 0, count)
	assert.Equal(t, int64(0), cooldown)
}

func TestCircuitBreaker_ConfigDefaults(t *testing.T) {
	resetCircuitsForTest()

	// Setting invalid values should fall back to defaults
	SetCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold:  -1,
		CooldownSec:       0,
		HalfOpenMaxProbes: -5,
	})

	// Verify defaults applied by tripping with 5 failures
	adapter := "test-defaults"
	for i := 0; i < 4; i++ {
		RecordFailure(adapter)
	}
	state, _, _ := GetCircuitState(adapter)
	assert.Equal(t, CircuitClosed, state, "should still be closed with default threshold of 5")

	RecordFailure(adapter)
	state, _, _ = GetCircuitState(adapter)
	assert.Equal(t, CircuitOpen, state, "should trip at default threshold of 5")
}
