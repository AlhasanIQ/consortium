package runtime

import (
	"context"
	"testing"
	"time"
)

func TestTaskQueue_EnqueueDequeueAndCapacity(t *testing.T) {
	q := NewTaskQueue(2)

	if err := q.Enqueue("p1", &TaskItem{ID: "t1"}); err != nil {
		t.Fatalf("enqueue t1 failed: %v", err)
	}
	if err := q.Enqueue("p1", &TaskItem{ID: "t2"}); err != nil {
		t.Fatalf("enqueue t2 failed: %v", err)
	}
	if err := q.Enqueue("p1", &TaskItem{ID: "t3"}); err == nil {
		t.Fatal("expected capacity error on third enqueue")
	}

	if got := q.Len("p1"); got != 2 {
		t.Fatalf("expected queue length 2, got %d", got)
	}

	first := q.Dequeue("p1")
	second := q.Dequeue("p1")
	if first == nil || second == nil {
		t.Fatal("expected two dequeued items")
	}
	if first.ID != "t1" || second.ID != "t2" {
		t.Fatalf("expected FIFO order [t1,t2], got [%s,%s]", first.ID, second.ID)
	}
	if first.PartitionID != "p1" || second.PartitionID != "p1" {
		t.Fatalf("expected partition id p1 on dequeued items, got [%s,%s]", first.PartitionID, second.PartitionID)
	}
}

func TestTaskQueue_CloseBehavior(t *testing.T) {
	q := NewTaskQueue(1)
	if err := q.Enqueue("p1", &TaskItem{ID: "t1"}); err != nil {
		t.Fatalf("enqueue before close failed: %v", err)
	}

	q.Close()

	if err := q.Enqueue("p1", &TaskItem{ID: "t2"}); err == nil {
		t.Fatal("expected enqueue error after close")
	}

	first := q.Dequeue("p1")
	if first == nil || first.ID != "t1" {
		t.Fatalf("expected buffered item t1 after close, got %+v", first)
	}
	if got := q.Dequeue("p1"); got != nil {
		t.Fatalf("expected nil when dequeuing closed and drained partition, got %+v", got)
	}
}

func TestHistoryEventTypeHelpers(t *testing.T) {
	terminal := []HistoryEventType{
		HistoryWorkflowCompleted,
		HistoryWorkflowFailed,
		HistoryWorkflowCancelled,
		HistoryWorkflowTimedOut,
	}
	for _, typ := range terminal {
		if !typ.IsTerminal() {
			t.Fatalf("expected %s to be terminal", typ)
		}
	}
	if HistoryActivityCompleted.IsTerminal() {
		t.Fatal("activity_completed should not be workflow terminal")
	}

	activityTerminal := []HistoryEventType{
		HistoryActivityCompleted,
		HistoryActivityFailed,
		HistoryActivityTimedOut,
	}
	for _, typ := range activityTerminal {
		if !typ.IsActivityTerminal() {
			t.Fatalf("expected %s to be activity-terminal", typ)
		}
	}
	if HistoryWorkflowCompleted.IsActivityTerminal() {
		t.Fatal("workflow_completed should not be activity-terminal")
	}
}

func TestRetryHelpers(t *testing.T) {
	if !IsRetryableCode("timeout") {
		t.Fatal("expected timeout to be retryable")
	}
	if IsRetryableCode("AUTHENTICATION") {
		t.Fatal("expected AUTHENTICATION to be non-retryable")
	}
	if IsRetryableCode("INSUFFICIENT_CREDITS") {
		t.Fatal("expected INSUFFICIENT_CREDITS to be non-retryable")
	}

	if !IsNonRetryable(context.Canceled, "") {
		t.Fatal("expected context cancellation to be non-retryable")
	}
	if !IsNonRetryable(nil, "AUTHORIZATION") {
		t.Fatal("expected AUTHORIZATION to be non-retryable")
	}
	if !IsNonRetryable(nil, "INSUFFICIENT_CREDITS") {
		t.Fatal("expected INSUFFICIENT_CREDITS to be non-retryable")
	}
	if !IsNonRetryable(nil, "AUTH_ERROR") {
		t.Fatal("expected AUTH_ERROR to be non-retryable")
	}
	if IsNonRetryable(nil, "TIMEOUT") {
		t.Fatal("expected TIMEOUT to remain retryable")
	}
}

func TestTimeoutHelpers(t *testing.T) {
	cfg := DefaultActivityTimeoutConfig()
	if cfg.ScheduleToStart != DefaultScheduleToStart {
		t.Fatalf("expected default schedule_to_start %v, got %v", DefaultScheduleToStart, cfg.ScheduleToStart)
	}
	if cfg.StartToClose != DefaultStartToClose {
		t.Fatalf("expected default start_to_close %v, got %v", DefaultStartToClose, cfg.StartToClose)
	}
	if cfg.HeartbeatTimeout != DefaultHeartbeatTimeout {
		t.Fatalf("expected default heartbeat_timeout %v, got %v", DefaultHeartbeatTimeout, cfg.HeartbeatTimeout)
	}

	if got := EffectiveStartToClose(nil, 15); got != 15*time.Second {
		t.Fatalf("expected legacy timeout 15s, got %v", got)
	}
	if got := EffectiveStartToClose(&ActivityTimeoutConfig{StartToClose: 3 * time.Second}, 30); got != 3*time.Second {
		t.Fatalf("expected explicit timeout 3s, got %v", got)
	}
	if got := EffectiveStartToClose(nil, 0); got != DefaultStartToClose {
		t.Fatalf("expected default timeout %v, got %v", DefaultStartToClose, got)
	}
}

func TestJSONCodecV1_RoundTripAndErrors(t *testing.T) {
	codec := DefaultCodec()

	type payload struct {
		Name string `json:"name"`
		Cnt  int    `json:"cnt"`
	}
	original := payload{Name: "test", Cnt: 2}
	data, err := codec.Encode(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	var decoded payload
	if err := codec.Decode(data, &decoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded != original {
		t.Fatalf("expected %+v, got %+v", original, decoded)
	}
	if codec.Version() != "json/v1" {
		t.Fatalf("expected codec version json/v1, got %s", codec.Version())
	}

	if err := codec.Decode([]byte("{not-json"), &decoded); err == nil {
		t.Fatal("expected decode error for invalid JSON")
	}
}
