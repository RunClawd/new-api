package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// NormalizeUsage tests
// ===========================================================================

func TestNormalizeUsage_TokenBased(t *testing.T) {
	truncateBgTables(t)

	usage, err := NormalizeUsage("resp_usage_1", &relaycommon.ProviderUsage{
		PromptTokens:     100,
		CompletionTokens: 200,
		TotalTokens:      300,
	})
	require.NoError(t, err)
	require.NotNil(t, usage)

	assert.Equal(t, "token", usage.BillableUnit)
	assert.Equal(t, float64(300), usage.BillableUnits)
	assert.Equal(t, float64(100), usage.InputUnits)
	assert.Equal(t, float64(200), usage.OutputUnits)

	// Verify persisted
	var records []model.BgUsageRecord
	err = model.DB.Where("response_id = ?", "resp_usage_1").Find(&records).Error
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "token", records[0].BillableUnit)
}

func TestNormalizeUsage_DurationBased(t *testing.T) {
	truncateBgTables(t)

	usage, err := NormalizeUsage("resp_usage_2", &relaycommon.ProviderUsage{
		DurationSec: 15.5,
	})
	require.NoError(t, err)
	assert.Equal(t, "second", usage.BillableUnit)
	assert.Equal(t, 15.5, usage.BillableUnits)
}

func TestNormalizeUsage_RequestBased(t *testing.T) {
	truncateBgTables(t)

	// No specific usage data → defaults to request-based
	usage, err := NormalizeUsage("resp_usage_3", &relaycommon.ProviderUsage{})
	require.NoError(t, err)
	assert.Equal(t, "request", usage.BillableUnit)
	assert.Equal(t, float64(1), usage.BillableUnits)
}

func TestNormalizeUsage_NilUsage(t *testing.T) {
	usage, err := NormalizeUsage("resp_nil", nil)
	assert.NoError(t, err)
	assert.Nil(t, usage)
}

// ===========================================================================
// CalculateBilling tests
// ===========================================================================

func TestCalculateBilling_TokenPricing(t *testing.T) {
	truncateBgTables(t)

	usage := &relaycommon.CanonicalUsage{
		BillableUnits: 1000,
		BillableUnit:  "token",
		InputUnits:    400,
		OutputUnits:   600,
	}
	pricing := &relaycommon.PricingSnapshot{
		BillingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003, // $0.03/1K tokens
		Currency:     "usd",
	}

	bill, err := CalculateBilling("resp_bill_1", usage, pricing)
	require.NoError(t, err)
	require.NotNil(t, bill)

	assert.InDelta(t, 0.03, bill.Amount, 0.0001) // 1000 * 0.00003 = 0.03
	assert.Equal(t, "usd", bill.Currency)
	assert.Equal(t, "metered", bill.BillingMode)

	// Verify persisted
	var records []model.BgBillingRecord
	err = model.DB.Where("response_id = ?", "resp_bill_1").Find(&records).Error
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestCalculateBilling_PerCallFixed(t *testing.T) {
	truncateBgTables(t)

	usage := &relaycommon.CanonicalUsage{
		BillableUnits: 1,
		BillableUnit:  "request",
	}
	pricing := &relaycommon.PricingSnapshot{
		BillingMode:  "per_call",
		BillableUnit: "request",
		UnitPrice:    0.05,
		Currency:     "usd",
	}

	bill, err := CalculateBilling("resp_bill_2", usage, pricing)
	require.NoError(t, err)
	assert.InDelta(t, 0.05, bill.Amount, 0.0001)
}

func TestCalculateBilling_NilInputs(t *testing.T) {
	bill, err := CalculateBilling("resp_nil", nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, bill)
}

// ===========================================================================
// PostLedgerEntry tests
// ===========================================================================

func TestPostLedgerEntry_Debit(t *testing.T) {
	truncateBgTables(t)

	bill := &model.BgBillingRecord{
		BillingID:  "bill_test_1",
		ResponseID: "resp_ledger_1",
		Amount:     0.03,
		Currency:   "usd",
	}
	require.NoError(t, bill.Insert())

	entry, err := PostLedgerEntry(1, bill, "debit")
	require.NoError(t, err)
	require.NotNil(t, entry)

	assert.Equal(t, "debit", entry.EntryType)
	assert.InDelta(t, 0.03, entry.Amount, 0.0001)
	assert.Equal(t, 1, entry.OrgID)

	// Verify persisted
	var entries []model.BgLedgerEntry
	err = model.DB.Where("response_id = ?", "resp_ledger_1").Find(&entries).Error
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// ===========================================================================
// FinalizeBilling integration test
// ===========================================================================

func TestFinalizeBilling_FullPipeline(t *testing.T) {
	truncateBgTables(t)

	rawUsage := &relaycommon.ProviderUsage{
		PromptTokens:     500,
		CompletionTokens: 1500,
		TotalTokens:      2000,
	}
	pricing := &relaycommon.PricingSnapshot{
		BillingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003,
		Currency:     "usd",
	}

	err := FinalizeBilling("resp_full_pipe", 42, rawUsage, pricing)
	require.NoError(t, err)

	// Verify all 3 tables have records
	var usageCount, billingCount, ledgerCount int64
	model.DB.Model(&model.BgUsageRecord{}).Where("response_id = ?", "resp_full_pipe").Count(&usageCount)
	model.DB.Model(&model.BgBillingRecord{}).Where("response_id = ?", "resp_full_pipe").Count(&billingCount)
	model.DB.Model(&model.BgLedgerEntry{}).Where("response_id = ?", "resp_full_pipe").Count(&ledgerCount)

	assert.Equal(t, int64(1), usageCount)
	assert.Equal(t, int64(1), billingCount)
	assert.Equal(t, int64(1), ledgerCount)
}

func TestFinalizeBilling_NilUsage(t *testing.T) {
	err := FinalizeBilling("resp_no_usage", 1, nil, nil)
	assert.NoError(t, err) // no-op
}
