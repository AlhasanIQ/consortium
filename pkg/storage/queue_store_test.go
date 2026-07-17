package storage

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"
)

func createQueueStoreTestExecution(t *testing.T, store *Storage, id, status, dagHash string, createdAt time.Time) {
	t.Helper()

	exec := &WorkflowExecution{
		ID:          id,
		Description: "queue test " + id,
		Model:       "workflow",
		Status:      status,
		WorkflowID:  "wf-" + id,
	}
	if dagHash != "" {
		exec.WorkflowExecutionID = id
		exec.RunID = id
		exec.RunNumber = 1
		exec.DAGSnapshot = `{"id":"` + exec.WorkflowID + `","nodes":[{"id":"n1","type":"prompt"}],"edges":[]}`
		exec.DAGHash = dagHash
	}
	if err := store.CreateExecution(exec); err != nil {
		t.Fatalf("create execution %s failed: %v", id, err)
	}

	if !createdAt.IsZero() {
		if _, err := store.db.Exec(`
			UPDATE jobs SET created_at = ?, updated_at = ? WHERE id = ?
		`, createdAt, createdAt, id); err != nil {
			t.Fatalf("set created_at for %s failed: %v", id, err)
		}
	}
}

func setQueueStoreTestParentID(t *testing.T, store *Storage, id, parentID string) {
	t.Helper()
	if _, err := store.db.Exec(`UPDATE jobs SET parent_execution_id = ? WHERE id = ?`, parentID, id); err != nil {
		t.Fatalf("set parent_execution_id for %s failed: %v", id, err)
	}
}

func TestCountDurableActiveJobs_Filtering(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour)
	createQueueStoreTestExecution(t, store, "dur-pending", "pending", "hash-1", base)
	createQueueStoreTestExecution(t, store, "dur-running", "running", "hash-2", base.Add(time.Second))
	createQueueStoreTestExecution(t, store, "dur-child-pending", "pending", "hash-child-1", base.Add(1500*time.Millisecond))
	createQueueStoreTestExecution(t, store, "dur-child-running", "running", "hash-child-2", base.Add(1700*time.Millisecond))
	createQueueStoreTestExecution(t, store, "dur-completed", "completed", "hash-3", base.Add(2*time.Second))
	createQueueStoreTestExecution(t, store, "legacy-pending", "pending", "", base.Add(3*time.Second))
	createQueueStoreTestExecution(t, store, "legacy-running", "running", "", base.Add(4*time.Second))
	setQueueStoreTestParentID(t, store, "dur-child-pending", "parent-1")
	setQueueStoreTestParentID(t, store, "dur-child-running", "parent-2")

	count, err := store.CountDurableActiveJobs(context.Background())
	if err != nil {
		t.Fatalf("CountDurableActiveJobs failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 durable active jobs, got %d", count)
	}
}

func TestClaimPendingDurableJob_ClaimsOldestDurableOnly(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	base := time.Now().Add(-2 * time.Hour)
	createQueueStoreTestExecution(t, store, "legacy-older", "pending", "", base)
	createQueueStoreTestExecution(t, store, "dur-oldest", "pending", "hash-a", base.Add(time.Second))
	createQueueStoreTestExecution(t, store, "dur-newer", "pending", "hash-b", base.Add(2*time.Second))

	ctx := context.Background()

	first, err := store.ClaimPendingDurableJob(ctx)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if first == nil || first.ID != "dur-oldest" {
		t.Fatalf("expected first claim to be dur-oldest, got %+v", first)
	}
	if first.Status != "running" {
		t.Fatalf("expected first claimed status running, got %s", first.Status)
	}

	second, err := store.ClaimPendingDurableJob(ctx)
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if second == nil || second.ID != "dur-newer" {
		t.Fatalf("expected second claim to be dur-newer, got %+v", second)
	}

	none, err := store.ClaimPendingDurableJob(ctx)
	if err != nil {
		t.Fatalf("third claim failed: %v", err)
	}
	if none != nil {
		t.Fatalf("expected no claim after durable queue drained, got %+v", none)
	}

	legacy, err := store.GetExecution("legacy-older")
	if err != nil {
		t.Fatalf("GetExecution legacy-older failed: %v", err)
	}
	if legacy.Status != "pending" {
		t.Fatalf("expected legacy job to remain pending, got %s", legacy.Status)
	}
}

func TestListResumableDurableRunningJobs_ExcludeAndLimit(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	base := time.Now().Add(-3 * time.Hour)
	createQueueStoreTestExecution(t, store, "run-a", "running", "hash-a", base)
	createQueueStoreTestExecution(t, store, "run-b", "running", "hash-b", base.Add(time.Second))
	createQueueStoreTestExecution(t, store, "run-c", "running", "hash-c", base.Add(2*time.Second))
	createQueueStoreTestExecution(t, store, "run-legacy", "running", "", base.Add(3*time.Second))
	createQueueStoreTestExecution(t, store, "pend-dur", "pending", "hash-p", base.Add(4*time.Second))

	ctx := context.Background()

	limited, err := store.ListResumableDurableRunningJobs(ctx, nil, 1)
	if err != nil {
		t.Fatalf("ListResumableDurableRunningJobs(limit=1) failed: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != "run-a" {
		t.Fatalf("expected first durable running job run-a, got %+v", limited)
	}

	excluded, err := store.ListResumableDurableRunningJobs(ctx, []string{"run-a"}, 5)
	if err != nil {
		t.Fatalf("ListResumableDurableRunningJobs(exclude) failed: %v", err)
	}
	if len(excluded) != 2 {
		t.Fatalf("expected 2 resumable jobs after excluding run-a, got %d", len(excluded))
	}
	if excluded[0].ID != "run-b" || excluded[1].ID != "run-c" {
		t.Fatalf("expected [run-b run-c], got [%s %s]", excluded[0].ID, excluded[1].ID)
	}
}

func TestClaimPendingDurableRootAndChildJobs(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	base := time.Now().Add(-2 * time.Hour)
	createQueueStoreTestExecution(t, store, "root-oldest", "pending", "hash-r1", base)
	createQueueStoreTestExecution(t, store, "child-oldest", "pending", "hash-c1", base.Add(time.Second))
	createQueueStoreTestExecution(t, store, "root-newer", "pending", "hash-r2", base.Add(2*time.Second))
	createQueueStoreTestExecution(t, store, "child-newer", "pending", "hash-c2", base.Add(3*time.Second))

	setQueueStoreTestParentID(t, store, "child-oldest", "parent-1")
	setQueueStoreTestParentID(t, store, "child-newer", "parent-2")

	ctx := context.Background()

	child, err := store.ClaimPendingDurableChildJob(ctx)
	if err != nil {
		t.Fatalf("ClaimPendingDurableChildJob failed: %v", err)
	}
	if child == nil || child.ID != "child-oldest" {
		t.Fatalf("expected child-oldest, got %+v", child)
	}
	if child.ParentExecutionID == "" {
		t.Fatalf("expected child claim to preserve parent_execution_id")
	}

	root, err := store.ClaimPendingDurableRootJob(ctx)
	if err != nil {
		t.Fatalf("ClaimPendingDurableRootJob failed: %v", err)
	}
	if root == nil || root.ID != "root-oldest" {
		t.Fatalf("expected root-oldest, got %+v", root)
	}
	if root.ParentExecutionID != "" {
		t.Fatalf("expected root job to have empty parent_execution_id, got %q", root.ParentExecutionID)
	}
}

func TestClaimPendingDurableJob_ConcurrentClaimsEachJobOnce(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	const jobCount = 8
	base := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < jobCount; i++ {
		createQueueStoreTestExecution(t, store, "claim-concurrent-"+string(rune('a'+i)), "pending", "hash-concurrent", base.Add(time.Duration(i)*time.Minute))
	}

	start := make(chan struct{})
	results := make(chan *WorkflowExecution, jobCount)
	errCh := make(chan error, jobCount)
	var wg sync.WaitGroup
	for i := 0; i < jobCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, err := store.ClaimPendingDurableJob(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			results <- claimed
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent claim failed: %v", err)
	}
	seen := make(map[string]bool, jobCount)
	for claimed := range results {
		if claimed == nil {
			t.Fatal("concurrent claim returned nil while durable jobs remained")
		}
		if claimed.Status != "running" {
			t.Errorf("claimed job %s status = %q, want running", claimed.ID, claimed.Status)
		}
		if seen[claimed.ID] {
			t.Errorf("job %s was claimed more than once", claimed.ID)
		}
		seen[claimed.ID] = true
	}
	if len(seen) != jobCount {
		t.Fatalf("claimed %d unique jobs, want %d", len(seen), jobCount)
	}

	pending, err := store.CountPendingDurableJobs(context.Background())
	if err != nil {
		t.Fatalf("CountPendingDurableJobs after concurrent claims: %v", err)
	}
	if pending != 0 {
		t.Fatalf("pending durable jobs after claiming each worker once = %d, want 0", pending)
	}
}

func TestDurableQueueQueryPlansUseQueueIndexes(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	cases := []struct {
		name      string
		query     string
		args      []interface{}
		wantIndex string
	}{
		{
			name:      "pending root claim",
			query:     pendingDurableJobClaimSQL(durableQueueScopeRoot),
			args:      []interface{}{time.Now()},
			wantIndex: "idx_jobs_pending_durable_root_created",
		},
		{
			name:      "pending child claim",
			query:     pendingDurableJobClaimSQL(durableQueueScopeChild),
			args:      []interface{}{time.Now()},
			wantIndex: "idx_jobs_pending_durable_child_created",
		},
		{
			name:      "running count",
			query:     durableJobCountSQL("running", durableQueueScopeAny),
			wantIndex: "idx_jobs_running_durable_created",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := explainQueryPlan(t, store.db, tc.query, tc.args...)
			if !strings.Contains(plan, tc.wantIndex) {
				t.Fatalf("expected plan to use %s, got:\n%s", tc.wantIndex, plan)
			}
			if strings.Contains(plan, "USE TEMP B-TREE") {
				t.Fatalf("expected plan to avoid temp b-tree, got:\n%s", plan)
			}
		})
	}
}

func explainQueryPlan(t *testing.T, db *sql.DB, query string, args ...interface{}) string {
	t.Helper()

	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain query plan failed: %v", err)
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan explain row failed: %v", err)
		}
		parts = append(parts, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate explain rows failed: %v", err)
	}
	return strings.Join(parts, "\n")
}
