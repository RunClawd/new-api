package basegate

import (
	"fmt"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ProviderAdapter is the unified interface for all BaseGate capability providers.
type ProviderAdapter interface {
	Name() string
	DescribeCapabilities() []relaycommon.CapabilityBinding
	Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult
	Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error)
	Poll(providerRequestID string) (*relaycommon.AdapterResult, error)
	Cancel(providerRequestID string) (*relaycommon.AdapterResult, error)
	Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error)
}

// SessionCapableAdapter extends ProviderAdapter for stateful session capabilities.
type SessionCapableAdapter interface {
	ProviderAdapter
	CreateSession(req *relaycommon.CanonicalRequest) (*relaycommon.SessionResult, error)
	ExecuteAction(providerSessionID string, action *SessionActionRequest) (*SessionActionResult, error)
	CloseSession(providerSessionID string) (*SessionCloseResult, error)
	GetSessionStatus(providerSessionID string) (*SessionStatusResult, error)
}

type SessionActionRequest struct {
	Action    string      `json:"action"`
	Input     interface{} `json:"input"`
	TimeoutMs int         `json:"timeout_ms,omitempty"`
}

type SessionActionResult struct {
	ActionID string                     `json:"action_id,omitempty"`
	Status   string                     `json:"status"`
	Output   interface{}                `json:"output,omitempty"`
	Usage    *relaycommon.ProviderUsage `json:"usage,omitempty"`
	Error    *relaycommon.AdapterError  `json:"error,omitempty"`
}

type SessionCloseResult struct {
	FinalUsage *relaycommon.ProviderUsage `json:"final_usage,omitempty"`
}

type SessionStatusResult struct {
	Status    string                     `json:"status"`
	Usage     *relaycommon.ProviderUsage `json:"usage,omitempty"`
	ExpiresAt int64                      `json:"expires_at,omitempty"`
}

// ErrStreamNotSupported is returned by adapters that don't support streaming.
var ErrStreamNotSupported = fmt.Errorf("streaming is not supported by this adapter")
