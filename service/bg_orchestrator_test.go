package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock ProviderAdapter for testing
// ---------------------------------------------------------------------------

type mockProviderAdapter struct {
	name           string
	capabilities   []relaycommon.CapabilityBinding
	validateResult *relaycommon.ValidationResult
	invokeResult   *relaycommon.AdapterResult
	invokeErr      error
	pollResult     *relaycommon.AdapterResult
	pollErr        error
}

func (m *mockProviderAdapter) Name() string { return m.name }
func (m *mockProviderAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	return m.capabilities
}
func (m *mockProviderAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	if m.validateResult != nil {
		return m.validateResult
	}
	return &relaycommon.ValidationResult{Valid: true, ResolvedModel: req.Model}
}
func (m *mockProviderAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	return m.invokeResult, m.invokeErr
}
func (m *mockProviderAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return m.pollResult, m.pollErr
}
func (m *mockProviderAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return &relaycommon.AdapterResult{Status: "canceled"}, nil
}
func (m *mockProviderAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	return nil, basegate.ErrStreamNotSupported
}

// ---------------------------------------------------------------------------
// Orchestrator — DispatchSync tests
// ---------------------------------------------------------------------------

func TestDispatchSync_Success(t *testing.T) {
	truncateBgTables(t)
	basegate.ClearRegistry()
	cacheInitialized.Store(true) // Enable routing engine (no policies = fallback to LookupAdapters)

	// Register a mock adapter
	adapter := &mockProviderAdapter{
		name: "mock_llm",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.llm.chat.test", AdapterName: "mock_llm"},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status: "succeeded",
			Output: []relaycommon.OutputItem{
				{Type: "text", Content: "Hello from mock!"},
			},
			RawUsage: &relaycommon.ProviderUsage{
				PromptTokens:     50,
				CompletionTokens: 100,
				TotalTokens:      150,
			},
		},
	}
	basegate.RegisterAdapter(adapter)

	req := &relaycommon.CanonicalRequest{
		RequestID:  "req_test_sync",
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      "bg.llm.chat.test",
		OrgID:      1,
		Input:      "Hello",
		ExecutionOptions: relaycommon.ExecutionOptions{
			Mode: "sync",
		},
	}

	resp, err := DispatchSync(req)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", resp.Status)
	assert.Equal(t, req.ResponseID, resp.ID)

	// Verify DB state
	dbResp, err := model.GetBgResponseByResponseID(req.ResponseID)
	require.NoError(t, err)
	assert.Equal(t, model.BgResponseStatusSucceeded, dbResp.Status)
	assert.True(t, dbResp.FinalizedAt > 0)
}

func TestDispatchSync_AdapterNotFound(t *testing.T) {
	truncateBgTables(t)
	basegate.ClearRegistry()
	cacheInitialized.Store(true)

	req := &relaycommon.CanonicalRequest{
		RequestID:  "req_test_404",
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      "bg.nonexistent.model",
		OrgID:      1,
		Input:      "Hello",
	}

	_, err := DispatchSync(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no adapter")
}

func TestDispatchSync_AdapterFails(t *testing.T) {
	truncateBgTables(t)
	basegate.ClearRegistry()
	cacheInitialized.Store(true)

	adapter := &mockProviderAdapter{
		name: "mock_fail",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.llm.chat.fail", AdapterName: "mock_fail"},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status: "failed",
			Error: &relaycommon.AdapterError{
				Code:    "provider_error",
				Message: "upstream 500",
			},
		},
	}
	basegate.RegisterAdapter(adapter)

	req := &relaycommon.CanonicalRequest{
		RequestID:  "req_test_fail",
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      "bg.llm.chat.fail",
		OrgID:      1,
		Input:      "Hello",
	}

	resp, err := DispatchSync(req)
	require.NoError(t, err) // dispatch itself doesn't error; the response carries the error
	assert.Equal(t, "failed", resp.Status)
	assert.NotNil(t, resp.Error)

	// Verify DB state
	dbResp, err := model.GetBgResponseByResponseID(req.ResponseID)
	require.NoError(t, err)
	assert.Equal(t, model.BgResponseStatusFailed, dbResp.Status)
}

// ---------------------------------------------------------------------------
// Orchestrator — DispatchAsync tests
// ---------------------------------------------------------------------------

func TestDispatchAsync_Queued(t *testing.T) {
	truncateBgTables(t)
	basegate.ClearRegistry()
	cacheInitialized.Store(true)

	adapter := &mockProviderAdapter{
		name: "mock_video",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.video.generate.test", AdapterName: "mock_video"},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status:            "accepted",
			ProviderRequestID: "prov_task_789",
			PollAfterMs:       10000,
		},
	}
	basegate.RegisterAdapter(adapter)

	req := &relaycommon.CanonicalRequest{
		RequestID:  "req_test_async",
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      "bg.video.generate.test",
		OrgID:      1,
		Input:      map[string]interface{}{"prompt": "test"},
		ExecutionOptions: relaycommon.ExecutionOptions{
			Mode: "async",
		},
	}

	resp, err := DispatchAsync(req)
	require.NoError(t, err)
	assert.Equal(t, "queued", resp.Status)
	assert.Equal(t, req.ResponseID, resp.ID)

	// Verify DB: response should be queued
	dbResp, err := model.GetBgResponseByResponseID(req.ResponseID)
	require.NoError(t, err)
	assert.Equal(t, model.BgResponseStatusQueued, dbResp.Status)

	// Verify attempt was created with poll schedule
	attempts, err := model.GetBgAttemptsByResponseID(req.ResponseID)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, "prov_task_789", attempts[0].ProviderRequestID)
	assert.True(t, attempts[0].PollAfterAt > 0)
}

// ---------------------------------------------------------------------------
// Orchestrator — Idempotency tests
// ---------------------------------------------------------------------------

func TestDispatch_Idempotency(t *testing.T) {
	truncateBgTables(t)
	basegate.ClearRegistry()
	cacheInitialized.Store(true)

	adapter := &mockProviderAdapter{
		name: "mock_idem",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.llm.chat.idem", AdapterName: "mock_idem"},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status: "succeeded",
			Output: []relaycommon.OutputItem{{Type: "text", Content: "first call"}},
		},
	}
	basegate.RegisterAdapter(adapter)

	req := &relaycommon.CanonicalRequest{
		RequestID:      "req_idem_1",
		ResponseID:     relaycommon.GenerateResponseID(),
		Model:          "bg.llm.chat.idem",
		OrgID:          1,
		IdempotencyKey: "idem_key_unique_001",
		Input:          "Hello",
	}

	// First call
	resp1, err := DispatchSync(req)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", resp1.Status)

	// Second call with same idempotency key — should return cached response
	req2 := &relaycommon.CanonicalRequest{
		RequestID:      "req_idem_2",
		ResponseID:     relaycommon.GenerateResponseID(), // different response_id
		Model:          "bg.llm.chat.idem",
		OrgID:          1,
		IdempotencyKey: "idem_key_unique_001",
		Input:          "Hello",
	}

	resp2, err := DispatchSync(req2)
	require.NoError(t, err)
	// Should return the FIRST response's ID
	assert.Equal(t, resp1.ID, resp2.ID)
}

// ---------------------------------------------------------------------------
// GetResponse test
// ---------------------------------------------------------------------------

func TestGetResponse(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_get_test",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 1,
		OrgID:         1,
		OutputJSON:    `[{"type":"text","content":"Hello!"}]`,
	}
	require.NoError(t, resp.Insert())

	result, err := GetResponse("resp_get_test")
	require.NoError(t, err)
	assert.Equal(t, "resp_get_test", result.ID)
	assert.Equal(t, "succeeded", result.Status)
}
