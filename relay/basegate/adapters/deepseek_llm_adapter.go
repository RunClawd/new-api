package adapters

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// DeepSeekLLMAdapter mimicking OpenAI for deepseek-reasoner
type DeepSeekLLMAdapter struct {
	channelID   int
	channelName string
	apiKey      string
	baseURL     string
	modelMap    map[string]string // basegate model -> upstream model
	transport   http.RoundTripper
}

var _ basegate.ProviderAdapter = (*DeepSeekLLMAdapter)(nil)

func NewDeepSeekLLMAdapter(channelID int, channelName string, apiKey string, baseURL string) *DeepSeekLLMAdapter {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	} else {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}
	return &DeepSeekLLMAdapter{
		channelID:   channelID,
		channelName: channelName,
		apiKey:      apiKey,
		baseURL:     baseURL,
		modelMap: map[string]string{
			"bg.llm.reasoning.pro": "deepseek-reasoner",
			"bg.llm.chat.pro":      "deepseek-chat",
		},
	}
}

func (a *DeepSeekLLMAdapter) SetTransport(t http.RoundTripper) {
	a.transport = t
}

func (a *DeepSeekLLMAdapter) resolveAPIKey(req *relaycommon.CanonicalRequest) string {
	if req.CredentialOverride != nil && req.CredentialOverride.APIKey != "" {
		return req.CredentialOverride.APIKey
	}
	return a.apiKey
}

func (a *DeepSeekLLMAdapter) getClient() *http.Client {
	if a.transport != nil {
		return &http.Client{Transport: a.transport}
	}
	return &http.Client{}
}

func (a *DeepSeekLLMAdapter) Name() string {
	return fmt.Sprintf("deepseek_native_ch%d", a.channelID)
}

func (a *DeepSeekLLMAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	var bindings []relaycommon.CapabilityBinding
	for key, upstream := range a.modelMap {
		bindings = append(bindings, relaycommon.CapabilityBinding{
			CapabilityPattern: key,
			AdapterName:       a.Name(),
			Provider:          "deepseek",
			UpstreamModel:     upstream,
			Priority:          0,
			Weight:            1,
		})
	}
	return bindings
}

func (a *DeepSeekLLMAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	if _, ok := a.modelMap[req.Model]; !ok {
		return &relaycommon.ValidationResult{
			Valid: false,
			Error: &relaycommon.AdapterError{Code: "not_supported", Message: "unsupported model"},
		}
	}
	return &relaycommon.ValidationResult{Valid: true}
}

func (a *DeepSeekLLMAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("poll not supported for chat")
}

func (a *DeepSeekLLMAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("cancel not supported for chat")
}

func (a *DeepSeekLLMAdapter) buildPayload(req *relaycommon.CanonicalRequest, stream bool) ([]byte, error) {
	upstreamModel, ok := a.modelMap[req.Model]
	if !ok {
		upstreamModel = req.Model // Passthrough
	}

	var payload map[string]interface{}
	if inputJSON, err := common.Marshal(req.Input); err == nil {
		if err := common.Unmarshal(inputJSON, &payload); err != nil {
			return nil, fmt.Errorf("invalid input format")
		}
	} else {
		return nil, fmt.Errorf("invalid input format")
	}

	payload["model"] = upstreamModel
	payload["stream"] = stream

	return common.Marshal(payload)
}

func (a *DeepSeekLLMAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	payloadBytes, err := a.buildPayload(req, false)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/chat/completions", a.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.resolveAPIKey(req))

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

	var providerResp struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content,omitempty"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens         int `json:"prompt_tokens"`
			CompletionTokens     int `json:"completion_tokens"`
			TotalTokens          int `json:"total_tokens"`
			PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
		} `json:"usage"`
	}

	if err := common.Unmarshal(respBody, &providerResp); err != nil {
		return nil, fmt.Errorf("failed to parse DeepSeek response: %w", err)
	}

	var output []relaycommon.OutputItem
	if len(providerResp.Choices) > 0 {
		msg := providerResp.Choices[0].Message

		// Include reasoning output explicitly if present
		if msg.ReasoningContent != "" {
			output = append(output, relaycommon.OutputItem{
				Type:    "reasoning",
				Content: msg.ReasoningContent, // BaseGate internal mapping
			})
		}
		output = append(output, relaycommon.OutputItem{
			Type:    "text",
			Content: msg.Content,
		})
	}

	cachedTokens := providerResp.Usage.PromptCacheHitTokens
	inputTokens := providerResp.Usage.PromptTokens - cachedTokens
	if inputTokens < 0 {
		inputTokens = 0
	}

	usage := relaycommon.ProviderUsage{
		PromptTokens:     providerResp.Usage.PromptTokens,
		CompletionTokens: providerResp.Usage.CompletionTokens,
		TotalTokens:      providerResp.Usage.TotalTokens,
		InputTokens:      inputTokens,
		CachedTokens:     cachedTokens,
	}

	return &relaycommon.AdapterResult{
		Status:   "succeeded",
		Output:   output,
		RawUsage: &usage,
	}, nil
}

func (a *DeepSeekLLMAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	payloadBytes, err := a.buildPayload(req, true)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/chat/completions", a.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.resolveAPIKey(req))
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
		var accumulatedUsage *relaycommon.ProviderUsage

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					break // clean stream end usually yields [DONE] before EOF
				}
				ch <- relaycommon.SSEEvent{
					Type: relaycommon.SSEEventError,
					Data: map[string]interface{}{
						"code":    "stream_error",
						"message": err.Error(),
					},
				}
				break
			}
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			// "data: ..." format
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))

			if string(data) == "[DONE]" {
				break
			}

			// Parse chunk
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content,omitempty"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens         int `json:"prompt_tokens"`
					CompletionTokens     int `json:"completion_tokens"`
					TotalTokens          int `json:"total_tokens"`
					PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
				} `json:"usage,omitempty"`
			}
			if err := common.Unmarshal(data, &chunk); err != nil {
				continue // ignoring parsing errors per SSE robustness specs
			}

			if chunk.Usage != nil {
				cachedTokens := chunk.Usage.PromptCacheHitTokens
				inputTokens := chunk.Usage.PromptTokens - cachedTokens
				if inputTokens < 0 {
					inputTokens = 0
				}
				accumulatedUsage = &relaycommon.ProviderUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
					InputTokens:      inputTokens,
					CachedTokens:     cachedTokens,
				}
			}

			if len(chunk.Choices) > 0 {
				deltaContent := chunk.Choices[0].Delta.Content
				deltaReasoning := chunk.Choices[0].Delta.ReasoningContent
				
				if deltaContent != "" {
					accumulatedText += deltaContent
					ch <- relaycommon.SSEEvent{
						Type: relaycommon.SSEEventTextDelta,
						Data: relaycommon.TextDeltaData{
							ItemIndex:  0,
							Delta:      deltaContent,
							OutputText: accumulatedText,
						},
					}
				}

				if deltaReasoning != "" {
					accumulatedReasoning += deltaReasoning
					ch <- relaycommon.SSEEvent{
						Type: "reasoning_delta", // Custom internal type
						Data: relaycommon.TextDeltaData{
							ItemIndex:  0,
							Delta:      deltaReasoning,
							OutputText: accumulatedReasoning,
						},
					}
				}
			}
		}

		// Yield terminal metadata packet
		var finalOutput []relaycommon.OutputItem
		if accumulatedReasoning != "" {
			finalOutput = append(finalOutput, relaycommon.OutputItem{Type: "reasoning", Content: accumulatedReasoning})
		}
		if accumulatedText != "" {
			finalOutput = append(finalOutput, relaycommon.OutputItem{Type: "text", Content: accumulatedText})
		}

		ch <- relaycommon.SSEEvent{
			Type: relaycommon.SSEEventResponseSucceeded,
			Data: map[string]interface{}{
				"status":    "succeeded",
				"raw_usage": accumulatedUsage,
				"output":    finalOutput,
			},
		}

		// Close transmission loop
		ch <- relaycommon.SSEEvent{
			Type: relaycommon.SSEEventDone,
		}
	}()

	return ch, nil
}
