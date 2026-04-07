package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaseGateRequest_Serialize(t *testing.T) {
	req := BaseGateRequest{
		Model: "bg.llm.chat.standard",
		Input: "Hello, world!",
		ExecutionOptions: &BGExecutionOptions{
			Mode:      "sync",
			TimeoutMs: 30000,
		},
		Metadata: map[string]string{
			"user_id":  "u_123",
			"trace_id": "t_abc",
		},
	}

	data, err := common.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"model":"bg.llm.chat.standard"`)
	assert.Contains(t, string(data), `"input":"Hello, world!"`)
	assert.Contains(t, string(data), `"mode":"sync"`)

	var decoded BaseGateRequest
	err = common.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "bg.llm.chat.standard", decoded.Model)
	assert.Equal(t, "sync", decoded.ExecutionOptions.Mode)
	assert.Equal(t, 30000, decoded.ExecutionOptions.TimeoutMs)
}

func TestBaseGateRequest_AsyncWithWebhook(t *testing.T) {
	req := BaseGateRequest{
		Model: "bg.video.generate.kling",
		Input: map[string]interface{}{
			"prompt":     "a cat dancing",
			"resolution": "1080p",
			"seconds":    5,
		},
		ExecutionOptions: &BGExecutionOptions{
			Mode:       "async",
			WebhookURL: "https://example.com/callback",
		},
	}

	data, err := common.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"mode":"async"`)
	assert.Contains(t, string(data), `"webhook_url":"https://example.com/callback"`)
}

func TestBaseGateResponse_Serialize(t *testing.T) {
	resp := BaseGateResponse{
		ID:        "resp_abc123",
		Object:    "response",
		CreatedAt: 1770000000,
		Status:    "succeeded",
		Model:     "bg.llm.chat.standard",
		Output: []BGOutputItem{
			{Type: "text", Content: "Hello! How can I help?"},
		},
		Usage: &BGUsage{
			BillableUnits: 150,
			BillableUnit:  "token",
			InputUnits:    50,
			OutputUnits:   100,
		},
		Pricing: &BGPricing{
			BillingMode:  "metered",
			BillableUnit: "token",
			UnitPrice:    0.00003,
			Total:        0.0045,
			Currency:     "usd",
		},
		Meta: &BGMeta{
			RequestID: "req_xyz",
			Provider:  "openai",
			LatencyMs: 1200,
		},
	}

	data, err := common.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"id":"resp_abc123"`)
	assert.Contains(t, string(data), `"status":"succeeded"`)
	assert.Contains(t, string(data), `"object":"response"`)

	var decoded BaseGateResponse
	err = common.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "resp_abc123", decoded.ID)
	assert.Equal(t, "succeeded", decoded.Status)
	assert.Len(t, decoded.Output, 1)
	assert.Equal(t, "text", decoded.Output[0].Type)
	assert.NotNil(t, decoded.Usage)
	assert.Equal(t, float64(150), decoded.Usage.BillableUnits)
	assert.NotNil(t, decoded.Pricing)
	assert.Equal(t, "usd", decoded.Pricing.Currency)
}

func TestBaseGateResponse_FailedWithError(t *testing.T) {
	resp := BaseGateResponse{
		ID:        "resp_fail_001",
		Object:    "response",
		CreatedAt: 1770000000,
		Status:    "failed",
		Model:     "bg.video.generate.kling",
		Error: &BGError{
			Code:    "provider_error",
			Message: "upstream returned 500",
			Detail:  "Internal server error from kling API",
		},
	}

	data, err := common.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"code":"provider_error"`)

	var decoded BaseGateResponse
	err = common.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "failed", decoded.Status)
	assert.Nil(t, decoded.Output)
	assert.Nil(t, decoded.Usage)
	assert.NotNil(t, decoded.Error)
	assert.Equal(t, "provider_error", decoded.Error.Code)
}

func TestBGSessionResponse_Serialize(t *testing.T) {
	resp := BGSessionResponse{
		ID:             "sess_abc",
		Object:         "session",
		CreatedAt:      1770000000,
		Status:         "active",
		Model:          "bg.browser.session.standard",
		ResponseID:     "resp_xyz",
		ExpiresAt:      1770003600,
		IdleTimeoutSec: 300,
		MaxDurationSec: 3600,
	}

	data, err := common.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"id":"sess_abc"`)
	assert.Contains(t, string(data), `"object":"session"`)
	assert.Contains(t, string(data), `"status":"active"`)
}

func TestBGModelObject_Serialize(t *testing.T) {
	model := BGModelObject{
		ID:      "bg.llm.chat.standard",
		Object:  "model",
		OwnedBy: "basegate",
		Capability: &BGCapability{
			Domain: "llm",
			Action: "chat",
			Tier:   "standard",
		},
		Execution: &BGExecution{
			Mode:      "sync",
			Streaming: true,
		},
		Pricing: &BGModelPricing{
			BillingMode:  "metered",
			BillableUnit: "token",
			InputPrice:   0.00003,
			OutputPrice:  0.00006,
			Currency:     "usd",
		},
	}

	data, err := common.Marshal(model)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"domain":"llm"`)
	assert.Contains(t, string(data), `"streaming":true`)
}

func TestBGSessionActionRequest_Serialize(t *testing.T) {
	req := BGSessionActionRequest{
		Action: "navigate",
		Input: map[string]interface{}{
			"url": "https://example.com",
		},
	}

	data, err := common.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"action":"navigate"`)
	assert.Contains(t, string(data), `"url":"https://example.com"`)
}
