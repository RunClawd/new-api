package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// DispatchSync handles synchronous capability requests (LLM chat, etc.).
// Flow: lookup adapter → idempotency check → create response → invoke → finalize.
func DispatchSync(req *relaycommon.CanonicalRequest) (*dto.BaseGateResponse, error) {
	// 1. Idempotency check
	if req.IdempotencyKey != "" {
		existing, err := model.GetBgResponseByIdempotencyKey(req.OrgID, req.IdempotencyKey)
		if err == nil && existing != nil {
			return buildResponseFromDB(existing)
		}
	}

	// 2. Lookup adapter
	adapter := basegate.LookupAdapter(req.Model)
	if adapter == nil {
		return nil, fmt.Errorf("no adapter found for model: %s", req.Model)
	}

	// 3. Validate
	validation := adapter.Validate(req)
	if validation != nil && !validation.Valid {
		errMsg := "validation failed"
		if validation.Error != nil {
			errMsg = validation.Error.Message
		}
		return nil, fmt.Errorf("validation error: %s", errMsg)
	}

	// 4. Create response record
	now := time.Now().Unix()
	bgResp := &model.BgResponse{
		ResponseID:     req.ResponseID,
		RequestID:      req.RequestID,
		OrgID:          req.OrgID,
		ProjectID:      req.ProjectID,
		ApiKeyID:       req.ApiKeyID,
		EndUserID:      req.EndUserID,
		Model:          req.Model,
		// Sync requests start at "queued" (not "running") because:
		// 1. queued → succeeded/failed are valid transitions (no auto-advance needed)
		// 2. The adapter hasn't been invoked yet at this point, so "running" would be premature
		// 3. This status is transient — never visible to the API client since the entire
		//    dispatch (create → invoke → finalize) happens within one HTTP request
		Status:         model.BgResponseStatusQueued,
		StatusVersion:  1,
		IdempotencyKey: req.IdempotencyKey,
		BillingMode:    req.BillingContext.BillingMode,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if bgResp.BillingMode == "" {
		bgResp.BillingMode = "hosted"
	}
	inputJSON, _ := common.Marshal(req.Input)
	bgResp.InputJSON = string(inputJSON)

	if err := bgResp.Insert(); err != nil {
		return nil, fmt.Errorf("failed to create response record: %w", err)
	}

	// 5. Create attempt
	attemptID := relaycommon.GenerateAttemptID()
	attempt := &model.BgResponseAttempt{
		AttemptID:     attemptID,
		ResponseID:    req.ResponseID,
		AttemptNo:     1,
		AdapterName:   adapter.Name(),
		Status:        model.BgAttemptStatusDispatching,
		StatusVersion: 1,
		StartedAt:     now,
	}
	if err := attempt.Insert(); err != nil {
		return nil, fmt.Errorf("failed to create attempt record: %w", err)
	}

	bgResp.ActiveAttemptID = attempt.ID

	// 6. Invoke
	result, invokeErr := adapter.Invoke(req)

	// 7. Map result to provider event and apply state machine
	var event ProviderEvent
	if invokeErr != nil {
		event = ProviderEvent{
			Status: "failed",
			Error: map[string]interface{}{
				"code":    "invoke_error",
				"message": invokeErr.Error(),
			},
		}
	} else {
		event = adapterResultToEvent(result)
	}

	// Apply the state machine
	if err := ApplyProviderEvent(req.ResponseID, attemptID, event); err != nil {
		common.SysLog(fmt.Sprintf("orchestrator: failed to apply event for %s: %v", req.ResponseID, err))
	}

	// 8. Build API response from DB (source of truth)
	return buildResponseFromDB(bgResp)
}

// DispatchAsync handles async capability requests (video, audio, etc.).
// Flow: create response → invoke → persist attempt with poll schedule → return queued.
func DispatchAsync(req *relaycommon.CanonicalRequest) (*dto.BaseGateResponse, error) {
	// 1. Idempotency check
	if req.IdempotencyKey != "" {
		existing, err := model.GetBgResponseByIdempotencyKey(req.OrgID, req.IdempotencyKey)
		if err == nil && existing != nil {
			return buildResponseFromDB(existing)
		}
	}

	// 2. Lookup adapter
	adapter := basegate.LookupAdapter(req.Model)
	if adapter == nil {
		return nil, fmt.Errorf("no adapter found for model: %s", req.Model)
	}

	// 3. Create response record
	now := time.Now().Unix()
	bgResp := &model.BgResponse{
		ResponseID:     req.ResponseID,
		RequestID:      req.RequestID,
		OrgID:          req.OrgID,
		ProjectID:      req.ProjectID,
		ApiKeyID:       req.ApiKeyID,
		EndUserID:      req.EndUserID,
		Model:          req.Model,
		Status:         model.BgResponseStatusAccepted,
		StatusVersion:  1,
		IdempotencyKey: req.IdempotencyKey,
		BillingMode:    req.BillingContext.BillingMode,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if bgResp.BillingMode == "" {
		bgResp.BillingMode = "hosted"
	}
	inputJSON, _ := common.Marshal(req.Input)
	bgResp.InputJSON = string(inputJSON)

	if err := bgResp.Insert(); err != nil {
		return nil, fmt.Errorf("failed to create response record: %w", err)
	}

	// 4. Create attempt
	attemptID := relaycommon.GenerateAttemptID()
	attempt := &model.BgResponseAttempt{
		AttemptID:     attemptID,
		ResponseID:    req.ResponseID,
		AttemptNo:     1,
		AdapterName:   adapter.Name(),
		Status:        model.BgAttemptStatusDispatching,
		StatusVersion: 1,
		StartedAt:     now,
	}
	if err := attempt.Insert(); err != nil {
		return nil, fmt.Errorf("failed to create attempt record: %w", err)
	}

	bgResp.ActiveAttemptID = attempt.ID

	// 5. Invoke (async: returns accepted/queued with provider_request_id)
	result, invokeErr := adapter.Invoke(req)

	if invokeErr != nil {
		// Mark as failed immediately
		event := ProviderEvent{
			Status: "failed",
			Error: map[string]interface{}{
				"code":    "invoke_error",
				"message": invokeErr.Error(),
			},
		}
		_ = ApplyProviderEvent(req.ResponseID, attemptID, event)
		return buildResponseFromDB(bgResp)
	}

	// 6. Apply initial state (accepted/queued)
	event := adapterResultToEvent(result)
	_ = ApplyProviderEvent(req.ResponseID, attemptID, event)

	// 7. Reload and return
	bgResp, _ = model.GetBgResponseByResponseID(req.ResponseID)
	return buildResponseFromDB(bgResp)
}

// GetResponse retrieves a response by its public ID.
func GetResponse(responseID string) (*dto.BaseGateResponse, error) {
	bgResp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil {
		return nil, fmt.Errorf("response not found: %w", err)
	}
	return buildResponseFromDB(bgResp)
}

// CancelResponse requests cancellation of an in-progress response.
func CancelResponse(responseID string) (*dto.BaseGateResponse, error) {
	bgResp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil {
		return nil, fmt.Errorf("response not found: %w", err)
	}

	if bgResp.Status.IsTerminal() {
		return buildResponseFromDB(bgResp)
	}

	// Find active attempt
	attempts, err := model.GetBgAttemptsByResponseID(responseID)
	if err != nil || len(attempts) == 0 {
		return nil, fmt.Errorf("no attempts found for response %s", responseID)
	}

	activeAttempt := &attempts[len(attempts)-1]

	// Apply cancel event
	event := ProviderEvent{Status: "canceled"}
	if err := ApplyProviderEvent(responseID, activeAttempt.AttemptID, event); err != nil {
		return nil, fmt.Errorf("failed to cancel: %w", err)
	}

	bgResp, _ = model.GetBgResponseByResponseID(responseID)
	return buildResponseFromDB(bgResp)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// adapterResultToEvent converts an AdapterResult to a ProviderEvent.
func adapterResultToEvent(result *relaycommon.AdapterResult) ProviderEvent {
	event := ProviderEvent{
		Status:            result.Status,
		ProviderRequestID: result.ProviderRequestID,
		PollAfterMs:       result.PollAfterMs,
	}

	if len(result.Output) > 0 {
		output := make([]interface{}, len(result.Output))
		for i, o := range result.Output {
			output[i] = map[string]interface{}{
				"type":    o.Type,
				"content": o.Content,
			}
		}
		event.Output = output
	}

	if result.Error != nil {
		event.Error = map[string]interface{}{
			"code":    result.Error.Code,
			"message": result.Error.Message,
		}
	}

	return event
}

// buildResponseFromDB constructs an API response from a DB record.
func buildResponseFromDB(bgResp *model.BgResponse) (*dto.BaseGateResponse, error) {
	// Reload from DB for latest state
	latest, err := model.GetBgResponseByResponseID(bgResp.ResponseID)
	if err == nil {
		bgResp = latest
	}

	resp := &dto.BaseGateResponse{
		ID:        bgResp.ResponseID,
		Object:    "response",
		CreatedAt: bgResp.CreatedAt,
		Status:    string(bgResp.Status),
		Model:     bgResp.Model,
	}

	// Parse output
	if bgResp.OutputJSON != "" {
		var output []dto.BGOutputItem
		if err := common.UnmarshalJsonStr(bgResp.OutputJSON, &output); err == nil {
			resp.Output = output
		}
	}

	// Parse error
	if bgResp.ErrorJSON != "" {
		var bgErr dto.BGError
		if err := common.UnmarshalJsonStr(bgResp.ErrorJSON, &bgErr); err == nil {
			resp.Error = &bgErr
		}
	}

	resp.Meta = &dto.BGMeta{
		RequestID: bgResp.RequestID,
	}

	return resp, nil
}
