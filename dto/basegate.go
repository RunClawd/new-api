package dto

// BaseGateRequest is the API-facing request body for POST /v1/responses.
type BaseGateRequest struct {
	Model            string                 `json:"model" binding:"required"`
	Input            interface{}            `json:"input"`
	ExecutionOptions *BGExecutionOptions    `json:"execution_options,omitempty"`
	ResponseFormat   *BGResponseFormat      `json:"response_format,omitempty"`
	Metadata         map[string]string      `json:"metadata,omitempty"`
}

// BGExecutionOptions controls execution behavior.
type BGExecutionOptions struct {
	Mode       string `json:"mode,omitempty"`        // sync | async | stream | session
	WebhookURL string `json:"webhook_url,omitempty"`
	TimeoutMs  int    `json:"timeout_ms,omitempty"`
}

// BGResponseFormat specifies desired output format.
type BGResponseFormat struct {
	Type       string      `json:"type,omitempty"`        // json_schema | text | auto
	JSONSchema interface{} `json:"json_schema,omitempty"`
}

// BaseGateResponse is the API-facing response body.
type BaseGateResponse struct {
	ID        string         `json:"id"`
	Object    string         `json:"object"` // "response"
	CreatedAt int64          `json:"created_at"`
	Status    string         `json:"status"`
	Model     string         `json:"model"`
	Output    []BGOutputItem `json:"output,omitempty"`
	Usage     *BGUsage       `json:"usage,omitempty"`
	Pricing   *BGPricing     `json:"pricing,omitempty"`
	Error     *BGError       `json:"error,omitempty"`
	Meta      *BGMeta        `json:"meta,omitempty"`
	// PollURL is the relative URL to poll for async responses.
	// Only present when status is non-terminal (accepted, queued, running).
	PollURL string `json:"poll_url,omitempty"`
}

// BGOutputItem is a single element in the response output array.
type BGOutputItem struct {
	Type    string      `json:"type"`             // text | image | video | audio | file | session | tool_call
	Content interface{} `json:"content"`
	Role    string      `json:"role,omitempty"` // assistant | system | tool
}

// BGUsage represents normalized usage in the API response.
type BGUsage struct {
	BillableUnits float64 `json:"billable_units"`
	BillableUnit  string  `json:"billable_unit"`
	InputUnits    float64 `json:"input_units,omitempty"`
	OutputUnits   float64 `json:"output_units,omitempty"`
}

// BGPricing represents pricing information in the API response.
type BGPricing struct {
	BillingMode  string  `json:"billing_mode"`
	BillableUnit string  `json:"billable_unit"`
	UnitPrice    float64 `json:"unit_price"`
	Total        float64 `json:"total"`
	Currency     string  `json:"currency"`
}

// BGError represents an error in the API response.
type BGError struct {
	// Type is an OpenAI-compatible discriminator (e.g. invalid_request_error, api_error, rate_limit_error).
	Type    string `json:"type,omitempty"`
	// Code is a BaseGate-internal error code (e.g. provider_unavailable, billing_failed, adapter_timeout).
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param,omitempty"` // OpenAI-compatible parameter name that caused the error
	Detail  string `json:"detail,omitempty"`
}

// BGMeta contains metadata about the response execution.
type BGMeta struct {
	RequestID string `json:"request_id,omitempty"`
	Provider  string `json:"provider,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	AttemptNo int    `json:"attempt_no,omitempty"`
}

// BGSessionOutput is the content when output type is "session".
type BGSessionOutput struct {
	SessionID  string `json:"session_id"`
	SessionURL string `json:"session_url"`
	LiveURL    string `json:"live_url,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

// BGSessionActionRequest is the API-facing request for POST /v1/sessions/:id/actions.
type BGSessionActionRequest struct {
	Action           string                 `json:"action" binding:"required"`
	Input            interface{}            `json:"input"`
	IdempotencyKey   string                 `json:"idempotency_key,omitempty"` // Idempotency key for action retry
	ExecutionOptions *BGExecutionOptions    `json:"execution_options,omitempty"`
}

// BGSessionActionResponse is the API-facing response for session actions.
type BGSessionActionResponse struct {
	ID        string      `json:"id"`
	Object    string      `json:"object"` // "session_action"
	SessionID string      `json:"session_id"`
	Status    string      `json:"status"`
	Output    interface{} `json:"output,omitempty"`
	Usage     *BGUsage    `json:"usage,omitempty"`
	Error     *BGError    `json:"error,omitempty"`
}

// BGSessionResponse is the API-facing session object.
type BGSessionResponse struct {
	ID             string   `json:"id"`
	Object         string   `json:"object"` // "session"
	CreatedAt      int64    `json:"created_at"`
	Status         string   `json:"status"`
	Model          string   `json:"model"`
	ResponseID     string   `json:"response_id"`
	Usage          *BGUsage `json:"usage,omitempty"`
	ExpiresAt      int64    `json:"expires_at,omitempty"`
	IdleTimeoutSec int      `json:"idle_timeout_sec,omitempty"`
	MaxDurationSec int      `json:"max_duration_sec,omitempty"`
	LastActionAt   int64    `json:"last_action_at,omitempty"`
	ClosedAt       int64    `json:"closed_at,omitempty"`
}

// BGModelObject is the enhanced model object for /v1/models (BaseGate capability format).
type BGModelObject struct {
	ID          string          `json:"id"`
	Object      string          `json:"object"` // "model"
	OwnedBy    string          `json:"owned_by"`
	Capability *BGCapability   `json:"capability,omitempty"`
	Execution  *BGExecution    `json:"execution,omitempty"`
	Pricing    *BGModelPricing `json:"pricing,omitempty"`
	Limits     *BGModelLimits  `json:"limits,omitempty"`
}

// BGCapability describes a model's capability classification.
type BGCapability struct {
	Domain string `json:"domain"` // llm | video | audio | image | browser | sandbox
	Action string `json:"action"` // chat | generate | upscale | session | ...
	Tier   string `json:"tier"`   // standard | premium | ...
}

// BGExecution describes execution modes supported.
type BGExecution struct {
	Mode       string `json:"mode"`       // sync | async | hybrid | session
	Streaming  bool   `json:"streaming"`
	Idempotent bool   `json:"idempotent"`
	MaxDurSec  int    `json:"max_duration_sec,omitempty"`
}

// BGModelPricing describes model pricing.
type BGModelPricing struct {
	BillingMode  string  `json:"billing_mode"`  // metered | per_call
	BillableUnit string  `json:"billable_unit"` // token | second | minute | request
	InputPrice   float64 `json:"input_price,omitempty"`
	OutputPrice  float64 `json:"output_price,omitempty"`
	UnitPrice    float64 `json:"unit_price,omitempty"`
	Currency     string  `json:"currency"`
}

// BGModelLimits describes model usage limits.
type BGModelLimits struct {
	MaxInputTokens  int `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	MaxDurationSec  int `json:"max_duration_sec,omitempty"`
	MaxFileSizeMB   int `json:"max_file_size_mb,omitempty"`
}
