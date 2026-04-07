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

type OpenAILLMAdapter struct {
	channelID   int
	channelName string
	apiKey      string
	baseURL     string
	modelMap    map[string]string // basegate model -> upstream model
}

var _ basegate.ProviderAdapter = (*OpenAILLMAdapter)(nil)

func NewOpenAILLMAdapter(channelID int, channelName string, apiKey string, baseURL string) *OpenAILLMAdapter {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	} else {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}
	return &OpenAILLMAdapter{
		channelID:   channelID,
		channelName: channelName,
		apiKey:      apiKey,
		baseURL:     baseURL,
		modelMap: map[string]string{
			"bg.llm.chat.fast":     "gpt-5.4-nano",
			"bg.llm.chat.standard": "gpt-5.4-mini",
			"bg.llm.chat.pro":      "gpt-5.4",
			"bg.llm.reasoning.pro": "gpt-5.4-pro",
		},
	}
}

func (a *OpenAILLMAdapter) Name() string {
	return fmt.Sprintf("openai_native_ch%d", a.channelID)
}

func (a *OpenAILLMAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	var bindings []relaycommon.CapabilityBinding
	for key, upstream := range a.modelMap {
		bindings = append(bindings, relaycommon.CapabilityBinding{
			CapabilityPattern: key,
			AdapterName:       a.Name(),
			Provider:          "openai",
			UpstreamModel:     upstream,
			Priority:          0,
			Weight:            1,
		})
	}
	return bindings
}

func (a *OpenAILLMAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	if _, ok := a.modelMap[req.Model]; !ok {
		return &relaycommon.ValidationResult{
			Valid: false,
			Error:   &relaycommon.AdapterError{Code: "not_supported", Message: "unsupported model"},
		}
	}
	return &relaycommon.ValidationResult{Valid: true}
}

func (a *OpenAILLMAdapter) buildPayload(req *relaycommon.CanonicalRequest, stream bool) ([]byte, error) {
	upstreamModel := a.modelMap[req.Model]

	// req.Input should be a map representing the JSON body
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

func (a *OpenAILLMAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	payloadBytes, err := a.buildPayload(req, false)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1/chat/completions", a.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	client := &http.Client{}
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
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage relaycommon.ProviderUsage `json:"usage"`
	}

	if err := common.Unmarshal(respBody, &providerResp); err != nil {
		return nil, fmt.Errorf("parse provider response failed: %w", err)
	}

	var output []relaycommon.OutputItem
	for _, c := range providerResp.Choices {
		output = append(output, relaycommon.OutputItem{
			Type:    "text",
			Content: c.Message.Content,
		})
	}

	return &relaycommon.AdapterResult{
		Status:   "succeeded",
		Output:   output,
		RawUsage: &providerResp.Usage,
	}, nil
}

func (a *OpenAILLMAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	payloadBytes, err := a.buildPayload(req, true)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1/chat/completions", a.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{}
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
		var accumulatedUsage *relaycommon.ProviderUsage

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				ch <- relaycommon.SSEEvent{
					Type: relaycommon.SSEEventError,
					Data: relaycommon.ErrorData{
						Code:    "stream_error",
						Message: err.Error(),
					},
				}
				break
			}
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}

			dataStr := strings.TrimPrefix(line, "data: ")
			if dataStr == "[DONE]" {
				break
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *relaycommon.ProviderUsage `json:"usage,omitempty"`
			}
			if err := common.Unmarshal([]byte(dataStr), &chunk); err != nil {
				continue
			}

			if chunk.Usage != nil {
				accumulatedUsage = chunk.Usage
			}

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta.Content
				if delta != "" {
					accumulatedText += delta
					ch <- relaycommon.SSEEvent{
						Type: relaycommon.SSEEventTextDelta,
						Data: relaycommon.TextDeltaData{
							ItemIndex:  0,
							Delta:      delta,
							OutputText: accumulatedText,
						},
					}
				}
			}
		}

		// Send output format alignment according to schema
		ch <- relaycommon.SSEEvent{
			Type: relaycommon.SSEEventResponseSucceeded,
			Data: map[string]interface{}{
				"status":    "succeeded",
				"raw_usage": accumulatedUsage,
				"output": []relaycommon.OutputItem{
					{
						Type:    "text",
						Content: accumulatedText,
					},
				},
			},
		}

		ch <- relaycommon.SSEEvent{
			Type: relaycommon.SSEEventDone,
		}
	}()

	return ch, nil
}

func (a *OpenAILLMAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("poll not supported for sync/stream chat capabilities")
}

func (a *OpenAILLMAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("cancel not supported for chat")
}
