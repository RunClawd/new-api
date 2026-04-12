package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// CalculateBYOFee unit tests
// ===========================================================================

func TestCalculateBYOFee_PerRequest(t *testing.T) {
	fee := CalculateBYOFee(&relaycommon.BYOFeeConfig{
		FeeType:     "per_request",
		FixedAmount: 0.05,
	}, 1.25)
	assert.InDelta(t, 0.05, fee, 0.0001) // ignores base amount
}

func TestCalculateBYOFee_Percentage(t *testing.T) {
	fee := CalculateBYOFee(&relaycommon.BYOFeeConfig{
		FeeType:        "percentage",
		PercentageRate: 0.10,
	}, 2.00)
	assert.InDelta(t, 0.20, fee, 0.0001) // 10% of $2.00
}

func TestCalculateBYOFee_NilConfig(t *testing.T) {
	fee := CalculateBYOFee(nil, 5.0)
	assert.Equal(t, 0.0, fee)
}

func TestCalculateBYOFee_UnknownType(t *testing.T) {
	fee := CalculateBYOFee(&relaycommon.BYOFeeConfig{
		FeeType: "subscription",
	}, 10.0)
	assert.Equal(t, 0.0, fee)
}

// ===========================================================================
// EstimateCost BYO tests
// ===========================================================================

func TestEstimateCost_BYOPerRequest_NoPricing(t *testing.T) {
	// BYO flat fee should work even without underlying pricing
	cost := EstimateCost(nil, "hello", &relaycommon.BYOFeeConfig{
		FeeType:     "per_request",
		FixedAmount: 0.01,
	})
	assert.Equal(t, int(0.01*500000), cost) // $0.01 → 5000 quota
}

func TestEstimateCost_BYOPerRequest_ZeroUnitPrice(t *testing.T) {
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0, // no base price
		Currency:     "usd",
	}
	cost := EstimateCost(pricing, "hello", &relaycommon.BYOFeeConfig{
		FeeType:     "per_request",
		FixedAmount: 0.05,
	})
	assert.Equal(t, int(0.05*500000), cost) // $0.05 → 25000 quota
}

func TestEstimateCost_BYOPercentage(t *testing.T) {
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.000002,
		Currency:     "usd",
	}
	// 10% of estimated provider cost
	cost := EstimateCost(pricing, "hello world test", &relaycommon.BYOFeeConfig{
		FeeType:        "percentage",
		PercentageRate: 0.10,
	})
	assert.Greater(t, cost, 0)
}

func TestEstimateCost_HostedUnchanged(t *testing.T) {
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.000002,
		Currency:     "usd",
	}
	// With nil feeConfig, behavior should match hosted path
	cost := EstimateCost(pricing, "hello world", nil)
	assert.Greater(t, cost, 0)
}

// ===========================================================================
// FinalizeBilling BYO integration tests
// ===========================================================================

func TestFinalizeBilling_BYO_PerRequest(t *testing.T) {
	truncateBgTables(t)

	rawUsage := &relaycommon.ProviderUsage{
		PromptTokens:     500,
		CompletionTokens: 1500,
		TotalTokens:      2000,
	}
	// No base pricing needed for per_request
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0,
		Currency:     "usd",
	}
	feeConfig := &relaycommon.BYOFeeConfig{
		FeeType:     "per_request",
		FixedAmount: 0.05,
	}

	quotaUsed, err := FinalizeBilling("resp_byo_flat", 42, 1, "bg.llm.chat.standard", "anthropic_native_ch1", rawUsage, pricing, "byo", feeConfig)
	require.NoError(t, err)

	// $0.05 → 25000 quota
	assert.Equal(t, int(0.05*500000), quotaUsed)

	// Verify billing record was created
	var billingCount int64
	model.DB.Model(&model.BgBillingRecord{}).Where("response_id = ?", "resp_byo_flat").Count(&billingCount)
	assert.Equal(t, int64(1), billingCount)

	// Verify usage was still recorded (consumption tracking is independent of billing)
	var usageCount int64
	model.DB.Model(&model.BgUsageRecord{}).Where("response_id = ?", "resp_byo_flat").Count(&usageCount)
	assert.Equal(t, int64(1), usageCount)

	// Verify ledger entry was created
	var ledgerCount int64
	model.DB.Model(&model.BgLedgerEntry{}).Where("response_id = ?", "resp_byo_flat").Count(&ledgerCount)
	assert.Equal(t, int64(1), ledgerCount)
}

func TestFinalizeBilling_BYO_Percentage(t *testing.T) {
	truncateBgTables(t)

	rawUsage := &relaycommon.ProviderUsage{
		PromptTokens:     1000,
		CompletionTokens: 2000,
		TotalTokens:      3000,
	}
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003, // $0.03/1K tokens
		Currency:     "usd",
	}
	feeConfig := &relaycommon.BYOFeeConfig{
		FeeType:        "percentage",
		PercentageRate: 0.10, // 10%
	}

	// Base cost: 3000 * 0.00003 = $0.09
	// 10% fee: $0.009
	quotaUsed, err := FinalizeBilling("resp_byo_pct", 42, 1, "bg.llm.chat.standard", "openai_native_ch1", rawUsage, pricing, "byo", feeConfig)
	require.NoError(t, err)

	expectedFee := 3000 * 0.00003 * 0.10 // $0.009
	expectedQuota := int(expectedFee * 500000)
	assert.Equal(t, expectedQuota, quotaUsed)
}

func TestFinalizeBilling_BYO_NoFeeConfig(t *testing.T) {
	truncateBgTables(t)

	rawUsage := &relaycommon.ProviderUsage{
		TotalTokens: 1000,
	}
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003,
		Currency:     "usd",
	}

	// BYO without feeConfig → 0 platform fee
	quotaUsed, err := FinalizeBilling("resp_byo_nofee", 42, 1, "bg.llm.chat.standard", "test-adapter", rawUsage, pricing, "byo", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, quotaUsed)

	// Usage should still be recorded
	var usageCount int64
	model.DB.Model(&model.BgUsageRecord{}).Where("response_id = ?", "resp_byo_nofee").Count(&usageCount)
	assert.Equal(t, int64(1), usageCount)

	// No billing record (zero fee)
	var billingCount int64
	model.DB.Model(&model.BgBillingRecord{}).Where("response_id = ?", "resp_byo_nofee").Count(&billingCount)
	assert.Equal(t, int64(0), billingCount)
}

func TestFinalizeBilling_Hosted_Unaffected(t *testing.T) {
	truncateBgTables(t)

	rawUsage := &relaycommon.ProviderUsage{
		PromptTokens:     500,
		CompletionTokens: 1500,
		TotalTokens:      2000,
	}
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003,
		Currency:     "usd",
	}

	// Hosted path with nil feeConfig should work as before
	quotaUsed, err := FinalizeBilling("resp_hosted_check", 42, 1, "bg.llm.chat.standard", "openai_native_ch1", rawUsage, pricing, "hosted", nil)
	require.NoError(t, err)

	expectedAmount := 2000 * 0.00003
	expectedQuota := int(expectedAmount * 500000)
	assert.Equal(t, expectedQuota, quotaUsed)
}
