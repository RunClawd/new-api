package adapters

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestGeminiLLMAdapter_Invoke(t *testing.T) {
	adapter := NewGeminiLLMAdapter(1, "test-gemini", "gem-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v1beta/models/gemini-3.1-pro-preview:generateContent", r.URL.Path)
		assert.Equal(t, "gem-key", r.URL.Query().Get("key"))
		return newMockHTTPResponse(http.StatusOK, `{
			"candidates": [{
				"content": {"parts": [{"text": "Hello from Gemini!"}]}
			}],
			"usageMetadata": {
				"promptTokenCount": 11,
				"candidatesTokenCount": 7,
				"totalTokenCount": 18
			}
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
	assert.Equal(t, "Hello from Gemini!", result.Output[0].Content)
	assert.Equal(t, 18, result.RawUsage.TotalTokens)
}

func TestGeminiLLMAdapter_Stream(t *testing.T) {
	streamBody := "" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Gem\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ini\"}]}}],\"usageMetadata\":{\"promptTokenCount\":4,\"candidatesTokenCount\":3,\"totalTokenCount\":7}}\n\n"

	adapter := NewGeminiLLMAdapter(1, "test-gemini", "gem-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v1beta/models/gemini-3.1-flash-lite-preview:streamGenerateContent", r.URL.Path)
		assert.Equal(t, "sse", r.URL.Query().Get("alt"))
		assert.Equal(t, "gem-key", r.URL.Query().Get("key"))
		return newMockHTTPResponse(http.StatusOK, streamBody, map[string]string{"Content-Type": "text/event-stream"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.fast",
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
	output := dataMap["output"].([]common.OutputItem)
	assert.Len(t, output, 1)
	assert.Equal(t, "Gemini", output[0].Content)
	usage := dataMap["raw_usage"].(*common.ProviderUsage)
	assert.Equal(t, 7, usage.TotalTokens)
}

func TestGeminiLLMAdapter_CredentialOverride(t *testing.T) {
	adapter := NewGeminiLLMAdapter(1, "test-gemini", "channel-key", "https://example.test")
	adapter.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "byo-key", r.URL.Query().Get("key"))
		return newMockHTTPResponse(http.StatusOK, `{
			"candidates": [{
				"content": {"parts": [{"text": "OK"}]}
			}],
			"usageMetadata": {"totalTokenCount": 3}
		}`, map[string]string{"Content-Type": "application/json"}), nil
	}))

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.pro",
		Input: map[string]interface{}{
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "test"},
			},
		},
		CredentialOverride: &common.CredentialOverride{
			APIKey: "byo-key",
		},
	}

	result, err := adapter.Invoke(req)
	assert.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
}
