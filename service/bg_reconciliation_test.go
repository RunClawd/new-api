package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Case 1: Stalled request tests
// ---------------------------------------------------------------------------

func TestReconcile_StalledAccepted(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 400
	const quota = 50000
	seedUser(t, orgID, quota-10000) // 40000 remaining (10000 reserved)

	// Create a response that's been accepted for > 5 minutes
	bgResp := &model.BgResponse{
		ResponseID:     "resp_reconcile_stall_1",
		Model:          "bg.llm.chat.test",
		OrgID:          orgID,
		Status:         model.BgResponseStatusAccepted,
		StatusVersion:  1,
		EstimatedQuota: 10000,
		CreatedAt:      time.Now().Unix() - 600, // 10 minutes ago
	}
	require.NoError(t, bgResp.Insert())

	err := ReconcileStalledResponses()
	require.NoError(t, err)

	// Verify response was marked as failed
	updated, _ := model.GetBgResponseByResponseID("resp_reconcile_stall_1")
	require.NotNil(t, updated)
	assert.Equal(t, model.BgResponseStatusFailed, updated.Status)

	// Verify quota was refunded (10000 back)
	remaining, _ := model.GetUserQuota(orgID, false)
	assert.Equal(t, quota, remaining) // 40000 + 10000 = 50000
}

func TestReconcile_RunningNotStalled(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 401
	seedUser(t, orgID, 40000)

	// Create a response that's been running for 10 minutes (under 30 min threshold)
	bgResp := &model.BgResponse{
		ResponseID:     "resp_reconcile_running_ok",
		Model:          "bg.video.generate.test",
		OrgID:          orgID,
		Status:         model.BgResponseStatusRunning,
		StatusVersion:  1,
		EstimatedQuota: 20000,
		CreatedAt:      time.Now().Unix() - 600, // 10 minutes ago
	}
	require.NoError(t, bgResp.Insert())

	err := ReconcileStalledResponses()
	require.NoError(t, err)

	// Verify response was NOT touched
	updated, _ := model.GetBgResponseByResponseID("resp_reconcile_running_ok")
	require.NotNil(t, updated)
	assert.Equal(t, model.BgResponseStatusRunning, updated.Status) // still running
}

func TestReconcile_RunningStalled(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 402
	seedUser(t, orgID, 30000) // 50000 - 20000 reserved

	// Create a response that's been running for > 30 minutes
	bgResp := &model.BgResponse{
		ResponseID:     "resp_reconcile_running_stall",
		Model:          "bg.video.generate.test",
		OrgID:          orgID,
		Status:         model.BgResponseStatusRunning,
		StatusVersion:  1,
		EstimatedQuota: 20000,
		CreatedAt:      time.Now().Unix() - 2400, // 40 minutes ago
	}
	require.NoError(t, bgResp.Insert())

	err := ReconcileStalledResponses()
	require.NoError(t, err)

	// Verify response was marked as failed
	updated, _ := model.GetBgResponseByResponseID("resp_reconcile_running_stall")
	require.NotNil(t, updated)
	assert.Equal(t, model.BgResponseStatusFailed, updated.Status)

	// Verify quota was refunded
	remaining, _ := model.GetUserQuota(orgID, false)
	assert.Equal(t, 50000, remaining) // 30000 + 20000
}

// ---------------------------------------------------------------------------
// Case 2: Missing billing (idempotent)
// ---------------------------------------------------------------------------

func TestReconcile_MissingBilling_AlreadyExists(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	// Create a succeeded response with billing_status != "completed"
	bgResp := &model.BgResponse{
		ResponseID:    "resp_reconcile_billing_flag",
		Model:         "bg.llm.chat.test",
		OrgID:         410,
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 1,
		BillingStatus: "pending", // should be "completed"
		FinalizedAt:   time.Now().Unix() - 60,
		CreatedAt:     time.Now().Unix() - 120,
	}
	require.NoError(t, bgResp.Insert())

	// Create a billing record (FinalizeBilling succeeded, but billing_status update failed)
	billing := &model.BgBillingRecord{
		BillingID:  "bill_reconcile_exist",
		ResponseID: "resp_reconcile_billing_flag",
		OrgID:      410,
		Amount:     0.05,
		Status:     model.BgBillingStatusPosted,
		CreatedAt:  time.Now().Unix() - 60,
	}
	require.NoError(t, billing.Insert())

	err := ReconcileStalledResponses()
	require.NoError(t, err)

	// Verify billing_status was fixed
	updated, _ := model.GetBgResponseByResponseID("resp_reconcile_billing_flag")
	require.NotNil(t, updated)
	assert.Equal(t, "completed", updated.BillingStatus)
}

// ---------------------------------------------------------------------------
// Case 3: Stale estimated records
// ---------------------------------------------------------------------------

func TestReconcile_StaleEstimated_TerminalResponse(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	// Create a terminal response with a lingering estimated billing record
	bgResp := &model.BgResponse{
		ResponseID:               "resp_reconcile_stale_term",
		Model:                    "bg.video.generate.test",
		OrgID:                    420,
		Status:                   model.BgResponseStatusSucceeded,
		StatusVersion:            1,
		ReservationBillingID:     "bill_stale_term",
		ReservationLedgerEntryID: "led_stale_term",
		FinalizedAt:              time.Now().Unix() - 60,
		CreatedAt:                time.Now().Unix() - 300,
	}
	require.NoError(t, bgResp.Insert())

	billing := &model.BgBillingRecord{
		BillingID:  "bill_stale_term",
		ResponseID: "resp_reconcile_stale_term",
		OrgID:      420,
		Amount:     0.10,
		Status:     model.BgBillingStatusEstimated, // should have been voided at terminal state
		CreatedAt:  time.Now().Unix() - 300,
	}
	require.NoError(t, billing.Insert())

	ledger := &model.BgLedgerEntry{
		LedgerEntryID: "led_stale_term",
		OrgID:         420,
		BillingID:     "bill_stale_term",
		EntryType:     "hold",
		Direction:     "debit",
		Amount:        0.10,
		Status:        "pending",
		CreatedAt:     time.Now().Unix() - 300,
	}
	require.NoError(t, ledger.Insert())

	err := ReconcileStalledResponses()
	require.NoError(t, err)

	// Verify estimated billing was voided
	updatedBilling, _ := model.GetBillingRecordByBillingID("bill_stale_term")
	require.NotNil(t, updatedBilling)
	assert.Equal(t, model.BgBillingStatusVoided, updatedBilling.Status)
}

func TestReconcile_StaleEstimated_ActiveResponse(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	// Create an active response with an estimated billing record
	bgResp := &model.BgResponse{
		ResponseID:           "resp_reconcile_stale_active",
		Model:                "bg.video.generate.test",
		OrgID:                421,
		Status:               model.BgResponseStatusRunning,
		StatusVersion:        1,
		ReservationBillingID: "bill_stale_active",
		CreatedAt:            time.Now().Unix() - 600, // 10 minutes ago — under 30 min running threshold
	}
	require.NoError(t, bgResp.Insert())

	billing := &model.BgBillingRecord{
		BillingID:  "bill_stale_active",
		ResponseID: "resp_reconcile_stale_active",
		OrgID:      421,
		Amount:     0.10,
		Status:     model.BgBillingStatusEstimated,
		CreatedAt:  time.Now().Unix() - 600,
	}
	require.NoError(t, billing.Insert())

	err := ReconcileStalledResponses()
	require.NoError(t, err)

	// Verify estimated billing was NOT voided (response still active)
	updatedBilling, _ := model.GetBillingRecordByBillingID("bill_stale_active")
	require.NotNil(t, updatedBilling)
	assert.Equal(t, model.BgBillingStatusEstimated, updatedBilling.Status)
}
