package adapters

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeLongPrefix(nonce string, count int) string {
	var b strings.Builder
	b.WriteString("basegate cache smoke ")
	b.WriteString(nonce)
	b.WriteByte(' ')
	for i := 0; i < count; i++ {
		b.WriteString("cachetoken")
		b.WriteString(fmt.Sprintf("%d ", i))
	}
	return b.String()
}

func doJSONRequest(t *testing.T, client *http.Client, method string, url string, headers map[string]string, payload interface{}, out interface{}) {
	t.Helper()

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Less(t, resp.StatusCode, 300, "unexpected status=%d body=%s", resp.StatusCode, string(respBody))
	require.NoError(t, common.Unmarshal(respBody, out), "failed to decode response: %s", string(respBody))
}

func TestSmoke_OpenAI_PromptCache_Invoke(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["OPENAI_API_KEY"]
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	nonce := fmt.Sprintf("openai-%d", time.Now().UnixNano())
	prefix := makeLongPrefix(nonce, 1800)

	adapter := NewOpenAILLMAdapter(0, "smoke_openai_cache", key, "")
	req := &relaycommon.CanonicalRequest{
		RequestID:  "smoke_openai_cache",
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      "bg.llm.chat.standard",
		Input: map[string]interface{}{
			"messages": []map[string]interface{}{
				{"role": "system", "content": prefix},
				{"role": "user", "content": "Reply exactly with OK"},
			},
			"max_completion_tokens": 16,
			"prompt_cache_key":      "basegate-smoke-" + nonce,
		},
	}

	first, err := adapter.Invoke(req)
	require.NoError(t, err)
	require.Equal(t, "succeeded", first.Status)
	require.NotNil(t, first.RawUsage)

	time.Sleep(2 * time.Second)

	second, err := adapter.Invoke(req)
	require.NoError(t, err)
	require.Equal(t, "succeeded", second.Status)
	require.NotNil(t, second.RawUsage)

	t.Logf("OpenAI first usage: prompt=%d cached=%d completion=%d total=%d",
		first.RawUsage.PromptTokens, first.RawUsage.CachedTokens, first.RawUsage.CompletionTokens, first.RawUsage.TotalTokens)
	t.Logf("OpenAI second usage: prompt=%d cached=%d completion=%d total=%d",
		second.RawUsage.PromptTokens, second.RawUsage.CachedTokens, second.RawUsage.CompletionTokens, second.RawUsage.TotalTokens)

	assert.Greater(t, second.RawUsage.CachedTokens, 0, "second request should hit prompt cache")
	assert.GreaterOrEqual(t, second.RawUsage.CachedTokens, first.RawUsage.CachedTokens)
}

func TestSmoke_Anthropic_PromptCache_Invoke(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["ANTHROPIC_API_KEY"]
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	nonce := fmt.Sprintf("anthropic-%d", time.Now().UnixNano())
	prefix := makeLongPrefix(nonce, 1800)
	client := &http.Client{Timeout: 60 * time.Second}

	payload := map[string]interface{}{
		"model":      "claude-sonnet-4-5-20250929",
		"max_tokens": 16,
		"system": []map[string]interface{}{
			{
				"type": "text",
				"text": prefix,
				"cache_control": map[string]interface{}{
					"type": "ephemeral",
				},
			},
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Reply exactly with OK"},
		},
	}

	var first struct {
		Model   string `json:"model"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	}
	doJSONRequest(t, client, "POST", "https://api.anthropic.com/v1/messages", map[string]string{
		"x-api-key":         key,
		"anthropic-beta":    "prompt-caching-2024-07-31",
		"anthropic-version": "2023-06-01",
		"content-type":      "application/json",
	}, payload, &first)

	time.Sleep(2 * time.Second)

	var second struct {
		Model   string `json:"model"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	}
	doJSONRequest(t, client, "POST", "https://api.anthropic.com/v1/messages", map[string]string{
		"x-api-key":         key,
		"anthropic-beta":    "prompt-caching-2024-07-31",
		"anthropic-version": "2023-06-01",
		"content-type":      "application/json",
	}, payload, &second)

	require.NotEmpty(t, first.Content)
	require.NotEmpty(t, second.Content)
	t.Logf("Anthropic first usage: input=%d cache_create=%d cache_read=%d output=%d",
		first.Usage.InputTokens, first.Usage.CacheCreationInputTokens, first.Usage.CacheReadInputTokens, first.Usage.OutputTokens)
	t.Logf("Anthropic second usage: input=%d cache_create=%d cache_read=%d output=%d",
		second.Usage.InputTokens, second.Usage.CacheCreationInputTokens, second.Usage.CacheReadInputTokens, second.Usage.OutputTokens)

	assert.Greater(t, first.Usage.CacheCreationInputTokens, 0, "first request should create prompt cache")
	assert.Greater(t, second.Usage.CacheReadInputTokens, 0, "second request should read prompt cache")
	assert.Equal(t, first.Usage.InputTokens, second.Usage.InputTokens, "Claude input_tokens should remain pure non-cache input")
}

func TestSmoke_Gemini_ExplicitCache_Invoke(t *testing.T) {
	env := loadEnvFile(t, "../../../.env.local")
	key := env["GEMINI_API_KEY"]
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	nonce := fmt.Sprintf("gemini-%d", time.Now().UnixNano())
	prefix := makeLongPrefix(nonce, 2200)
	client := &http.Client{Timeout: 60 * time.Second}

	createPayload := map[string]interface{}{
		"model": "models/gemini-3.1-flash-lite-preview",
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": prefix},
				},
			},
		},
		"ttl": "300s",
	}

	var createResp struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		UsageMetadata struct {
			TotalTokenCount int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	doJSONRequest(t, client, "POST",
		"https://generativelanguage.googleapis.com/v1beta/cachedContents?key="+key,
		map[string]string{"Content-Type": "application/json"},
		createPayload,
		&createResp,
	)
	require.NotEmpty(t, createResp.Name)

	generatePayload := map[string]interface{}{
		"cachedContent": createResp.Name,
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": "Reply exactly with OK"},
				},
			},
		},
	}

	var generateResp struct {
		ModelVersion string `json:"modelVersion"`
		Candidates   []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount        int `json:"promptTokenCount"`
			CachedContentTokenCount int `json:"cachedContentTokenCount"`
			CandidatesTokenCount    int `json:"candidatesTokenCount"`
			TotalTokenCount         int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	doJSONRequest(t, client, "POST",
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-3.1-flash-lite-preview:generateContent?key="+key,
		map[string]string{"Content-Type": "application/json"},
		generatePayload,
		&generateResp,
	)

	t.Logf("Gemini create usage: total=%d", createResp.UsageMetadata.TotalTokenCount)
	t.Logf("Gemini generate usage: prompt=%d cached=%d completion=%d total=%d",
		generateResp.UsageMetadata.PromptTokenCount,
		generateResp.UsageMetadata.CachedContentTokenCount,
		generateResp.UsageMetadata.CandidatesTokenCount,
		generateResp.UsageMetadata.TotalTokenCount,
	)

	assert.Greater(t, createResp.UsageMetadata.TotalTokenCount, 0)
	assert.Greater(t, generateResp.UsageMetadata.CachedContentTokenCount, 0, "generateContent should report cachedContentTokenCount")

	deleteReq, err := http.NewRequest("DELETE", "https://generativelanguage.googleapis.com/v1beta/"+createResp.Name+"?key="+key, nil)
	require.NoError(t, err)
	deleteResp, err := client.Do(deleteReq)
	require.NoError(t, err)
	_ = deleteResp.Body.Close()
	assert.Less(t, deleteResp.StatusCode, 300)
}
