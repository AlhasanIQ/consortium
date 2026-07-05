package runtime

import (
	"testing"
	"time"
)

func TestHistoryEventType_IsTerminal(t *testing.T) {
	terminal := []HistoryEventType{
		HistoryWorkflowCompleted,
		HistoryWorkflowFailed,
		HistoryWorkflowCancelled,
		HistoryWorkflowTimedOut,
	}
	for _, et := range terminal {
		if !et.IsTerminal() {
			t.Errorf("%q should be terminal", et)
		}
	}

	nonTerminal := []HistoryEventType{
		HistoryWorkflowStarted,
		HistoryScheduleActivity,
		HistoryActivityStarted,
		HistoryActivityCompleted,
		HistoryActivityFailed,
		HistoryNodeReady,
		HistoryNodeComplete,
		HistoryNodeFailed,
		HistorySignalReceived,
		HistoryTimerStarted,
		HistoryTimerFired,
		HistoryChangeVersionMarker,
		HistoryEventType("unknown_type"),
	}
	for _, et := range nonTerminal {
		if et.IsTerminal() {
			t.Errorf("%q should NOT be terminal", et)
		}
	}
}

func TestHistoryEventType_IsActivityTerminal(t *testing.T) {
	terminal := []HistoryEventType{
		HistoryActivityCompleted,
		HistoryActivityFailed,
		HistoryActivityTimedOut,
	}
	for _, et := range terminal {
		if !et.IsActivityTerminal() {
			t.Errorf("%q should be activity-terminal", et)
		}
	}

	nonTerminal := []HistoryEventType{
		HistoryActivityStarted,
		HistoryActivityHeartbeat,
		HistoryWorkflowCompleted,
		HistoryWorkflowFailed,
		HistoryScheduleActivity,
		HistoryNodeReady,
		HistoryEventType("something"),
	}
	for _, et := range nonTerminal {
		if et.IsActivityTerminal() {
			t.Errorf("%q should NOT be activity-terminal", et)
		}
	}
}

func TestHistoryEvent_Fields(t *testing.T) {
	now := time.Now()
	evt := HistoryEvent{
		ID:         1,
		RunID:      "run-abc",
		Sequence:   5,
		Type:       HistoryActivityCompleted,
		NodeID:     "node-1",
		ActivityID: "act-1",
		Timestamp:  now,
		Attributes: map[string]interface{}{"key": "value"},
	}

	if evt.ID != 1 {
		t.Errorf("ID = %d, want 1", evt.ID)
	}
	if evt.RunID != "run-abc" {
		t.Errorf("RunID = %q, want %q", evt.RunID, "run-abc")
	}
	if evt.Sequence != 5 {
		t.Errorf("Sequence = %d, want 5", evt.Sequence)
	}
	if evt.Type != HistoryActivityCompleted {
		t.Errorf("Type = %q, want %q", evt.Type, HistoryActivityCompleted)
	}
	if evt.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", evt.NodeID, "node-1")
	}
	if evt.ActivityID != "act-1" {
		t.Errorf("ActivityID = %q, want %q", evt.ActivityID, "act-1")
	}
	if !evt.Timestamp.Equal(now) {
		t.Errorf("Timestamp mismatch")
	}
	if evt.Attributes["key"] != "value" {
		t.Errorf("Attributes[key] = %v, want %q", evt.Attributes["key"], "value")
	}
}

func TestHistoryEventType_Constants(t *testing.T) {
	// Verify constant string values match expected taxonomy
	checks := map[HistoryEventType]string{
		HistoryWorkflowStarted:     "workflow_started",
		HistoryWorkflowCompleted:   "workflow_completed",
		HistoryWorkflowFailed:      "workflow_failed",
		HistoryWorkflowCancelled:   "workflow_cancelled",
		HistoryWorkflowTimedOut:    "workflow_timed_out",
		HistoryScheduleActivity:    "schedule_activity",
		HistoryCancelActivity:      "cancel_activity",
		HistoryStartChildRun:       "start_child_run",
		HistoryRequestCancelChild:  "request_cancel_child",
		HistoryActivityStarted:     "activity_started",
		HistoryActivityCompleted:   "activity_completed",
		HistoryActivityFailed:      "activity_failed",
		HistoryActivityTimedOut:    "activity_timed_out",
		HistoryActivityHeartbeat:   "activity_heartbeat",
		HistoryNodeReady:           "node_ready",
		HistoryNodeComplete:        "node_complete",
		HistoryNodeFailed:          "node_failed",
		HistorySignalReceived:      "signal_received",
		HistoryUpdateReceived:      "update_received",
		HistoryUpdateAccepted:      "update_accepted",
		HistoryUpdateRejected:      "update_rejected",
		HistoryChangeVersionMarker: "change_version_marker",
		HistoryTimerStarted:        "timer_started",
		HistoryTimerFired:          "timer_fired",
	}
	for et, want := range checks {
		if string(et) != want {
			t.Errorf("constant %v = %q, want %q", et, string(et), want)
		}
	}
}
