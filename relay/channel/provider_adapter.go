package channel

import (
	"fmt"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ProviderAdapter is the unified interface for all BaseGate capability providers.
// Every provider — whether native or wrapped from legacy Adaptor/TaskAdaptor — must
// implement this 7-method interface.
//
// Lifecycle:
//   Orchestrator → Validate → Invoke/Stream → (Poll if async) → Cancel
//
// The adapter is stateless; all state is managed by the Orchestrator + State Machine.
type ProviderAdapter interface {
	// Name returns the adapter's unique identifier (e.g. "openai_gpt4", "kling_video").
	Name() string

	// DescribeCapabilities returns the list of capability bindings this adapter serves.
	// Used by the capability registry to build the model → adapter mapping.
	DescribeCapabilities() []relaycommon.CapabilityBinding

	// Validate checks whether the adapter can handle the given request.
	// Returns a ValidationResult indicating whether the request is valid
	// and any resolved values (e.g. upstream model name).
	Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult

	// Invoke sends the request to the provider and returns the result.
	// For sync capabilities: returns the final result immediately.
	// For async capabilities: returns a queued/accepted status with provider_request_id
	//   for subsequent polling.
	Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error)

	// Poll checks the status of an async operation by its provider-side ID.
	// Returns the current status. If terminal, includes output and usage.
	Poll(providerRequestID string) (*relaycommon.AdapterResult, error)

	// Cancel requests cancellation of an in-progress operation.
	// Not all providers support cancellation; adapters may return a no-op result.
	Cancel(providerRequestID string) (*relaycommon.AdapterResult, error)

	// Stream initiates a streaming invocation and returns a channel of SSE events.
	// The channel is closed when the stream completes or errors.
	// For adapters that don't support streaming, return (nil, ErrStreamNotSupported).
	Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error)
}

// SessionCapableAdapter extends ProviderAdapter for stateful session capabilities
// (browser, sandbox, proxy).
type SessionCapableAdapter interface {
	ProviderAdapter

	// CreateSession creates a new session with the provider.
	CreateSession(req *relaycommon.CanonicalRequest) (*relaycommon.SessionResult, error)

	// ExecuteAction executes an action within an existing session.
	ExecuteAction(providerSessionID string, action *SessionActionRequest) (*SessionActionResult, error)

	// CloseSession closes an active session and returns final usage.
	CloseSession(providerSessionID string) (*SessionCloseResult, error)

	// GetSessionStatus queries the current status of a session.
	GetSessionStatus(providerSessionID string) (*SessionStatusResult, error)
}

// SessionActionRequest is sent to SessionCapableAdapter.ExecuteAction.
type SessionActionRequest struct {
	Action    string      `json:"action"`
	Input     interface{} `json:"input"`
	TimeoutMs int         `json:"timeout_ms,omitempty"`
}

// SessionActionResult is returned from SessionCapableAdapter.ExecuteAction.
type SessionActionResult struct {
	ActionID string                  `json:"action_id,omitempty"`
	Status   string                  `json:"status"` // succeeded | failed
	Output   interface{}             `json:"output,omitempty"`
	Usage    *relaycommon.ProviderUsage `json:"usage,omitempty"`
	Error    *relaycommon.AdapterError  `json:"error,omitempty"`
}

// SessionCloseResult is returned from SessionCapableAdapter.CloseSession.
type SessionCloseResult struct {
	FinalUsage *relaycommon.ProviderUsage `json:"final_usage,omitempty"`
}

// SessionStatusResult is returned from SessionCapableAdapter.GetSessionStatus.
type SessionStatusResult struct {
	Status    string                  `json:"status"` // active | idle | expired | closed | failed
	Usage     *relaycommon.ProviderUsage `json:"usage,omitempty"`
	ExpiresAt int64                   `json:"expires_at,omitempty"`
}

// ErrStreamNotSupported is returned by adapters that don't support streaming.
var ErrStreamNotSupported = fmt.Errorf("streaming is not supported by this adapter")
