package service

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ProviderEvent represents a status update from a provider adapter.
type ProviderEvent struct {
	Status            string                 `json:"status"`
	ProviderRequestID string                 `json:"provider_request_id,omitempty"`
	Output            []interface{}          `json:"output,omitempty"`
	Error             map[string]interface{} `json:"error,omitempty"`
	RawUsage          map[string]interface{} `json:"raw_usage,omitempty"`
	Progress          int                    `json:"progress,omitempty"`
	PollAfterMs       int                    `json:"poll_after_ms,omitempty"`
}

// DeriveResponseStatus maps an attempt status to the corresponding response status.
// This is the single source of truth for the attempt → response status derivation.
func DeriveResponseStatus(attemptStatus model.BgAttemptStatus) model.BgResponseStatus {
	switch attemptStatus {
	case model.BgAttemptStatusSucceeded:
		return model.BgResponseStatusSucceeded
	case model.BgAttemptStatusFailed:
		return model.BgResponseStatusFailed
	case model.BgAttemptStatusCanceled:
		return model.BgResponseStatusCanceled
	case model.BgAttemptStatusAbandoned:
		return model.BgResponseStatusFailed // abandoned attempts mean failure
	case model.BgAttemptStatusRunning:
		return model.BgResponseStatusRunning
	case model.BgAttemptStatusDispatching, model.BgAttemptStatusAccepted:
		return model.BgResponseStatusQueued
	default:
		return model.BgResponseStatusQueued
	}
}

// ValidateTransition checks if a state transition is valid for BgResponse.
func ValidateTransition(from, to model.BgResponseStatus) error {
	if from.IsTerminal() {
		return fmt.Errorf("cannot transition from terminal status %s", from)
	}
	if !from.CanTransitionTo(to) {
		return fmt.Errorf("transition %s → %s is not allowed", from, to)
	}
	return nil
}

// MapProviderEventToAttemptStatus converts a provider event status string to
// BgAttemptStatus. Returns the status and whether it's terminal.
func MapProviderEventToAttemptStatus(event ProviderEvent) (model.BgAttemptStatus, bool) {
	switch event.Status {
	case "succeeded", "completed", "success":
		return model.BgAttemptStatusSucceeded, true
	case "failed", "failure", "error":
		return model.BgAttemptStatusFailed, true
	case "canceled", "cancelled":
		return model.BgAttemptStatusCanceled, true
	case "running", "processing", "in_progress":
		return model.BgAttemptStatusRunning, false
	case "queued", "accepted", "submitted":
		return model.BgAttemptStatusAccepted, false
	default:
		return model.BgAttemptStatusUnknown, false
	}
}

// ApplyProviderEvent is the core state machine function. It atomically:
//  1. Loads the response + attempt
//  2. Checks for terminal/duplicate → no-op
//  3. Maps the event to attempt status
//  4. CAS-updates the attempt
//  5. Derives the response status
//  6. CAS-updates the response
//  7. If terminal: sets finalized_at
//
// This function is idempotent and safe for concurrent calls.
func ApplyProviderEvent(responseID, attemptID string, event ProviderEvent) error {
	// 1. Load response
	resp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil {
		return fmt.Errorf("response not found: %w", err)
	}

	// 2. Terminal check on response
	if resp.Status.IsTerminal() {
		return nil // no-op: response already finalized
	}

	// Load attempt
	attempts, err := model.GetBgAttemptsByResponseID(responseID)
	if err != nil {
		return fmt.Errorf("failed to load attempts: %w", err)
	}

	var attempt *model.BgResponseAttempt
	for i := range attempts {
		if attempts[i].AttemptID == attemptID {
			attempt = &attempts[i]
			break
		}
	}
	if attempt == nil {
		return fmt.Errorf("attempt %s not found for response %s", attemptID, responseID)
	}

	// Terminal check on attempt
	if attempt.Status.IsTerminal() {
		return nil // no-op: attempt already finalized
	}

	// 3. Map event → attempt status
	newAttemptStatus, isTerminal := MapProviderEventToAttemptStatus(event)
	if newAttemptStatus == model.BgAttemptStatusUnknown {
		// Unknown status — log but don't fail
		common.SysLog(fmt.Sprintf("unknown provider event status: %s for attempt %s", event.Status, attemptID))
		return nil
	}

	// 4. CAS update attempt
	prevAttemptStatus := attempt.Status
	prevAttemptVersion := attempt.StatusVersion

	attempt.Status = newAttemptStatus
	if event.ProviderRequestID != "" {
		attempt.ProviderRequestID = event.ProviderRequestID
	}
	if isTerminal {
		attempt.CompletedAt = time.Now().Unix()
		attempt.PollAfterAt = 0 // stop polling
	} else if event.PollAfterMs > 0 {
		attempt.PollAfterAt = time.Now().Unix() + int64(event.PollAfterMs/1000)
		if event.PollAfterMs%1000 > 0 {
			attempt.PollAfterAt++ // round up
		}
	} else {
		// Default exponential backoff with jitter
		base := 5
		exponent := attempt.PollCount
		if exponent > 6 {
			exponent = 6
		}
		backoff := base * (1 << exponent)
		if backoff > 300 {
			backoff = 300 // Cap at 5 minutes
		}
		jitter := rand.Int63n(int64(backoff)/4 + 1)
		attempt.PollAfterAt = time.Now().Unix() + int64(backoff) + jitter
	}
	attempt.PollCount++
	attempt.LastPollAt = time.Now().Unix()

	won, err := attempt.CASUpdateStatus(prevAttemptStatus, prevAttemptVersion)
	if err != nil {
		return fmt.Errorf("failed to CAS update attempt: %w", err)
	}
	if !won {
		return nil // another goroutine won the race — no-op
	}

	// 5. Derive response status from attempt
	newResponseStatus := DeriveResponseStatus(newAttemptStatus)

	// Validate the transition
	if err := ValidateTransition(resp.Status, newResponseStatus); err != nil {
		// Same status → no-op (idempotent)
		if resp.Status == newResponseStatus {
			return nil
		}

		// If the desired transition is invalid, try to auto-advance through
		// intermediate states. This handles the common case where a provider
		// skips "running" and goes directly to "succeeded" from "queued"
		// (which IS valid in the transition table).
		//
		// However, we do NOT allow arbitrary jumps. Each intermediate step
		// must also be valid per the transition table.
		//
		// Example allowed path: accepted → (auto-advance to queued) → succeeded
		// Example blocked path: accepted → succeeded (no valid auto-advance since
		//                       accepted → succeeded is not in the table)
		advanced := tryAutoAdvance(resp, newResponseStatus)
		if !advanced {
			common.SysLog(fmt.Sprintf("state machine: rejected invalid transition %s → %s for %s: %v",
				resp.Status, newResponseStatus, responseID, err))
			return nil
		}
		// After auto-advance, resp.Status has been updated in DB.
		// Reload to get the new version.
		resp, err = model.GetBgResponseByResponseID(responseID)
		if err != nil {
			return fmt.Errorf("failed to reload response after auto-advance: %w", err)
		}
		// Re-validate after advance
		if err := ValidateTransition(resp.Status, newResponseStatus); err != nil {
			common.SysLog(fmt.Sprintf("state machine: still invalid after auto-advance %s → %s for %s",
				resp.Status, newResponseStatus, responseID))
			return nil
		}
	}

	// 6. CAS update response
	prevRespStatus := resp.Status
	prevRespVersion := resp.StatusVersion

	resp.Status = newResponseStatus
	if isTerminal {
		resp.FinalizedAt = time.Now().Unix()
		// Serialize output and error
		if len(event.Output) > 0 {
			outputJSON, _ := common.Marshal(event.Output)
			resp.OutputJSON = string(outputJSON)
		}
		if event.Error != nil {
			errorJSON, _ := common.Marshal(event.Error)
			resp.ErrorJSON = string(errorJSON)
		}
	}
	resp.ActiveAttemptID = attempt.ID

	won, err = resp.CASUpdateStatus(prevRespStatus, prevRespVersion)
	if err != nil {
		return fmt.Errorf("failed to CAS update response: %w", err)
	}
	if !won {
		return nil // race lost — another event was applied first
	}

	// 7. If terminal: finalize billing + trigger webhook outbox
	if isTerminal {
		common.SysLog(fmt.Sprintf("state machine: response %s finalized with status %s", responseID, newResponseStatus))
		
		// 7a. Billing pipeline (transactional: usage + billing + ledger)
		// Only bill for succeeded responses (failed/canceled/expired = no charge)
		billingStatus := "none"
		if resp.Status == model.BgResponseStatusSucceeded && event.RawUsage != nil {
			rawUsage := eventRawUsageToProviderUsage(event.RawUsage)
			pricing := LookupPricing(resp.Model, resp.BillingMode)
			if err := FinalizeBilling(responseID, resp.OrgID, rawUsage, pricing); err != nil {
				common.SysError(fmt.Sprintf("billing failed for %s: %v", responseID, err))
				billingStatus = "failed"
			} else {
				billingStatus = "completed"
			}
		}
		// Update billing_status (best-effort, does not affect response state)
		_ = model.DB.Model(&model.BgResponse{}).
			Where("id = ?", resp.ID).
			Update("billing_status", billingStatus).Error
		
		// 7b. Webhook outbox
		if resp.WebhookURL != "" {
			payload := map[string]interface{}{
				"id":     resp.ResponseID,
				"object": "response",
				"status": resp.Status,
			}
			
			// Event type mapping:
			//   succeeded → response.completed (default, matches OpenAI convention)
			//   failed    → response.failed
			//   canceled  → response.canceled
			//   expired   → response.expired
			eventType := "response.completed"
			if resp.Status == model.BgResponseStatusFailed || resp.Status == model.BgResponseStatusCanceled || resp.Status == model.BgResponseStatusExpired {
				eventType = fmt.Sprintf("response.%s", resp.Status)
			}

			_ = EnqueueWebhookEvent(resp.ResponseID, resp.OrgID, eventType, payload)
		}
	}

	return nil
}

// stateOrder defines the canonical lifecycle ordering for auto-advance.
// A state can only auto-advance forward through this sequence.
var stateOrder = []model.BgResponseStatus{
	model.BgResponseStatusAccepted,
	model.BgResponseStatusQueued,
	model.BgResponseStatusRunning,
	// streaming is a parallel track, not auto-advanced into
}

// tryAutoAdvance attempts to advance the response through intermediate states
// to reach a position where the target transition becomes valid.
//
// For example, if response is "accepted" and target is "succeeded":
//   - accepted → queued is valid (auto-advance)
//   - queued → succeeded is valid (now the caller can apply it)
//
// Returns true if auto-advance succeeded (resp was updated in DB).
// Returns false if no valid auto-advance path exists.
func tryAutoAdvance(resp *model.BgResponse, target model.BgResponseStatus) bool {
	current := resp.Status

	// Find current position in the state order
	currentIdx := -1
	for i, s := range stateOrder {
		if s == current {
			currentIdx = i
			break
		}
	}
	if currentIdx < 0 {
		return false // current state not in the auto-advance chain
	}

	// Try advancing one step at a time until the target becomes a valid transition
	for i := currentIdx + 1; i < len(stateOrder); i++ {
		next := stateOrder[i]

		// Check if current → next is valid
		if !current.CanTransitionTo(next) {
			return false
		}

		// Check if next → target would be valid (look-ahead)
		if next.CanTransitionTo(target) {
			// Advance to 'next' via CAS
			prevStatus := resp.Status
			prevVersion := resp.StatusVersion
			resp.Status = next
			won, err := resp.CASUpdateStatus(prevStatus, prevVersion)
			if err != nil || !won {
				return false
			}
			common.SysLog(fmt.Sprintf("state machine: auto-advanced %s → %s (target: %s)",
				prevStatus, next, target))
			return true
		}

		// Advance and continue looking
		prevStatus := resp.Status
		prevVersion := resp.StatusVersion
		resp.Status = next
		won, err := resp.CASUpdateStatus(prevStatus, prevVersion)
		if err != nil || !won {
			return false
		}
		common.SysLog(fmt.Sprintf("state machine: auto-advanced %s → %s (continuing toward %s)",
			prevStatus, next, target))
		current = next
	}

	return false
}

// eventRawUsageToProviderUsage converts the untyped RawUsage map from a
// ProviderEvent into a typed ProviderUsage struct for the billing pipeline.
func eventRawUsageToProviderUsage(raw map[string]interface{}) *relaycommon.ProviderUsage {
	if raw == nil {
		return nil
	}
	usage := &relaycommon.ProviderUsage{}
	if v, ok := raw["prompt_tokens"]; ok {
		usage.PromptTokens = toInt(v)
	}
	if v, ok := raw["completion_tokens"]; ok {
		usage.CompletionTokens = toInt(v)
	}
	if v, ok := raw["total_tokens"]; ok {
		usage.TotalTokens = toInt(v)
	}
	if v, ok := raw["duration_sec"]; ok {
		usage.DurationSec = toFloat64(v)
	}
	if v, ok := raw["session_minutes"]; ok {
		usage.SessionMinutes = toFloat64(v)
	}
	if v, ok := raw["actions"]; ok {
		usage.Actions = toInt(v)
	}
	if v, ok := raw["billable_units"]; ok {
		usage.BillableUnits = toFloat64(v)
	}
	if v, ok := raw["billable_unit"]; ok {
		if s, ok := v.(string); ok {
			usage.BillableUnit = s
		}
	}
	// Auto-compute total_tokens if missing
	if usage.TotalTokens == 0 && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
