package adapters

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestDeepSeekLLMAdapter_Invoke(t *testing.T) {
	adapter := NewDeepSeekLLMAdapter(1, "test-ds", "ds-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "Bearer ds-key", r.Header.Get("Authorization"))
		assert.Equal(t, "/chat/completions", r.URL.Path)
		return newMockHTTPResponse(http.StatusOK, `{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hello from DeepSeek!"
				}
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 8,
				"total_tokens": 18
			}
		}`, map[string]string{"Content-Type": "application/json"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.pro",
		Input: map[string]interface{}{
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
		},
	}

	result, err := adapter.Invoke(req)
	assert.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
	assert.Len(t, result.Output, 1)
	assert.Equal(t, "Hello from DeepSeek!", result.Output[0].Content)
	assert.Equal(t, 18, result.RawUsage.TotalTokens)
}

func TestDeepSeekLLMAdapter_Invoke_WithReasoning(t *testing.T) {
	adapter := NewDeepSeekLLMAdapter(1, "test-ds", "ds-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return newMockHTTPResponse(http.StatusOK, `{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "The answer is 42.",
					"reasoning_content": "Let me think step by step..."
				}
			}],
			"usage": {
				"prompt_tokens": 20,
				"completion_tokens": 50,
				"total_tokens": 70
			}
		}`, map[string]string{"Content-Type": "application/json"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.reasoning.pro",
		Input: map[string]interface{}{
			"messages": []map[string]string{
				{"role": "user", "content": "What is the meaning of life?"},
			},
		},
	}

	result, err := adapter.Invoke(req)
	assert.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
	assert.Len(t, result.Output, 2)
	assert.Equal(t, "reasoning", result.Output[0].Type)
	assert.Equal(t, "Let me think step by step...", result.Output[0].Content)
	assert.Equal(t, "text", result.Output[1].Type)
	assert.Equal(t, "The answer is 42.", result.Output[1].Content)
}

func TestDeepSeekLLMAdapter_Stream(t *testing.T) {
	streamBody := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Deep\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Seek!\"}}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"

	adapter := NewDeepSeekLLMAdapter(1, "test-ds", "ds-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return newMockHTTPResponse(http.StatusOK, streamBody, map[string]string{"Content-Type": "text/event-stream"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.pro",
		Input: map[string]interface{}{
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
		},
	}

	ch, err := adapter.Stream(req)
	assert.NoError(t, err)

	var events []common.SSEEvent
	for event := range ch {
		events = append(events, event)
	}

	assert.GreaterOrEqual(t, len(events), 3)
	succeeded := events[len(events)-2]
	assert.Equal(t, common.SSEEventResponseSucceeded, succeeded.Type)
	dataMap := succeeded.Data.(map[string]interface{})
	assert.Equal(t, "succeeded", dataMap["status"])
	usage := dataMap["raw_usage"].(*common.ProviderUsage)
	assert.Equal(t, 7, usage.TotalTokens)
	done := events[len(events)-1]
	assert.Equal(t, common.SSEEventDone, done.Type)
}

func TestDeepSeekLLMAdapter_CredentialOverride(t *testing.T) {
	adapter := NewDeepSeekLLMAdapter(1, "test-ds", "channel-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "Bearer byo-user-key", r.Header.Get("Authorization"))
		return newMockHTTPResponse(http.StatusOK, `{"choices":[{"message":{"content":"OK"}}],"usage":{"total_tokens":5}}`, map[string]string{"Content-Type": "application/json"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.pro",
		Input: map[string]interface{}{
			"messages": []map[string]string{{"role": "user", "content": "test"}},
		},
		CredentialOverride: &common.CredentialOverride{
			APIKey: "byo-user-key",
		},
	}

	result, err := adapter.Invoke(req)
	assert.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
}
