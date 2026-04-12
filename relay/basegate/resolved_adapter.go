package basegate

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ResolvedAdapter represents an instantiated capability with routing metadata
// strictly bound to a singular attempt execution.
type ResolvedAdapter struct {
	Adapter            ProviderAdapter
	CredentialOverride *relaycommon.CredentialOverride // To be overridden per attempt
	BillingSource      string                          // 'hosted' or 'byo'
	BYOCredentialID    int64                           // ID of BYO credentials
	FeeConfig          *relaycommon.BYOFeeConfig       // Attached pricing overlay (ratio or flat)
}
