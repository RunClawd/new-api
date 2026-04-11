package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ReserveQuota tests (Sync/Stream path)
// ---------------------------------------------------------------------------

func TestReserveQuota_OK(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 100
	const quota = 50000
	seedUser(t, orgID, quota)

	err := ReserveQuota(orgID, 10000)
	require.NoError(t, err)

	// Verify quota was deducted
	remaining, _ := model.GetUserQuota(orgID, false)
	assert.Equal(t, quota-10000, remaining)
}

func TestReserveQuota_Insufficient(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 101
	seedUser(t, orgID, 500)

	err := ReserveQuota(orgID, 10000)
	assert.ErrorIs(t, err, ErrInsufficientQuota)
}

func TestReserveQuota_ZeroCost(t *testing.T) {
	truncate(t)

	// Zero cost should be a no-op (no DB interaction needed)
	err := ReserveQuota(999, 0)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ReserveQuotaWithBillingHold tests (Async/Session path)
// ---------------------------------------------------------------------------

func TestReserveQuotaWithBillingHold_WritesRecords(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 102
	const quota = 100000
	seedUser(t, orgID, quota)

	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.000002,
		Currency:     "usd",
	}

	result, err := ReserveQuotaWithBillingHold(
		orgID, 1,
		"resp_test_hold_001", "bg.llm.chat.test",
		pricing, 20000,
		"hosted",
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.BillingID)
	assert.NotEmpty(t, result.LedgerEntryID)
	assert.Equal(t, orgID, result.OrgID)
	assert.Equal(t, 20000, result.EstimatedQuota)

	// Verify billing record was created with estimated status
	billing, err := model.GetBillingRecordByBillingID(result.BillingID)
	require.NoError(t, err)
	require.NotNil(t, billing)
	assert.Equal(t, model.BgBillingStatusEstimated, billing.Status)
	assert.Equal(t, "resp_test_hold_001", billing.ResponseID)

	// Verify ledger entry was created with pending status
	ledger, err := model.GetLedgerEntryByBillingID(result.BillingID)
	require.NoError(t, err)
	require.NotNil(t, ledger)
	assert.Equal(t, "pending", ledger.Status)
	assert.Equal(t, "hold", ledger.EntryType)

	// Verify quota was deducted
	remaining, _ := model.GetUserQuota(orgID, false)
	assert.Equal(t, quota-20000, remaining)
}

func TestReserveQuotaWithBillingHold_InsufficientQuota(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 103
	seedUser(t, orgID, 100)

	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.000002,
		Currency:     "usd",
	}

	result, err := ReserveQuotaWithBillingHold(
		orgID, 1,
		"resp_test_hold_fail", "bg.llm.chat.test",
		pricing, 50000,
		"hosted",
	)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// VoidEstimatedBilling tests
// ---------------------------------------------------------------------------

func TestVoidEstimatedBilling_Success(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	// Create an estimated billing record + hold ledger
	billingID := "bill_test_void_001"
	ledgerEntryID := "led_test_void_001"

	billing := &model.BgBillingRecord{
		BillingID:  billingID,
		ResponseID: "resp_void_001",
		OrgID:      200,
		Status:     model.BgBillingStatusEstimated,
		Amount:     0.05,
		Currency:   "usd",
		CreatedAt:  time.Now().Unix(),
	}
	require.NoError(t, billing.Insert())

	ledger := &model.BgLedgerEntry{
		LedgerEntryID: ledgerEntryID,
		OrgID:         200,
		BillingID:     billingID,
		EntryType:     "hold",
		Direction:     "debit",
		Amount:        0.05,
		Status:        "pending",
		CreatedAt:     time.Now().Unix(),
	}
	require.NoError(t, ledger.Insert())

	// Void
	err := VoidEstimatedBilling(billingID, ledgerEntryID)
	require.NoError(t, err)

	// Verify billing status is voided
	updatedBilling, _ := model.GetBillingRecordByBillingID(billingID)
	require.NotNil(t, updatedBilling)
	assert.Equal(t, model.BgBillingStatusVoided, updatedBilling.Status)

	// Verify ledger status is voided
	updatedLedger, _ := model.GetLedgerEntryByBillingID(billingID)
	require.NotNil(t, updatedLedger)
	assert.Equal(t, "voided", updatedLedger.Status)
}

func TestVoidEstimatedBilling_AlreadyVoided(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	billingID := "bill_test_void_idem"
	billing := &model.BgBillingRecord{
		BillingID:  billingID,
		ResponseID: "resp_void_idem",
		OrgID:      201,
		Status:     model.BgBillingStatusVoided, // already voided
		Amount:     0.05,
		Currency:   "usd",
		CreatedAt:  time.Now().Unix(),
	}
	require.NoError(t, billing.Insert())

	// Should not error (idempotent)
	err := VoidEstimatedBilling(billingID, "")
	assert.NoError(t, err)
}

func TestVoidEstimatedBilling_EmptyBillingID(t *testing.T) {
	// Empty billing ID should be a no-op
	err := VoidEstimatedBilling("", "")
	assert.NoError(t, err)
}
