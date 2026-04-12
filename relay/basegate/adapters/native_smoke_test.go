package adapters

import (
	"os"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadEnvFile reads a .env.local-style file into a map.
func loadEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("env file not found: %s", path)
	}
	env := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			env[key] = val
		}
	}
	return env
}

func makeMinimalRequest(model string) *relaycommon.CanonicalRequest {
	return &relaycommon.CanonicalRequest{
		RequestID:  "smoke_test",
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      model,
		Input: map[string]interface{}{
			"model": model,
			"messages": []map[string]interface{}{
				{"role": "user", "content": "Say exactly: hello smoke test"},
			},
			"max_completion_tokens": 50,
		},
	}
}

// ---------------------------------------------------------------------------
// OpenAI Smoke Test
// ---------------------------------------------------------------------------

func TestSmoke_OpenAI_Invoke(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["OPENAI_API_KEY"]
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	adapter := NewOpenAILLMAdapter(0, "smoke_openai", key, "")
	req := makeMinimalRequest("bg.llm.chat.standard")

	result, err := adapter.Invoke(req)
	require.NoError(t, err, "OpenAI Invoke should not error")
	require.NotNil(t, result)

	if result.Status == "failed" && result.Error != nil {
		t.Logf("OpenAI error: code=%s message=%s detail=%s", result.Error.Code, result.Error.Message, result.Error.Detail)
	}
	require.Equal(t, "succeeded", result.Status, "OpenAI should succeed")
	require.NotEmpty(t, result.Output, "OpenAI should return output")
	t.Logf("OpenAI output: %v", result.Output[0].Content)

	require.NotNil(t, result.RawUsage, "OpenAI should return usage")
	assert.Greater(t, result.RawUsage.TotalTokens, 0, "should have token usage")
	t.Logf("OpenAI usage: prompt=%d completion=%d total=%d",
		result.RawUsage.PromptTokens, result.RawUsage.CompletionTokens, result.RawUsage.TotalTokens)
}

// ---------------------------------------------------------------------------
// Anthropic (Claude) Smoke Test
// ---------------------------------------------------------------------------

func TestSmoke_Anthropic_Invoke(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["ANTHROPIC_API_KEY"]
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	adapter := NewAnthropicLLMAdapter(0, "smoke_anthropic", key, "")
	req := makeMinimalRequest("bg.llm.chat.fast")

	result, err := adapter.Invoke(req)
	require.NoError(t, err, "Anthropic Invoke should not error")
	require.NotNil(t, result)

	if result.Status == "failed" && result.Error != nil {
		t.Logf("Anthropic error: code=%s message=%s detail=%s", result.Error.Code, result.Error.Message, result.Error.Detail)
	}
	require.Equal(t, "succeeded", result.Status, "Anthropic should succeed")
	require.NotEmpty(t, result.Output, "Anthropic should return output")
	t.Logf("Anthropic output: %v", result.Output[0].Content)

	require.NotNil(t, result.RawUsage, "Anthropic should return usage")
	assert.Greater(t, result.RawUsage.TotalTokens, 0, "should have token usage")
	t.Logf("Anthropic usage: prompt=%d completion=%d total=%d",
		result.RawUsage.PromptTokens, result.RawUsage.CompletionTokens, result.RawUsage.TotalTokens)
}

// ---------------------------------------------------------------------------
// Anthropic (Claude) Streaming Smoke Test
// ---------------------------------------------------------------------------

func TestSmoke_Anthropic_Stream(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["ANTHROPIC_API_KEY"]
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	adapter := NewAnthropicLLMAdapter(0, "smoke_anthropic_stream", key, "")
	req := makeMinimalRequest("bg.llm.chat.fast")

	ch, err := adapter.Stream(req)
	require.NoError(t, err, "Anthropic Stream should not error")
	require.NotNil(t, ch)

	var textDeltas int
	var gotDone bool
	var gotSucceeded bool

	for event := range ch {
		switch event.Type {
		case relaycommon.SSEEventTextDelta:
			textDeltas++
		case relaycommon.SSEEventResponseSucceeded:
			gotSucceeded = true
			if data, ok := event.Data.(map[string]interface{}); ok {
				t.Logf("Anthropic stream final: status=%v", data["status"])
				if usage, ok := data["raw_usage"].(*relaycommon.ProviderUsage); ok {
					assert.Greater(t, usage.TotalTokens, 0)
					t.Logf("Anthropic stream usage: prompt=%d completion=%d total=%d",
						usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
				}
			}
		case relaycommon.SSEEventDone:
			gotDone = true
		case relaycommon.SSEEventError:
			t.Fatalf("Anthropic stream error: %v", event.Data)
		}
	}

	assert.Greater(t, textDeltas, 0, "should receive text deltas")
	assert.True(t, gotSucceeded, "should receive succeeded event")
	assert.True(t, gotDone, "should receive done event")
	t.Logf("Anthropic stream: received %d text deltas", textDeltas)
}

// ---------------------------------------------------------------------------
// Gemini Smoke Test
// ---------------------------------------------------------------------------

func TestSmoke_Gemini_Invoke(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["GEMINI_API_KEY"]
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	adapter := NewGeminiLLMAdapter(0, "smoke_gemini", key, "")
	req := makeMinimalRequest("bg.llm.chat.fast")

	result, err := adapter.Invoke(req)
	require.NoError(t, err, "Gemini Invoke should not error")
	require.NotNil(t, result)

	if result.Status == "failed" && result.Error != nil {
		t.Logf("Gemini error: code=%s message=%s detail=%s", result.Error.Code, result.Error.Message, result.Error.Detail)
	}
	require.Equal(t, "succeeded", result.Status, "Gemini should succeed")
	require.NotEmpty(t, result.Output, "Gemini should return output")
	t.Logf("Gemini output: %v", result.Output[0].Content)

	require.NotNil(t, result.RawUsage, "Gemini should return usage")
	assert.Greater(t, result.RawUsage.TotalTokens, 0, "should have token usage")
	t.Logf("Gemini usage: prompt=%d completion=%d total=%d",
		result.RawUsage.PromptTokens, result.RawUsage.CompletionTokens, result.RawUsage.TotalTokens)
}

// ---------------------------------------------------------------------------
// Gemini Streaming Smoke Test
// ---------------------------------------------------------------------------

func TestSmoke_Gemini_Stream(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["GEMINI_API_KEY"]
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	adapter := NewGeminiLLMAdapter(0, "smoke_gemini_stream", key, "")
	req := makeMinimalRequest("bg.llm.chat.fast")

	ch, err := adapter.Stream(req)
	require.NoError(t, err, "Gemini Stream should not error")
	require.NotNil(t, ch)

	var textDeltas int
	var gotDone bool
	var gotSucceeded bool

	for event := range ch {
		switch event.Type {
		case relaycommon.SSEEventTextDelta:
			textDeltas++
		case relaycommon.SSEEventResponseSucceeded:
			gotSucceeded = true
		case relaycommon.SSEEventDone:
			gotDone = true
		case relaycommon.SSEEventError:
			t.Fatalf("Gemini stream error: %v", event.Data)
		}
	}

	assert.Greater(t, textDeltas, 0, "should receive text deltas")
	assert.True(t, gotSucceeded, "should receive succeeded event")
	assert.True(t, gotDone, "should receive done event")
	t.Logf("Gemini stream: received %d text deltas", textDeltas)
}

// ---------------------------------------------------------------------------
// BYO Credential Override Smoke Test (Anthropic)
// ---------------------------------------------------------------------------

func TestSmoke_Anthropic_BYOCredentialOverride(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["ANTHROPIC_API_KEY"]
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create adapter with a dummy key — BYO override should take precedence
	adapter := NewAnthropicLLMAdapter(0, "smoke_byo", "sk-INVALID-DUMMY-KEY", "")
	req := makeMinimalRequest("bg.llm.chat.fast")
	req.CredentialOverride = &relaycommon.CredentialOverride{
		APIKey: key, // real key injected via BYO override
	}

	result, err := adapter.Invoke(req)
	require.NoError(t, err, "BYO override Invoke should not error")
	require.NotNil(t, result)

	if result.Status == "failed" && result.Error != nil {
		t.Logf("BYO error: code=%s message=%s detail=%s", result.Error.Code, result.Error.Message, result.Error.Detail)
	}
	require.Equal(t, "succeeded", result.Status, "BYO override should succeed with real key")
	require.NotEmpty(t, result.Output)
	t.Logf("BYO override output: %v", result.Output[0].Content)
}
