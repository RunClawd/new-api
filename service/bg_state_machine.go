package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
		// If the transition isn't valid (e.g. accepted → running skipping queued),
		// we still allow it if the target is "more advanced" in the lifecycle.
		// This handles cases where events arrive out of order.
		if !newResponseStatus.IsTerminal() && resp.Status == newResponseStatus {
			return nil // same status, no-op
		}
		// For terminal statuses, always allow (provider says done = done)
		if !newResponseStatus.IsTerminal() {
			common.SysLog(fmt.Sprintf("state machine: skipping invalid transition %s → %s for %s",
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

	// 7. If terminal: trigger billing finalization (placeholder for Phase 2)
	if isTerminal {
		// TODO: Phase 2 - Finalize usage → billing → ledger → outbox
		common.SysLog(fmt.Sprintf("state machine: response %s finalized with status %s", responseID, newResponseStatus))
	}

	return nil
}
