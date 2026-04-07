package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
) (*model.BgBillingRecord, error) {
	if usage == nil || pricing == nil {
		return nil, nil
	}

	amount := usage.BillableUnits * pricing.UnitPrice

	billingRecord := &model.BgBillingRecord{
		BillingID:    relaycommon.GenerateBillingID(),
		ResponseID:   responseID,
		BillingMode:  pricing.BillingMode,
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

// FinalizeBilling is the full 4-layer billing pipeline for a completed response:
//   Usage → Billing → Ledger → (Outbox not yet implemented)
//
// Called by the state machine when a response reaches a terminal state.
func FinalizeBilling(responseID string, orgID int, rawUsage *relaycommon.ProviderUsage, pricing *relaycommon.PricingSnapshot) error {
	// 1. Normalize usage
	canonicalUsage, err := NormalizeUsage(responseID, rawUsage)
	if err != nil {
		return fmt.Errorf("finalize billing: usage normalization failed: %w", err)
	}

	// 2. Calculate billing
	billingRecord, err := CalculateBilling(responseID, canonicalUsage, pricing)
	if err != nil {
		return fmt.Errorf("finalize billing: billing calculation failed: %w", err)
	}

	// 3. Post ledger entry
	_, err = PostLedgerEntry(orgID, billingRecord, "debit")
	if err != nil {
		return fmt.Errorf("finalize billing: ledger posting failed: %w", err)
	}

	// 4. Outbox (webhook notification) — TODO Phase 4
	// Will create a bg_webhook_event for external billing systems

	if billingRecord != nil {
		common.SysLog(fmt.Sprintf("billing: finalized %s — %.2f %s (%.4f units @ %.4f/%s)",
			responseID, billingRecord.Amount, billingRecord.Currency,
			canonicalUsage.BillableUnits, pricing.UnitPrice, canonicalUsage.BillableUnit))
	}

	return nil
}
