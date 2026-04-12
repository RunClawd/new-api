package adapters

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/basegate"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

type GeminiLLMAdapter struct {
	channelID   int
	channelName string
	apiKey      string
	baseURL     string
	modelMap    map[string]string
	transport   http.RoundTripper
}

var _ basegate.ProviderAdapter = (*GeminiLLMAdapter)(nil)

func NewGeminiLLMAdapter(channelID int, channelName string, apiKey string, baseURL string) *GeminiLLMAdapter {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	} else {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}
	return &GeminiLLMAdapter{
		channelID:   channelID,
		channelName: channelName,
		apiKey:      apiKey,
		baseURL:     baseURL,
		modelMap: map[string]string{
			"bg.llm.chat.fast":     "gemini-3.1-flash-lite-preview",
			"bg.llm.chat.standard": "gemini-3.1-pro-preview",
			"bg.llm.chat.pro":      "gemini-3.1-pro-preview",
		},
	}
}

func (a *GeminiLLMAdapter) SetTransport(t http.RoundTripper) {
	a.transport = t
}

func (a *GeminiLLMAdapter) resolveAPIKey(req *relaycommon.CanonicalRequest) string {
	if req.CredentialOverride != nil && req.CredentialOverride.APIKey != "" {
		return req.CredentialOverride.APIKey
	}
	return a.apiKey
}

func (a *GeminiLLMAdapter) getClient() *http.Client {
	if a.transport != nil {
		return &http.Client{Transport: a.transport}
	}
	return &http.Client{}
}

func (a *GeminiLLMAdapter) Name() string {
	return fmt.Sprintf("gemini_native_ch%d", a.channelID)
}

func (a *GeminiLLMAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	var bindings []relaycommon.CapabilityBinding
	for key, upstream := range a.modelMap {
		bindings = append(bindings, relaycommon.CapabilityBinding{
			CapabilityPattern: key,
			AdapterName:       a.Name(),
			Provider:          "gemini",
			UpstreamModel:     upstream,
			Priority:          0,
			Weight:            1,
		})
	}
	return bindings
}

func (a *GeminiLLMAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	if _, ok := a.modelMap[req.Model]; !ok {
		return &relaycommon.ValidationResult{
			Valid: false,
			Error: &relaycommon.AdapterError{Code: "not_supported", Message: "unsupported model"},
		}
	}
	return &relaycommon.ValidationResult{Valid: true}
}

func (a *GeminiLLMAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("poll not supported for chat")
}

func (a *GeminiLLMAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("cancel not supported for chat")
}

func (a *GeminiLLMAdapter) buildPayload(req *relaycommon.CanonicalRequest, upstreamModel string) ([]byte, error) {
	var openAIReq dto.GeneralOpenAIRequest
	inputJSON, err := common.Marshal(req.Input)
	if err != nil {
		return nil, fmt.Errorf("invalid input format")
	}
	if err := common.Unmarshal(inputJSON, &openAIReq); err != nil {
		return nil, fmt.Errorf("failed to parse input as OpenAI format")
	}

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			UpstreamModelName: upstreamModel,
		},
	}

	geminiReq, err := gemini.CovertOpenAI2Gemini(nil, openAIReq, info)
	if err != nil {
		return nil, fmt.Errorf("failed to map gemini request: %v", err)
	}

	return common.Marshal(geminiReq)
}

func geminiThoughtSignature(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	var signature string
	if err := common.Unmarshal(raw, &signature); err == nil {
		return signature
	}

	return strings.Trim(string(raw), `"`)
}

func buildGeminiReasoningOutput(reasoningText string, thoughtSignature string) []relaycommon.OutputItem {
	if reasoningText == "" && thoughtSignature == "" {
		return nil
	}

	content := map[string]interface{}{}
	if reasoningText != "" {
		content["text"] = reasoningText
	}
	if thoughtSignature != "" {
		content["thought_signature"] = thoughtSignature
	}

	return []relaycommon.OutputItem{{
		Type:    "reasoning",
		Content: content,
	}}
}

func buildGeminiOutput(candidate dto.GeminiChatCandidate) []relaycommon.OutputItem {
	output := make([]relaycommon.OutputItem, 0, 2)
	var textParts []string
	var reasoningParts []string
	thoughtSignature := ""

	for _, part := range candidate.Content.Parts {
		if sig := geminiThoughtSignature(part.ThoughtSignature); sig != "" && thoughtSignature == "" {
			thoughtSignature = sig
		}

		if part.Thought {
			if part.Text != "" {
				reasoningParts = append(reasoningParts, part.Text)
			}
			continue
		}

		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}

	output = append(output, buildGeminiReasoningOutput(strings.Join(reasoningParts, ""), thoughtSignature)...)
	if len(textParts) > 0 {
		output = append(output, relaycommon.OutputItem{
			Type:    "text",
			Content: strings.Join(textParts, ""),
		})
	}

	return output
}

func buildGeminiUsage(metadata dto.GeminiUsageMetadata) relaycommon.ProviderUsage {
	completionTokens := metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount
	cachedTokens := metadata.CachedContentTokenCount
	inputTokens := metadata.PromptTokenCount - cachedTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	return relaycommon.ProviderUsage{
		PromptTokens:     metadata.PromptTokenCount,
		CompletionTokens: completionTokens,
		TotalTokens:      metadata.TotalTokenCount,
		InputTokens:      inputTokens,
		CachedTokens:     cachedTokens,
	}
}

func (a *GeminiLLMAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	upstreamModel, ok := a.modelMap[req.Model]
	if !ok {
		upstreamModel = req.Model
	}

	payloadBytes, err := a.buildPayload(req, upstreamModel)
	if err != nil {
		return nil, err
	}

	// For standard invocation: generateContent
	// https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=...
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", a.baseURL, upstreamModel, a.resolveAPIKey(req))
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := a.getClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		if err := common.Unmarshal(respBody, &errData); err == nil {
			msg := ""
			if e, ok := errData["error"].(map[string]interface{}); ok {
				if m, ok := e["message"].(string); ok {
					msg = m
				}
			}
			return &relaycommon.AdapterResult{
				Status: "failed",
				Error: &relaycommon.AdapterError{
					Code:    "provider_error",
					Message: msg,
					Detail:  string(respBody),
				},
			}, nil
		}
		return &relaycommon.AdapterResult{
			Status: "failed",
			Error: &relaycommon.AdapterError{
				Code:    "http_error",
				Message: fmt.Sprintf("HTTP %d", resp.StatusCode),
				Detail:  string(respBody),
			},
		}, nil
	}

	var providerResp dto.GeminiChatResponse
	if err := common.Unmarshal(respBody, &providerResp); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	var output []relaycommon.OutputItem
	if len(providerResp.Candidates) > 0 {
		output = buildGeminiOutput(providerResp.Candidates[0])
	}

	var usage relaycommon.ProviderUsage
	if providerResp.UsageMetadata.TotalTokenCount > 0 {
		usage = buildGeminiUsage(providerResp.UsageMetadata)
	}

	return &relaycommon.AdapterResult{
		Status:   "succeeded",
		Output:   output,
		RawUsage: &usage,
	}, nil
}

func (a *GeminiLLMAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	upstreamModel, ok := a.modelMap[req.Model]
	if !ok {
		upstreamModel = req.Model
	}

	payloadBytes, err := a.buildPayload(req, upstreamModel)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", a.baseURL, upstreamModel, a.resolveAPIKey(req))
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	client := a.getClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan relaycommon.SSEEvent)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		reader := bufio.NewReader(resp.Body)
		var accumulatedText string
		var accumulatedReasoning string
		var thoughtSignature string
		var accumulatedUsage relaycommon.ProviderUsage

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				ch <- relaycommon.SSEEvent{
					Type: relaycommon.SSEEventError,
					Data: map[string]interface{}{"code": "stream_error", "message": err.Error()},
				}
				break
			}
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}

			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if string(data) == "[DONE]" {
				break
			}

			var chunk dto.GeminiChatResponse
			if err := common.Unmarshal(data, &chunk); err != nil {
				continue
			}

			if chunk.UsageMetadata.TotalTokenCount > 0 {
				accumulatedUsage = buildGeminiUsage(chunk.UsageMetadata)
			}

			if len(chunk.Candidates) > 0 {
				cand := chunk.Candidates[0]
				for _, part := range cand.Content.Parts {
					if sig := geminiThoughtSignature(part.ThoughtSignature); sig != "" && thoughtSignature == "" {
						thoughtSignature = sig
					}

					deltaText := part.Text
					if part.Thought {
						if deltaText != "" {
							accumulatedReasoning += deltaText
							ch <- relaycommon.SSEEvent{
								Type: "reasoning_delta",
								Data: relaycommon.TextDeltaData{
									ItemIndex:  0,
									Delta:      deltaText,
									OutputText: accumulatedReasoning,
								},
							}
						}
						continue
					}

					if deltaText != "" {
						accumulatedText += deltaText
						ch <- relaycommon.SSEEvent{
							Type: relaycommon.SSEEventTextDelta,
							Data: relaycommon.TextDeltaData{
								ItemIndex:  0,
								Delta:      deltaText,
								OutputText: accumulatedText,
							},
						}
					}
				}
			}
		}

		ch <- relaycommon.SSEEvent{
			Type: relaycommon.SSEEventResponseSucceeded,
			Data: map[string]interface{}{
				"status":    "succeeded",
				"raw_usage": &accumulatedUsage,
				"output": func() []relaycommon.OutputItem {
					output := buildGeminiReasoningOutput(accumulatedReasoning, thoughtSignature)
					if accumulatedText != "" {
						output = append(output, relaycommon.OutputItem{
							Type:    "text",
							Content: accumulatedText,
						})
					}
					return output
				}(),
			},
		}

		ch <- relaycommon.SSEEvent{Type: relaycommon.SSEEventDone}
	}()

	return ch, nil
}
