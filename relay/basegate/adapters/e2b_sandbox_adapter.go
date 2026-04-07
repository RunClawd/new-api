package adapters

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// E2BSandboxAdapter implements SessionCapableAdapter for the E2B sandbox platform.
// It manages sandbox lifecycle via the E2B REST API (https://api.e2b.app)
// and executes code via the envd Connect RPC API (https://49983-{sandboxID}.e2b.app).
type E2BSandboxAdapter struct {
	channelID int
	apiKey    string
	baseURL   string // Management API: https://api.e2b.app
	template  string // e.g. "base", "code-interpreter-v1"

	// Track sandbox start times for billing calculations
	mu             sync.RWMutex
	sandboxStarted map[string]time.Time // provider session ID → start time
}

var _ basegate.ProviderAdapter = (*E2BSandboxAdapter)(nil)
var _ basegate.SessionCapableAdapter = (*E2BSandboxAdapter)(nil)

// NewE2BSandboxAdapter creates an E2B adapter from channel configuration.
func NewE2BSandboxAdapter(channelID int, apiKey, baseURL, template string) *E2BSandboxAdapter {
	if baseURL == "" {
		baseURL = "https://api.e2b.app"
	} else {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}
	if template == "" {
		template = "code-interpreter-v1"
	}
	return &E2BSandboxAdapter{
		channelID:      channelID,
		apiKey:         apiKey,
		baseURL:        baseURL,
		template:       template,
		sandboxStarted: make(map[string]time.Time),
	}
}

func (a *E2BSandboxAdapter) Name() string {
	return fmt.Sprintf("e2b_sandbox_ch%d", a.channelID)
}

func (a *E2BSandboxAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	return []relaycommon.CapabilityBinding{
		{
			CapabilityPattern: "bg.sandbox.session.standard",
			AdapterName:       a.Name(),
			Provider:          "e2b",
			Weight:            1,
		},
	}
}

func (a *E2BSandboxAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	if req.Model != "bg.sandbox.session.standard" {
		return &relaycommon.ValidationResult{
			Valid: false,
			Error: &relaycommon.AdapterError{Code: "not_supported", Message: "model not supported by E2B adapter"},
		}
	}
	return &relaycommon.ValidationResult{Valid: true}
}

// Invoke proxies to CreateSession for session-mode capabilities, per approved design.
func (a *E2BSandboxAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	sess, err := a.CreateSession(req)
	if err != nil {
		return nil, err
	}
	return &relaycommon.AdapterResult{
		Status: "succeeded",
		Output: []relaycommon.OutputItem{{Type: "session", Content: sess}},
		Session: sess,
	}, nil
}

func (a *E2BSandboxAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("poll not applicable for session-mode adapter")
}

func (a *E2BSandboxAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("cancel not applicable for session-mode adapter; use CloseSession")
}

func (a *E2BSandboxAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	return nil, basegate.ErrStreamNotSupported
}

// ---------------------------------------------------------------------------
// SessionCapableAdapter methods
// ---------------------------------------------------------------------------

// CreateSession creates an E2B sandbox.
// POST https://api.e2b.app/sandboxes
func (a *E2BSandboxAdapter) CreateSession(req *relaycommon.CanonicalRequest) (*relaycommon.SessionResult, error) {
	payload := map[string]interface{}{
		"templateID": a.template,
		"timeout":    3600, // 1 hour default
	}

	// Pass through metadata if provided
	if req.Metadata != nil {
		payload["metadata"] = req.Metadata
	}

	payloadBytes, _ := common.Marshal(payload)

	httpReq, err := http.NewRequest("POST", a.baseURL+"/sandboxes", bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("build create-sandbox request failed: %w", err)
	}
	a.setAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("create-sandbox request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("E2B create sandbox failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var e2bResp struct {
		SandboxID  string `json:"sandboxID"`
		ClientID   string `json:"clientID"`
		TemplateID string `json:"templateID"`
	}
	if err := common.Unmarshal(respBody, &e2bResp); err != nil {
		return nil, fmt.Errorf("parse create-sandbox response failed: %w", err)
	}

	// Track start time for billing
	a.mu.Lock()
	a.sandboxStarted[e2bResp.SandboxID] = time.Now()
	a.mu.Unlock()

	sandboxURL := fmt.Sprintf("https://49983-%s.e2b.app", e2bResp.SandboxID)

	return &relaycommon.SessionResult{
		SessionID: e2bResp.SandboxID,
		LiveURL:   sandboxURL,
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}, nil
}

// ExecuteAction executes code in an E2B sandbox.
// Uses the envd gRPC-web API: POST https://49983-{sandboxID}.e2b.app/process.Process/Start
// Protocol: gRPC-web+JSON with 5-byte envelope framing per message.
func (a *E2BSandboxAdapter) ExecuteAction(providerSessionID string, action *basegate.SessionActionRequest) (*basegate.SessionActionResult, error) {
	// Build gRPC StartRequest JSON
	cmd := action.Action
	if action.Input != nil {
		if inputStr, ok := action.Input.(string); ok {
			cmd = inputStr
		}
	}

	processPayload := map[string]interface{}{
		"process": map[string]interface{}{
			"cmd":  "/bin/bash",
			"args": []string{"-c", cmd},
		},
		"wait": true,
	}

	jsonBody, _ := common.Marshal(processPayload)

	// gRPC-web envelope: 1 byte flags (0x00 = no compression) + 4 bytes big-endian length + body
	framedBody := grpcWebEncode(jsonBody)

	envdURL := envdBaseURL(providerSessionID) + "/process.Process/Start"
	httpReq, err := http.NewRequest("POST", envdURL, bytes.NewReader(framedBody))
	if err != nil {
		return nil, fmt.Errorf("build execute request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/grpc-web+json")
	httpReq.Header.Set("x-grpc-web", "1")

	timeout := 60 * time.Second
	if action.TimeoutMs > 0 {
		timeout = time.Duration(action.TimeoutMs) * time.Millisecond
	}
	client := &http.Client{Timeout: timeout}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return &basegate.SessionActionResult{
			Status: "failed",
			Error: &relaycommon.AdapterError{
				Code:    "execution_error",
				Message: fmt.Sprintf("E2B execution failed (HTTP %d)", resp.StatusCode),
				Detail:  string(respBody),
			},
		}, nil
	}

	// Parse grpc-web framed response: sequence of [flags:1][length:4][json-body:N] messages.
	// Data messages have flags=0x00; trailers have flags=0x80.
	var stdout, stderr strings.Builder
	var exitCode int
	var pid string
	exited := false

	for pos := 0; pos+5 <= len(respBody); {
		flags := respBody[pos]
		msgLen := int(binary.BigEndian.Uint32(respBody[pos+1 : pos+5]))
		pos += 5
		if pos+msgLen > len(respBody) {
			break
		}
		msgBytes := respBody[pos : pos+msgLen]
		pos += msgLen

		if flags&0x80 != 0 {
			continue // trailer frame — skip
		}

		var envelope struct {
			Event struct {
				Start *struct {
					Pid int `json:"pid"`
				} `json:"start,omitempty"`
				Data *struct {
					Stdout string `json:"stdout,omitempty"` // base64-encoded bytes
					Stderr string `json:"stderr,omitempty"` // base64-encoded bytes
				} `json:"data,omitempty"`
				End *struct {
					ExitCode int    `json:"exitCode"`
					Exited   bool   `json:"exited"`
					Status   string `json:"status"`
					Error    string `json:"error"`
				} `json:"end,omitempty"`
			} `json:"event"`
		}
		if err := common.Unmarshal(msgBytes, &envelope); err != nil {
			continue
		}

		ev := envelope.Event
		if ev.Start != nil {
			pid = fmt.Sprintf("%d", ev.Start.Pid)
		}
		if ev.Data != nil {
			if ev.Data.Stdout != "" {
				if decoded, err := base64.StdEncoding.DecodeString(ev.Data.Stdout); err == nil {
					stdout.Write(decoded)
				}
			}
			if ev.Data.Stderr != "" {
				if decoded, err := base64.StdEncoding.DecodeString(ev.Data.Stderr); err == nil {
					stderr.Write(decoded)
				}
			}
		}
		if ev.End != nil {
			exitCode = ev.End.ExitCode
			exited = true
		}
	}

	status := "succeeded"
	var execErr *relaycommon.AdapterError
	if !exited {
		status = "failed"
		execErr = &relaycommon.AdapterError{
			Code:    "execution_error",
			Message: "process did not produce an end event",
			Detail:  string(respBody),
		}
	} else if exitCode != 0 {
		status = "failed"
		execErr = &relaycommon.AdapterError{
			Code:    "exit_nonzero",
			Message: fmt.Sprintf("process exited with code %d", exitCode),
			Detail:  stderr.String(),
		}
	}

	output := map[string]interface{}{
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": exitCode,
	}

	return &basegate.SessionActionResult{
		ActionID: pid,
		Status:   status,
		Output:   output,
		Error:    execErr,
		Usage: &relaycommon.ProviderUsage{
			Actions:       1,
			BillableUnits: 1,
			BillableUnit:  "action",
		},
	}, nil
}

// grpcWebEncode wraps a JSON body in a gRPC-web envelope:
// [flags:1 byte (0x00)][length:4 bytes big-endian][body:N bytes]
func grpcWebEncode(body []byte) []byte {
	frame := make([]byte, 5+len(body))
	frame[0] = 0x00 // no compression
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(body)))
	copy(frame[5:], body)
	return frame
}

// CloseSession stops an E2B sandbox and calculates session billing.
// DELETE https://api.e2b.app/sandboxes/{sandboxID}
func (a *E2BSandboxAdapter) CloseSession(providerSessionID string) (*basegate.SessionCloseResult, error) {
	httpReq, err := http.NewRequest("DELETE", a.baseURL+"/sandboxes/"+providerSessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("build close request failed: %w", err)
	}
	a.setAuth(httpReq)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("close request failed: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content is expected; 404 means already closed
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("E2B delete sandbox failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Calculate elapsed time
	a.mu.RLock()
	startTime, found := a.sandboxStarted[providerSessionID]
	a.mu.RUnlock()

	sessionMinutes := 1.0 // minimum 1 minute
	if found {
		elapsed := time.Since(startTime).Minutes()
		if elapsed > sessionMinutes {
			sessionMinutes = elapsed
		}
		// Clean up
		a.mu.Lock()
		delete(a.sandboxStarted, providerSessionID)
		a.mu.Unlock()
	}

	return &basegate.SessionCloseResult{
		FinalUsage: &relaycommon.ProviderUsage{
			SessionMinutes: sessionMinutes,
			BillableUnits:  sessionMinutes,
			BillableUnit:   "minute",
		},
	}, nil
}

// GetSessionStatus checks if an E2B sandbox is still running.
// GET https://api.e2b.app/sandboxes/{sandboxID}
func (a *E2BSandboxAdapter) GetSessionStatus(providerSessionID string) (*basegate.SessionStatusResult, error) {
	httpReq, err := http.NewRequest("GET", a.baseURL+"/sandboxes/"+providerSessionID, nil)
	if err != nil {
		return nil, err
	}
	a.setAuth(httpReq)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &basegate.SessionStatusResult{Status: "closed"}, nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var sandbox struct {
		State string `json:"state"` // "running" or "paused"
		EndAt string `json:"endAt"`
	}
	if err := common.Unmarshal(respBody, &sandbox); err != nil {
		return nil, err
	}

	status := "idle"
	if sandbox.State == "paused" {
		status = "paused"
	}

	return &basegate.SessionStatusResult{
		Status: status,
	}, nil
}

func (a *E2BSandboxAdapter) setAuth(req *http.Request) {
	req.Header.Set("X-API-Key", a.apiKey)
}

// envdBaseURL returns the envd runtime URL for a given sandbox.
// The envd daemon listens on port 49983 of the sandbox subdomain.
func envdBaseURL(sandboxID string) string {
	return fmt.Sprintf("https://49983-%s.e2b.app", sandboxID)
}
