package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefundBilling_PostedRecord(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	const orgID = 300
	const initQuota = 100000
	seedUser(t, orgID, initQuota)

	// Create a posted billing record (as FinalizeBilling would)
	billingID := relaycommon.GenerateBillingID()
	billing := &model.BgBillingRecord{
		BillingID:  billingID,
		ResponseID: "resp_refund_001",
		OrgID:      orgID,
		Model:      "bg.llm.chat.test",
		Amount:     0.10,
		Currency:   "usd",
		Status:     model.BgBillingStatusPosted,
		CreatedAt:  time.Now().Unix(),
	}
	require.NoError(t, billing.Insert())

	// Refund
	err := RefundBilling(billingID, orgID, "test refund")
	require.NoError(t, err)

	// Verify a refund billing record was created
	var refundRecords []model.BgBillingRecord
	model.DB.Where("response_id = ? AND status = ?", "resp_refund_001", model.BgBillingStatusRefunded).
		Find(&refundRecords)
	require.Len(t, refundRecords, 1)
	assert.Equal(t, -0.10, refundRecords[0].Amount) // negative for refund

	// Verify a credit ledger entry was created
	var creditLedgers []model.BgLedgerEntry
	model.DB.Where("billing_id = ? AND direction = ?", refundRecords[0].BillingID, "credit").
		Find(&creditLedgers)
	require.Len(t, creditLedgers, 1)
	assert.Equal(t, "refund", creditLedgers[0].EntryType)
	assert.Equal(t, 0.10, creditLedgers[0].Amount)
}

func TestRefundBilling_EstimatedRecord(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	// Create an estimated billing record (should be rejected)
	billingID := relaycommon.GenerateBillingID()
	billing := &model.BgBillingRecord{
		BillingID:  billingID,
		ResponseID: "resp_refund_est",
		OrgID:      301,
		Amount:     0.05,
		Status:     model.BgBillingStatusEstimated,
		CreatedAt:  time.Now().Unix(),
	}
	require.NoError(t, billing.Insert())

	err := RefundBilling(billingID, 301, "should fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be posted or settled")
}

func TestRefundBilling_NotFound(t *testing.T) {
	truncate(t)
	truncateBgTables(t)

	err := RefundBilling("bill_nonexistent", 999, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
