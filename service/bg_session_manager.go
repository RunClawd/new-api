package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var (
	ErrSessionNotFound     = errors.New("session not found")
	ErrSessionBusy         = errors.New("session is currently busy with another action")
	ErrSessionTerminal     = errors.New("session is in a terminal state")
	ErrSessionAdapter      = errors.New("adapter missing or not session capable")
	ErrSessionValidation   = errors.New("validation failed")
)

// CreateSession initiates a new stateful session using a SessionCapableAdapter.
func CreateSession(req *relaycommon.CanonicalRequest) (*dto.BGSessionResponse, error) {
	// Lookup adapters
	adapters := basegate.LookupAdapters(req.Model)
	if len(adapters) == 0 {
		return nil, fmt.Errorf("no adapters found for model: %s", req.Model)
	}

	var providerResult *relaycommon.SessionResult
	var providerErr error
	var selectedAdapter basegate.SessionCapableAdapter
	var lastValidAdapter bool

	for _, providerAdapter := range adapters {
		sessionAdapter, ok := providerAdapter.(basegate.SessionCapableAdapter)
		if !ok {
			continue
		}

		// Validate request
		validation := sessionAdapter.Validate(req)
		if validation != nil && !validation.Valid {
			continue
		}
		lastValidAdapter = true

		providerResult, providerErr = sessionAdapter.CreateSession(req)
		if providerErr != nil {
			common.SysLog(fmt.Sprintf("fallback: %s failed to create session: %v", sessionAdapter.Name(), providerErr))
			continue
		}
		// Success
		selectedAdapter = sessionAdapter
		break
	}

	if providerResult == nil {
		if !lastValidAdapter {
			return nil, fmt.Errorf("%w: no valid adapter found", ErrSessionValidation)
		}
		return nil, fmt.Errorf("all %d adapters failed to create session. last err: %v", len(adapters), providerErr)
	}

	// Generate session ID
	sessionID := relaycommon.GenerateSessionID()
	now := time.Now().Unix()

	// Initial DB record
	bgSess := &model.BgSession{
		SessionID:         sessionID,
		ResponseID:        req.ResponseID, // Link to orchestrator response
		OrgID:             req.OrgID,
		ProjectID:         req.ProjectID,
		ApiKeyID:          req.ApiKeyID,
		Model:             req.Model,
		AdapterName:       selectedAdapter.Name(),
		Status:            model.BgSessionStatusCreating,
		StatusVersion:     1,
		ActionLockVersion: 1,
		CreatedAt:         now,
		LastActionAt:      now,
		WebhookURL:        req.ExecutionOptions.WebhookURL,
	}

	if err := bgSess.Insert(); err != nil {
		return nil, fmt.Errorf("failed to create session record: %w", err)
	}

	// Calculate expiration
	expiresAt := providerResult.ExpiresAt
	if expiresAt <= 0 {
		// Fallback to config max duration. In a real system, we'd lookup model meta.
		// Using 3600 timeout as a default fallback
		expiresAt = now + 3600
	}

	// Successfully created -> active
	bgSess.ProviderSessionID = providerResult.SessionID
	bgSess.ExpiresAt = expiresAt
	bgSess.IdleTimeoutSec = 300 // default 5 mins idle timeout
	
	// Atomically assign Provider properties and transition to Active
	expectedStatus := model.BgSessionStatusCreating
	expectedVersion := bgSess.StatusVersion
	targetStatus := model.BgSessionStatusActive

	updates := map[string]interface{}{
		"provider_session_id": bgSess.ProviderSessionID,
		"expires_at":          bgSess.ExpiresAt,
		"idle_timeout_sec":    bgSess.IdleTimeoutSec,
		"status":              targetStatus,
		"status_version":      expectedVersion + 1,
	}

	resultDB := model.DB.Model(&model.BgSession{}).
		Where("id = ? AND status = ? AND status_version = ?", bgSess.ID, expectedStatus, expectedVersion).
		Updates(updates)

	if resultDB.Error != nil || resultDB.RowsAffected == 0 {
		common.SysLog(fmt.Sprintf("session_manager: failed to atomically activate session %s: %v", sessionID, resultDB.Error))
	} else {
		bgSess.Status = targetStatus
		bgSess.StatusVersion = expectedVersion + 1
	}

	return buildSessionResponseFromDB(bgSess)
}

// GetSession retrieves the current state of a session.
func GetSession(sessionID string) (*dto.BGSessionResponse, error) {
	bgSess, err := model.GetBgSessionBySessionID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	return buildSessionResponseFromDB(bgSess)
}

// ExecuteSessionAction runs an action against a session with concurrency guards.
func ExecuteSessionAction(sessionID string, req *dto.BGSessionActionRequest) (*dto.BGSessionActionResponse, error) {
	// 1. Idempotency Check
	if req.IdempotencyKey != "" {
		existing, err := model.GetBgSessionActionByIdempotencyKey(sessionID, req.IdempotencyKey)
		if err == nil && existing != nil {
			return buildSessionActionResponseFromDB(existing)
		}
	}

	// 2. Lookup Session
	bgSess, err := model.GetBgSessionBySessionID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSessionNotFound, err)
	}

	if bgSess.Status.IsTerminal() {
		return nil, fmt.Errorf("%w: session %s is in state %s", ErrSessionTerminal, sessionID, bgSess.Status)
	}

	// 3. Acquire CAS Lock
	locked, err := bgSess.AcquireActionLock()
	if err != nil || !locked {
		return nil, fmt.Errorf("%w: %s", ErrSessionBusy, sessionID)
	}

	// If session was idle, wake it up
	if bgSess.Status == model.BgSessionStatusIdle {
		_, _ = bgSess.CASUpdateStatus(model.BgSessionStatusIdle, bgSess.StatusVersion, model.BgSessionStatusActive)
	}

	// 4. Create Action Log
	actionLog := &model.BgSessionAction{
		ActionID:       relaycommon.GenerateActionID(),
		SessionID:      sessionID,
		ActionType:     req.Action,
		IdempotencyKey: req.IdempotencyKey,
		Status:         "running",
		StartedAt:      time.Now().Unix(),
	}
	inputJSON, _ := common.Marshal(req.Input)
	actionLog.InputJSON = string(inputJSON)
	_ = actionLog.Insert()

	// 5. Lookup Adapter
	providerAdapter := basegate.LookupAdapterByName(bgSess.AdapterName)
	if providerAdapter == nil {
		_ = markActionFailed(actionLog, "internal_error", "Adapter missing")
		return nil, fmt.Errorf("%w: missing adapter %s", ErrSessionAdapter, bgSess.AdapterName)
	}
	sessionAdapter, ok := providerAdapter.(basegate.SessionCapableAdapter)
	if !ok {
		_ = markActionFailed(actionLog, "internal_error", "Not session capable")
		return nil, fmt.Errorf("%w: not capable", ErrSessionAdapter)
	}

	// 6. Execute Provider Action
	timeoutMs := 0
	if req.ExecutionOptions != nil && req.ExecutionOptions.TimeoutMs > 0 {
		timeoutMs = req.ExecutionOptions.TimeoutMs
	}

	providerReq := &basegate.SessionActionRequest{
		Action:    req.Action,
		Input:     req.Input,
		TimeoutMs: timeoutMs,
	}

	result, invokeErr := sessionAdapter.ExecuteAction(bgSess.ProviderSessionID, providerReq)

	// 7. Process Result
	actionLog.CompletedAt = time.Now().Unix()
	
	if invokeErr != nil {
		_ = markActionFailed(actionLog, "invoke_error", invokeErr.Error())
		return buildSessionActionResponseFromDB(actionLog)
	}

	actionLog.Status = result.Status
	
	if result.Output != nil {
		outBytes, _ := common.Marshal(result.Output)
		actionLog.OutputJSON = string(outBytes)
	}
	
	if result.Usage != nil {
		usgBytes, _ := common.Marshal(result.Usage)
		actionLog.UsageJSON = string(usgBytes)
	}
	
	if result.Error != nil {
		errBytes, _ := common.Marshal(result.Error)
		actionLog.ErrorJSON = string(errBytes)
	}

	model.DB.Save(actionLog)
	return buildSessionActionResponseFromDB(actionLog)
}

// CloseSession cleanly terminates a session, closing the upstream connection and triggering billing.
func CloseSession(sessionID string) (*dto.BGSessionResponse, error) {
	bgSess, err := model.GetBgSessionBySessionID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	if bgSess.Status.IsTerminal() {
		return buildSessionResponseFromDB(bgSess)
	}

	// Protect against multiple closures
	if bgSess.Status == model.BgSessionStatusExpired {
		// Wait, if it's already expired, getting here means we still want to finalize it.
		// Actually expired is terminal. If it was idle and we're expiring it, we use Expired.
	}

	// Call provider if we have an active session
	providerAdapter := basegate.LookupAdapterByName(bgSess.AdapterName)
	if sessionAdapter, ok := providerAdapter.(basegate.SessionCapableAdapter); ok {
		// Best effort termination
		sessionAdapter.CloseSession(bgSess.ProviderSessionID)
	}

	// Apply closed status
	success, err := bgSess.CASUpdateStatus(bgSess.Status, bgSess.StatusVersion, model.BgSessionStatusClosed)
	if err != nil || !success {
		// Possibly concurrent close? Force reload and return.
		return GetSession(sessionID)
	}

	// Trigger Phase 3 Billing!
	if err := FinalizeSessionBilling(bgSess); err != nil {
		common.SysLog(fmt.Sprintf("session_manager: FinalizeSessionBilling failed for %s: %v", sessionID, err))
	}
	
	// Phase 4 Webhook Outbox
	if bgSess.WebhookURL != "" {
		payload := map[string]interface{}{
			"id":     bgSess.SessionID,
			"object": "session",
			"status": bgSess.Status,
		}
		_ = EnqueueWebhookEvent(bgSess.SessionID, bgSess.OrgID, "session.closed", payload)
	}

	return buildSessionResponseFromDB(bgSess)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func markActionFailed(actionLog *model.BgSessionAction, code, msg string) error {
	actionLog.Status = "failed"
	actionLog.CompletedAt = time.Now().Unix()
	errObj := map[string]interface{}{"code": code, "message": msg}
	errJSON, _ := common.Marshal(errObj)
	actionLog.ErrorJSON = string(errJSON)
	return model.DB.Save(actionLog).Error
}

func buildSessionResponseFromDB(bgSess *model.BgSession) (*dto.BGSessionResponse, error) {
	// To ensure we get latest status version
	if latest, err := model.GetBgSessionBySessionID(bgSess.SessionID); err == nil {
		bgSess = latest
	}

	resp := &dto.BGSessionResponse{
		ID:        bgSess.SessionID,
		Object:    "session",
		Status:    string(bgSess.Status),
		Model:     bgSess.Model,
		CreatedAt: bgSess.CreatedAt,
		ExpiresAt: bgSess.ExpiresAt,
	}

	if bgSess.UsageJSON != "" {
		var u dto.BGUsage
		_ = common.UnmarshalJsonStr(bgSess.UsageJSON, &u)
		resp.Usage = &u
	}

	// Config could be returned in future.


	return resp, nil
}

func buildSessionActionResponseFromDB(actionLog *model.BgSessionAction) (*dto.BGSessionActionResponse, error) {
	resp := &dto.BGSessionActionResponse{
		ID:        actionLog.ActionID,
		Object:    "session_action",
		SessionID: actionLog.SessionID,
		Status:    actionLog.Status,
	}

	if actionLog.OutputJSON != "" {
		var out interface{}
		_ = common.UnmarshalJsonStr(actionLog.OutputJSON, &out)
		resp.Output = out
	}
	
	if actionLog.UsageJSON != "" {
		var u dto.BGUsage
		_ = common.UnmarshalJsonStr(actionLog.UsageJSON, &u)
		resp.Usage = &u
	}

	if actionLog.ErrorJSON != "" {
		var errObj dto.BGError
		_ = common.UnmarshalJsonStr(actionLog.ErrorJSON, &errObj)
		resp.Error = &errObj
	}

	return resp, nil
}
