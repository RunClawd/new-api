package adapters

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// KlingVideoAdapter is a native ProviderAdapter + CallbackCapableAdapter
// for Kling video generation. It directly calls the Kling REST API,
// bypassing the legacy TaskAdaptor bridge.
type KlingVideoAdapter struct {
	channelID   int
	channelName string
	apiKey      string // "access_key|secret_key"
	baseURL     string
	modelMap    map[string]klingModel
}

type klingModel struct {
	UpstreamModel string
	Action        string // "text2video" or "image2video"
}

var _ basegate.ProviderAdapter = (*KlingVideoAdapter)(nil)
var _ basegate.CallbackCapableAdapter = (*KlingVideoAdapter)(nil)

// NewKlingVideoAdapter creates a Kling video adapter from channel config.
func NewKlingVideoAdapter(channelID int, channelName, apiKey, baseURL string) *KlingVideoAdapter {
	if baseURL == "" {
		baseURL = "https://api.klingai.com"
	} else {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}
	return &KlingVideoAdapter{
		channelID:   channelID,
		channelName: channelName,
		apiKey:      apiKey,
		baseURL:     baseURL,
		modelMap: map[string]klingModel{
			"bg.video.generate.standard": {UpstreamModel: "kling-v1", Action: "text2video"},
			"bg.video.generate.pro":      {UpstreamModel: "kling-v2-master", Action: "text2video"},
		},
	}
}

func (a *KlingVideoAdapter) Name() string {
	return fmt.Sprintf("kling_native_ch%d", a.channelID)
}

func (a *KlingVideoAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	var bindings []relaycommon.CapabilityBinding
	for capName := range a.modelMap {
		bindings = append(bindings, relaycommon.CapabilityBinding{
			CapabilityPattern: capName,
			AdapterName:       a.Name(),
			Provider:          "kling",
			UpstreamModel:     a.modelMap[capName].UpstreamModel,
			Weight:            1,
			SupportsAsync:     true,
			SupportsStreaming:  false,
		})
	}
	return bindings
}

func (a *KlingVideoAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	if _, ok := a.modelMap[req.Model]; !ok {
		return &relaycommon.ValidationResult{
			Valid: false,
			Error: &relaycommon.AdapterError{Code: "not_supported", Message: "unsupported model: " + req.Model},
		}
	}
	return &relaycommon.ValidationResult{Valid: true}
}

// Invoke submits a video generation job and returns accepted + provider task ID.
func (a *KlingVideoAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	model, ok := a.modelMap[req.Model]
	if !ok {
		return nil, fmt.Errorf("unsupported model: %s", req.Model)
	}

	// Build request payload from canonical input
	var payload map[string]interface{}
	if inputJSON, err := common.Marshal(req.Input); err == nil {
		if err := common.Unmarshal(inputJSON, &payload); err != nil {
			return nil, fmt.Errorf("invalid input format: %w", err)
		}
	} else {
		return nil, fmt.Errorf("invalid input format: %w", err)
	}

	payload["model_name"] = model.UpstreamModel
	payload["model"] = model.UpstreamModel

	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload failed: %w", err)
	}

	// Determine endpoint path
	path := fmt.Sprintf("/v1/videos/%s", model.Action)
	if strings.HasPrefix(a.apiKey, "sk-") {
		path = fmt.Sprintf("/kling/v1/videos/%s", model.Action)
	}
	url := a.baseURL + path

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}

	if err := a.setAuth(httpReq); err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var klingResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			TaskId string `json:"task_id"`
		} `json:"data"`
	}
	if err := common.Unmarshal(respBody, &klingResp); err != nil {
		return nil, fmt.Errorf("parse kling response failed: %w", err)
	}
	if klingResp.Code != 0 {
		return &relaycommon.AdapterResult{
			Status: "failed",
			Error: &relaycommon.AdapterError{
				Code:    "provider_error",
				Message: klingResp.Message,
				Detail:  string(respBody),
			},
		}, nil
	}

	return &relaycommon.AdapterResult{
		Status:            "accepted",
		ProviderRequestID: klingResp.Data.TaskId,
		PollAfterMs:       15000, // initial 15s
	}, nil
}

// Poll checks the status of a video generation task.
// Implements progressive polling intervals: 30s while processing, 10s when near completion.
func (a *KlingVideoAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	// Default to text2video; action doesn't matter for GET polling
	path := fmt.Sprintf("/v1/videos/text2video/%s", providerRequestID)
	if strings.HasPrefix(a.apiKey, "sk-") {
		path = fmt.Sprintf("/kling/v1/videos/text2video/%s", providerRequestID)
	}
	url := a.baseURL + path

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if err := a.setAuth(httpReq); err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var klingResp struct {
		Code int    `json:"code"`
		Data struct {
			TaskStatus         string `json:"task_status"`
			TaskStatusMsg      string `json:"task_status_msg"`
			FinalUnitDeduction string `json:"final_unit_deduction"`
			TaskResult         struct {
				Videos []struct {
					URL      string `json:"url"`
					Duration string `json:"duration"`
				} `json:"videos"`
			} `json:"task_result"`
		} `json:"data"`
	}
	if err := common.Unmarshal(respBody, &klingResp); err != nil {
		return nil, fmt.Errorf("parse poll response failed: %w", err)
	}

	switch klingResp.Data.TaskStatus {
	case "submitted":
		return &relaycommon.AdapterResult{
			Status:            "accepted",
			ProviderRequestID: providerRequestID,
			PollAfterMs:       30000, // 30s — early stage
		}, nil

	case "processing":
		return &relaycommon.AdapterResult{
			Status:            "running",
			ProviderRequestID: providerRequestID,
			PollAfterMs:       10000, // 10s — processing, could complete soon
		}, nil

	case "succeed":
		result := &relaycommon.AdapterResult{
			Status:            "succeeded",
			ProviderRequestID: providerRequestID,
		}

		// Extract video output
		if videos := klingResp.Data.TaskResult.Videos; len(videos) > 0 {
			for _, v := range videos {
				result.Output = append(result.Output, relaycommon.OutputItem{
					Type:    "video",
					Content: v.URL,
				})
			}

			// Extract duration for billing
			if dur, err := strconv.ParseFloat(videos[0].Duration, 64); err == nil && dur > 0 {
				result.RawUsage = &relaycommon.ProviderUsage{
					DurationSec:   dur,
					BillableUnits: dur,
					BillableUnit:  "second",
				}
			}
		}

		// Fallback: use FinalUnitDeduction for billing
		if result.RawUsage == nil {
			if units, err := strconv.ParseFloat(klingResp.Data.FinalUnitDeduction, 64); err == nil && units > 0 {
				result.RawUsage = &relaycommon.ProviderUsage{
					BillableUnits: math.Ceil(units),
					BillableUnit:  "second",
				}
			}
		}

		return result, nil

	case "failed":
		return &relaycommon.AdapterResult{
			Status: "failed",
			Error: &relaycommon.AdapterError{
				Code:    "provider_error",
				Message: klingResp.Data.TaskStatusMsg,
			},
		}, nil

	default:
		return &relaycommon.AdapterResult{
			Status:      "running",
			PollAfterMs: 15000,
		}, nil
	}
}

func (a *KlingVideoAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("cancel is not supported by the Kling video API")
}

func (a *KlingVideoAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	return nil, basegate.ErrStreamNotSupported
}

// ParseCallback handles inbound Kling webhook notifications.
func (a *KlingVideoAdapter) ParseCallback(req *http.Request) (*relaycommon.AdapterResult, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read callback body failed: %w", err)
	}
	defer req.Body.Close()

	var klingCB struct {
		TaskId     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
		Data       struct {
			TaskResult struct {
				Videos []struct {
					URL      string `json:"url"`
					Duration string `json:"duration"`
				} `json:"videos"`
			} `json:"task_result"`
			FinalUnitDeduction string `json:"final_unit_deduction"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &klingCB); err != nil {
		return nil, fmt.Errorf("parse kling callback failed: %w", err)
	}

	result := &relaycommon.AdapterResult{
		ProviderRequestID: klingCB.TaskId,
	}

	switch klingCB.TaskStatus {
	case "succeed":
		result.Status = "succeeded"
		for _, v := range klingCB.Data.TaskResult.Videos {
			result.Output = append(result.Output, relaycommon.OutputItem{
				Type:    "video",
				Content: v.URL,
			})
		}
		if units, err := strconv.ParseFloat(klingCB.Data.FinalUnitDeduction, 64); err == nil && units > 0 {
			result.RawUsage = &relaycommon.ProviderUsage{
				BillableUnits: math.Ceil(units),
				BillableUnit:  "second",
			}
		}
	case "failed":
		result.Status = "failed"
		result.Error = &relaycommon.AdapterError{Code: "provider_error", Message: "task failed via callback"}
	default:
		result.Status = "running"
	}

	return result, nil
}

// setAuth sets the Authorization header via JWT or direct key.
func (a *KlingVideoAdapter) setAuth(req *http.Request) error {
	token, err := CreateKlingJWT(a.apiKey)
	if err != nil {
		return fmt.Errorf("kling JWT generation failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "kling-sdk/1.0")
	return nil
}
