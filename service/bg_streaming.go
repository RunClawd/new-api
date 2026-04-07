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
	// 1. Adapter Lookup
	adapters := basegate.LookupAdapters(req.Model)
	if len(adapters) == 0 {
		return fmt.Errorf("no adapter found for model: %s", req.Model)
	}

	// 2. Create response record
	now := time.Now().Unix()
	bgResp := &model.BgResponse{
		ResponseID:     req.ResponseID,
		RequestID:      req.RequestID,
		OrgID:          req.OrgID,
		ProjectID:      req.ProjectID,
		ApiKeyID:       req.ApiKeyID,
		EndUserID:      req.EndUserID,
		Model:          req.Model,
		Status:         model.BgResponseStatusStreaming, // Starts off actively streaming
		StatusVersion:  1,
		IdempotencyKey: req.IdempotencyKey,
		BillingMode:    req.BillingContext.BillingMode,
		WebhookURL:     req.ExecutionOptions.WebhookURL,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if bgResp.BillingMode == "" {
		bgResp.BillingMode = "hosted"
	}
	inputJSON, _ := common.Marshal(req.Input)
	bgResp.InputJSON = string(inputJSON)

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
		validation := adapter.Validate(req)
		if validation != nil && !validation.Valid {
			common.SysLog(fmt.Sprintf("fallback: %s invalidated pre-execution", adapter.Name()))
			if i < len(adapters)-1 {
				continue
			}
			return fmt.Errorf("all adapters failed validation")
		}

		// 3. Create attempt
		attemptID := relaycommon.GenerateAttemptID()
		attempt := &model.BgResponseAttempt{
			AttemptID:     attemptID,
			ResponseID:    req.ResponseID,
			AttemptNo:     i + 1,
			AdapterName:   adapter.Name(),
			Status:        model.BgAttemptStatusRunning,
			StatusVersion: 1,
			StartedAt:     time.Now().Unix(),
		}
		if err := attempt.Insert(); err != nil {
			return fmt.Errorf("failed to create attempt record: %w", err)
		}

		bgResp.ActiveAttemptID = attempt.ID
		activeAttemptID = attemptID

		// 4. Start streaming via adapter
		s, streamErr := adapter.Stream(req)

		if streamErr != nil {
			common.SysLog(fmt.Sprintf("fallback: %s failed pre-execution (stream err): %v", adapter.Name(), streamErr))
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

		// Success
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
		// Push usage for streaming
		terminalEvent.RawUsage = map[string]interface{}{
			"actions": actionCount,
			"duration_sec": float64(time.Now().Unix() - now),
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
