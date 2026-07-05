package admin

import (
	"context"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
)

func TestLoadJobCountSnapshotDerivesScopedCounts(t *testing.T) {
	store, db, cleanup := setupTestDB(t)
	defer cleanup()

	createAdminCountTestExecution(t, store, "root-completed-1", "completed", "")
	createAdminCountTestExecution(t, store, "root-completed-2", "completed", "")
	createAdminCountTestExecution(t, store, "root-failed", "failed", "")
	createAdminCountTestExecution(t, store, "child-running", "running", "root-completed-1")
	createAdminCountTestExecution(t, store, "child-failed", "failed", "root-completed-1")

	server := NewServer(store, db, providers.NewRegistry(), nil, "")
	snapshot, err := server.loadJobCountSnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadJobCountSnapshot failed: %v", err)
	}

	parentStatus := snapshot.statusCountsForShow("parents")
	if parentStatus["completed"] != 2 || parentStatus["failed"] != 1 {
		t.Fatalf("unexpected parent status counts: %+v", parentStatus)
	}
	childStatus := snapshot.statusCountsForShow("children")
	if childStatus["running"] != 1 || childStatus["failed"] != 1 {
		t.Fatalf("unexpected child status counts: %+v", childStatus)
	}
	allStatus := snapshot.statusCountsForShow("all")
	if allStatus["completed"] != 2 || allStatus["failed"] != 2 || allStatus["running"] != 1 {
		t.Fatalf("unexpected all status counts: %+v", allStatus)
	}

	parentCount, childCount, allCount := snapshot.scopeCounts("")
	if parentCount != 3 || childCount != 2 || allCount != 5 {
		t.Fatalf("unexpected unfiltered scope counts: parents=%d children=%d all=%d", parentCount, childCount, allCount)
	}
	parentCount, childCount, allCount = snapshot.scopeCounts("failed")
	if parentCount != 1 || childCount != 1 || allCount != 2 {
		t.Fatalf("unexpected failed scope counts: parents=%d children=%d all=%d", parentCount, childCount, allCount)
	}
	if got := snapshot.totalItems("parents", "completed"); got != 2 {
		t.Fatalf("parents completed total = %d, want 2", got)
	}
	if got := snapshot.activeRunningAll(); got != 1 {
		t.Fatalf("active running all = %d, want 1", got)
	}
}

func TestJobCountSnapshotSQLUsesScopeStatusIndexes(t *testing.T) {
	query := jobCountSnapshotSQL()
	if !strings.Contains(query, "INDEXED BY idx_jobs_root_status") {
		t.Fatalf("expected parent count branch to force root status index, got:\n%s", query)
	}
	if !strings.Contains(query, "INDEXED BY idx_jobs_child_status") {
		t.Fatalf("expected child count branch to force child status index, got:\n%s", query)
	}
}

func TestBuildExecutionCostSummariesQueryDrivesAttemptLookupFromTreeJobs(t *testing.T) {
	query, args := buildExecutionCostSummariesQuery([]string{"job-a"})
	if len(args) != 1 || args[0] != "job-a" {
		t.Fatalf("unexpected args: %+v", args)
	}
	if !strings.Contains(query, "CROSS JOIN workflow_node_execution_attempts") {
		t.Fatalf("expected cost summary query to force tree_jobs before attempts, got:\n%s", query)
	}
	if !strings.Contains(query, "INDEXED BY idx_workflow_node_execution_attempts_job_node_attempt") {
		t.Fatalf("expected cost summary query to force the attempts job index, got:\n%s", query)
	}
}

func createAdminCountTestExecution(t *testing.T, store *storage.Storage, id, status, parentID string) {
	t.Helper()
	exec := &storage.WorkflowExecution{
		ID:                id,
		Description:       "admin count test " + id,
		Model:             "workflow",
		Status:            status,
		ParentExecutionID: parentID,
	}
	if err := store.CreateExecution(exec); err != nil {
		t.Fatalf("create execution %s failed: %v", id, err)
	}
}
