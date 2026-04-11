package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

// NormalizeUsage converts raw provider usage into a canonical usage record
// and persists it to bg_usage_records.
func NormalizeUsage(responseID string, rawUsage *relaycommon.ProviderUsage) (*relaycommon.CanonicalUsage, error) {
	if rawUsage == nil {
		return nil, nil
	}

	canonical := &relaycommon.CanonicalUsage{
		RawUsage: rawUsage,
	}

	// Determine the primary billable dimension
	switch {
	case rawUsage.TotalTokens > 0:
		canonical.BillableUnit = "token"
		canonical.BillableUnits = float64(rawUsage.TotalTokens)
		canonical.InputUnits = float64(rawUsage.PromptTokens)
		canonical.OutputUnits = float64(rawUsage.CompletionTokens)
	case rawUsage.DurationSec > 0:
		canonical.BillableUnit = "second"
		canonical.BillableUnits = rawUsage.DurationSec
	case rawUsage.SessionMinutes > 0:
		canonical.BillableUnit = "minute"
		canonical.BillableUnits = rawUsage.SessionMinutes
	case rawUsage.Actions > 0:
		canonical.BillableUnit = "action"
		canonical.BillableUnits = float64(rawUsage.Actions)
	case rawUsage.BillableUnits > 0:
		canonical.BillableUnit = rawUsage.BillableUnit
		canonical.BillableUnits = rawUsage.BillableUnits
	default:
		// Request-based billing (1 per call)
		canonical.BillableUnit = "request"
		canonical.BillableUnits = 1
	}

	// Persist to bg_usage_records
	usageJSON, _ := common.Marshal(rawUsage)
	usageRecord := &model.BgUsageRecord{
		UsageID:       relaycommon.GenerateUsageID(),
		ResponseID:    responseID,
		BillableUnits: canonical.BillableUnits,
		BillableUnit:  canonical.BillableUnit,
		InputUnits:    canonical.InputUnits,
		OutputUnits:   canonical.OutputUnits,
		RawUsageJSON:  string(usageJSON),
		CreatedAt:     time.Now().Unix(),
	}
	if err := usageRecord.Insert(); err != nil {
		common.SysError(fmt.Sprintf("usage normalizer: failed to persist usage for %s: %v", responseID, err))
		// Don't fail the request — usage recording is non-critical
	}

	return canonical, nil
}

// CalculateBilling computes the billing amount from normalized usage and pricing.
func CalculateBilling(
	responseID string,
	usage *relaycommon.CanonicalUsage,
	pricing *relaycommon.PricingSnapshot,
	billingSource string,
) (*model.BgBillingRecord, error) {
	if usage == nil || pricing == nil {
		return nil, nil
	}

	amount := usage.BillableUnits * pricing.UnitPrice

	billingRecord := &model.BgBillingRecord{
		BillingID:    relaycommon.GenerateBillingID(),
		ResponseID:   responseID,
		BillingSource: billingSource,
		PricingMode:  pricing.PricingMode,
		BillingMode:  billingSource, // legacy
		BillableUnit: usage.BillableUnit,
		Quantity:     usage.BillableUnits,
		UnitPrice:    pricing.UnitPrice,
		Amount:       amount,
		Currency:     pricing.Currency,
		CreatedAt:    time.Now().Unix(),
	}

	if err := billingRecord.Insert(); err != nil {
		return nil, fmt.Errorf("billing engine: failed to persist billing for %s: %w", responseID, err)
	}

	return billingRecord, nil
}

// PostLedgerEntry creates a ledger entry (debit/credit) for a billing record.
func PostLedgerEntry(
	orgID int,
	billingRecord *model.BgBillingRecord,
	entryType string, // "debit" or "credit"
) (*model.BgLedgerEntry, error) {
	if billingRecord == nil {
		return nil, nil
	}

	ledgerEntry := &model.BgLedgerEntry{
		LedgerEntryID: relaycommon.GenerateLedgerEntryID(),
		OrgID:         orgID,
		ResponseID:    billingRecord.ResponseID,
		BillingID:     billingRecord.BillingID,
		EntryType:     entryType,
		Amount:        billingRecord.Amount,
		Currency:      billingRecord.Currency,
		CreatedAt:     time.Now().Unix(),
	}

	if err := ledgerEntry.Insert(); err != nil {
		return nil, fmt.Errorf("ledger engine: failed to persist ledger entry: %w", err)
	}

	return ledgerEntry, nil
}

// FinalizeBilling is the full billing pipeline for a completed response:
//
//	Usage → Billing → Ledger (all in one transaction)
//
// Called by the state machine when a response reaches a terminal state.
// If any step fails, the entire transaction rolls back — no partial writes.
// orgID, projectID, model, provider are populated on every record so the
// Usage API (WHERE org_id = ?) returns rows from this billing path.
func FinalizeBilling(
	responseID string,
	orgID, projectID int,
	modelName, provider string,
	rawUsage *relaycommon.ProviderUsage,
	pricing *relaycommon.PricingSnapshot,
	billingSource string,
	feeConfig *relaycommon.BYOFeeConfig,
) (actualQuotaUsed int, err error) {
	if rawUsage == nil {
		return 0, nil
	}

	// 1. Build structs (pure computation, no DB)
	canonicalUsage := buildCanonicalUsage(rawUsage)

	usageRecord := &model.BgUsageRecord{
		UsageID:       relaycommon.GenerateUsageID(),
		ResponseID:    responseID,
		OrgID:         orgID,
		ProjectID:     projectID,
		Model:         modelName,
		Provider:      provider,
		BillableUnits: canonicalUsage.BillableUnits,
		BillableUnit:  canonicalUsage.BillableUnit,
		InputUnits:    canonicalUsage.InputUnits,
		OutputUnits:   canonicalUsage.OutputUnits,
		Status:        "finalized", // Mark as finalized — resource consumption is real regardless of pricing
		CreatedAt:     time.Now().Unix(),
	}
	usageJSON, _ := common.Marshal(rawUsage)
	usageRecord.RawUsageJSON = string(usageJSON)

	// 2. Transactional write: usage + billing + ledger
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		// 2a. Insert usage record
		if err := tx.Create(usageRecord).Error; err != nil {
			return fmt.Errorf("usage insert failed: %w", err)
		}

		// 2b. Calculate billing (skip if no pricing or zero amount)
		if pricing == nil || pricing.UnitPrice == 0 {
			common.SysLog(fmt.Sprintf("billing: %s — no pricing configured, usage recorded only", responseID))
			return nil
		}

		amount := canonicalUsage.BillableUnits * pricing.UnitPrice
		billingRecord := &model.BgBillingRecord{
			BillingID:    relaycommon.GenerateBillingID(),
			ResponseID:   responseID,
			OrgID:        orgID,
			ProjectID:    projectID,
			Model:        modelName,
			Provider:     provider,
			BillingSource: billingSource,
			PricingMode:  pricing.PricingMode,
			BillingMode:  billingSource, // legacy
			BillableUnit: canonicalUsage.BillableUnit,
			Quantity:     canonicalUsage.BillableUnits,
			UnitPrice:    pricing.UnitPrice,
			Amount:       amount,
			Currency:     pricing.Currency,
			CreatedAt:    time.Now().Unix(),
		}
		if err := tx.Create(billingRecord).Error; err != nil {
			return fmt.Errorf("billing insert failed: %w", err)
		}

		// 2c. Post ledger debit
		ledgerEntry := &model.BgLedgerEntry{
			LedgerEntryID: relaycommon.GenerateLedgerEntryID(),
			OrgID:         orgID,
			ResponseID:    responseID,
			BillingID:     billingRecord.BillingID,
			EntryType:     "debit",
			Amount:        amount,
			Currency:      pricing.Currency,
			CreatedAt:     time.Now().Unix(),
		}
		if err := tx.Create(ledgerEntry).Error; err != nil {
			return fmt.Errorf("ledger insert failed: %w", err)
		}

		common.SysLog(fmt.Sprintf("billing: finalized %s — %.2f %s (%.4f units @ %.4f/%s)",
			responseID, amount, pricing.Currency,
			canonicalUsage.BillableUnits, pricing.UnitPrice, canonicalUsage.BillableUnit))

		// BYO path (Phase 13): when billing_source == "byo", use existing Amount field
		// as platform fee, set ProviderCost=0, PlatformMargin=Amount.
		// No new PlatformFee column needed — reuse existing fields.
		// if billingSource == "byo" { return finalizeBYOBilling(...) }

		actualQuotaUsed = int(amount * 500000.0)
		return nil
	})
	return actualQuotaUsed, err
}

// RefundBilling creates a refund for a posted/settled billing record.
// Creates a new billing record (status=refunded, linked to original via response_id)
// and a credit ledger entry. Also refunds quota to the org.
// Returns error if original billing is not in posted/settled status.
func RefundBilling(billingID string, orgID int, reason string) error {
	// 1. Load original billing record
	original, err := model.GetBillingRecordByBillingID(billingID)
	if err != nil {
		return fmt.Errorf("refund: billing record %s not found: %w", billingID, err)
	}

	// 2. Validate status: must be posted or settled
	if original.Status != model.BgBillingStatusPosted && original.Status != model.BgBillingStatusSettled {
		return fmt.Errorf("refund: billing %s has status %s, must be posted or settled", billingID, original.Status)
	}

	// 3. Create refund billing record
	refundBilling := &model.BgBillingRecord{
		BillingID:    relaycommon.GenerateBillingID(),
		ResponseID:   original.ResponseID,
		OrgID:        original.OrgID,
		ProjectID:    original.ProjectID,
		Model:        original.Model,
		Provider:     original.Provider,
		BillingMode:  original.BillingMode,
		BillableUnit: original.BillableUnit,
		Quantity:     original.Quantity,
		UnitPrice:    original.UnitPrice,
		Amount:       -original.Amount, // negative for refund
		Currency:     original.Currency,
		Status:       model.BgBillingStatusRefunded,
		CreatedAt:    time.Now().Unix(),
	}
	if err := refundBilling.Insert(); err != nil {
		return fmt.Errorf("refund: failed to create refund record: %w", err)
	}

	// 4. Create credit ledger entry
	ledgerEntry := &model.BgLedgerEntry{
		LedgerEntryID: relaycommon.GenerateLedgerEntryID(),
		OrgID:         orgID,
		ResponseID:    original.ResponseID,
		BillingID:     refundBilling.BillingID,
		EntryType:     "refund",
		Direction:     "credit",
		Amount:        original.Amount,
		Currency:      original.Currency,
		Status:        "committed",
		CreatedAt:     time.Now().Unix(),
	}
	if err := ledgerEntry.Insert(); err != nil {
		return fmt.Errorf("refund: failed to create credit ledger entry: %w", err)
	}

	// 5. Refund quota
	quotaRefund := int(original.Amount * 500000) // reverse of EstimateCost conversion
	if quotaRefund > 0 {
		if err := model.IncreaseUserQuota(orgID, quotaRefund, false); err != nil {
			common.SysError(fmt.Sprintf("refund: failed to refund %d quota to org %d: %v", quotaRefund, orgID, err))
		}
	}

	common.SysLog(fmt.Sprintf("refund: created refund %s for billing %s (%.4f %s, reason: %s)",
		refundBilling.BillingID, billingID, original.Amount, original.Currency, reason))
	return nil
}

// buildCanonicalUsage converts raw provider usage into canonical form (pure computation, no DB).
func buildCanonicalUsage(rawUsage *relaycommon.ProviderUsage) *relaycommon.CanonicalUsage {
	canonical := &relaycommon.CanonicalUsage{RawUsage: rawUsage}
	switch {
	case rawUsage.TotalTokens > 0:
		canonical.BillableUnit = "token"
		canonical.BillableUnits = float64(rawUsage.TotalTokens)
		canonical.InputUnits = float64(rawUsage.PromptTokens)
		canonical.OutputUnits = float64(rawUsage.CompletionTokens)
	case rawUsage.DurationSec > 0:
		canonical.BillableUnit = "second"
		canonical.BillableUnits = rawUsage.DurationSec
	case rawUsage.SessionMinutes > 0:
		canonical.BillableUnit = "minute"
		canonical.BillableUnits = rawUsage.SessionMinutes
	case rawUsage.Actions > 0:
		canonical.BillableUnit = "action"
		canonical.BillableUnits = float64(rawUsage.Actions)
	case rawUsage.BillableUnits > 0:
		canonical.BillableUnit = rawUsage.BillableUnit
		canonical.BillableUnits = rawUsage.BillableUnits
	default:
		canonical.BillableUnit = "request"
		canonical.BillableUnits = 1
	}
	return canonical
}

// LookupPricing bridges BaseGate pricing to the existing ratio_setting system.
// Phase 5: reads model ratio from the system-wide configuration.
func LookupPricing(modelName string, billingSource string) *relaycommon.PricingSnapshot {
	value, usePrice, exists := ratio_setting.GetModelRatioOrPrice(modelName)
	if !exists {
		return &relaycommon.PricingSnapshot{
			PricingMode:  "metered",
			BillableUnit: "token",
			UnitPrice:    0,
			Currency:     "usd",
		}
	}

	if usePrice {
		// Price-based model (e.g. dall-e, suno): value is direct $ per request/image
		return &relaycommon.PricingSnapshot{
			PricingMode:  "per_call",
			BillableUnit: "request",
			UnitPrice:    value,
			Currency:     "usd",
		}
	}

	// Ratio-based model: value is the ratio (1 = $0.002/1K tokens)
	// Convert ratio to per-token price: ratio * $0.002 / 1000 = ratio * 0.000002
	perTokenPrice := value * 0.002 / 1000.0
	return &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    perTokenPrice,
		Currency:     "usd",
	}
}

// FinalizeSessionBilling is the billing pipeline for closed sessions.
// Wraps usage + billing + ledger in a single transaction.
func FinalizeSessionBilling(session *model.BgSession) error {
	// 1. Aggregate action usage
	actions, err := model.GetBgSessionActionsBySessionID(session.SessionID)
	if err != nil {
		return fmt.Errorf("finalize session billing: failed to list actions: %w", err)
	}

	actionCount := 0
	for _, action := range actions {
		if action.UsageJSON != "" {
			actionCount++
		}
	}

	// 2. Calculate session duration in minutes
	var totalMinutes float64
	if session.ClosedAt > 0 && session.CreatedAt > 0 {
		totalMinutes = float64(session.ClosedAt-session.CreatedAt) / 60.0
	}

	// 3. Build usage record struct
	rawUsageStr := fmt.Sprintf(`{"session_minutes":%f,"action_count":%d}`, totalMinutes, len(actions))
	usageRecord := &model.BgUsageRecord{
		UsageID:       relaycommon.GenerateUsageID(),
		ResponseID:    session.SessionID,
		OrgID:         session.OrgID,
		ProjectID:     session.ProjectID,
		Model:         session.Model,
		Provider:      session.AdapterName,
		BillableUnits: totalMinutes,
		BillableUnit:  "minute",
		InputUnits:    float64(actionCount),
		RawUsageJSON:  rawUsageStr,
		CreatedAt:     time.Now().Unix(),
	}

	// 4. Lookup real pricing from ratio_setting
	pricing := LookupPricing(session.Model, "hosted")

	// 5. Transactional write
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(usageRecord).Error; err != nil {
			return fmt.Errorf("session usage insert failed: %w", err)
		}

		if pricing.UnitPrice == 0 {
			common.SysLog(fmt.Sprintf("session_billing: finalized %s — 0.00 usd (%.2f mins), %d actions",
				session.SessionID, totalMinutes, actionCount))
			return nil
		}

		amount := totalMinutes * pricing.UnitPrice
		billingRecord := &model.BgBillingRecord{
			BillingID:    relaycommon.GenerateBillingID(),
			ResponseID:   session.SessionID,
			BillingSource: "hosted", // sessions currently only hosted
			PricingMode:  pricing.PricingMode,
			BillingMode:  "hosted", // legacy
			BillableUnit: "minute",
			Quantity:     totalMinutes,
			UnitPrice:    pricing.UnitPrice,
			Amount:       amount,
			Currency:     pricing.Currency,
			CreatedAt:    time.Now().Unix(),
		}
		if err := tx.Create(billingRecord).Error; err != nil {
			return fmt.Errorf("session billing insert failed: %w", err)
		}

		ledgerEntry := &model.BgLedgerEntry{
			LedgerEntryID: relaycommon.GenerateLedgerEntryID(),
			OrgID:         session.OrgID,
			ResponseID:    session.SessionID,
			BillingID:     billingRecord.BillingID,
			EntryType:     "debit",
			Amount:        amount,
			Currency:      pricing.Currency,
			CreatedAt:     time.Now().Unix(),
		}
		if err := tx.Create(ledgerEntry).Error; err != nil {
			return fmt.Errorf("session ledger insert failed: %w", err)
		}

		common.SysLog(fmt.Sprintf("session_billing: finalized %s — %.2f %s (%.2f mins)",
			session.SessionID, amount, pricing.Currency, totalMinutes))
		return nil
	})
}

