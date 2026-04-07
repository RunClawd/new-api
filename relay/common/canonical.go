package common

import (
	"crypto/rand"
	"math/big"
	"time"
)

// CanonicalRequest is the platform-normalized request passed to ProviderAdapter.
type CanonicalRequest struct {
	// Identity
	RequestID      string `json:"request_id"`
	ResponseID     string `json:"response_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`

	// Tenant context
	OrgID      int    `json:"org_id"`
	ProjectID  int    `json:"project_id"`
	ApiKeyID   int    `json:"api_key_id"`
	EndUserID  string `json:"end_user_id,omitempty"`

	// Capability
	Model  string            `json:"model"`
	Domain string            `json:"domain,omitempty"`
	Action string            `json:"action,omitempty"`
	Tier   string            `json:"tier,omitempty"`

	// Input
	Input interface{} `json:"input"`

	// Execution
	ExecutionOptions ExecutionOptions `json:"execution_options"`

	// Billing
	BillingContext BillingContext `json:"billing_context"`

	// Metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ExecutionOptions controls how the request is executed.
type ExecutionOptions struct {
	Mode       string `json:"mode"`                  // sync | async | stream | session
	WebhookURL string `json:"webhook_url,omitempty"`
	TimeoutMs  int    `json:"timeout_ms,omitempty"`
}

// BillingContext provides billing-related information for the request.
type BillingContext struct {
	BillingMode     string `json:"billing_mode"`                // hosted | byo
	BYOCredentialID string `json:"byo_credential_id,omitempty"`
}

// AdapterResult is the standardized response from a ProviderAdapter.
type AdapterResult struct {
	// Status
	Status    string `json:"status"`     // succeeded | failed | running | queued | accepted
	IsPartial bool   `json:"is_partial"` // true if more data coming (polling)

	// Provider tracking
	ProviderRequestID string `json:"provider_request_id,omitempty"`

	// Output (for terminal success)
	Output []OutputItem `json:"output,omitempty"`

	// Error (for terminal failure)
	Error *AdapterError `json:"error,omitempty"`

	// Usage (raw provider usage)
	RawUsage *ProviderUsage `json:"raw_usage,omitempty"`

	// Poll hint
	PollAfterMs int `json:"poll_after_ms,omitempty"` // suggested next poll interval

	// Session (for session-mode responses)
	Session *SessionResult `json:"session,omitempty"`
}

// OutputItem represents a single output element.
type OutputItem struct {
	Type    string      `json:"type"`    // text | image | video | audio | file | session | tool_call
	Content interface{} `json:"content"` // string or structured object
}

// AdapterError represents an error from the provider.
type AdapterError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// ProviderUsage represents raw usage data from a provider.
type ProviderUsage struct {
	// LLM usage
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`

	// Media usage
	DurationSec float64 `json:"duration_sec,omitempty"`
	FileSizeMB  float64 `json:"file_size_mb,omitempty"`
	Actions     int     `json:"actions,omitempty"`

	// Session usage
	SessionMinutes float64 `json:"session_minutes,omitempty"`

	// Generic
	BillableUnits float64 `json:"billable_units,omitempty"`
	BillableUnit  string  `json:"billable_unit,omitempty"` // token | second | minute | action | request
}

// CanonicalUsage is the platform-normalized usage.
type CanonicalUsage struct {
	BillableUnits float64 `json:"billable_units"`
	BillableUnit  string  `json:"billable_unit"` // token | second | minute | action | request

	// Detailed breakdown
	InputUnits  float64 `json:"input_units,omitempty"`
	OutputUnits float64 `json:"output_units,omitempty"`

	// Original raw usage reference
	RawUsage *ProviderUsage `json:"raw_usage,omitempty"`
}

// PricingSnapshot captures pricing at invocation time (immutable).
type PricingSnapshot struct {
	BillingMode  string  `json:"billing_mode"`  // metered | per_call | subscription
	BillableUnit string  `json:"billable_unit"` // token | second | minute | action | request
	UnitPrice    float64 `json:"unit_price"`
	Currency     string  `json:"currency"`
}

// SessionResult returned when a session-mode capability is invoked.
type SessionResult struct {
	SessionID  string `json:"session_id"`
	SessionURL string `json:"session_url,omitempty"` // /v1/sessions/:id
	LiveURL    string `json:"live_url,omitempty"`    // e.g. wss://browser.example.com/sess_456
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

// CapabilityBinding maps a capability name to a provider adapter.
type CapabilityBinding struct {
	CapabilityPattern string  `json:"capability_pattern"`
	AdapterName       string  `json:"adapter_name"`
	Provider          string  `json:"provider"`
	UpstreamModel     string  `json:"upstream_model"`
	Priority          int     `json:"priority"`
	Weight            int     `json:"weight"`
	Region            string  `json:"region,omitempty"`
	CostPerUnit       float64 `json:"cost_per_unit,omitempty"`
	SupportsStreaming  bool    `json:"supports_streaming"`
	SupportsAsync     bool    `json:"supports_async"`
	MaxConcurrency    int     `json:"max_concurrency,omitempty"`
}

// ValidationResult from ProviderAdapter.Validate.
type ValidationResult struct {
	Valid   bool         `json:"valid"`
	Error   *AdapterError `json:"error,omitempty"`
	// Resolved values that the adapter determined
	ResolvedModel string `json:"resolved_model,omitempty"` // actual upstream model name
}

// GenerateResponseID generates a unique response ID with "resp_" prefix.
func GenerateResponseID() string {
	return "resp_" + generateNanoID(20)
}

// GenerateAttemptID generates a unique attempt ID with "att_" prefix.
func GenerateAttemptID() string {
	return "att_" + generateNanoID(20)
}

// GenerateSessionID generates a unique session ID with "sess_" prefix.
func GenerateSessionID() string {
	return "sess_" + generateNanoID(20)
}

// GenerateUsageID generates a unique usage record ID.
func GenerateUsageID() string {
	return "usg_" + generateNanoID(20)
}

// GenerateActionID generates a unique session action ID.
func GenerateActionID() string {
	return "act_" + generateNanoID(20)
}

// GenerateBillingID generates a unique billing record ID.
func GenerateBillingID() string {
	return "bill_" + generateNanoID(20)
}

// GenerateLedgerEntryID generates a unique ledger entry ID.
func GenerateLedgerEntryID() string {
	return "led_" + generateNanoID(20)
}

// GenerateEventID generates a unique webhook event ID.
func GenerateEventID() string {
	return "evt_" + generateNanoID(20)
}

// GenerateProjectID generates a unique project ID with "proj_" prefix.
func GenerateProjectID() string {
	return "proj_" + generateNanoID(20)
}

// NowUnix returns current Unix timestamp.
func NowUnix() int64 {
	return time.Now().Unix()
}

// generateNanoID generates a URL-safe random string of given length using crypto/rand.
func generateNanoID(length int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	alphaLen := big.NewInt(int64(len(alphabet)))
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, alphaLen)
		if err != nil {
			// Fallback: should never happen with crypto/rand
			b[i] = alphabet[i%len(alphabet)]
			continue
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}
