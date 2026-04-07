package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DeriveResponseStatus — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestDeriveResponseStatus_AttemptSucceeded(t *testing.T) {
	status := DeriveResponseStatus(model.BgAttemptStatusSucceeded)
	assert.Equal(t, model.BgResponseStatusSucceeded, status)
}

func TestDeriveResponseStatus_AttemptFailed(t *testing.T) {
	status := DeriveResponseStatus(model.BgAttemptStatusFailed)
	assert.Equal(t, model.BgResponseStatusFailed, status)
}

func TestDeriveResponseStatus_AttemptCanceled(t *testing.T) {
	status := DeriveResponseStatus(model.BgAttemptStatusCanceled)
	assert.Equal(t, model.BgResponseStatusCanceled, status)
}

func TestDeriveResponseStatus_AttemptRunning(t *testing.T) {
	status := DeriveResponseStatus(model.BgAttemptStatusRunning)
	assert.Equal(t, model.BgResponseStatusRunning, status)
}

func TestDeriveResponseStatus_AttemptDispatching(t *testing.T) {
	status := DeriveResponseStatus(model.BgAttemptStatusDispatching)
	assert.Equal(t, model.BgResponseStatusQueued, status)
}

func TestDeriveResponseStatus_AttemptAccepted(t *testing.T) {
	status := DeriveResponseStatus(model.BgAttemptStatusAccepted)
	assert.Equal(t, model.BgResponseStatusQueued, status)
}

// ---------------------------------------------------------------------------
// ValidateTransition — pure logic tests
// ---------------------------------------------------------------------------

func TestValidateTransition_ValidPath(t *testing.T) {
	tests := []struct {
		from model.BgResponseStatus
		to   model.BgResponseStatus
	}{
		{model.BgResponseStatusAccepted, model.BgResponseStatusQueued},
		{model.BgResponseStatusQueued, model.BgResponseStatusRunning},
		{model.BgResponseStatusRunning, model.BgResponseStatusSucceeded},
		{model.BgResponseStatusQueued, model.BgResponseStatusStreaming},
		{model.BgResponseStatusStreaming, model.BgResponseStatusSucceeded},
	}
	for _, tt := range tests {
		err := ValidateTransition(tt.from, tt.to)
		assert.NoError(t, err, "transition %s → %s should be valid", tt.from, tt.to)
	}
}

func TestValidateTransition_TerminalToAnything(t *testing.T) {
	err := ValidateTransition(model.BgResponseStatusSucceeded, model.BgResponseStatusFailed)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "terminal")
}

func TestValidateTransition_InvalidPath(t *testing.T) {
	err := ValidateTransition(model.BgResponseStatusAccepted, model.BgResponseStatusSucceeded)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

// ---------------------------------------------------------------------------
// ProviderEvent mapping — pure logic tests
// ---------------------------------------------------------------------------

func TestMapProviderEvent_Queued(t *testing.T) {
	event := ProviderEvent{
		Status:            "queued",
		ProviderRequestID: "prov_123",
	}
	attemptStatus, _ := MapProviderEventToAttemptStatus(event)
	assert.Equal(t, model.BgAttemptStatusAccepted, attemptStatus)
}

func TestMapProviderEvent_Running(t *testing.T) {
	event := ProviderEvent{Status: "running"}
	attemptStatus, _ := MapProviderEventToAttemptStatus(event)
	assert.Equal(t, model.BgAttemptStatusRunning, attemptStatus)
}

func TestMapProviderEvent_Succeeded(t *testing.T) {
	event := ProviderEvent{Status: "succeeded"}
	attemptStatus, isTerminal := MapProviderEventToAttemptStatus(event)
	assert.Equal(t, model.BgAttemptStatusSucceeded, attemptStatus)
	assert.True(t, isTerminal)
}

func TestMapProviderEvent_Failed(t *testing.T) {
	event := ProviderEvent{Status: "failed"}
	attemptStatus, isTerminal := MapProviderEventToAttemptStatus(event)
	assert.Equal(t, model.BgAttemptStatusFailed, attemptStatus)
	assert.True(t, isTerminal)
}

func TestMapProviderEvent_Unknown(t *testing.T) {
	event := ProviderEvent{Status: "some_random_status"}
	attemptStatus, _ := MapProviderEventToAttemptStatus(event)
	assert.Equal(t, model.BgAttemptStatusUnknown, attemptStatus)
}

// ---------------------------------------------------------------------------
// Integration tests (require DB) — ApplyProviderEvent
// ---------------------------------------------------------------------------

func TestApplyProviderEvent_SyncSuccess(t *testing.T) {
	truncateBgTables(t)

	// Create a response in accepted state
	resp := &model.BgResponse{
		ResponseID:    "resp_sm_sync",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusQueued,
		StatusVersion: 1,
		OrgID:         1,
	}
	require.NoError(t, resp.Insert())

	// Create an attempt
	attempt := &model.BgResponseAttempt{
		AttemptID:     "att_sm_sync",
		ResponseID:    "resp_sm_sync",
		AttemptNo:     1,
		Status:        model.BgAttemptStatusRunning,
		StatusVersion: 1,
		AdapterName:   "openai_gpt4",
	}
	require.NoError(t, attempt.Insert())

	// Apply a succeeded event
	event := ProviderEvent{
		Status: "succeeded",
		Output: []interface{}{"Hello! How can I help?"},
		RawUsage: map[string]interface{}{
			"prompt_tokens":     50,
			"completion_tokens": 100,
		},
	}
	err := ApplyProviderEvent(resp.ResponseID, attempt.AttemptID, event)
	require.NoError(t, err)

	// Verify response is succeeded
	found, err := model.GetBgResponseByResponseID("resp_sm_sync")
	require.NoError(t, err)
	assert.Equal(t, model.BgResponseStatusSucceeded, found.Status)
	assert.True(t, found.FinalizedAt > 0)
}

func TestApplyProviderEvent_TerminalNoOp(t *testing.T) {
	truncateBgTables(t)

	// Create a response already in terminal state
	resp := &model.BgResponse{
		ResponseID:    "resp_sm_terminal",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 2,
		OrgID:         1,
		FinalizedAt:   time.Now().Unix(),
	}
	require.NoError(t, resp.Insert())

	attempt := &model.BgResponseAttempt{
		AttemptID:     "att_sm_terminal",
		ResponseID:    "resp_sm_terminal",
		AttemptNo:     1,
		Status:        model.BgAttemptStatusSucceeded,
		StatusVersion: 2,
	}
	require.NoError(t, attempt.Insert())

	// Try to apply another event — should be no-op
	event := ProviderEvent{Status: "failed"}
	err := ApplyProviderEvent(resp.ResponseID, attempt.AttemptID, event)
	assert.NoError(t, err) // no-op, not an error

	// Verify state unchanged
	found, err := model.GetBgResponseByResponseID("resp_sm_terminal")
	require.NoError(t, err)
	assert.Equal(t, model.BgResponseStatusSucceeded, found.Status)
}

func TestApplyProviderEvent_AsyncPolling(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_sm_async",
		Model:         "bg.video.generate.kling",
		Status:        model.BgResponseStatusQueued,
		StatusVersion: 1,
		OrgID:         1,
	}
	require.NoError(t, resp.Insert())

	attempt := &model.BgResponseAttempt{
		AttemptID:     "att_sm_async",
		ResponseID:    "resp_sm_async",
		AttemptNo:     1,
		Status:        model.BgAttemptStatusDispatching,
		StatusVersion: 1,
		AdapterName:   "kling_video",
	}
	require.NoError(t, attempt.Insert())

	// Event 1: accepted by provider
	err := ApplyProviderEvent(resp.ResponseID, attempt.AttemptID, ProviderEvent{
		Status:            "queued",
		ProviderRequestID: "kling_task_456",
	})
	require.NoError(t, err)

	found, _ := model.GetBgResponseByResponseID("resp_sm_async")
	assert.Equal(t, model.BgResponseStatusQueued, found.Status)

	// Event 2: provider starts processing
	err = ApplyProviderEvent(resp.ResponseID, attempt.AttemptID, ProviderEvent{
		Status: "running",
	})
	require.NoError(t, err)

	found, _ = model.GetBgResponseByResponseID("resp_sm_async")
	assert.Equal(t, model.BgResponseStatusRunning, found.Status)

	// Event 3: provider completes
	err = ApplyProviderEvent(resp.ResponseID, attempt.AttemptID, ProviderEvent{
		Status: "succeeded",
		Output: []interface{}{"https://cdn.example.com/video.mp4"},
	})
	require.NoError(t, err)

	found, _ = model.GetBgResponseByResponseID("resp_sm_async")
	assert.Equal(t, model.BgResponseStatusSucceeded, found.Status)
	assert.True(t, found.FinalizedAt > 0)
}
