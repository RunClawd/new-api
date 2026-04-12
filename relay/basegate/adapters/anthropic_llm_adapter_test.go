package adapters

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestAnthropicLLMAdapter_Invoke(t *testing.T) {
	adapter := NewAnthropicLLMAdapter(1, "test-claude", "test-anthropic-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "test-anthropic-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "/v1/messages", r.URL.Path)
		return newMockHTTPResponse(http.StatusOK, `{
			"type": "message",
			"content": [{"type": "text", "text": "Hello from Claude!"}],
			"usage": {"input_tokens": 15, "output_tokens": 10}
		}`, map[string]string{"Content-Type": "application/json"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.standard",
		Input: map[string]interface{}{
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "Hi"},
			},
		},
	}

	result, err := adapter.Invoke(req)
	assert.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
	assert.Len(t, result.Output, 1)
	assert.Equal(t, "Hello from Claude!", result.Output[0].Content)
	assert.Equal(t, 15, result.RawUsage.PromptTokens)
	assert.Equal(t, 10, result.RawUsage.CompletionTokens)
	assert.Equal(t, 25, result.RawUsage.TotalTokens)
}

func TestAnthropicLLMAdapter_Stream(t *testing.T) {
	streamBody := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Claude!\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":0,\"output_tokens\":8}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	adapter := NewAnthropicLLMAdapter(1, "test-claude", "test-anthropic-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "test-anthropic-key", r.Header.Get("x-api-key"))
		return newMockHTTPResponse(http.StatusOK, streamBody, map[string]string{"Content-Type": "text/event-stream"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.standard",
		Input: map[string]interface{}{
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "Hi"},
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
	usage := dataMap["raw_usage"].(*common.ProviderUsage)
	assert.Equal(t, 12, usage.PromptTokens)
	assert.Equal(t, 8, usage.CompletionTokens)
	assert.Equal(t, 20, usage.TotalTokens)
	output := dataMap["output"].([]common.OutputItem)
	assert.Len(t, output, 1)
	assert.Equal(t, "Hello Claude!", output[0].Content)
}

func TestAnthropicLLMAdapter_Stream_ThinkingNotMixedIntoText(t *testing.T) {
	streamBody := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"internal chain\"}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Visible answer\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	adapter := NewAnthropicLLMAdapter(1, "test-claude", "test-anthropic-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return newMockHTTPResponse(http.StatusOK, streamBody, map[string]string{"Content-Type": "text/event-stream"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.reasoning.pro",
		Input: map[string]interface{}{
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "Hi"},
			},
		},
	}

	ch, err := adapter.Stream(req)
	assert.NoError(t, err)

	var succeeded common.SSEEvent
	for event := range ch {
		if event.Type == common.SSEEventResponseSucceeded {
			succeeded = event
		}
	}

	dataMap := succeeded.Data.(map[string]interface{})
	output := dataMap["output"].([]common.OutputItem)
	assert.Len(t, output, 2)
	assert.Equal(t, "reasoning", output[0].Type)
	assert.Equal(t, "internal chain", output[0].Content)
	assert.Equal(t, "text", output[1].Type)
	assert.Equal(t, "Visible answer", output[1].Content)
}

func TestAnthropicLLMAdapter_CredentialOverride(t *testing.T) {
	adapter := NewAnthropicLLMAdapter(1, "test-claude", "channel-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "byo-claude-key", r.Header.Get("x-api-key"))
		return newMockHTTPResponse(http.StatusOK, `{
			"type": "message",
			"content": [{"type": "text", "text": "OK"}],
			"usage": {"input_tokens": 5, "output_tokens": 2}
		}`, map[string]string{"Content-Type": "application/json"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.standard",
		Input: map[string]interface{}{
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "test"},
			},
		},
		CredentialOverride: &common.CredentialOverride{
			APIKey: "byo-claude-key",
		},
	}

	result, err := adapter.Invoke(req)
	assert.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
}
