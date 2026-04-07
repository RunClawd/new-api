package basegate

import (
	"fmt"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// LegacyTaskBridge is a minimal interface extracted from channel.TaskAdaptor.
// This avoids importing relay/channel (which has an import cycle back to service).
type LegacyTaskBridge interface {
	GetModelList() []string
	GetChannelName() string
}

// LegacyTaskAdaptorWrapper bridges an existing TaskAdaptor to the ProviderAdapter interface.
type LegacyTaskAdaptorWrapper struct {
	Inner        LegacyTaskBridge
	PlatformName string
}

var _ ProviderAdapter = (*LegacyTaskAdaptorWrapper)(nil)

func (w *LegacyTaskAdaptorWrapper) Name() string {
	if w.PlatformName != "" {
		return "legacy_task_" + w.PlatformName
	}
	return "legacy_task_" + w.Inner.GetChannelName()
}

func (w *LegacyTaskAdaptorWrapper) DescribeCapabilities() []relaycommon.CapabilityBinding {
	models := w.Inner.GetModelList()
	bindings := make([]relaycommon.CapabilityBinding, 0, len(models))
	for _, m := range models {
		bindings = append(bindings, relaycommon.CapabilityBinding{
			CapabilityPattern: m,
			AdapterName:       w.Name(),
			Provider:          w.Inner.GetChannelName(),
			UpstreamModel:     m,
			SupportsAsync:     true,
			SupportsStreaming:  false,
		})
	}
	return bindings
}

func (w *LegacyTaskAdaptorWrapper) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	return &relaycommon.ValidationResult{Valid: true, ResolvedModel: req.Model}
}

func (w *LegacyTaskAdaptorWrapper) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	return &relaycommon.AdapterResult{Status: "accepted"}, nil
}

func (w *LegacyTaskAdaptorWrapper) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("poll must be called through the orchestrator")
}

func (w *LegacyTaskAdaptorWrapper) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return &relaycommon.AdapterResult{Status: "canceled"}, nil
}

func (w *LegacyTaskAdaptorWrapper) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	return nil, ErrStreamNotSupported
}

// IsLegacyTaskWrapper returns true if the given adapter is a LegacyTaskAdaptorWrapper.
func IsLegacyTaskWrapper(adapter ProviderAdapter) bool {
	_, ok := adapter.(*LegacyTaskAdaptorWrapper)
	return ok
}

// GetInnerTaskBridge extracts the inner LegacyTaskBridge from a wrapper.
func GetInnerTaskBridge(adapter ProviderAdapter) (LegacyTaskBridge, bool) {
	w, ok := adapter.(*LegacyTaskAdaptorWrapper)
	if !ok {
		return nil, false
	}
	return w.Inner, true
}
