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
	OrgID     int    `json:"org_id"`
	ProjectID int    `json:"project_id"`
	ApiKeyID  int    `json:"api_key_id"`
	EndUserID string `json:"end_user_id,omitempty"`

	// Capability
	Model  string `json:"model"`
	Domain string `json:"domain,omitempty"`
	Action string `json:"action,omitempty"`
	Tier   string `json:"tier,omitempty"`

	// Input
	Input interface{} `json:"input"`

	// Execution
	ExecutionOptions ExecutionOptions `json:"execution_options"`

	// Billing
	BillingContext BillingContext `json:"billing_context"`

	// Metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// CredentialOverride is set per-attempt by the orchestrator—NOT by ResolveRoute.
	CredentialOverride *CredentialOverride `json:"-"` // transient, never serialized
}

// ExecutionOptions controls how the request is executed.
type ExecutionOptions struct {
	Mode       string `json:"mode"` // sync | async | stream | session
	WebhookURL string `json:"webhook_url,omitempty"`
	TimeoutMs  int    `json:"timeout_ms,omitempty"`
}

// BillingContext provides billing-related information for the request.
type BillingContext struct {
	BillingSource   string `json:"billing_source"` // hosted | byo
	BYOCredentialID int64  `json:"byo_credential_id,omitempty"`
}

// CredentialOverride allows BYO credentials to be injected per-adapter.
type CredentialOverride struct {
	APIKey         string `json:"api_key,omitempty"`
	ServiceAccount string `json:"service_account,omitempty"` // future: Vertex AI
}

// BYOFeeConfig defines the BYO platform fee calculation parameters.
type BYOFeeConfig struct {
	FeeType        string  `json:"fee_type"`        // per_request | percentage
	FixedAmount    float64 `json:"fixed_amount"`    // for per_request: $ per request
	PercentageRate float64 `json:"percentage_rate"` // for percentage: 0.10 = 10%
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
	// LLM usage — legacy-compatible summary fields
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`

	// Explicit pricing buckets — used by FinalizeBilling for differentiated pricing.
	// Adapters populate these from provider-specific response fields.
	InputTokens           int `json:"input_tokens,omitempty"`             // pure non-cache prompt tokens
	CachedTokens          int `json:"cached_tokens,omitempty"`            // cache hit / cache read
	CacheCreationTokens   int `json:"cache_creation_tokens,omitempty"`    // total cache write
	CacheCreationTokens5m int `json:"cache_creation_tokens_5m,omitempty"` // Claude 5-min cache write
	CacheCreationTokens1h int `json:"cache_creation_tokens_1h,omitempty"` // Claude 1-hour cache write

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
// Ratio fields use omitempty so old snapshots (without ratios) deserialize with zero values.
// FinalizeBilling applies normalizePricingSnapshot to treat 0 as "use default".
type PricingSnapshot struct {
	PricingMode  string  `json:"pricing_mode"`  // metered | per_call | subscription
	BillableUnit string  `json:"billable_unit"` // token | second | minute | action | request
	UnitPrice    float64 `json:"unit_price"`    // input/base per-token price
	Currency     string  `json:"currency"`

	// Differentiated pricing ratios (relative to UnitPrice).
	// Zero means "not set" — normalizePricingSnapshot fills defaults.
	CompletionRatio      float64 `json:"completion_ratio,omitempty"`        // output multiplier (e.g. 4.0 for GPT-4o)
	CacheRatio           float64 `json:"cache_ratio,omitempty"`             // cache hit multiplier (e.g. 0.1 for Claude)
	CacheCreationRatio   float64 `json:"cache_creation_ratio,omitempty"`    // cache write multiplier (e.g. 1.25 for Claude)
	CacheCreation5mRatio float64 `json:"cache_creation_5m_ratio,omitempty"` // Claude 5-min cache write ratio
	CacheCreation1hRatio float64 `json:"cache_creation_1h_ratio,omitempty"` // Claude 1-hour cache write ratio
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
	SupportsStreaming bool    `json:"supports_streaming"`
	SupportsAsync     bool    `json:"supports_async"`
	MaxConcurrency    int     `json:"max_concurrency,omitempty"`
}

// ValidationResult from ProviderAdapter.Validate.
type ValidationResult struct {
	Valid bool          `json:"valid"`
	Error *AdapterError `json:"error,omitempty"`
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
