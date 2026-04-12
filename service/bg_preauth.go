package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ErrInsufficientQuota is returned when a user does not have enough quota for the estimated cost.
var ErrInsufficientQuota = fmt.Errorf("insufficient_quota")

// ReservationResult holds the IDs of estimated records created during Async/Session pre-auth.
// Persisted to bg_responses for crash recovery.
type ReservationResult struct {
	BillingID       string  // estimated billing record ID
	LedgerEntryID   string  // hold ledger entry ID
	OrgID           int
	EstimatedQuota  int
	EstimatedAmount float64
}

// EstimateCost produces a rough cost estimate in quota units (1 quota unit = $0.002 / 500 tokens).
// This is intentionally conservative (overestimates) to avoid post-hoc billing surprises.
func EstimateCost(pricing *relaycommon.PricingSnapshot, input interface{}, feeConfig *relaycommon.BYOFeeConfig) int {
	// BYO Platform Fee Early Calculation (if BYO mode is active)
	if feeConfig != nil {
		if feeConfig.FeeType == "per_request" && feeConfig.FixedAmount > 0 {
			quotaCost := int(feeConfig.FixedAmount * 500000)
			if quotaCost < 1 {
				quotaCost = 1
			}
			return quotaCost
		}
		// If percentage, we need the base hypothetical cost, which we'll calculate below.
	}

	if pricing == nil || pricing.UnitPrice == 0 {
		return 0 // free model or no pricing configured — no pre-auth needed
	}

	var estimatedUnits float64

	switch pricing.BillableUnit {
	case "token":
		// Rough estimation: input length / 4 characters ≈ tokens (English average).
		// Multiply by 2 for output buffer (conservative).
		inputLen := estimateInputLength(input)
		estimatedTokens := float64(inputLen) / 4.0
		estimatedUnits = estimatedTokens * 3 // input + ~2x output buffer
		if estimatedUnits < 1000 {
			estimatedUnits = 1000 // minimum 1K tokens
		}
	case "second":
		estimatedUnits = 30 // 30 seconds minimum for video
	case "minute":
		estimatedUnits = 5 // 5 minutes minimum for sessions
	case "action":
		estimatedUnits = 1
	case "per_call":
		estimatedUnits = 1 // 1 API call
	default:
		estimatedUnits = 1
	}

	estimatedCost := estimatedUnits * pricing.UnitPrice

	// Apply percentage calculation if in BYO percentage mode
	if feeConfig != nil && feeConfig.FeeType == "percentage" {
		estimatedCost = estimatedCost * feeConfig.PercentageRate
	}

	// Convert USD to quota units (1 USD = 500,000 Quota)
	quotaCost := int(estimatedCost * 500000)
	if quotaCost < 1 && estimatedCost > 0 {
		quotaCost = 1 // minimum 1 quota unit if there's any cost
	}
	return quotaCost
}

// ---------------------------------------------------------------------------
// Sync/Stream path: quota-only reservation
// ---------------------------------------------------------------------------

// ReserveQuota is the Sync/Stream pre-auth path.
// Checks quota sufficiency and deducts the estimated amount (1 UPDATE).
// Does NOT write estimated billing/ledger records (avoids write amplification).
// Crash safety: reconciliation sweep catches uncommitted deductions.
func ReserveQuota(orgID int, estimatedQuota int) error {
	if estimatedQuota <= 0 {
		return nil // no-op for free models
	}

	currentQuota, err := model.GetUserQuota(orgID, false)
	if err != nil {
		return fmt.Errorf("failed to check quota: %w", err)
	}

	if currentQuota < estimatedQuota {
		common.SysLog(fmt.Sprintf("preauth: org %d has %d quota, need %d — rejected",
			orgID, currentQuota, estimatedQuota))
		return ErrInsufficientQuota
	}

	// Reserve by deducting
	if err := model.DecreaseUserQuota(orgID, estimatedQuota); err != nil {
		return fmt.Errorf("failed to reserve quota: %w", err)
	}

	common.SysLog(fmt.Sprintf("preauth: reserved %d quota for org %d (remaining: ~%d)",
		estimatedQuota, orgID, currentQuota-estimatedQuota))
	return nil
}

// ---------------------------------------------------------------------------
// Async/Session path: quota + estimated billing record + hold ledger
// ---------------------------------------------------------------------------

// ReserveQuotaWithBillingHold is the Async/Session pre-auth path.
// Deducts quota (same as ReserveQuota) AND creates:
//   - bg_billing_records (status=estimated, amount=estimated_cost)
//   - bg_ledger_entries  (entry_type=hold, status=pending)
//
// Returns ReservationResult with IDs for persistence on bg_responses.
func ReserveQuotaWithBillingHold(
	orgID, projectID int,
	responseID, modelName string,
	pricing *relaycommon.PricingSnapshot,
	estimatedQuota int,
	billingSource string,
) (*ReservationResult, error) {
	// 1. Quota check + deduct (identical to ReserveQuota)
	if err := ReserveQuota(orgID, estimatedQuota); err != nil {
		return nil, err
	}

	// 2. Calculate estimated amount in billing currency
	estimatedAmount := float64(estimatedQuota) / 500000.0 // reverse of EstimateCost conversion

	// 3. Create estimated billing record
	billingID := relaycommon.GenerateBillingID()
	billingRecord := &model.BgBillingRecord{
		BillingID:   billingID,
		ResponseID:  responseID,
		OrgID:       orgID,
		ProjectID:   projectID,
		Model:       modelName,
		BillingSource: billingSource,
		PricingMode: pricing.PricingMode,
		BillingMode: billingSource, // legacy
		Amount:      estimatedAmount,
		Currency:    pricing.Currency,
		Status:      model.BgBillingStatusEstimated,
		CreatedAt:   time.Now().Unix(),
	}
	if pricing != nil {
		billingRecord.BillableUnit = pricing.BillableUnit
		billingRecord.UnitPrice = pricing.UnitPrice
	}
	if err := billingRecord.Insert(); err != nil {
		// Rollback quota deduction on billing record failure
		_ = model.IncreaseUserQuota(orgID, estimatedQuota, false)
		return nil, fmt.Errorf("preauth: failed to create estimated billing record: %w", err)
	}

	// 4. Create hold ledger entry
	ledgerEntryID := relaycommon.GenerateLedgerEntryID()
	ledgerEntry := &model.BgLedgerEntry{
		LedgerEntryID: ledgerEntryID,
		OrgID:         orgID,
		ResponseID:    responseID,
		BillingID:     billingID,
		EntryType:     "hold",
		Direction:     "debit",
		Amount:        estimatedAmount,
		Currency:      pricing.Currency,
		Status:        "pending",
		CreatedAt:     time.Now().Unix(),
	}
	if err := ledgerEntry.Insert(); err != nil {
		// Rollback: void the billing record and refund quota
		_, _ = model.UpdateBillingStatus(billingID, model.BgBillingStatusEstimated, model.BgBillingStatusVoided)
		_ = model.IncreaseUserQuota(orgID, estimatedQuota, false)
		return nil, fmt.Errorf("preauth: failed to create hold ledger entry: %w", err)
	}

	common.SysLog(fmt.Sprintf("preauth: reserved %d quota + estimated billing %s for org %d (response %s)",
		estimatedQuota, billingID, orgID, responseID))

	return &ReservationResult{
		BillingID:       billingID,
		LedgerEntryID:   ledgerEntryID,
		OrgID:           orgID,
		EstimatedQuota:  estimatedQuota,
		EstimatedAmount: estimatedAmount,
	}, nil
}

// ---------------------------------------------------------------------------
// Terminal state: void estimated records
// ---------------------------------------------------------------------------

// VoidEstimatedBilling voids the estimated billing record and its hold ledger entry.
// Called at terminal state for Async/Session paths — regardless of success or failure.
// The actual billing (if succeeded) is handled by FinalizeBilling separately.
// Idempotent: if already voided, returns nil.
func VoidEstimatedBilling(billingID, ledgerEntryID string) error {
	if billingID == "" {
		return nil
	}

	// Void billing record: estimated → voided
	won, err := model.UpdateBillingStatus(billingID, model.BgBillingStatusEstimated, model.BgBillingStatusVoided)
	if err != nil {
		return fmt.Errorf("void estimated billing: failed to update billing %s: %w", billingID, err)
	}
	if !won {
		// Already voided (idempotent) or status changed — check current state
		record, _ := model.GetBillingRecordByBillingID(billingID)
		if record != nil && record.Status != model.BgBillingStatusVoided {
			common.SysLog(fmt.Sprintf("void estimated billing: billing %s has unexpected status %s", billingID, record.Status))
		}
		// Still proceed to void ledger
	}

	// Void ledger entry: pending → voided
	if ledgerEntryID != "" {
		_, err := model.UpdateLedgerEntryStatus(ledgerEntryID, "pending", "voided")
		if err != nil {
			common.SysError(fmt.Sprintf("void estimated billing: failed to update ledger %s: %v", ledgerEntryID, err))
			// Non-fatal: billing record is already voided
		}
	}

	common.SysLog(fmt.Sprintf("preauth: voided estimated billing %s + ledger %s", billingID, ledgerEntryID))
	return nil
}

// ---------------------------------------------------------------------------
// Quota settlement (unified for Sync and Async)
// ---------------------------------------------------------------------------

// SettleReservation reconciles the pre-authorized amount with the actual cost.
// If actual < estimated, the difference is refunded.
// If actual > estimated, the difference is charged (best-effort).
func SettleReservation(orgID int, estimatedQuota int, actualQuota int) {
	if estimatedQuota <= 0 {
		return
	}

	diff := estimatedQuota - actualQuota
	if diff > 0 {
		// Refund the overestimate
		if err := model.IncreaseUserQuota(orgID, diff, false); err != nil {
			common.SysError(fmt.Sprintf("preauth: failed to refund %d quota to org %d: %v", diff, orgID, err))
		} else {
			common.SysLog(fmt.Sprintf("preauth: refunded %d quota to org %d (estimated=%d, actual=%d)",
				diff, orgID, estimatedQuota, actualQuota))
		}
	} else if diff < 0 {
		// Charge the underestimate (best-effort)
		extra := -diff
		if err := model.DecreaseUserQuota(orgID, extra); err != nil {
			common.SysError(fmt.Sprintf("preauth: failed to charge extra %d quota from org %d: %v", extra, orgID, err))
		} else {
			common.SysLog(fmt.Sprintf("preauth: charged extra %d quota from org %d (estimated=%d, actual=%d)",
				extra, orgID, estimatedQuota, actualQuota))
		}
	}
	// diff == 0: perfect estimate, no action needed
}

// estimateInputLength returns a rough character count of the input for token estimation.
func estimateInputLength(input interface{}) int {
	if input == nil {
		return 0
	}
	switch v := input.(type) {
	case string:
		return len(v)
	case map[string]interface{}:
		// Serialize to get approximate size
		b, _ := common.Marshal(v)
		return len(b)
	case []interface{}:
		b, _ := common.Marshal(v)
		return len(b)
	default:
		b, _ := common.Marshal(v)
		return len(b)
	}
}
