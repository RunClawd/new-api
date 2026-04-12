package service

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// DispatchStream handles streaming requests.
// Flow: create response -> create attempt -> adapter.Stream -> SSE loop -> ApplyProviderEvent (terminal).
func DispatchStream(req *relaycommon.CanonicalRequest, c *gin.Context) error {
	// 1. Adapter Lookup via routing policy engine
	adapters, routeErr := ResolveRoute(req.OrgID, req.ProjectID, req.ApiKeyID, req.Model)
	if routeErr != nil {
		return fmt.Errorf("route resolution failed for %s: %w", req.Model, routeErr)
	}
	if len(adapters) == 0 {
		return fmt.Errorf("no adapter found for model: %s", req.Model)
	}

	billingSource := adapters[0].BillingSource
	if billingSource == "" {
		billingSource = "hosted"
	}

	// 2. Create response record
	now := time.Now().Unix()
	bgResp := &model.BgResponse{
		ResponseID:      req.ResponseID,
		RequestID:       req.RequestID,
		OrgID:           req.OrgID,
		ProjectID:       req.ProjectID,
		ApiKeyID:        req.ApiKeyID,
		EndUserID:       req.EndUserID,
		Model:           req.Model,
		Status:          model.BgResponseStatusStreaming, // Starts off actively streaming
		StatusVersion:   1,
		IdempotencyKey:  req.IdempotencyKey,
		BillingSource:   billingSource,
		BYOCredentialID: adapters[0].BYOCredentialID,
		FeeConfigJSON:   serializeFeeConfigJSON(adapters[0].FeeConfig),
		BillingMode:     billingSource, // legacy
		WebhookURL:      req.ExecutionOptions.WebhookURL,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	inputJSON, _ := common.Marshal(req.Input)
	bgResp.InputJSON = string(inputJSON)

	// Freeze pricing snapshot at invocation time (same as Sync/Async dispatch paths)
	pricingSnapshot := LookupPricing(req.Model, billingSource)
	snapshotJSON, _ := common.Marshal(pricingSnapshot)
	bgResp.PricingSnapshotJSON = string(snapshotJSON)

	// Pre-auth: Sync-equivalent quota reservation (Stream = Sync lifecycle)
	estimatedQuota := EstimateCost(pricingSnapshot, req.Input, adapters[0].FeeConfig)
	if err := ReserveQuota(req.OrgID, estimatedQuota); err != nil {
		return err // 402 insufficient quota
	}
	bgResp.EstimatedQuota = estimatedQuota

	if err := bgResp.Insert(); err != nil {
		return fmt.Errorf("failed to create response record: %w", err)
	}

	_ = model.RecordBgAuditLog(req.OrgID, req.RequestID, req.ResponseID, "response_created", map[string]interface{}{
		"model": req.Model,
		"mode":  req.ExecutionOptions.Mode,
	})

	var stream <-chan relaycommon.SSEEvent
	var activeAttemptID string
	var finalErr error

	// 4. Fallback Loop
	for i, adapter := range adapters {
		// Circuit breaker check — skip adapters whose circuit is OPEN
		if !basegate.CanAttempt(adapter.Adapter.Name()) {
			common.SysLog(fmt.Sprintf("fallback: %s circuit open, skipping", adapter.Adapter.Name()))
			if i < len(adapters)-1 {
				continue
			}
			return fmt.Errorf("all adapters unavailable (circuit open)")
		}

		attemptReq := *req
		attemptReq.CredentialOverride = adapter.CredentialOverride

		validation := adapter.Adapter.Validate(&attemptReq)
		if validation != nil && !validation.Valid {
			common.SysLog(fmt.Sprintf("fallback: %s invalidated pre-execution", adapter.Adapter.Name()))
			if i < len(adapters)-1 {
				continue
			}
			return fmt.Errorf("all adapters failed validation")
		}

		attemptBillingSource := adapter.BillingSource
		if attemptBillingSource == "" {
			attemptBillingSource = "hosted"
		}
		attemptSnapshot := LookupPricing(req.Model, attemptBillingSource)
		attemptSnapshotJSON, _ := common.Marshal(attemptSnapshot)

		// 3. Create attempt
		attemptID := relaycommon.GenerateAttemptID()
		attempt := &model.BgResponseAttempt{
			AttemptID:           attemptID,
			ResponseID:          req.ResponseID,
			AttemptNo:           i + 1,
			AdapterName:         adapter.Adapter.Name(),
			Status:              model.BgAttemptStatusRunning,
			StatusVersion:       1,
			BillingSource:       attemptBillingSource,
			BYOCredentialID:     adapter.BYOCredentialID,
			FeeConfigJSON:       serializeFeeConfigJSON(adapter.FeeConfig),
			PricingSnapshotJSON: string(attemptSnapshotJSON),
			StartedAt:           time.Now().Unix(),
		}
		if err := attempt.Insert(); err != nil {
			return fmt.Errorf("failed to create attempt record: %w", err)
		}

		bgResp.ActiveAttemptID = attempt.ID
		activeAttemptID = attemptID

		// 4. Start streaming via adapter
		s, streamErr := adapter.Adapter.Stream(&attemptReq)

		if streamErr != nil {
			common.SysLog(fmt.Sprintf("fallback: %s failed pre-execution (stream err): %v", adapter.Adapter.Name(), streamErr))
			basegate.RecordFailure(adapter.Adapter.Name())
			// Immediately fail attempt
			event := ProviderEvent{
				Status: "failed",
				Error: map[string]interface{}{
					"code":    "invoke_error",
					"message": streamErr.Error(),
				},
			}
			_ = ApplyProviderEvent(req.ResponseID, attemptID, event)
			finalErr = streamErr
			if i < len(adapters)-1 {
				continue // fallback
			}
			break
		}

		// Stream established successfully
		basegate.RecordSuccess(adapter.Adapter.Name())
		bestEffortAdoptWinningAdapterMetadata(bgResp.ID, adapter)
		stream = s
		finalErr = nil
		break
	}

	if finalErr != nil {
		return finalErr
	}
	if stream == nil {
		return fmt.Errorf("stream failed: all adapters failed")
	}

	// 5. Setup SSE Headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		// Degradation handling - flushers are standard in gin, but in case tests mock it wrongly
		flusher = &mockFlusher{}
	}

	// 6. Pump SSE loop
	var actionCount int
	var terminalError *relaycommon.AdapterError
	var rawUsage interface{}

	for event := range stream {
		// Accumulate basic metrics
		if event.Type == relaycommon.SSEEventOutputItemDone {
			actionCount++
		}
		if event.Type == relaycommon.SSEEventError {
			// Extract terminal error
			errMap, _ := event.Data.(map[string]interface{})
			code, _ := errMap["code"].(string)
			msg, _ := errMap["message"].(string)
			terminalError = &relaycommon.AdapterError{
				Code:    code,
				Message: msg,
			}
		}
		if event.Type == relaycommon.SSEEventResponseSucceeded {
			if data, ok := event.Data.(map[string]interface{}); ok {
				if usage, uOK := data["raw_usage"]; uOK && usage != nil {
					rawUsage = usage
				}
			}
		}

		// Serialize and write
		eventJSON, marshalErr := common.Marshal(event)
		if marshalErr != nil {
			common.SysError(fmt.Sprintf("streaming marshal error: %v", marshalErr))
			continue
		}

		// Schema §3: SSE format is "event: <type>\ndata: <json>\n\n"
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Type, string(eventJSON))
		flusher.Flush()
	}

	// Terminate Stream signal
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	flusher.Flush()

	// 7. Calculate terminal state & Apply DB Event
	finalStatus := "succeeded"
	if terminalError != nil {
		finalStatus = "failed"
	}

	var outputInterfaces []interface{}
	// In the future if we accumulate output, we map streamOutput -> outputInterfaces here.

	terminalEvent := ProviderEvent{
		Status: finalStatus,
		Output: outputInterfaces,
	}

	if terminalError != nil {
		terminalEvent.Error = map[string]interface{}{
			"code":    terminalError.Code,
			"message": terminalError.Message,
		}
	} else {
		// Prefer provider-reported usage (actual token counts from adapter)
		if usageMap, ok := rawUsage.(map[string]interface{}); ok && usageMap != nil {
			terminalEvent.RawUsage = usageMap
		} else if rawUsage != nil {
			// raw_usage is a struct (e.g. *ProviderUsage) — round-trip through JSON
			if b, err := common.Marshal(rawUsage); err == nil {
				var m map[string]interface{}
				if err := common.Unmarshal(b, &m); err == nil {
					terminalEvent.RawUsage = m
				}
			}
		} else {
			// Synthetic fallback
			terminalEvent.RawUsage = map[string]interface{}{
				"actions":      actionCount,
				"duration_sec": float64(time.Now().Unix() - now),
			}
		}
	}

	// 8. Terminal update via state machine
	if err := ApplyProviderEvent(req.ResponseID, activeAttemptID, terminalEvent); err != nil {
		common.SysLog(fmt.Sprintf("streaming: failed to apply terminal event for %s: %v", req.ResponseID, err))
	}

	return nil
}

type mockFlusher struct{}

func (m *mockFlusher) Flush() {}
