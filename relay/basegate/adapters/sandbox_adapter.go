package adapters

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// DummySandboxAdapter implements SessionCapableAdapter and CallbackCapableAdapter
// purely for deterministic testing of the BaseGate session and routing engine.
type DummySandboxAdapter struct {
	NameID string
}

func (a *DummySandboxAdapter) Name() string {
	return a.NameID
}

func (a *DummySandboxAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	// Register specific names so LookupAdapters (exact-match) can find them.
	// A wildcard like "bg.sandbox.*" would NOT match a lookup for "bg.sandbox.session.standard".
	return []relaycommon.CapabilityBinding{
		{CapabilityPattern: "bg.sandbox.session.standard", AdapterName: a.NameID},
		{CapabilityPattern: "bg.sandbox.session.fast", AdapterName: a.NameID},
		{CapabilityPattern: "bg.sandbox.python", AdapterName: a.NameID},
	}
}

func (a *DummySandboxAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	return &relaycommon.ValidationResult{
		Valid: true,
	}
}

func (a *DummySandboxAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	return nil, basegate.ErrStreamNotSupported
}

func (a *DummySandboxAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	return nil, basegate.ErrStreamNotSupported
}

func (a *DummySandboxAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return &relaycommon.AdapterResult{Status: "succeeded"}, nil
}

func (a *DummySandboxAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return &relaycommon.AdapterResult{Status: "canceled"}, nil
}

// Session Methods

func (a *DummySandboxAdapter) CreateSession(req *relaycommon.CanonicalRequest) (*relaycommon.SessionResult, error) {
	return &relaycommon.SessionResult{
		SessionID: fmt.Sprintf("mock_sess_%d", time.Now().UnixNano()),
		LiveURL:   "ws://localhost/mock",
		ExpiresAt: time.Now().Unix() + 3600,
	}, nil
}

func (a *DummySandboxAdapter) ExecuteAction(providerSessionID string, action *basegate.SessionActionRequest) (*basegate.SessionActionResult, error) {
	return &basegate.SessionActionResult{
		ActionID: fmt.Sprintf("mock_act_%d", time.Now().UnixNano()),
		Status:   "succeeded",
		Output:   map[string]string{"result": fmt.Sprintf("executed %s", action.Action)},
		Usage: &relaycommon.ProviderUsage{
			BillableUnits: 1,
			BillableUnit:  "action",
		},
	}, nil
}

func (a *DummySandboxAdapter) CloseSession(providerSessionID string) (*basegate.SessionCloseResult, error) {
	return &basegate.SessionCloseResult{
		FinalUsage: &relaycommon.ProviderUsage{
			SessionMinutes: 5.0,
			BillableUnit:   "minute",
			BillableUnits:  5.0,
		},
	}, nil
}

func (a *DummySandboxAdapter) GetSessionStatus(providerSessionID string) (*basegate.SessionStatusResult, error) {
	return &basegate.SessionStatusResult{
		Status: "idle",
	}, nil
}

// Callback Method

func (a *DummySandboxAdapter) ParseCallback(req *http.Request) (*relaycommon.AdapterResult, error) {
	// Parse dummy callback directly returning success
	return &relaycommon.AdapterResult{
		Status: "succeeded",
		Output: []relaycommon.OutputItem{
			{Type: "text", Content: "Dummy callback payload"},
		},
	}, nil
}
