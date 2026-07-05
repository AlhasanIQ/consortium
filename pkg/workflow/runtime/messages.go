package runtime

// Signal represents an external signal delivered to a running execution.
type Signal struct {
	Name    string                 `json:"name"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// Update represents a validated mutation request to a running execution.
type Update struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// UpdateResult is the response to an update request.
type UpdateResult struct {
	Accepted bool                   `json:"accepted"`
	Result   map[string]interface{} `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// Query is a read-only introspection of execution state.
type Query struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// QueryResult is the response to a query.
type QueryResult struct {
	Result map[string]interface{} `json:"result,omitempty"`
}

// Built-in signal names.
const (
	SignalPause       = "pause"
	SignalResume      = "resume"
	SignalCancel      = "cancel"
	SignalInjectInput = "inject_input"
)

// Built-in query names.
const (
	QueryStatus   = "status"
	QueryProgress = "progress"
	QueryDebug    = "debug"
)
