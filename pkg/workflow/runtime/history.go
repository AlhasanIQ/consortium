package runtime

import "time"

// HistoryEventType is the internal event taxonomy for durable execution history.
// These events are NOT exposed on WebSocket — they are translated to existing event types.
type HistoryEventType string

const (
	// Workflow lifecycle
	HistoryWorkflowStarted   HistoryEventType = "workflow_started"
	HistoryWorkflowCompleted HistoryEventType = "workflow_completed"
	HistoryWorkflowFailed    HistoryEventType = "workflow_failed"
	HistoryWorkflowCancelled HistoryEventType = "workflow_cancelled"
	HistoryWorkflowTimedOut  HistoryEventType = "workflow_timed_out"

	// Scheduling commands
	HistoryScheduleActivity   HistoryEventType = "schedule_activity"
	HistoryCancelActivity     HistoryEventType = "cancel_activity"
	HistoryStartChildRun      HistoryEventType = "start_child_run"
	HistoryRequestCancelChild HistoryEventType = "request_cancel_child"

	// Activity lifecycle
	HistoryActivityStarted   HistoryEventType = "activity_started"
	HistoryActivityCompleted HistoryEventType = "activity_completed"
	HistoryActivityFailed    HistoryEventType = "activity_failed"
	HistoryActivityTimedOut  HistoryEventType = "activity_timed_out"
	HistoryActivityHeartbeat HistoryEventType = "activity_heartbeat"

	// Node decisions
	HistoryNodeReady    HistoryEventType = "node_ready"
	HistoryNodeComplete HistoryEventType = "node_complete"
	HistoryNodeFailed   HistoryEventType = "node_failed"

	// Messages
	HistorySignalReceived HistoryEventType = "signal_received"
	HistoryUpdateReceived HistoryEventType = "update_received"
	HistoryUpdateAccepted HistoryEventType = "update_accepted"
	HistoryUpdateRejected HistoryEventType = "update_rejected"

	// Versioning
	HistoryChangeVersionMarker HistoryEventType = "change_version_marker"

	// Timer
	HistoryTimerStarted HistoryEventType = "timer_started"
	HistoryTimerFired   HistoryEventType = "timer_fired"
)

// HistoryEvent represents a single event in the durable execution history.
// Events are immutable once written — they form the append-only log that
// enables deterministic replay.
type HistoryEvent struct {
	ID         int64                  `json:"id"`
	RunID      string                 `json:"run_id"`
	Sequence   int64                  `json:"sequence"`
	Type       HistoryEventType       `json:"type"`
	NodeID     string                 `json:"node_id,omitempty"`
	ActivityID string                 `json:"activity_id,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// IsTerminal returns true if this event type indicates workflow completion.
func (t HistoryEventType) IsTerminal() bool {
	return t == HistoryWorkflowCompleted ||
		t == HistoryWorkflowFailed ||
		t == HistoryWorkflowCancelled ||
		t == HistoryWorkflowTimedOut
}

// IsActivityTerminal returns true if this event type indicates activity completion.
func (t HistoryEventType) IsActivityTerminal() bool {
	return t == HistoryActivityCompleted ||
		t == HistoryActivityFailed ||
		t == HistoryActivityTimedOut
}
