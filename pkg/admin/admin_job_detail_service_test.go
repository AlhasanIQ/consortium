package admin

import (
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ---------------------------------------------------------------------------
// isTerminalExecutionStatus
// ---------------------------------------------------------------------------

func TestIsTerminalExecutionStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"completed", true},
		{"failed", true},
		{"cancelled", true},
		{"archived", true},
		{"COMPLETED", true}, // case-insensitive
		{"FAILED", true},
		{"running", false},
		{"pending", false},
		{"paused", false},
		{"", false},
		{"  completed  ", true}, // whitespace stripped
	}
	for _, tc := range tests {
		got := isTerminalExecutionStatus(tc.status)
		if got != tc.want {
			t.Errorf("isTerminalExecutionStatus(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// clampPercent
// ---------------------------------------------------------------------------

func TestClampPercent(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{50, 50},
		{0, 0},
		{100, 100},
		{-1, 0},
		{101, 100},
		{200, 100},
	}
	for _, tc := range tests {
		got := clampPercent(tc.in)
		if got != tc.want {
			t.Errorf("clampPercent(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// maxFloat
// ---------------------------------------------------------------------------

func TestMaxFloat(t *testing.T) {
	tests := []struct {
		a, b float64
		want float64
	}{
		{1, 2, 2},
		{2, 1, 2},
		{3, 3, 3},
		{-1, -2, -1},
		{0, 0, 0},
	}
	for _, tc := range tests {
		got := maxFloat(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("maxFloat(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// durationBetweenMs
// ---------------------------------------------------------------------------

func TestDurationBetweenMs(t *testing.T) {
	base := mustTime("2024-01-01T00:00:00Z")
	oneSecLater := base.Add(time.Second)
	halfSecLater := base.Add(500 * time.Millisecond)

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  float64
	}{
		{"one second apart", base, oneSecLater, 1000},
		{"half second", base, halfSecLater, 500},
		{"same time returns 0", base, base, 0},
		{"end before start returns 0", oneSecLater, base, 0},
		{"zero start returns 0", time.Time{}, oneSecLater, 0},
		{"zero end returns 0", base, time.Time{}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := durationBetweenMs(tc.start, tc.end)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// computeTimelineBar
// ---------------------------------------------------------------------------

func TestComputeTimelineBar(t *testing.T) {
	base := mustTime("2024-01-01T00:00:00Z")

	t.Run("zero start time yields zero offset", func(t *testing.T) {
		offset, width := computeTimelineBar(time.Time{}, 500, float64(base.UnixMilli()), 1000)
		if offset != 0 {
			t.Errorf("expected offset 0, got %v", offset)
		}
		if width < 0.5 {
			t.Errorf("expected minimum width >= 0.5, got %v", width)
		}
	})

	t.Run("start equals job start gives 0% offset", func(t *testing.T) {
		jobStartMs := float64(base.UnixMilli())
		offset, _ := computeTimelineBar(base, 500, jobStartMs, 1000)
		if offset != 0 {
			t.Errorf("expected 0%% offset, got %v", offset)
		}
	})

	t.Run("start at half of total duration gives 50% offset", func(t *testing.T) {
		jobStartMs := float64(base.UnixMilli())
		totalMs := 2000.0
		halfwayStart := base.Add(time.Second) // 1000ms in, total is 2000ms
		offset, _ := computeTimelineBar(halfwayStart, 500, jobStartMs, totalMs)
		want := 50.0
		if offset != want {
			t.Errorf("expected %v%% offset, got %v", want, offset)
		}
	})

	t.Run("width at minimum threshold", func(t *testing.T) {
		jobStartMs := float64(base.UnixMilli())
		_, width := computeTimelineBar(base, 0.1, jobStartMs, 1000)
		if width < 0.5 {
			t.Errorf("expected minimum width 0.5, got %v", width)
		}
	})

	t.Run("negative start offset is clamped to 0", func(t *testing.T) {
		// Node started before the job's own start timestamp
		early := base.Add(-time.Second)
		offset, _ := computeTimelineBar(early, 500, float64(base.UnixMilli()), 1000)
		if offset != 0 {
			t.Errorf("expected 0%% clamped offset, got %v", offset)
		}
	})

	t.Run("offset beyond 100 is clamped to 100", func(t *testing.T) {
		late := base.Add(5 * time.Second)
		offset, _ := computeTimelineBar(late, 500, float64(base.UnixMilli()), 1000)
		if offset != 100 {
			t.Errorf("expected 100%% clamped offset, got %v", offset)
		}
	})

	t.Run("full-duration width equals 100%", func(t *testing.T) {
		_, width := computeTimelineBar(base, 1000, float64(base.UnixMilli()), 1000)
		if width != 100 {
			t.Errorf("expected 100%% width, got %v", width)
		}
	})
}

// ---------------------------------------------------------------------------
// nodeTimelineParams
// ---------------------------------------------------------------------------

func TestNodeTimelineParams(t *testing.T) {
	base := mustTime("2024-01-01T00:00:00Z")
	completedAt := base.Add(500 * time.Millisecond)

	t.Run("uses CreatedAt when StartedAt is nil", func(t *testing.T) {
		node := storage.WorkflowNode{
			CreatedAt: base,
			LatencyMs: 200,
		}
		start, dur := nodeTimelineParams(node)
		if !start.Equal(base) {
			t.Errorf("expected start=%v, got %v", base, start)
		}
		if dur != 200 {
			t.Errorf("expected dur=200, got %v", dur)
		}
	})

	t.Run("prefers StartedAt over CreatedAt", func(t *testing.T) {
		startedAt := base.Add(100 * time.Millisecond)
		node := storage.WorkflowNode{
			CreatedAt: base,
			StartedAt: &startedAt,
			LatencyMs: 200,
		}
		start, _ := nodeTimelineParams(node)
		if !start.Equal(startedAt) {
			t.Errorf("expected start=%v, got %v", startedAt, start)
		}
	})

	t.Run("uses wall-clock duration when CompletedAt present", func(t *testing.T) {
		node := storage.WorkflowNode{
			CreatedAt:   base,
			CompletedAt: &completedAt,
			LatencyMs:   100, // should be overridden by wall-clock
		}
		_, dur := nodeTimelineParams(node)
		want := 500.0
		if dur != want {
			t.Errorf("expected dur=%v, got %v", want, dur)
		}
	})

	t.Run("minimum duration is 1 when latency is zero", func(t *testing.T) {
		node := storage.WorkflowNode{
			CreatedAt: base,
		}
		_, dur := nodeTimelineParams(node)
		if dur < 1 {
			t.Errorf("expected minimum dur=1, got %v", dur)
		}
	})
}

// ---------------------------------------------------------------------------
// timelineNodeDisplayName
// ---------------------------------------------------------------------------

func TestTimelineNodeDisplayName(t *testing.T) {
	tests := []struct {
		name string
		node storage.WorkflowNode
		want string
	}{
		{
			name: "prefers NodeLabel",
			node: storage.WorkflowNode{NodeLabel: "My Label", NodeName: "name", NodeType: "llm_call", NodeID: "id1"},
			want: "My Label",
		},
		{
			name: "falls back to NodeName when label empty",
			node: storage.WorkflowNode{NodeName: "node-name", NodeType: "llm_call", NodeID: "id1"},
			want: "node-name",
		},
		{
			name: "falls back to NodeType when name empty",
			node: storage.WorkflowNode{NodeType: "llm_call", NodeID: "id1"},
			want: "llm_call",
		},
		{
			name: "falls back to NodeID when all empty",
			node: storage.WorkflowNode{NodeID: "id1"},
			want: "id1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := timelineNodeDisplayName(tc.node)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// timelineAttemptDisplayName
// ---------------------------------------------------------------------------

func TestTimelineAttemptDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		attempt storage.NodeExecutionAttempt
		want    string
	}{
		{
			name:    "prefers NodeType",
			attempt: storage.NodeExecutionAttempt{NodeType: "llm_call", NodeID: "node1"},
			want:    "llm_call",
		},
		{
			name:    "falls back to NodeID",
			attempt: storage.NodeExecutionAttempt{NodeID: "node1"},
			want:    "node1",
		},
		{
			name:    "returns 'node' as final fallback",
			attempt: storage.NodeExecutionAttempt{},
			want:    "node",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := timelineAttemptDisplayName(tc.attempt)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// timelineAttemptStart
// ---------------------------------------------------------------------------

func TestTimelineAttemptStart(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	startedAt := mustTime("2024-01-01T10:00:05Z")

	t.Run("uses StartedAt when non-nil and non-zero", func(t *testing.T) {
		attempt := storage.NodeExecutionAttempt{
			CreatedAt: base,
			StartedAt: &startedAt,
		}
		got := timelineAttemptStart(attempt)
		if !got.Equal(startedAt) {
			t.Errorf("expected %v, got %v", startedAt, got)
		}
	})

	t.Run("falls back to CreatedAt when StartedAt is nil", func(t *testing.T) {
		attempt := storage.NodeExecutionAttempt{
			CreatedAt: base,
		}
		got := timelineAttemptStart(attempt)
		if !got.Equal(base) {
			t.Errorf("expected %v, got %v", base, got)
		}
	})

	t.Run("falls back to CreatedAt when StartedAt is zero", func(t *testing.T) {
		zero := time.Time{}
		attempt := storage.NodeExecutionAttempt{
			CreatedAt: base,
			StartedAt: &zero,
		}
		got := timelineAttemptStart(attempt)
		if !got.Equal(base) {
			t.Errorf("expected %v, got %v", base, got)
		}
	})
}

// ---------------------------------------------------------------------------
// mergedTimelineIntervalsDurationMs
// ---------------------------------------------------------------------------

func TestMergedTimelineIntervalsDurationMs(t *testing.T) {
	base := mustTime("2024-01-01T00:00:00Z")
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }

	t.Run("empty input returns 0", func(t *testing.T) {
		got := mergedTimelineIntervalsDurationMs(nil)
		if got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("single interval", func(t *testing.T) {
		intervals := []timelineInterval{{Start: sec(0), End: sec(2)}}
		got := mergedTimelineIntervalsDurationMs(intervals)
		if got != 2000 {
			t.Errorf("got %v, want 2000", got)
		}
	})

	t.Run("non-overlapping intervals are summed", func(t *testing.T) {
		intervals := []timelineInterval{
			{Start: sec(0), End: sec(1)},
			{Start: sec(2), End: sec(4)},
		}
		got := mergedTimelineIntervalsDurationMs(intervals)
		if got != 3000 {
			t.Errorf("got %v, want 3000", got)
		}
	})

	t.Run("overlapping intervals are merged", func(t *testing.T) {
		intervals := []timelineInterval{
			{Start: sec(0), End: sec(3)},
			{Start: sec(2), End: sec(5)},
		}
		got := mergedTimelineIntervalsDurationMs(intervals)
		if got != 5000 {
			t.Errorf("got %v, want 5000", got)
		}
	})

	t.Run("fully contained interval does not increase total", func(t *testing.T) {
		intervals := []timelineInterval{
			{Start: sec(0), End: sec(5)},
			{Start: sec(1), End: sec(3)}, // contained within first
		}
		got := mergedTimelineIntervalsDurationMs(intervals)
		if got != 5000 {
			t.Errorf("got %v, want 5000", got)
		}
	})

	t.Run("invalid intervals (end not after start) are skipped", func(t *testing.T) {
		intervals := []timelineInterval{
			{Start: sec(0), End: sec(2)},
			{Start: sec(5), End: sec(3)}, // invalid
		}
		got := mergedTimelineIntervalsDurationMs(intervals)
		if got != 2000 {
			t.Errorf("got %v, want 2000", got)
		}
	})

	t.Run("all invalid intervals returns 0", func(t *testing.T) {
		intervals := []timelineInterval{
			{Start: sec(2), End: sec(0)},
			{},
		}
		got := mergedTimelineIntervalsDurationMs(intervals)
		if got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("three overlapping intervals are fully merged", func(t *testing.T) {
		intervals := []timelineInterval{
			{Start: sec(0), End: sec(4)},
			{Start: sec(1), End: sec(5)},
			{Start: sec(3), End: sec(6)},
		}
		got := mergedTimelineIntervalsDurationMs(intervals)
		if got != 6000 {
			t.Errorf("got %v, want 6000", got)
		}
	})
}

// ---------------------------------------------------------------------------
// closeAttemptForDisplay
// ---------------------------------------------------------------------------

func TestCloseAttemptForDisplay(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	startedAt := mustTime("2024-01-01T10:00:00Z")
	endHint := mustTime("2024-01-01T10:00:02Z") // 2 seconds later

	t.Run("nil attempt is a no-op", func(t *testing.T) {
		closeAttemptForDisplay(nil, endHint) // must not panic
	})

	t.Run("already-closed attempt is unchanged", func(t *testing.T) {
		existing := mustTime("2024-01-01T10:00:01Z")
		attempt := &storage.NodeExecutionAttempt{
			CreatedAt:   base,
			CompletedAt: &existing,
		}
		closeAttemptForDisplay(attempt, endHint)
		if !attempt.CompletedAt.Equal(existing) {
			t.Errorf("CompletedAt should not change when already set")
		}
	})

	t.Run("sets CompletedAt and computes latency when StartedAt present", func(t *testing.T) {
		attempt := &storage.NodeExecutionAttempt{
			CreatedAt: base,
			StartedAt: &startedAt,
		}
		closeAttemptForDisplay(attempt, endHint)
		if attempt.CompletedAt == nil {
			t.Fatal("expected CompletedAt to be set")
		}
		if !attempt.CompletedAt.Equal(endHint) {
			t.Errorf("expected CompletedAt=%v, got %v", endHint, attempt.CompletedAt)
		}
		want := 2000.0
		if attempt.LatencyMs != want {
			t.Errorf("expected LatencyMs=%v, got %v", want, attempt.LatencyMs)
		}
	})

	t.Run("falls back to UpdatedAt when endHint is zero", func(t *testing.T) {
		updatedAt := mustTime("2024-01-01T10:00:03Z")
		attempt := &storage.NodeExecutionAttempt{
			CreatedAt: base,
			UpdatedAt: updatedAt,
		}
		closeAttemptForDisplay(attempt, time.Time{})
		if attempt.CompletedAt == nil {
			t.Fatal("expected CompletedAt to be set from UpdatedAt")
		}
		if !attempt.CompletedAt.Equal(updatedAt) {
			t.Errorf("expected CompletedAt=%v, got %v", updatedAt, attempt.CompletedAt)
		}
	})

	t.Run("no-op when both endHint and UpdatedAt are zero", func(t *testing.T) {
		attempt := &storage.NodeExecutionAttempt{
			CreatedAt: base,
		}
		closeAttemptForDisplay(attempt, time.Time{})
		if attempt.CompletedAt != nil {
			t.Errorf("expected CompletedAt to remain nil")
		}
	})
}

// ---------------------------------------------------------------------------
// normalizeAttemptStatusesForDisplay
// ---------------------------------------------------------------------------

func TestNormalizeAttemptStatusesForDisplay(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	secondLater := base.Add(time.Second)
	twoSecsLater := base.Add(2 * time.Second)

	t.Run("empty slice returns empty", func(t *testing.T) {
		got := normalizeAttemptStatusesForDisplay(nil, "completed")
		if len(got) != 0 {
			t.Errorf("expected empty, got %d items", len(got))
		}
	})

	t.Run("single completed attempt is not modified", func(t *testing.T) {
		attempts := []storage.NodeExecutionAttempt{
			{NodeID: "n1", Attempt: 1, Status: "completed", CreatedAt: base, UpdatedAt: secondLater},
		}
		got := normalizeAttemptStatusesForDisplay(attempts, "completed")
		if got[0].Status != "completed" {
			t.Errorf("expected completed, got %s", got[0].Status)
		}
	})

	t.Run("running attempt on non-terminal job stays running", func(t *testing.T) {
		attempts := []storage.NodeExecutionAttempt{
			{NodeID: "n1", Attempt: 1, Status: "running", CreatedAt: base, UpdatedAt: secondLater},
		}
		got := normalizeAttemptStatusesForDisplay(attempts, "running")
		if got[0].Status != "running" {
			t.Errorf("expected running, got %s", got[0].Status)
		}
	})

	t.Run("running attempt on terminal job becomes interrupted", func(t *testing.T) {
		attempts := []storage.NodeExecutionAttempt{
			{NodeID: "n1", Attempt: 1, Status: "running", CreatedAt: base, UpdatedAt: secondLater},
		}
		got := normalizeAttemptStatusesForDisplay(attempts, "failed")
		if got[0].Status != "interrupted" {
			t.Errorf("expected interrupted, got %s", got[0].Status)
		}
	})

	t.Run("first of two running attempts becomes retrying", func(t *testing.T) {
		startedAt2 := secondLater
		attempts := []storage.NodeExecutionAttempt{
			{NodeID: "n1", Attempt: 1, Status: "running", CreatedAt: base, UpdatedAt: secondLater},
			{NodeID: "n1", Attempt: 2, Status: "running", CreatedAt: secondLater, StartedAt: &startedAt2, UpdatedAt: twoSecsLater},
		}
		got := normalizeAttemptStatusesForDisplay(attempts, "completed")
		if got[0].Status != "retrying" {
			t.Errorf("expected retrying for first attempt, got %s", got[0].Status)
		}
	})

	t.Run("original slice is not mutated", func(t *testing.T) {
		attempts := []storage.NodeExecutionAttempt{
			{NodeID: "n1", Attempt: 1, Status: "running", CreatedAt: base, UpdatedAt: secondLater},
		}
		_ = normalizeAttemptStatusesForDisplay(attempts, "failed")
		if attempts[0].Status != "running" {
			t.Errorf("original slice should not be mutated")
		}
	})
}

// ---------------------------------------------------------------------------
// extractWorkflowContext
// ---------------------------------------------------------------------------

func TestExtractWorkflowContext(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		got := extractWorkflowContext("")
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("whitespace string returns nil", func(t *testing.T) {
		got := extractWorkflowContext("   ")
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("invalid json returns nil", func(t *testing.T) {
		got := extractWorkflowContext("{not json}")
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("extracts nested context key", func(t *testing.T) {
		raw := `{"context":{"user_prompt":"hello world","model":"gpt-4"}}`
		got := extractWorkflowContext(raw)
		if got == nil {
			t.Fatal("expected non-nil context")
		}
		if got["user_prompt"] != "hello world" {
			t.Errorf("expected user_prompt=hello world, got %v", got["user_prompt"])
		}
	})

	t.Run("falls back to top-level map when no context key", func(t *testing.T) {
		raw := `{"user_prompt":"fallback"}`
		got := extractWorkflowContext(raw)
		if got == nil {
			t.Fatal("expected non-nil context")
		}
		if got["user_prompt"] != "fallback" {
			t.Errorf("expected user_prompt=fallback, got %v", got["user_prompt"])
		}
	})
}

// ---------------------------------------------------------------------------
// extractInputPrompt
// ---------------------------------------------------------------------------

func TestExtractInputPrompt_PriorityAndFallback(t *testing.T) {
	t.Run("empty request data returns empty string", func(t *testing.T) {
		got := extractInputPrompt("")
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("returns user_prompt with priority", func(t *testing.T) {
		raw := `{"context":{"user_prompt":"priority prompt","input":"other input"}}`
		got := extractInputPrompt(raw)
		if got != "priority prompt" {
			t.Errorf("expected 'priority prompt', got %q", got)
		}
	})

	t.Run("falls back to input key", func(t *testing.T) {
		raw := `{"context":{"input":"fallback input"}}`
		got := extractInputPrompt(raw)
		if got != "fallback input" {
			t.Errorf("expected 'fallback input', got %q", got)
		}
	})

	t.Run("falls back to prompt key", func(t *testing.T) {
		raw := `{"context":{"prompt":"my prompt"}}`
		got := extractInputPrompt(raw)
		if got != "my prompt" {
			t.Errorf("expected 'my prompt', got %q", got)
		}
	})

	t.Run("extracts first node prompt from persisted workflow JSON", func(t *testing.T) {
		raw := `{
			"id": "990edebb-5e92-4484-a2d6-4f63614177b8",
			"name": "finding neemo - Novomo handoff chain",
			"description": "Three-step Novomo Agent workflow",
			"nodes": [
				{
					"id": "repo",
					"type": "agent_run",
					"prompt": "Create a small static web app for a fish tank scene."
				}
			],
			"edges": []
		}`
		got := extractInputPrompt(raw)
		if got != "Create a small static web app for a fish tank scene." {
			t.Errorf("expected node prompt, got %q", got)
		}
	})

	t.Run("falls back to first string value when no priority key", func(t *testing.T) {
		// Only "something" key exists, not in priority list
		raw := `{"context":{"something":"first value"}}`
		got := extractInputPrompt(raw)
		if got != "first value" {
			t.Errorf("expected 'first value', got %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// buildFullWorkflowContext
// ---------------------------------------------------------------------------

func TestBuildFullWorkflowContext(t *testing.T) {
	t.Run("merges request context with node outputs", func(t *testing.T) {
		reqCtx := map[string]interface{}{"user_prompt": "hello"}
		nodes := []storage.WorkflowNode{
			{NodeID: "analyze", Output: "analysis result"},
			{NodeID: "summarize", Output: "summary result"},
		}
		got := buildFullWorkflowContext(reqCtx, nodes)
		if got["user_prompt"] != "hello" {
			t.Errorf("expected user_prompt=hello, got %v", got["user_prompt"])
		}
		if got["analyze"] != "analysis result" {
			t.Errorf("expected analyze='analysis result', got %v", got["analyze"])
		}
		if got["summarize"] != "summary result" {
			t.Errorf("expected summarize='summary result', got %v", got["summarize"])
		}
	})

	t.Run("nodes with empty output are not included", func(t *testing.T) {
		reqCtx := map[string]interface{}{}
		nodes := []storage.WorkflowNode{
			{NodeID: "empty-node", Output: ""},
		}
		got := buildFullWorkflowContext(reqCtx, nodes)
		if _, ok := got["empty-node"]; ok {
			t.Errorf("expected empty-node to be absent from context")
		}
	})

	t.Run("nil request context still includes node outputs", func(t *testing.T) {
		nodes := []storage.WorkflowNode{
			{NodeID: "node1", Output: "output1"},
		}
		got := buildFullWorkflowContext(nil, nodes)
		if got["node1"] != "output1" {
			t.Errorf("expected node1='output1', got %v", got["node1"])
		}
	})

	t.Run("nil nodes returns copy of request context", func(t *testing.T) {
		reqCtx := map[string]interface{}{"key": "value"}
		got := buildFullWorkflowContext(reqCtx, nil)
		if got["key"] != "value" {
			t.Errorf("expected key=value, got %v", got["key"])
		}
	})
}

// ---------------------------------------------------------------------------
// extractFromSnapshot
// ---------------------------------------------------------------------------

func TestExtractFromSnapshot(t *testing.T) {
	t.Run("empty string returns empty maps", func(t *testing.T) {
		got := extractFromSnapshot("")
		if len(got.Configs) != 0 {
			t.Errorf("expected empty Configs, got %v", got.Configs)
		}
		if len(got.SystemPrompts) != 0 {
			t.Errorf("expected empty SystemPrompts, got %v", got.SystemPrompts)
		}
	})

	t.Run("invalid json returns empty maps", func(t *testing.T) {
		got := extractFromSnapshot("{bad json}")
		if len(got.Configs) != 0 || len(got.SystemPrompts) != 0 {
			t.Errorf("expected empty maps for invalid json")
		}
	})

	t.Run("extracts system_prompt for node", func(t *testing.T) {
		snap := `{"nodes":[{"id":"node1","system_prompt":"You are helpful.","model":"gpt-4"}]}`
		got := extractFromSnapshot(snap)
		if got.SystemPrompts["node1"] != "You are helpful." {
			t.Errorf("expected system_prompt, got %q", got.SystemPrompts["node1"])
		}
	})

	t.Run("excludes id, prompt, system_prompt from config", func(t *testing.T) {
		snap := `{"nodes":[{"id":"n1","prompt":"do something","system_prompt":"sys","model":"gpt-4","temperature":0.7}]}`
		got := extractFromSnapshot(snap)
		cfg := got.Configs["n1"]
		if cfg == "" {
			t.Fatal("expected config to be non-empty")
		}
		// id, prompt, system_prompt should be absent
		for _, excluded := range []string{`"id"`, `"prompt"`, `"system_prompt"`} {
			if containsSubstring(cfg, excluded) {
				t.Errorf("config should not contain %q, got: %s", excluded, cfg)
			}
		}
		// model and temperature should be present
		for _, included := range []string{`"model"`, `"temperature"`} {
			if !containsSubstring(cfg, included) {
				t.Errorf("config should contain %q, got: %s", included, cfg)
			}
		}
	})

	t.Run("node without id is skipped", func(t *testing.T) {
		snap := `{"nodes":[{"model":"gpt-4"}]}`
		got := extractFromSnapshot(snap)
		if len(got.Configs) != 0 {
			t.Errorf("expected no configs for node without id")
		}
	})

	t.Run("node with only id+prompt+system_prompt yields no config entry", func(t *testing.T) {
		snap := `{"nodes":[{"id":"n1","prompt":"p","system_prompt":"sp"}]}`
		got := extractFromSnapshot(snap)
		if _, ok := got.Configs["n1"]; ok {
			t.Errorf("expected no config entry when only excluded fields present")
		}
	})
}

// helper used in TestExtractFromSnapshot
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

// ---------------------------------------------------------------------------
// resolveNodeConfig
// ---------------------------------------------------------------------------

func TestResolveNodeConfig(t *testing.T) {
	t.Run("returns snapshot config when present", func(t *testing.T) {
		configs := map[string]string{"node1": `{"model":"gpt-4"}`}
		got := resolveNodeConfig("node1", `{"runtime":"metadata"}`, configs)
		if got != `{"model":"gpt-4"}` {
			t.Errorf("expected snapshot config, got %q", got)
		}
	})

	t.Run("falls back to pretty-printed runtime metadata", func(t *testing.T) {
		configs := map[string]string{}
		raw := `{"a":1}`
		got := resolveNodeConfig("node1", raw, configs)
		// Should be pretty-printed
		if got == raw {
			t.Errorf("expected pretty-printed JSON, got same raw %q", got)
		}
		if !containsSubstring(got, `"a"`) {
			t.Errorf("expected pretty JSON to contain key, got %q", got)
		}
	})

	t.Run("returns empty when no snapshot config and no metadata", func(t *testing.T) {
		configs := map[string]string{}
		got := resolveNodeConfig("node1", "", configs)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("returns empty string config when snapshot has empty value", func(t *testing.T) {
		configs := map[string]string{"node1": ""}
		got := resolveNodeConfig("node1", `{"fallback":"metadata"}`, configs)
		// Empty snapshot config means fall through to runtime metadata
		if got == "" && containsSubstring(got, "fallback") {
			t.Errorf("unexpected: %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// applyChildWorkflowDisplayCost
// ---------------------------------------------------------------------------

func TestApplyChildWorkflowDisplayCost_EdgeCases(t *testing.T) {
	t.Run("nil node returns empty string", func(t *testing.T) {
		got := applyChildWorkflowDisplayCost(nil)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("node with no metadata returns empty string", func(t *testing.T) {
		node := &storage.WorkflowNode{NodeID: "n1"}
		got := applyChildWorkflowDisplayCost(node)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("non-child_workflow node returns child_job_id but does not update cost", func(t *testing.T) {
		node := &storage.WorkflowNode{
			NodeID:   "n1",
			NodeType: "llm_call",
			Cost:     1.0,
			Metadata: `{"child_job_id":"job-abc","child_total_cost":9.99}`,
		}
		got := applyChildWorkflowDisplayCost(node)
		if got != "job-abc" {
			t.Errorf("expected child_job_id=job-abc, got %q", got)
		}
		if node.Cost != 1.0 {
			t.Errorf("cost should not be updated for non-child_workflow node, got %v", node.Cost)
		}
	})

	t.Run("child_workflow node with child_total_cost updates node.Cost", func(t *testing.T) {
		node := &storage.WorkflowNode{
			NodeID:   "n1",
			NodeType: "child_workflow",
			Cost:     0.5,
			Metadata: `{"child_job_id":"job-xyz","child_total_cost":3.14}`,
		}
		got := applyChildWorkflowDisplayCost(node)
		if got != "job-xyz" {
			t.Errorf("expected child_job_id=job-xyz, got %q", got)
		}
		if node.Cost != 3.14 {
			t.Errorf("expected cost=3.14, got %v", node.Cost)
		}
	})

	t.Run("child_workflow node without child_total_cost leaves cost unchanged", func(t *testing.T) {
		node := &storage.WorkflowNode{
			NodeID:   "n1",
			NodeType: "child_workflow",
			Cost:     0.5,
			Metadata: `{"child_job_id":"job-xyz"}`,
		}
		applyChildWorkflowDisplayCost(node)
		if node.Cost != 0.5 {
			t.Errorf("expected cost unchanged=0.5, got %v", node.Cost)
		}
	})

	t.Run("invalid metadata JSON returns empty string", func(t *testing.T) {
		node := &storage.WorkflowNode{
			NodeID:   "n1",
			Metadata: "{bad json",
		}
		got := applyChildWorkflowDisplayCost(node)
		if got != "" {
			t.Errorf("expected empty for invalid JSON, got %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// reorderNodesParentFirst
// ---------------------------------------------------------------------------

func TestReorderNodesParentFirst(t *testing.T) {
	t.Run("nodes without parent-child relationships are unchanged", func(t *testing.T) {
		nodes := []storage.WorkflowNode{
			{NodeID: "a"},
			{NodeID: "b"},
			{NodeID: "c"},
		}
		ordered, childrenOf, childSet, parentSet := reorderNodesParentFirst(nodes)
		if len(ordered) != 3 {
			t.Errorf("expected 3 nodes, got %d", len(ordered))
		}
		if len(childrenOf) != 0 || len(childSet) != 0 || len(parentSet) != 0 {
			t.Errorf("expected empty relationship maps for flat nodes")
		}
	})

	t.Run("children are placed immediately after their parent", func(t *testing.T) {
		nodes := []storage.WorkflowNode{
			{NodeID: "parent", ParentNodeID: ""},
			{NodeID: "sibling", ParentNodeID: ""},
			{NodeID: "child1", ParentNodeID: "parent"},
			{NodeID: "child2", ParentNodeID: "parent"},
		}
		ordered, _, childSet, parentSet := reorderNodesParentFirst(nodes)

		// parent must appear before its children
		parentIdx := -1
		for i, n := range ordered {
			if n.NodeID == "parent" {
				parentIdx = i
			}
		}
		if parentIdx < 0 {
			t.Fatal("parent not found in ordered output")
		}
		for _, n := range ordered[parentIdx+1:] {
			if n.NodeID == "child1" || n.NodeID == "child2" {
				// children appear after parent - good
				break
			}
		}

		if !parentSet["parent"] {
			t.Errorf("parent should be in parentSet")
		}
		if !childSet["child1"] || !childSet["child2"] {
			t.Errorf("children should be in childSet")
		}
	})

	t.Run("child nodes are excluded from top-level ordering", func(t *testing.T) {
		nodes := []storage.WorkflowNode{
			{NodeID: "root"},
			{NodeID: "child", ParentNodeID: "root"},
		}
		ordered, _, _, _ := reorderNodesParentFirst(nodes)
		if len(ordered) != 2 {
			t.Errorf("expected 2 nodes total (root + child), got %d", len(ordered))
		}
		if ordered[0].NodeID != "root" {
			t.Errorf("expected root first, got %s", ordered[0].NodeID)
		}
		if ordered[1].NodeID != "child" {
			t.Errorf("expected child second, got %s", ordered[1].NodeID)
		}
	})

	t.Run("compiled aggregation internals group under terminal result node", func(t *testing.T) {
		nodes := []storage.WorkflowNode{
			{NodeID: "agent-a", ParentNodeID: ""},
			{NodeID: "agg--judge", ParentNodeID: "agg--result"},
			{NodeID: "agg--parse-selection", ParentNodeID: "agg--result"},
			{NodeID: "agg--result", ParentNodeID: ""},
			{NodeID: "downstream", ParentNodeID: ""},
		}

		ordered, childrenOf, childSet, parentSet := reorderNodesParentFirst(nodes)

		wantOrder := []string{"agent-a", "agg--result", "agg--judge", "agg--parse-selection", "downstream"}
		if len(ordered) != len(wantOrder) {
			t.Fatalf("ordered len = %d, want %d: %+v", len(ordered), len(wantOrder), ordered)
		}
		for i, want := range wantOrder {
			if ordered[i].NodeID != want {
				t.Fatalf("ordered[%d] = %s, want %s; all=%+v", i, ordered[i].NodeID, want, ordered)
			}
		}
		if len(childrenOf["agg--result"]) != 2 {
			t.Fatalf("agg--result children len = %d, want 2", len(childrenOf["agg--result"]))
		}
		if !childSet["agg--judge"] || !childSet["agg--parse-selection"] {
			t.Fatalf("compiled internals missing from childSet: %+v", childSet)
		}
		if !parentSet["agg--result"] {
			t.Fatalf("agg--result missing from parentSet: %+v", parentSet)
		}
	})
}

// ---------------------------------------------------------------------------
// initializeJobLifecycle
// ---------------------------------------------------------------------------

func TestInitializeJobLifecycle(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	updatedAt := base.Add(3 * time.Second)

	job := &storage.Job{
		ID:        "job-1",
		Status:    "completed",
		CreatedAt: base,
		UpdatedAt: updatedAt,
	}

	jobStartMs, totalDurationMs, lifecycle := initializeJobLifecycle(job)

	if jobStartMs != float64(base.UnixMilli()) {
		t.Errorf("expected jobStartMs=%v, got %v", float64(base.UnixMilli()), jobStartMs)
	}
	want := float64(updatedAt.UnixMilli()) - float64(base.UnixMilli())
	if totalDurationMs != want {
		t.Errorf("expected totalDurationMs=%v, got %v", want, totalDurationMs)
	}
	if !lifecycle.SubmittedAt.Equal(base) {
		t.Errorf("expected SubmittedAt=%v, got %v", base, lifecycle.SubmittedAt)
	}
	if !lifecycle.CompletedAt.Equal(updatedAt) {
		t.Errorf("expected CompletedAt=%v, got %v", updatedAt, lifecycle.CompletedAt)
	}
	if lifecycle.EndToEndDurationMs != want {
		t.Errorf("expected EndToEndDurationMs=%v, got %v", want, lifecycle.EndToEndDurationMs)
	}
	if lifecycle.ExecutionWidthPercent != 100 {
		t.Errorf("expected ExecutionWidthPercent=100, got %v", lifecycle.ExecutionWidthPercent)
	}
}

func TestInitializeJobLifecycle_ZeroDurationClamped(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	job := &storage.Job{
		ID:        "job-1",
		CreatedAt: base,
		UpdatedAt: base, // same time → duration 0
	}
	_, totalDurationMs, _ := initializeJobLifecycle(job)
	if totalDurationMs < 1 {
		t.Errorf("expected at least 1ms to avoid division by zero, got %v", totalDurationMs)
	}
}

// ---------------------------------------------------------------------------
// finalizeLifecycleTiming
// ---------------------------------------------------------------------------

func TestFinalizeLifecycleTiming(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	executionStart := base.Add(500 * time.Millisecond)
	completedAt := base.Add(3 * time.Second)

	t.Run("sets ExecutionStartedAt from earliestAttemptStart", func(t *testing.T) {
		lifecycle := JobLifecycleMetrics{
			SubmittedAt:        base,
			CompletedAt:        completedAt,
			EndToEndDurationMs: 3000,
		}
		finalizeLifecycleTiming(
			&lifecycle,
			float64(base.UnixMilli()),
			3000,
			executionStart,
			time.Time{},
			nil,
			0,
		)
		if lifecycle.ExecutionStartedAt == nil {
			t.Fatal("expected ExecutionStartedAt to be set")
		}
		if !lifecycle.ExecutionStartedAt.Equal(executionStart) {
			t.Errorf("expected %v, got %v", executionStart, lifecycle.ExecutionStartedAt)
		}
		if lifecycle.QueueDelayMs != 500 {
			t.Errorf("expected QueueDelayMs=500, got %v", lifecycle.QueueDelayMs)
		}
	})

	t.Run("falls back to earliestNodeStart when no attempt start", func(t *testing.T) {
		lifecycle := JobLifecycleMetrics{
			SubmittedAt:        base,
			CompletedAt:        completedAt,
			EndToEndDurationMs: 3000,
		}
		finalizeLifecycleTiming(
			&lifecycle,
			float64(base.UnixMilli()),
			3000,
			time.Time{},    // no attempt start
			executionStart, // node start
			nil,
			0,
		)
		if lifecycle.ExecutionStartedAt == nil {
			t.Fatal("expected ExecutionStartedAt to be set from node start")
		}
	})

	t.Run("ExecutionStartedAt before SubmittedAt is clamped to SubmittedAt", func(t *testing.T) {
		earlyStart := base.Add(-time.Second)
		lifecycle := JobLifecycleMetrics{
			SubmittedAt:        base,
			CompletedAt:        completedAt,
			EndToEndDurationMs: 3000,
		}
		finalizeLifecycleTiming(
			&lifecycle,
			float64(base.UnixMilli()),
			3000,
			earlyStart,
			time.Time{},
			nil,
			0,
		)
		if lifecycle.ExecutionStartedAt == nil || !lifecycle.ExecutionStartedAt.Equal(base) {
			t.Errorf("expected ExecutionStartedAt clamped to SubmittedAt, got %v", lifecycle.ExecutionStartedAt)
		}
	})

	t.Run("no ExecutionStartedAt means full width assigned to execution", func(t *testing.T) {
		lifecycle := JobLifecycleMetrics{
			SubmittedAt:        base,
			CompletedAt:        completedAt,
			EndToEndDurationMs: 3000,
		}
		finalizeLifecycleTiming(&lifecycle, float64(base.UnixMilli()), 3000, time.Time{}, time.Time{}, nil, 0)
		if lifecycle.ExecutionWidthPercent != 100 {
			t.Errorf("expected 100%% execution width, got %v", lifecycle.ExecutionWidthPercent)
		}
		if lifecycle.ExecutionDurationMs != 3000 {
			t.Errorf("expected ExecutionDurationMs=3000, got %v", lifecycle.ExecutionDurationMs)
		}
	})

	t.Run("active attempt duration uses attempt intervals", func(t *testing.T) {
		sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
		intervals := []timelineInterval{
			{Start: sec(1), End: sec(2)}, // 1000ms
		}
		lifecycle := JobLifecycleMetrics{
			SubmittedAt:        base,
			CompletedAt:        completedAt,
			EndToEndDurationMs: 3000,
		}
		finalizeLifecycleTiming(&lifecycle, float64(base.UnixMilli()), 3000, executionStart, time.Time{}, intervals, 0)
		if lifecycle.ActiveAttemptDurationMs != 1000 {
			t.Errorf("expected ActiveAttemptDurationMs=1000, got %v", lifecycle.ActiveAttemptDurationMs)
		}
	})

	t.Run("IdleDurationMs is execution minus active", func(t *testing.T) {
		sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
		intervals := []timelineInterval{
			{Start: sec(1), End: sec(2)}, // 1000ms active
		}
		lifecycle := JobLifecycleMetrics{
			SubmittedAt:        base,
			CompletedAt:        completedAt,
			EndToEndDurationMs: 3000,
		}
		finalizeLifecycleTiming(&lifecycle, float64(base.UnixMilli()), 3000, executionStart, time.Time{}, intervals, 0)
		// ExecutionDurationMs = 2500ms (3s - 0.5s queue), ActiveAttemptDurationMs = 1000ms
		expectedIdle := lifecycle.ExecutionDurationMs - lifecycle.ActiveAttemptDurationMs
		if lifecycle.IdleDurationMs != expectedIdle {
			t.Errorf("expected IdleDurationMs=%v, got %v", expectedIdle, lifecycle.IdleDurationMs)
		}
	})
}

// ---------------------------------------------------------------------------
// computeNodeActiveDurations
// ---------------------------------------------------------------------------

func TestComputeNodeActiveDurations(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }

	t.Run("sets ActiveDurationMs and WaitDurationMs for node with intervals", func(t *testing.T) {
		nodes := []NodeWithMetrics{
			{
				WorkflowNode:       storage.WorkflowNode{NodeID: "n1"},
				TimelineDurationMs: 3000,
			},
		}
		intervals := map[string][]timelineInterval{
			"n1": {{Start: sec(0), End: sec(2)}}, // 2000ms active
		}
		computeNodeActiveDurations(nodes, intervals)
		if nodes[0].ActiveDurationMs != 2000 {
			t.Errorf("expected ActiveDurationMs=2000, got %v", nodes[0].ActiveDurationMs)
		}
		if nodes[0].WaitDurationMs != 1000 {
			t.Errorf("expected WaitDurationMs=1000, got %v", nodes[0].WaitDurationMs)
		}
	})

	t.Run("active duration is capped at timeline duration", func(t *testing.T) {
		nodes := []NodeWithMetrics{
			{
				WorkflowNode:       storage.WorkflowNode{NodeID: "n1"},
				TimelineDurationMs: 1000, // shorter than active
			},
		}
		intervals := map[string][]timelineInterval{
			"n1": {{Start: sec(0), End: sec(5)}}, // 5000ms, capped to 1000
		}
		computeNodeActiveDurations(nodes, intervals)
		if nodes[0].ActiveDurationMs != 1000 {
			t.Errorf("expected ActiveDurationMs capped at 1000, got %v", nodes[0].ActiveDurationMs)
		}
	})

	t.Run("node without intervals has zero active duration", func(t *testing.T) {
		nodes := []NodeWithMetrics{
			{
				WorkflowNode:       storage.WorkflowNode{NodeID: "n1"},
				TimelineDurationMs: 3000,
			},
		}
		computeNodeActiveDurations(nodes, map[string][]timelineInterval{})
		if nodes[0].ActiveDurationMs != 0 {
			t.Errorf("expected 0 active duration, got %v", nodes[0].ActiveDurationMs)
		}
		if nodes[0].WaitDurationMs != 0 {
			t.Errorf("expected 0 wait duration, got %v", nodes[0].WaitDurationMs)
		}
	})
}

// ---------------------------------------------------------------------------
// buildJobDetailPayload
// ---------------------------------------------------------------------------

func TestBuildJobDetailPayload(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	job := &storage.Job{
		ID:          "job-1",
		RequestData: `{"context":{"user_prompt":"test question"}}`,
		CreatedAt:   base,
		UpdatedAt:   base.Add(time.Second),
	}
	lifecycle := JobLifecycleMetrics{
		SubmittedAt:        base,
		CompletedAt:        base.Add(time.Second),
		EndToEndDurationMs: 1000,
	}
	perfMetrics := PerformanceMetrics{TotalLatencyMs: 800}
	nodes := []storage.WorkflowNode{{NodeID: "n1", Status: "completed"}}

	components := JobDetailComponents{
		Job:                job,
		Lifecycle:          lifecycle,
		NodeExecutions:     nodes,
		PerfMetrics:        perfMetrics,
		ChildJobMetrics:    map[string][]NodeWithMetrics{},
		ChildCostSummaries: map[string]ExecutionCostSummary{},
	}
	payload := buildJobDetailPayload(components)

	if payload == nil {
		t.Fatal("expected non-nil payload")
	}
	if payload.Job != job {
		t.Errorf("expected same job pointer")
	}
	if payload.InputPrompt != "test question" {
		t.Errorf("expected InputPrompt='test question', got %q", payload.InputPrompt)
	}
	if payload.PerfMetrics.TotalLatencyMs != 800 {
		t.Errorf("expected TotalLatencyMs=800, got %v", payload.PerfMetrics.TotalLatencyMs)
	}
	if len(payload.NodeExecutions) != 1 {
		t.Errorf("expected 1 node execution, got %d", len(payload.NodeExecutions))
	}
}

// ---------------------------------------------------------------------------
// assignAttemptsToNodes
// ---------------------------------------------------------------------------

func TestAssignAttemptsToNodes(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }

	t.Run("single-attempt nodes do not get timeline bars", func(t *testing.T) {
		nodes := []NodeWithMetrics{
			{WorkflowNode: storage.WorkflowNode{NodeID: "n1"}},
		}
		ap := attemptProcessingResult{
			totalRetryAttempts: 0,
			attemptBars: []AttemptWithMetrics{
				{NodeExecutionAttempt: storage.NodeExecutionAttempt{NodeID: "n1", Attempt: 1}},
			},
			attemptCountByNode:         map[string]int{"n1": 1},
			earliestAttemptStartByNode: map[string]time.Time{},
		}
		nodeRenderOrder := map[string]int{"n1": 0}
		nodeTimelineStart := map[string]time.Time{"n1": base}

		orphans := assignAttemptsToNodes(nodes, ap, nodeRenderOrder, nodeTimelineStart)
		if len(orphans) != 0 {
			t.Errorf("expected no orphans, got %d", len(orphans))
		}
		if len(nodes[0].AttemptsAbove) != 0 || len(nodes[0].AttemptsBelow) != 0 {
			t.Errorf("expected no attempt bars on single-attempt node")
		}
	})

	t.Run("orphan attempt bar for unknown node ID", func(t *testing.T) {
		nodes := []NodeWithMetrics{
			{WorkflowNode: storage.WorkflowNode{NodeID: "n1"}},
		}
		startedAt := sec(0)
		ap := attemptProcessingResult{
			totalRetryAttempts: 1,
			attemptBars: []AttemptWithMetrics{
				{NodeExecutionAttempt: storage.NodeExecutionAttempt{NodeID: "unknown-node", Attempt: 2, StartedAt: &startedAt}},
			},
			attemptCountByNode:         map[string]int{"unknown-node": 2},
			earliestAttemptStartByNode: map[string]time.Time{},
		}
		nodeRenderOrder := map[string]int{"n1": 0}
		nodeTimelineStart := map[string]time.Time{"n1": base}

		orphans := assignAttemptsToNodes(nodes, ap, nodeRenderOrder, nodeTimelineStart)
		if len(orphans) != 1 {
			t.Errorf("expected 1 orphan, got %d", len(orphans))
		}
	})

	t.Run("retry attempt before node start goes to AttemptsAbove", func(t *testing.T) {
		nodeStart := sec(2)
		earlyStart := sec(0)
		nodes := []NodeWithMetrics{
			{WorkflowNode: storage.WorkflowNode{NodeID: "n1"}},
		}
		ap := attemptProcessingResult{
			totalRetryAttempts: 1,
			attemptBars: []AttemptWithMetrics{
				{NodeExecutionAttempt: storage.NodeExecutionAttempt{NodeID: "n1", Attempt: 1, StartedAt: &earlyStart}},
				{NodeExecutionAttempt: storage.NodeExecutionAttempt{NodeID: "n1", Attempt: 2, StartedAt: &nodeStart}},
			},
			attemptCountByNode:         map[string]int{"n1": 2},
			earliestAttemptStartByNode: map[string]time.Time{},
		}
		nodeRenderOrder := map[string]int{"n1": 0}
		nodeTimelineStart := map[string]time.Time{"n1": nodeStart}

		assignAttemptsToNodes(nodes, ap, nodeRenderOrder, nodeTimelineStart)
		if len(nodes[0].AttemptsAbove) == 0 {
			t.Errorf("expected early attempt in AttemptsAbove")
		}
	})
}

// ---------------------------------------------------------------------------
// processAttemptData
// ---------------------------------------------------------------------------

func TestProcessAttemptData(t *testing.T) {
	base := mustTime("2024-01-01T10:00:00Z")
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }

	t.Run("empty logs produces empty result", func(t *testing.T) {
		nm := nodeMetricsResult{
			nodeDisplayOrder: map[string]string{},
			nodeDisplayName:  map[string]string{},
			nodeRenderOrder:  map[string]int{},
		}
		result := processAttemptData(nil, "completed", nm, float64(base.UnixMilli()), 10000)
		if len(result.logs) != 0 || len(result.attemptBars) != 0 {
			t.Errorf("expected empty result for nil logs")
		}
		if result.retriedNodeCount != 0 || result.totalRetryAttempts != 0 {
			t.Errorf("expected zero retry counts")
		}
	})

	t.Run("single attempt is counted correctly", func(t *testing.T) {
		completedAt := sec(1)
		logs := []storage.NodeExecutionAttempt{
			{
				NodeID:      "n1",
				Attempt:     1,
				Status:      "completed",
				CreatedAt:   base,
				StartedAt:   &base,
				CompletedAt: &completedAt,
				UpdatedAt:   sec(1),
			},
		}
		nm := nodeMetricsResult{
			nodeDisplayOrder: map[string]string{"n1": "0"},
			nodeDisplayName:  map[string]string{"n1": "Node 1"},
			nodeRenderOrder:  map[string]int{"n1": 0},
		}
		result := processAttemptData(logs, "completed", nm, float64(base.UnixMilli()), 10000)
		if len(result.logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(result.logs))
		}
		if result.attemptCountByNode["n1"] != 1 {
			t.Errorf("expected 1 attempt for n1, got %d", result.attemptCountByNode["n1"])
		}
		if result.retriedNodeCount != 0 {
			t.Errorf("expected 0 retried nodes, got %d", result.retriedNodeCount)
		}
		if result.totalRetryAttempts != 0 {
			t.Errorf("expected 0 retry attempts, got %d", result.totalRetryAttempts)
		}
	})

	t.Run("retried node is counted", func(t *testing.T) {
		completedAt1 := sec(1)
		completedAt2 := sec(3)
		startedAt2 := sec(1)
		logs := []storage.NodeExecutionAttempt{
			{
				NodeID:      "n1",
				Attempt:     1,
				Status:      "failed",
				CreatedAt:   base,
				StartedAt:   &base,
				CompletedAt: &completedAt1,
				UpdatedAt:   sec(1),
			},
			{
				NodeID:      "n1",
				Attempt:     2,
				Status:      "completed",
				CreatedAt:   sec(1),
				StartedAt:   &startedAt2,
				CompletedAt: &completedAt2,
				UpdatedAt:   sec(3),
			},
		}
		nm := nodeMetricsResult{
			nodeDisplayOrder: map[string]string{"n1": "0"},
			nodeDisplayName:  map[string]string{"n1": "Node 1"},
			nodeRenderOrder:  map[string]int{"n1": 0},
		}
		result := processAttemptData(logs, "completed", nm, float64(base.UnixMilli()), 10000)
		if result.retriedNodeCount != 1 {
			t.Errorf("expected 1 retried node, got %d", result.retriedNodeCount)
		}
		if result.totalRetryAttempts != 1 {
			t.Errorf("expected 1 total retry attempt, got %d", result.totalRetryAttempts)
		}
	})

	t.Run("attempt bars are sorted by start time", func(t *testing.T) {
		start1 := sec(2)
		start2 := sec(0)
		logs := []storage.NodeExecutionAttempt{
			{NodeID: "n1", Attempt: 1, Status: "completed", CreatedAt: sec(2), StartedAt: &start1, UpdatedAt: sec(3)},
			{NodeID: "n2", Attempt: 1, Status: "completed", CreatedAt: sec(0), StartedAt: &start2, UpdatedAt: sec(1)},
		}
		nm := nodeMetricsResult{
			nodeDisplayOrder: map[string]string{"n1": "1", "n2": "0"},
			nodeDisplayName:  map[string]string{"n1": "N1", "n2": "N2"},
			nodeRenderOrder:  map[string]int{"n1": 1, "n2": 0},
		}
		result := processAttemptData(logs, "completed", nm, float64(base.UnixMilli()), 10000)
		if len(result.attemptBars) != 2 {
			t.Fatalf("expected 2 attempt bars, got %d", len(result.attemptBars))
		}
		if result.attemptBars[0].NodeID != "n2" {
			t.Errorf("expected first bar to be n2 (earliest start), got %s", result.attemptBars[0].NodeID)
		}
	})

	t.Run("unknown node ID uses fallback display values", func(t *testing.T) {
		start := sec(0)
		end := sec(1)
		logs := []storage.NodeExecutionAttempt{
			{NodeID: "unknown", NodeType: "llm_call", Attempt: 1, Status: "completed",
				CreatedAt: base, StartedAt: &start, CompletedAt: &end, UpdatedAt: sec(1)},
		}
		nm := nodeMetricsResult{
			nodeDisplayOrder: map[string]string{},
			nodeDisplayName:  map[string]string{},
			nodeRenderOrder:  map[string]int{},
		}
		result := processAttemptData(logs, "completed", nm, float64(base.UnixMilli()), 10000)
		if len(result.attemptBars) != 1 {
			t.Fatalf("expected 1 bar, got %d", len(result.attemptBars))
		}
		if result.attemptBars[0].NodeDisplayOrder != "?" {
			t.Errorf("expected fallback order '?', got %q", result.attemptBars[0].NodeDisplayOrder)
		}
		if result.attemptBars[0].NodeDisplayName != "llm_call" {
			t.Errorf("expected fallback name 'llm_call', got %q", result.attemptBars[0].NodeDisplayName)
		}
	})
}
