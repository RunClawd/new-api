package service

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestEstimateCost_Token(t *testing.T) {
	pricing := &relaycommon.PricingSnapshot{
		BillableUnit: "token",
		UnitPrice:    0.000002, // $0.002/1K tokens → per-token
		Currency:     "usd",
	}
	cost := EstimateCost(pricing, "Hello, how are you doing today?")
	assert.True(t, cost > 0, "token-based estimate should be > 0")
}

func TestEstimateCost_Second(t *testing.T) {
	pricing := &relaycommon.PricingSnapshot{
		BillableUnit: "second",
		UnitPrice:    0.01,
		Currency:     "usd",
	}
	cost := EstimateCost(pricing, map[string]interface{}{"prompt": "a cat"})
	assert.True(t, cost > 0, "second-based estimate should be > 0")
}

func TestEstimateCost_FreePricing(t *testing.T) {
	pricing := &relaycommon.PricingSnapshot{
		BillableUnit: "token",
		UnitPrice:    0,
	}
	cost := EstimateCost(pricing, "anything")
	assert.Equal(t, 0, cost, "free pricing should estimate 0")
}

func TestEstimateCost_NilPricing(t *testing.T) {
	cost := EstimateCost(nil, "anything")
	assert.Equal(t, 0, cost, "nil pricing should estimate 0")
}

func TestEstimateInputLength(t *testing.T) {
	assert.Equal(t, 5, estimateInputLength("hello"))
	assert.Equal(t, 0, estimateInputLength(nil))
	assert.True(t, estimateInputLength(map[string]interface{}{"key": "value"}) > 0)
}
