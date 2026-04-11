package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// stalledCondition defines a timeout threshold for a set of response statuses.
type stalledCondition struct {
	statuses []model.BgResponseStatus
	timeout  time.Duration
	reason   string
}

// stalledConditions defines per-status timeout thresholds for detecting stalled responses.
// - accepted/queued: 5 min (dispatch phase — no adapter running)
// - running: 30 min (Async video processing can take a long time)
// - streaming: 10 min (should complete in minutes)
var stalledConditions = []stalledCondition{
	{
		statuses: []model.BgResponseStatus{model.BgResponseStatusAccepted, model.BgResponseStatusQueued},
		timeout:  5 * time.Minute,
		reason:   "dispatch_stalled",
	},
	{
		statuses: []model.BgResponseStatus{model.BgResponseStatusRunning},
		timeout:  30 * time.Minute,
		reason:   "execution_timeout",
	},
	{
		statuses: []model.BgResponseStatus{model.BgResponseStatusStreaming},
		timeout:  10 * time.Minute,
		reason:   "stream_stalled",
	},
}

// ReconcileStalledResponses is the crash-safety sweep for billing integrity.
// Handles three cases:
//   - Case 1: Stalled non-terminal responses (per-status timeouts)
//   - Case 2: Terminal succeeded but billing not completed (idempotent re-run)
//   - Case 3: Stale estimated billing records for terminal/stalled responses
func ReconcileStalledResponses() error {
	common.SysLog("reconciliation: starting sweep")

	// Case 1: Stalled non-terminal responses
	if err := reconcileStalledRequests(); err != nil {
		common.SysError(fmt.Sprintf("reconciliation: case 1 error: %v", err))
	}

	// Case 2: Terminal succeeded but billing incomplete
	if err := reconcileMissingBilling(); err != nil {
		common.SysError(fmt.Sprintf("reconciliation: case 2 error: %v", err))
	}

	// Case 3: Stale estimated billing records
	if err := reconcileStaleEstimated(); err != nil {
		common.SysError(fmt.Sprintf("reconciliation: case 3 error: %v", err))
	}

	common.SysLog("reconciliation: sweep complete")
	return nil
}

// reconcileStalledRequests handles Case 1: non-terminal responses that exceeded their timeout.
func reconcileStalledRequests() error {
	now := time.Now().Unix()

	for _, cond := range stalledConditions {
		cutoff := now - int64(cond.timeout.Seconds())

		// Build status list for query
		statusStrings := make([]string, len(cond.statuses))
		for i, s := range cond.statuses {
			statusStrings[i] = string(s)
		}

		var responses []model.BgResponse
		err := model.DB.Where("status IN ? AND created_at < ?", statusStrings, cutoff).
			Limit(100).
			Find(&responses).Error
		if err != nil {
			return fmt.Errorf("query stalled responses (%s): %w", cond.reason, err)
		}

		for _, resp := range responses {
			common.SysLog(fmt.Sprintf("reconciliation: marking stalled response %s as failed (reason: %s, status: %s, age: %ds)",
				resp.ResponseID, cond.reason, resp.Status, now-resp.CreatedAt))

			// Mark as failed
			_ = model.DB.Model(&model.BgResponse{}).
				Where("id = ? AND status IN ?", resp.ID, statusStrings).
				Updates(map[string]interface{}{
					"status":         model.BgResponseStatusFailed,
					"finalized_at":   now,
					"billing_status": "none",
					"error_json":     fmt.Sprintf(`{"code":"reconciliation","message":"%s"}`, cond.reason),
				}).Error

			// Void estimated billing if Async path
			if resp.ReservationBillingID != "" {
				_ = VoidEstimatedBilling(resp.ReservationBillingID, resp.ReservationLedgerEntryID)
			}

			// Refund quota
			if resp.EstimatedQuota > 0 {
				SettleReservation(resp.OrgID, resp.EstimatedQuota, 0)
			}
		}
	}
	return nil
}

// reconcileMissingBilling handles Case 2: succeeded responses with incomplete billing.
// Idempotent: checks for existing billing records before re-running FinalizeBilling.
func reconcileMissingBilling() error {
	var responses []model.BgResponse
	err := model.DB.Where("status = ? AND billing_status != ?",
		model.BgResponseStatusSucceeded, "completed").
		Where("finalized_at > 0").
		Limit(50).
		Find(&responses).Error
	if err != nil {
		return fmt.Errorf("query missing billing: %w", err)
	}

	for _, resp := range responses {
		// Idempotency check: see if billing records already exist
		existing, _ := model.GetBillingRecordByResponseID(resp.ResponseID)
		if existing != nil {
			// Records exist — just fix the flag (FinalizeBilling succeeded but status update failed)
			common.SysLog(fmt.Sprintf("reconciliation: fixing billing_status flag for %s (billing record exists)", resp.ResponseID))
			_ = model.DB.Model(&model.BgResponse{}).
				Where("id = ?", resp.ID).
				Update("billing_status", "completed").Error
			continue
		}

		// No billing records — log for investigation
		// We cannot safely re-run FinalizeBilling without the original raw usage data,
		// which is not stored on the response record. Log and flag for manual review.
		common.SysLog(fmt.Sprintf("reconciliation: response %s succeeded but has no billing record and no raw usage — flagging for review",
			resp.ResponseID))
		_ = model.DB.Model(&model.BgResponse{}).
			Where("id = ?", resp.ID).
			Update("billing_status", "needs_review").Error
	}
	return nil
}

// reconcileStaleEstimated handles Case 3: estimated billing records whose responses are terminal or stalled.
func reconcileStaleEstimated() error {
	var records []model.BgBillingRecord
	err := model.DB.Where("status = ?", model.BgBillingStatusEstimated).
		Limit(100).
		Find(&records).Error
	if err != nil {
		return fmt.Errorf("query stale estimated: %w", err)
	}

	for _, record := range records {
		// Check the associated response's state
		resp, err := model.GetBgResponseByResponseID(record.ResponseID)
		if err != nil || resp == nil {
			// Response not found — orphaned estimated record, void it
			common.SysLog(fmt.Sprintf("reconciliation: voiding orphaned estimated billing %s (response not found)", record.BillingID))
			_ = VoidEstimatedBilling(record.BillingID, "")
			continue
		}

		if resp.Status.IsTerminal() {
			// Response is terminal but estimated wasn't voided — fix it
			// Use record.BillingID (from the loop), not resp.ReservationBillingID,
			// because the response field may be empty or stale.
			ledgerEntryID := lookupLedgerEntryID(record.BillingID)
			common.SysLog(fmt.Sprintf("reconciliation: voiding estimated billing %s (response %s is terminal: %s)",
				record.BillingID, resp.ResponseID, resp.Status))
			_ = VoidEstimatedBilling(record.BillingID, ledgerEntryID)
			continue
		}

		// Response is non-terminal — check if it's stalled using the same timeout rules
		if isResponseStalled(resp) {
			ledgerEntryID := lookupLedgerEntryID(record.BillingID)
			common.SysLog(fmt.Sprintf("reconciliation: voiding estimated billing %s (response %s is stalled, status: %s)",
				record.BillingID, resp.ResponseID, resp.Status))
			_ = VoidEstimatedBilling(record.BillingID, ledgerEntryID)
			// Also refund quota
			if resp.EstimatedQuota > 0 {
				SettleReservation(resp.OrgID, resp.EstimatedQuota, 0)
			}
		}
		// else: response is still active — don't touch the estimated record
	}
	return nil
}

// lookupLedgerEntryID finds the ledger entry associated with a billing record.
// Returns empty string if not found (VoidEstimatedBilling handles empty gracefully).
func lookupLedgerEntryID(billingID string) string {
	entry, _ := model.GetLedgerEntryByBillingID(billingID)
	if entry != nil {
		return entry.LedgerEntryID
	}
	return ""
}

// isResponseStalled checks if a non-terminal response has exceeded its status-specific timeout.
func isResponseStalled(resp *model.BgResponse) bool {
	now := time.Now().Unix()
	for _, cond := range stalledConditions {
		for _, s := range cond.statuses {
			if resp.Status == s {
				cutoff := now - int64(cond.timeout.Seconds())
				return resp.CreatedAt < cutoff
			}
		}
	}
	return false
}

// StartReconciliationWorker starts a background goroutine that periodically runs reconciliation.
func StartReconciliationWorker(interval time.Duration) {
	go func() {
		common.SysLog(fmt.Sprintf("reconciliation: worker started (interval: %s)", interval))
		// Run immediately on startup
		_ = ReconcileStalledResponses()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			_ = ReconcileStalledResponses()
		}
	}()
}
