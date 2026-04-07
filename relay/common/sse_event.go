package common

// SSEEventType defines the standard SSE event types for BaseGate streaming.
type SSEEventType string

const (
	// Response lifecycle events
	SSEEventResponseCreated    SSEEventType = "response.created"
	SSEEventResponseInProgress SSEEventType = "response.in_progress"
	SSEEventResponseSucceeded  SSEEventType = "response.succeeded"
	SSEEventResponseFailed     SSEEventType = "response.failed"
	SSEEventResponseCanceled   SSEEventType = "response.canceled"

	// Output events
	SSEEventOutputItemAdded SSEEventType = "response.output_item.added"
	SSEEventOutputItemDone  SSEEventType = "response.output_item.done"

	// Text streaming
	SSEEventTextDelta SSEEventType = "response.output_text.delta"
	SSEEventTextDone  SSEEventType = "response.output_text.done"

	// Progress (async tasks)
	SSEEventProgress SSEEventType = "response.progress"

	// Error
	SSEEventError SSEEventType = "error"

	// Done marker
	SSEEventDone SSEEventType = "[DONE]"
)

// SSEEvent represents a single server-sent event.
type SSEEvent struct {
	Type SSEEventType `json:"type"`
	Data interface{}  `json:"data,omitempty"`
}

// TextDeltaData is the payload for response.output_text.delta events.
type TextDeltaData struct {
	ItemIndex  int    `json:"item_index"`
	Delta      string `json:"delta"`
	OutputText string `json:"output_text,omitempty"` // accumulated text so far
}

// ProgressData is the payload for response.progress events.
type ProgressData struct {
	Percent    int    `json:"percent,omitempty"`
	Message    string `json:"message,omitempty"`
	Stage      string `json:"stage,omitempty"`
}

// ErrorData is the payload for error events.
type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
