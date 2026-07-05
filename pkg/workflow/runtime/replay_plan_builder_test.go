package runtime

import "testing"

func TestBuildReplayPlan_AllReusableWhenDAGUnchanged(t *testing.T) {
	base := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "a", Type: "prompt", Prompt: "A"},
		{ID: "b", Type: "prompt", Prompt: "B"},
		{ID: "c", Type: "prompt", Prompt: "C"},
	}, []EdgeForFreeze{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	})

	candidate := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "a", Type: "prompt", Prompt: "A"},
		{ID: "b", Type: "prompt", Prompt: "B"},
		{ID: "c", Type: "prompt", Prompt: "C"},
	}, []EdgeForFreeze{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	})

	plan, err := BuildReplayPlan(base, candidate, nil)
	if err != nil {
		t.Fatalf("BuildReplayPlan failed: %v", err)
	}

	assertStringSliceEqual(t, "reuse", plan.ReuseNodeIDs, []string{"a", "b", "c"})
	assertStringSliceEqual(t, "execute", plan.ExecuteNodeIDs, nil)
}

func TestBuildReplayPlan_NodeChangePropagatesDownstream(t *testing.T) {
	base := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "a", Type: "prompt", Prompt: "A"},
		{ID: "b", Type: "prompt", Prompt: "B"},
		{ID: "c", Type: "prompt", Prompt: "C"},
	}, []EdgeForFreeze{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	})

	candidate := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "a", Type: "prompt", Prompt: "A"},
		{ID: "b", Type: "prompt", Prompt: "B changed"},
		{ID: "c", Type: "prompt", Prompt: "C"},
	}, []EdgeForFreeze{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	})

	plan, err := BuildReplayPlan(base, candidate, nil)
	if err != nil {
		t.Fatalf("BuildReplayPlan failed: %v", err)
	}

	assertStringSliceEqual(t, "reuse", plan.ReuseNodeIDs, []string{"a"})
	assertStringSliceEqual(t, "execute", plan.ExecuteNodeIDs, []string{"b", "c"})
	if got := plan.ReasonByNode["b"]; got != "node_changed" {
		t.Fatalf("reason[b] = %q, want %q", got, "node_changed")
	}
	if got := plan.ReasonByNode["c"]; got != "upstream_dirty:b" {
		t.Fatalf("reason[c] = %q, want %q", got, "upstream_dirty:b")
	}
}

func TestBuildReplayPlan_EdgeChangeMarksDependencyTargetDirty(t *testing.T) {
	base := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "a", Type: "prompt", Prompt: "A"},
		{ID: "b", Type: "prompt", Prompt: "B"},
		{ID: "c", Type: "prompt", Prompt: "C"},
	}, []EdgeForFreeze{
		{Source: "a", Target: "c"},
		{Source: "b", Target: "c"},
	})

	candidate := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "a", Type: "prompt", Prompt: "A"},
		{ID: "b", Type: "prompt", Prompt: "B"},
		{ID: "c", Type: "prompt", Prompt: "C"},
	}, []EdgeForFreeze{
		{Source: "a", Target: "c"},
	})

	plan, err := BuildReplayPlan(base, candidate, nil)
	if err != nil {
		t.Fatalf("BuildReplayPlan failed: %v", err)
	}

	assertStringSliceEqual(t, "reuse", plan.ReuseNodeIDs, []string{"a", "b"})
	assertStringSliceEqual(t, "execute", plan.ExecuteNodeIDs, []string{"c"})
	if got := plan.ReasonByNode["c"]; got != "dependencies_changed" {
		t.Fatalf("reason[c] = %q, want %q", got, "dependencies_changed")
	}
}

func TestBuildReplayPlan_ForceDirtyOverridesDiff(t *testing.T) {
	base := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "child", Type: "child_workflow"},
		{ID: "contract", Type: "prompt"},
		{ID: "result", Type: "result"},
	}, []EdgeForFreeze{
		{Source: "child", Target: "contract"},
		{Source: "contract", Target: "result"},
	})

	candidate := mustFreezeReplayTestWorkflow(t, []NodeForFreeze{
		{ID: "child", Type: "child_workflow"},
		{ID: "contract", Type: "prompt"},
		{ID: "result", Type: "result"},
	}, []EdgeForFreeze{
		{Source: "child", Target: "contract"},
		{Source: "contract", Target: "result"},
	})

	plan, err := BuildReplayPlan(base, candidate, &ReplayPlanOptions{
		ForceDirtyNodeIDs: []string{"child"},
		ForceReasonByNode: map[string]string{"child": "child_workflow_changed"},
	})
	if err != nil {
		t.Fatalf("BuildReplayPlan failed: %v", err)
	}

	assertStringSliceEqual(t, "reuse", plan.ReuseNodeIDs, nil)
	assertStringSliceEqual(t, "execute", plan.ExecuteNodeIDs, []string{"child", "contract", "result"})
	if got := plan.ReasonByNode["child"]; got != "child_workflow_changed" {
		t.Fatalf("reason[child] = %q, want %q", got, "child_workflow_changed")
	}
}

func mustFreezeReplayTestWorkflow(t *testing.T, nodes []NodeForFreeze, edges []EdgeForFreeze) *FrozenSnapshot {
	t.Helper()
	snapshot, err := FreezeWorkflow("wf-replay-test", "Replay Test", "", nodes, edges, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}
	return snapshot
}

func assertStringSliceEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d (%v vs %v)", label, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q (got=%v want=%v)", label, i, got[i], want[i], got, want)
		}
	}
}
