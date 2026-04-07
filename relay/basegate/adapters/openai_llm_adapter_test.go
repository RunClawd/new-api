package adapters

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestOpenAILLMAdapter_Invoke(t *testing.T) {
	// 1. Mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "gpt-5.4-mini",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello there!"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 9,
				"completion_tokens": 12,
				"total_tokens": 21
			}
		}`)
	}))
	defer mockServer.Close()

	// 2. Setup adapter
	adapter := NewOpenAILLMAdapter(1, "test-openai", "test-key", mockServer.URL)

	// 3. Create canonical request
	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.standard", // Maps to gpt-5.4-mini
		Input: map[string]interface{}{
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
		},
	}

	// 4. Validate
	valRes := adapter.Validate(req)
	assert.True(t, valRes.Valid)

	// 5. Invoke
	result, err := adapter.Invoke(req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "succeeded", result.Status)

	assert.Len(t, result.Output, 1)
	assert.Equal(t, "Hello there!", result.Output[0].Content)

	assert.NotNil(t, result.RawUsage)
	assert.Equal(t, 21, result.RawUsage.TotalTokens)
	assert.Equal(t, 9, result.RawUsage.PromptTokens)
	assert.Equal(t, 12, result.RawUsage.CompletionTokens)
}

func TestOpenAILLMAdapter_Stream(t *testing.T) {
	// 1. Mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send chunks
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"Hi "}}]}`)
		w.(http.Flusher).Flush()
		time.Sleep(10 * time.Millisecond)

		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"there!"}}]}`)
		w.(http.Flusher).Flush()
		time.Sleep(10 * time.Millisecond)

		fmt.Fprintf(w, "data: %s\n\n", `{"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
		w.(http.Flusher).Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer mockServer.Close()

	// 2. Setup adapter
	adapter := NewOpenAILLMAdapter(1, "test-openai", "test-key", mockServer.URL)

	req := &common.CanonicalRequest{
		Model: "bg.llm.chat.standard",
		Input: map[string]interface{}{
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
		},
	}

	// 3. Stream
	ch, err := adapter.Stream(req)
	assert.NoError(t, err)

	var events []common.SSEEvent
	for event := range ch {
		events = append(events, event)
	}

	// Should emit: 2 TextDeltas, 1 Succeeded, 1 Done
	assert.Len(t, events, 4)

	// Event 1: Delta
	assert.Equal(t, common.SSEEventTextDelta, events[0].Type)
	delta1 := events[0].Data.(common.TextDeltaData)
	assert.Equal(t, "Hi ", delta1.Delta)
	assert.Equal(t, "Hi ", delta1.OutputText)

	// Event 2: Delta
	assert.Equal(t, common.SSEEventTextDelta, events[1].Type)
	delta2 := events[1].Data.(common.TextDeltaData)
	assert.Equal(t, "there!", delta2.Delta)
	assert.Equal(t, "Hi there!", delta2.OutputText)

	// Event 3: Succeeded
	assert.Equal(t, common.SSEEventResponseSucceeded, events[2].Type)
	dataMap := events[2].Data.(map[string]interface{})
	assert.Equal(t, "succeeded", dataMap["status"])
	output := dataMap["output"].([]common.OutputItem)
	assert.Len(t, output, 1)
	assert.Equal(t, "Hi there!", output[0].Content)
	usage := dataMap["raw_usage"].(*common.ProviderUsage)
	assert.Equal(t, 8, usage.TotalTokens)

	// Event 4: Done
	assert.Equal(t, common.SSEEventDone, events[3].Type)
}
