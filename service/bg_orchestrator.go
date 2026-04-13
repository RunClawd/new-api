package service

import (
	"fmt"
	"reflect"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ErrIdempotencyConflict is returned when an idempotency key is reused with a different payload.
var ErrIdempotencyConflict = fmt.Errorf("idempotency_conflict")

// checkIdempotency looks up an existing response by idempotency key.
// Returns (existing, nil) if found and payload matches.
// Returns (nil, ErrIdempotencyConflict) if found but payload differs.
// Returns (nil, nil) if not found (proceed normally).
func checkIdempotency(orgID int, idempotencyKey string, currentInput interface{}) (*model.BgResponse, error) {
	if idempotencyKey == "" {
		return nil, nil
	}
	existing, err := model.GetBgResponseByIdempotencyKey(orgID, idempotencyKey)
	if err != nil || existing == nil {
		return nil, nil // not found — proceed
	}

	// Canonical deep-compare: marshal→unmarshal the current input, then DeepEqual.
	// This avoids map key ordering non-determinism in byte-level comparison.
	var existingInput interface{}
	if existing.InputJSON != "" {
		if err := common.UnmarshalJsonStr(existing.InputJSON, &existingInput); err != nil {
			// Can't compare — treat as matching (conservative: don't reject)
			return existing, nil
		}
	}

	var currentNorm interface{}
	currentBytes, _ := common.Marshal(currentInput)
	_ = common.UnmarshalJsonStr(string(currentBytes), &currentNorm)

	if !reflect.DeepEqual(existingInput, currentNorm) {
		return nil, ErrIdempotencyConflict
	}

	return existing, nil
}

// DispatchSync handles synchronous capability requests (LLM chat, etc.).
// Flow: idempotency check → lookup adapter → create response (with pricing snapshot) → invoke → finalize.
func DispatchSync(req *relaycommon.CanonicalRequest) (*dto.BaseGateResponse, error) {
	// 1. Idempotency check
	if existing, err := checkIdempotency(req.OrgID, req.IdempotencyKey, req.Input); err != nil {
		return nil, err // ErrIdempotencyConflict
	} else if existing != nil {
		return buildResponseFromDB(existing)
	}

	// 2. Lookup adapters via routing policy engine
	adapters, routeErr := ResolveRoute(req.OrgID, req.ProjectID, req.ApiKeyID, req.Model)
	if routeErr != nil {
		return nil, fmt.Errorf("route resolution failed for %s: %w", req.Model, routeErr)
	}
	if len(adapters) == 0 {
		return nil, fmt.Errorf("no adapters found for model: %s", req.Model)
	}

	billingSource := adapters[0].BillingSource
	if billingSource == "" {
		billingSource = "hosted"
	}
	// 3. Freeze pricing snapshot at invocation time based on primary adapter
	pricingSnapshot := LookupPricing(req.Model, billingSource)
	snapshotJSON, _ := common.Marshal(pricingSnapshot)

	// 3a. Pre-authorization: estimate cost and reserve quota (Sync = quota-only, no estimated billing record)
	estimatedQuota := EstimateCost(pricingSnapshot, req.Input, adapters[0].FeeConfig)
	if err := ReserveQuota(req.OrgID, estimatedQuota); err != nil {
		return nil, err // ErrInsufficientQuota
	}

	// 4. Create response record
	now := time.Now().Unix()
	bgResp := &model.BgResponse{
		ResponseID:          req.ResponseID,
		RequestID:           req.RequestID,
		OrgID:               req.OrgID,
		ProjectID:           req.ProjectID,
		ApiKeyID:            req.ApiKeyID,
		EndUserID:           req.EndUserID,
		Model:               req.Model,
		Status:              model.BgResponseStatusQueued,
		StatusVersion:       1,
		IdempotencyKey:      req.IdempotencyKey,
		BillingSource:       billingSource,
		BYOCredentialID:     adapters[0].BYOCredentialID,
		FeeConfigJSON:       serializeFeeConfigJSON(adapters[0].FeeConfig),
		BillingMode:         billingSource, // legacy
		PricingSnapshotJSON: string(snapshotJSON),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	inputJSON, _ := common.Marshal(req.Input)
	bgResp.InputJSON = string(inputJSON)

	if err := bgResp.Insert(); err != nil {
		// Refund reservation on insert failure
		SettleReservation(req.OrgID, estimatedQuota, 0)
		return nil, fmt.Errorf("failed to create response record: %w", err)
	}

	// Store estimated quota on response for settlement at terminal state
	bgResp.EstimatedQuota = estimatedQuota

	_ = model.RecordBgAuditLog(req.OrgID, req.RequestID, req.ResponseID, "response_created", map[string]interface{}{
		"model":           req.Model,
		"mode":            req.ExecutionOptions.Mode,
		"estimated_quota": estimatedQuota,
	})

	// 5. Fallback loop for Sync Invocation
	for i, adapter := range adapters {
		// Circuit breaker check — skip adapters whose circuit is OPEN
		if !basegate.CanAttempt(adapter.Adapter.Name()) {
			common.SysLog(fmt.Sprintf("fallback: %s circuit open, skipping", adapter.Adapter.Name()))
			if i < len(adapters)-1 {
				continue
			}
			return nil, fmt.Errorf("all adapters unavailable (circuit open)")
		}

		// Inject Credentials Override on a shallow copy of req
		attemptReq := *req
		attemptReq.CredentialOverride = adapter.CredentialOverride

		validation := adapter.Adapter.Validate(&attemptReq)
		if validation != nil && !validation.Valid {
			common.SysLog(fmt.Sprintf("fallback: %s invalidated pre-execution", adapter.Adapter.Name()))
			if i < len(adapters)-1 {
				continue
			}
			return nil, fmt.Errorf("all adapters failed validation")
		}

		attemptBillingSource := adapter.BillingSource
		if attemptBillingSource == "" {
			attemptBillingSource = "hosted"
		}
		attemptSnapshot := LookupPricing(req.Model, attemptBillingSource)
		attemptSnapshotJSON, _ := common.Marshal(attemptSnapshot)

		attemptID := relaycommon.GenerateAttemptID()
		attempt := &model.BgResponseAttempt{
			AttemptID:           attemptID,
			ResponseID:          req.ResponseID,
			AttemptNo:           i + 1,
			AdapterName:         adapter.Adapter.Name(),
			Status:              model.BgAttemptStatusDispatching,
			StatusVersion:       1,
			BillingSource:       attemptBillingSource,
			BYOCredentialID:     adapter.BYOCredentialID,
			FeeConfigJSON:       serializeFeeConfigJSON(adapter.FeeConfig),
			PricingSnapshotJSON: string(attemptSnapshotJSON),
			StartedAt:           time.Now().Unix(),
		}
		if err := attempt.Insert(); err != nil {
			return nil, fmt.Errorf("failed to create attempt record: %w", err)
		}

		bgResp.ActiveAttemptID = attempt.ID

		result, invokeErr := adapter.Adapter.Invoke(&attemptReq)

		if invokeErr != nil {
			common.SysLog(fmt.Sprintf("fallback: %s failed pre-execution (invoke err): %v", adapter.Adapter.Name(), invokeErr))
			basegate.RecordFailure(adapter.Adapter.Name())
			event := ProviderEvent{
				Status: "failed",
				Error: map[string]interface{}{
					"code":    "invoke_error",
					"message": invokeErr.Error(),
				},
			}
			_ = ApplyProviderEvent(req.ResponseID, attemptID, event)
			if i < len(adapters)-1 {
				continue // try next
			}
			break
		}

		if result.Status == "failed" && result.Error != nil && result.Error.Code == "provider_unavailable" {
			common.SysLog(fmt.Sprintf("fallback: %s returned provider_unavailable: %s", adapter.Adapter.Name(), result.Error.Message))
			basegate.RecordFailure(adapter.Adapter.Name())
			_ = ApplyProviderEvent(req.ResponseID, attemptID, adapterResultToEvent(result))
			if i < len(adapters)-1 {
				continue // try next
			}
			break
		}

		// Success or un-retryable failure
		basegate.RecordSuccess(adapter.Adapter.Name())
		if adapterResultAccepted(result) {
			bestEffortAdoptWinningAdapterMetadata(bgResp.ID, adapter)
		}
		_ = ApplyProviderEvent(req.ResponseID, attemptID, adapterResultToEvent(result))
		break
	}

	// 6. Build API response from DB
	bgResp, _ = model.GetBgResponseByResponseID(req.ResponseID)
	return buildResponseFromDB(bgResp)
}

// DispatchAsync handles async capability requests (video, audio, etc.).
// Flow: idempotency check → create response (with pricing snapshot) → invoke → return queued.
func DispatchAsync(req *relaycommon.CanonicalRequest) (*dto.BaseGateResponse, error) {
	// 1. Idempotency check
	if existing, err := checkIdempotency(req.OrgID, req.IdempotencyKey, req.Input); err != nil {
		return nil, err // ErrIdempotencyConflict
	} else if existing != nil {
		return buildResponseFromDB(existing)
	}

	// 2. Lookup adapters via routing policy engine
	adapters, routeErr := ResolveRoute(req.OrgID, req.ProjectID, req.ApiKeyID, req.Model)
	if routeErr != nil {
		return nil, fmt.Errorf("route resolution failed for %s: %w", req.Model, routeErr)
	}
	if len(adapters) == 0 {
		return nil, fmt.Errorf("no adapters found for model: %s", req.Model)
	}

	billingSource := adapters[0].BillingSource
	if billingSource == "" {
		billingSource = "hosted"
	}

	// 3. Freeze pricing snapshot at invocation time based on primary adapter
	pricingSnapshot := LookupPricing(req.Model, billingSource)
	snapshotJSON, _ := common.Marshal(pricingSnapshot)

	// 3a. Pre-authorization: estimate cost and reserve quota (Async = quota + estimated billing record)
	estimatedQuota := EstimateCost(pricingSnapshot, req.Input, adapters[0].FeeConfig)
	reservation, err := ReserveQuotaWithBillingHold(
		req.OrgID, req.ProjectID,
		req.ResponseID, req.Model,
		pricingSnapshot, estimatedQuota,
		billingSource,
	)
	if err != nil {
		return nil, err // ErrInsufficientQuota or billing record failure
	}

	// 4. Create response record
	now := time.Now().Unix()
	bgResp := &model.BgResponse{
		ResponseID:               req.ResponseID,
		RequestID:                req.RequestID,
		OrgID:                    req.OrgID,
		ProjectID:                req.ProjectID,
		ApiKeyID:                 req.ApiKeyID,
		EndUserID:                req.EndUserID,
		Model:                    req.Model,
		Status:                   model.BgResponseStatusAccepted,
		StatusVersion:            1,
		IdempotencyKey:           req.IdempotencyKey,
		BillingSource:            billingSource,
		BYOCredentialID:          adapters[0].BYOCredentialID,
		FeeConfigJSON:            serializeFeeConfigJSON(adapters[0].FeeConfig),
		BillingMode:              billingSource, // legacy
		PricingSnapshotJSON:      string(snapshotJSON),
		EstimatedQuota:           estimatedQuota,
		ReservationBillingID:     reservation.BillingID,
		ReservationLedgerEntryID: reservation.LedgerEntryID,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	inputJSON, _ := common.Marshal(req.Input)
	bgResp.InputJSON = string(inputJSON)

	if err := bgResp.Insert(); err != nil {
		// Clean up estimated billing + refund quota on insert failure
		_ = VoidEstimatedBilling(reservation.BillingID, reservation.LedgerEntryID)
		SettleReservation(req.OrgID, estimatedQuota, 0)
		return nil, fmt.Errorf("failed to create response record: %w", err)
	}

	_ = model.RecordBgAuditLog(req.OrgID, req.RequestID, req.ResponseID, "response_created", map[string]interface{}{
		"model": req.Model,
		"mode":  req.ExecutionOptions.Mode,
	})

	// 5. Fallback loop for Async Invocation
	for i, adapter := range adapters {
		// Circuit breaker check — skip adapters whose circuit is OPEN
		if !basegate.CanAttempt(adapter.Adapter.Name()) {
			common.SysLog(fmt.Sprintf("fallback: %s circuit open, skipping", adapter.Adapter.Name()))
			if i < len(adapters)-1 {
				continue
			}
			return nil, fmt.Errorf("all adapters unavailable (circuit open)")
		}

		attemptReq := *req
		attemptReq.CredentialOverride = adapter.CredentialOverride

		validation := adapter.Adapter.Validate(&attemptReq)
		if validation != nil && !validation.Valid {
			common.SysLog(fmt.Sprintf("fallback: %s invalidated pre-execution", adapter.Adapter.Name()))
			if i < len(adapters)-1 {
				continue
			}
			return nil, fmt.Errorf("all adapters failed validation")
		}

		attemptBillingSource := adapter.BillingSource
		if attemptBillingSource == "" {
			attemptBillingSource = "hosted"
		}
		attemptSnapshot := LookupPricing(req.Model, attemptBillingSource)
		attemptSnapshotJSON, _ := common.Marshal(attemptSnapshot)

		attemptID := relaycommon.GenerateAttemptID()
		attempt := &model.BgResponseAttempt{
			AttemptID:           attemptID,
			ResponseID:          req.ResponseID,
			AttemptNo:           i + 1,
			AdapterName:         adapter.Adapter.Name(),
			Status:              model.BgAttemptStatusDispatching,
			StatusVersion:       1,
			BillingSource:       attemptBillingSource,
			BYOCredentialID:     adapter.BYOCredentialID,
			FeeConfigJSON:       serializeFeeConfigJSON(adapter.FeeConfig),
			PricingSnapshotJSON: string(attemptSnapshotJSON),
			StartedAt:           time.Now().Unix(),
		}
		if err := attempt.Insert(); err != nil {
			return nil, fmt.Errorf("failed to create attempt record: %w", err)
		}

		bgResp.ActiveAttemptID = attempt.ID

		result, invokeErr := adapter.Adapter.Invoke(&attemptReq)

		if invokeErr != nil {
			common.SysLog(fmt.Sprintf("fallback: %s failed pre-execution (invoke err): %v", adapter.Adapter.Name(), invokeErr))
			basegate.RecordFailure(adapter.Adapter.Name())
			event := ProviderEvent{
				Status: "failed",
				Error: map[string]interface{}{
					"code":    "invoke_error",
					"message": invokeErr.Error(),
				},
			}
			_ = ApplyProviderEvent(req.ResponseID, attemptID, event)
			if i < len(adapters)-1 {
				continue
			}
			break
		}

		if result.Status == "failed" && result.Error != nil && result.Error.Code == "provider_unavailable" {
			common.SysLog(fmt.Sprintf("fallback: %s returned provider_unavailable: %s", adapter.Adapter.Name(), result.Error.Message))
			basegate.RecordFailure(adapter.Adapter.Name())
			_ = ApplyProviderEvent(req.ResponseID, attemptID, adapterResultToEvent(result))
			if i < len(adapters)-1 {
				continue
			}
			break
		}

		basegate.RecordSuccess(adapter.Adapter.Name())
		if adapterResultAccepted(result) {
			bestEffortAdoptWinningAdapterMetadata(bgResp.ID, adapter)
		}
		event := adapterResultToEvent(result)
		_ = ApplyProviderEvent(req.ResponseID, attemptID, event)
		break
	}

	// 6. Reload and return
	bgResp, _ = model.GetBgResponseByResponseID(req.ResponseID)
	return buildResponseFromDB(bgResp)
}

// GetResponse retrieves a response by its public ID.
// orgID enforces tenant isolation — pass 0 to skip (admin context).
func GetResponse(responseID string, orgID int) (*dto.BaseGateResponse, error) {
	bgResp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil {
		return nil, fmt.Errorf("response not found: %w", err)
	}
	if orgID > 0 && bgResp.OrgID != orgID {
		return nil, fmt.Errorf("response not found: %s", responseID)
	}
	return buildResponseFromDB(bgResp)
}

// CancelResponse requests cancellation of an in-progress response.
// orgID enforces tenant isolation — pass 0 to skip (admin context).
func CancelResponse(responseID string, orgID int) (*dto.BaseGateResponse, error) {
	bgResp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil {
		return nil, fmt.Errorf("response not found: %w", err)
	}
	if orgID > 0 && bgResp.OrgID != orgID {
		return nil, fmt.Errorf("response not found: %s", responseID)
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

	// Audit log: response_canceled
	_ = model.RecordBgAuditLog(bgResp.OrgID, bgResp.RequestID, responseID, "response_canceled", map[string]interface{}{
		"status": string(bgResp.Status),
	})

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

	// Map RawUsage directly from struct fields to avoid marshal→unmarshal round-trip.
	// The state machine reads event.RawUsage to drive FinalizeBilling.
	if u := result.RawUsage; u != nil {
		rawMap := map[string]interface{}{}
		if u.PromptTokens > 0 {
			rawMap["prompt_tokens"] = u.PromptTokens
		}
		if u.CompletionTokens > 0 {
			rawMap["completion_tokens"] = u.CompletionTokens
		}
		if u.TotalTokens > 0 {
			rawMap["total_tokens"] = u.TotalTokens
		}
		// Explicit pricing bucket fields for differentiated billing
		if u.InputTokens > 0 {
			rawMap["input_tokens"] = u.InputTokens
		}
		if u.CachedTokens > 0 {
			rawMap["cached_tokens"] = u.CachedTokens
		}
		if u.CacheCreationTokens > 0 {
			rawMap["cache_creation_tokens"] = u.CacheCreationTokens
		}
		if u.CacheCreationTokens5m > 0 {
			rawMap["cache_creation_tokens_5m"] = u.CacheCreationTokens5m
		}
		if u.CacheCreationTokens1h > 0 {
			rawMap["cache_creation_tokens_1h"] = u.CacheCreationTokens1h
		}
		if u.DurationSec > 0 {
			rawMap["duration_sec"] = u.DurationSec
		}
		if u.SessionMinutes > 0 {
			rawMap["session_minutes"] = u.SessionMinutes
		}
		if u.Actions > 0 {
			rawMap["actions"] = u.Actions
		}
		if u.BillableUnits > 0 {
			rawMap["billable_units"] = u.BillableUnits
			rawMap["billable_unit"] = u.BillableUnit
		}
		event.RawUsage = rawMap
	}

	return event
}

// buildResponseFromDB constructs an API response from a DB record.
// Step 3.2: PollURL is populated for non-terminal statuses only.
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

	// Populate poll_url for non-terminal responses (async polling)
	if !bgResp.Status.IsTerminal() {
		resp.PollURL = "/v1/bg/responses/" + bgResp.ResponseID
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
