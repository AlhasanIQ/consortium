package admin

import (
	"strings"
	"testing"
)

func TestBuildWorkflowExecutionStatsQueryUsesINLookup(t *testing.T) {
	query, args := buildWorkflowExecutionStatsQuery([]string{"wf-a", "wf-b"})
	if len(args) != 2 {
		t.Fatalf("args len = %d, want 2", len(args))
	}
	if !strings.Contains(query, "j.workflow_id IN (?,?)") {
		t.Fatalf("expected workflow stats query to use indexed IN lookup, got:\n%s", query)
	}
	if strings.Contains(query, "JOIN workflow_ids") {
		t.Fatalf("workflow stats query should not join VALUES workflow_ids, got:\n%s", query)
	}
}
