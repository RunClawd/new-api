package adapters

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/basegate"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

type AnthropicLLMAdapter struct {
	channelID   int
	channelName string
	apiKey      string
	baseURL     string
	modelMap    map[string]string
	transport   http.RoundTripper
}

var _ basegate.ProviderAdapter = (*AnthropicLLMAdapter)(nil)

func NewAnthropicLLMAdapter(channelID int, channelName string, apiKey string, baseURL string) *AnthropicLLMAdapter {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	} else {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}
	return &AnthropicLLMAdapter{
		channelID:   channelID,
		channelName: channelName,
		apiKey:      apiKey,
		baseURL:     baseURL,
		modelMap: map[string]string{
			"bg.llm.chat.fast":     "claude-haiku-4-5-20251001",
			"bg.llm.chat.standard": "claude-sonnet-4-5-20250929",
			"bg.llm.chat.pro":      "claude-opus-4-6",
			"bg.llm.reasoning.pro": "claude-opus-4-6",
		},
	}
}

func (a *AnthropicLLMAdapter) SetTransport(t http.RoundTripper) {
	a.transport = t
}

func (a *AnthropicLLMAdapter) resolveAPIKey(req *relaycommon.CanonicalRequest) string {
	if req.CredentialOverride != nil && req.CredentialOverride.APIKey != "" {
		return req.CredentialOverride.APIKey
	}
	return a.apiKey
}

func (a *AnthropicLLMAdapter) getClient() *http.Client {
	if a.transport != nil {
		return &http.Client{Transport: a.transport}
	}
	return &http.Client{}
}

func (a *AnthropicLLMAdapter) Name() string {
	return fmt.Sprintf("anthropic_native_ch%d", a.channelID)
}

func (a *AnthropicLLMAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	var bindings []relaycommon.CapabilityBinding
	for key, upstream := range a.modelMap {
		bindings = append(bindings, relaycommon.CapabilityBinding{
			CapabilityPattern: key,
			AdapterName:       a.Name(),
			Provider:          "anthropic",
			UpstreamModel:     upstream,
			Priority:          0,
			Weight:            1,
		})
	}
	return bindings
}

func (a *AnthropicLLMAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	if _, ok := a.modelMap[req.Model]; !ok {
		return &relaycommon.ValidationResult{
			Valid: false,
			Error: &relaycommon.AdapterError{Code: "not_supported", Message: "unsupported model"},
		}
	}
	return &relaycommon.ValidationResult{Valid: true}
}

func (a *AnthropicLLMAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("poll not supported for chat")
}

func (a *AnthropicLLMAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("cancel not supported for chat")
}

func (a *AnthropicLLMAdapter) buildPayload(req *relaycommon.CanonicalRequest, stream bool) ([]byte, error) {
	upstreamModel, ok := a.modelMap[req.Model]
	if !ok {
		upstreamModel = req.Model // Passthrough
	}

	var openAIReq dto.GeneralOpenAIRequest
	inputJSON, err := common.Marshal(req.Input)
	if err != nil {
		return nil, fmt.Errorf("invalid input format")
	}
	if err := common.Unmarshal(inputJSON, &openAIReq); err != nil {
		return nil, fmt.Errorf("failed to parse input as OpenAI format")
	}

	claudeReq, err := claude.RequestOpenAI2ClaudeMessage(nil, openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to map claude request: %v", err)
	}

	claudeReq.Model = upstreamModel

	streamVal := stream
	claudeReq.Stream = &streamVal

	// Max tokens are generally enforced by Claude API as required.
	if claudeReq.MaxTokens == nil || *claudeReq.MaxTokens == 0 {
		defaultMax := uint(4096)
		claudeReq.MaxTokens = &defaultMax
	}

	return common.Marshal(claudeReq)
}

func (a *AnthropicLLMAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	payloadBytes, err := a.buildPayload(req, false)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1/messages", a.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.resolveAPIKey(req))
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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
		var errData dto.ClaudeResponse
		if err := common.Unmarshal(respBody, &errData); err == nil && errData.Error != nil {
			claudeErr := errData.GetClaudeError()
			return &relaycommon.AdapterResult{
				Status: "failed",
				Error: &relaycommon.AdapterError{
					Code:    "provider_error",
					Message: claudeErr.Message,
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

	var providerResp dto.ClaudeResponse
	if err := common.Unmarshal(respBody, &providerResp); err != nil {
		return nil, fmt.Errorf("failed to parse Claude response: %w", err)
	}

	var output []relaycommon.OutputItem
	for _, content := range providerResp.Content {
		if content.Type == "text" {
			output = append(output, relaycommon.OutputItem{
				Type:    "text",
				Content: content.GetText(),
			})
		}
	}

	var usage relaycommon.ProviderUsage
	if providerResp.Usage != nil {
		usage.PromptTokens = providerResp.Usage.InputTokens
		usage.CompletionTokens = providerResp.Usage.OutputTokens
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return &relaycommon.AdapterResult{
		Status:   "succeeded",
		Output:   output,
		RawUsage: &usage,
	}, nil
}

func (a *AnthropicLLMAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	payloadBytes, err := a.buildPayload(req, true)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1/messages", a.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.resolveAPIKey(req))
	httpReq.Header.Set("anthropic-version", "2023-06-01")
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
		var accumulatedThinking string
		var accumulatedUsage relaycommon.ProviderUsage
		var currentEvent string
		blockTypes := make(map[int]string)

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

			if bytes.HasPrefix(line, []byte("event:")) {
				currentEvent = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
				continue
			}

			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}

			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))

			var chunk dto.ClaudeResponse
			if err := common.Unmarshal(data, &chunk); err != nil {
				continue // parse error
			}

			switch currentEvent {
			case "message_start":
				if chunk.Message != nil && chunk.Message.Usage != nil {
					accumulatedUsage.PromptTokens = chunk.Message.Usage.InputTokens
					accumulatedUsage.CompletionTokens = chunk.Message.Usage.OutputTokens
					accumulatedUsage.TotalTokens = accumulatedUsage.PromptTokens + accumulatedUsage.CompletionTokens
				}
			case "content_block_start":
				if chunk.ContentBlock != nil {
					blockTypes[chunk.GetIndex()] = chunk.ContentBlock.Type
				}
			case "message_delta":
				if chunk.Usage != nil {
					if accumulatedUsage.PromptTokens == 0 && chunk.Usage.InputTokens > 0 {
						accumulatedUsage.PromptTokens = chunk.Usage.InputTokens
					}
					accumulatedUsage.CompletionTokens = chunk.Usage.OutputTokens
					accumulatedUsage.TotalTokens = accumulatedUsage.PromptTokens + accumulatedUsage.CompletionTokens
				}
			case "content_block_delta":
				if chunk.Delta != nil {
					if chunk.Delta.Type == "signature_delta" {
						continue
					}
					if chunk.Delta.Type == "thinking_delta" || blockTypes[chunk.GetIndex()] == "thinking" {
						if chunk.Delta.Thinking != nil {
							accumulatedThinking += *chunk.Delta.Thinking
						}
						continue
					}
					deltaText := chunk.Delta.GetText()
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
			case "ping":
				continue
			case "message_stop":
				// Stream finalized
			}
		}

		output := make([]relaycommon.OutputItem, 0, 2)
		if accumulatedThinking != "" {
			output = append(output, relaycommon.OutputItem{
				Type:    "reasoning",
				Content: accumulatedThinking,
			})
		}
		if accumulatedText != "" {
			output = append(output, relaycommon.OutputItem{
				Type:    "text",
				Content: accumulatedText,
			})
		}

		ch <- relaycommon.SSEEvent{
			Type: relaycommon.SSEEventResponseSucceeded,
			Data: map[string]interface{}{
				"status":    "succeeded",
				"raw_usage": &accumulatedUsage,
				"output":    output,
			},
		}

		ch <- relaycommon.SSEEvent{Type: relaycommon.SSEEventDone}
	}()

	return ch, nil
}
