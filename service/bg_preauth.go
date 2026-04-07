package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ErrInsufficientQuota is returned when a user does not have enough quota for the estimated cost.
var ErrInsufficientQuota = fmt.Errorf("insufficient_quota")

// EstimateCost produces a rough cost estimate in quota units (1 quota unit = $0.002 / 500 tokens).
// This is intentionally conservative (overestimates) to avoid post-hoc billing surprises.
func EstimateCost(pricing *relaycommon.PricingSnapshot, input interface{}) int {
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
	case "request":
		estimatedUnits = 1
	default:
		estimatedUnits = 1
	}

	// Convert to quota units: amount = units * unitPrice, then to quota (1 quota = $0.002 / 500)
	amount := estimatedUnits * pricing.UnitPrice
	// The existing system uses quota units where 1 unit ≈ $0.002/500 tokens
	// Use a simple ratio: quotaCost = amount / 0.002 * 500
	// But for simplicity, we'll just use the raw amount * 500000 as integer quota units
	quotaCost := int(amount * 500000)
	if quotaCost < 1 && amount > 0 {
		quotaCost = 1 // minimum 1 quota unit if there's any cost
	}
	return quotaCost
}

// TryReserveQuota checks if the user has sufficient quota and deducts the estimated amount.
// Returns nil on success. The deducted amount will be reconciled in SettleReservation.
func TryReserveQuota(orgID int, estimatedQuota int) error {
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
