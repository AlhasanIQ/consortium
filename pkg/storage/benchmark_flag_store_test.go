package storage

import (
	"strings"
	"testing"
)

func newFlagStore(t *testing.T) *Storage {
	t.Helper()
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestUpsertActiveFlags_InsertSingle(t *testing.T) {
	store := newFlagStore(t)

	flags, err := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "global-mmlu-lite", Split: "test", ItemID: "row-393", Reason: "Wrong gold label", Source: "human:manual"},
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if len(flags) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(flags))
	}
	f := flags[0]
	if f.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if f.Benchmark != "global-mmlu-lite" || f.Split != "test" || f.ItemID != "row-393" {
		t.Errorf("unexpected natural key: %s/%s/%s", f.Benchmark, f.Split, f.ItemID)
	}
	if f.Reason != "Wrong gold label" {
		t.Errorf("unexpected reason: %s", f.Reason)
	}
	if f.Source != "human:manual" {
		t.Errorf("unexpected source: %s", f.Source)
	}
	if f.ResolvedAt != nil {
		t.Error("new flag should not be resolved")
	}
}

func TestUpsertActiveFlags_BatchInsert(t *testing.T) {
	store := newFlagStore(t)

	flags, err := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong A"},
		{Benchmark: "bench", Split: "test", ItemID: "row-2", Reason: "wrong B"},
		{Benchmark: "bench", Split: "test", ItemID: "row-3", Reason: "wrong C"},
	})
	if err != nil {
		t.Fatalf("batch upsert failed: %v", err)
	}
	if len(flags) != 3 {
		t.Fatalf("expected 3 flags, got %d", len(flags))
	}

	// Each should have a unique ID
	ids := map[int64]bool{}
	for _, f := range flags {
		ids[f.ID] = true
	}
	if len(ids) != 3 {
		t.Error("expected 3 unique IDs")
	}
}

func TestUpsertActiveFlags_UpdateExisting(t *testing.T) {
	store := newFlagStore(t)

	// Insert initial flag
	flags, err := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "initial reason", Source: "v1"},
	})
	if err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}
	origID := flags[0].ID

	// Upsert same item — should update, not insert
	flags2, err := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "updated reason", Source: "v2"},
	})
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}
	if len(flags2) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(flags2))
	}
	if flags2[0].ID != origID {
		t.Errorf("expected same ID %d, got %d", origID, flags2[0].ID)
	}
	if flags2[0].Reason != "updated reason" {
		t.Errorf("expected updated reason, got %q", flags2[0].Reason)
	}
	if flags2[0].Source != "v2" {
		t.Errorf("expected updated source, got %q", flags2[0].Source)
	}
}

func TestUpsertActiveFlags_ValidationErrors(t *testing.T) {
	store := newFlagStore(t)

	tests := []struct {
		name string
		flag DatasetFlag
	}{
		{"empty benchmark", DatasetFlag{Split: "test", ItemID: "row-1", Reason: "r"}},
		{"empty split", DatasetFlag{Benchmark: "bench", ItemID: "row-1", Reason: "r"}},
		{"empty item_id", DatasetFlag{Benchmark: "bench", Split: "test", Reason: "r"}},
		{"empty reason", DatasetFlag{Benchmark: "bench", Split: "test", ItemID: "row-1"}},
		{"whitespace reason", DatasetFlag{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "   "}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.UpsertActiveFlags([]DatasetFlag{tc.flag})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUpsertActiveFlags_EmptySlice(t *testing.T) {
	store := newFlagStore(t)

	flags, err := store.UpsertActiveFlags([]DatasetFlag{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags != nil {
		t.Errorf("expected nil, got %v", flags)
	}
}

func TestResolveFlag_ByID(t *testing.T) {
	store := newFlagStore(t)

	flags, _ := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong"},
	})
	id := flags[0].ID

	err := store.ResolveFlag(id, "tester", "verified correct")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	// Verify it's resolved
	all, _ := store.ListFlags("bench", "test", false)
	if len(all) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(all))
	}
	if all[0].ResolvedAt == nil {
		t.Error("expected resolved_at to be set")
	}
	if all[0].ResolvedBy != "tester" {
		t.Errorf("expected resolved_by=tester, got %q", all[0].ResolvedBy)
	}
	if all[0].ResolvedReason != "verified correct" {
		t.Errorf("expected resolved_reason, got %q", all[0].ResolvedReason)
	}
}

func TestResolveFlag_AlreadyResolved(t *testing.T) {
	store := newFlagStore(t)

	flags, _ := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong"},
	})
	_ = store.ResolveFlag(flags[0].ID, "tester", "verified correct")

	// Second resolve should fail
	err := store.ResolveFlag(flags[0].ID, "tester", "again")
	if err == nil {
		t.Fatal("expected error for already-resolved flag")
	}
	if !strings.Contains(err.Error(), "already resolved") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolveFlag_RequiresReason(t *testing.T) {
	store := newFlagStore(t)

	flags, _ := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong"},
	})
	err := store.ResolveFlag(flags[0].ID, "tester", "")
	if err == nil {
		t.Fatal("expected error for empty reason")
	}
}

func TestResolveFlagByItem(t *testing.T) {
	store := newFlagStore(t)

	store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong"},
	})

	err := store.ResolveFlagByItem("bench", "test", "row-1", "tester", "verified correct")
	if err != nil {
		t.Fatalf("resolve by item failed: %v", err)
	}

	// Verify it's resolved
	active, _ := store.ListFlags("bench", "test", true)
	if len(active) != 0 {
		t.Errorf("expected 0 active flags, got %d", len(active))
	}
}

func TestResolveFlagByItem_NotFound(t *testing.T) {
	store := newFlagStore(t)

	err := store.ResolveFlagByItem("bench", "test", "row-1", "tester", "reason")
	if err == nil {
		t.Fatal("expected error for non-existent flag")
	}
	if !strings.Contains(err.Error(), "no active flag") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolvedFlagDoesNotBlockNewFlag(t *testing.T) {
	store := newFlagStore(t)

	// Create and resolve a flag
	store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong v1"},
	})
	store.ResolveFlagByItem("bench", "test", "row-1", "tester", "verified")

	// Should be able to create a new active flag for the same item
	flags, err := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong v2"},
	})
	if err != nil {
		t.Fatalf("expected re-flag to succeed: %v", err)
	}
	if flags[0].Reason != "wrong v2" {
		t.Errorf("expected new reason, got %q", flags[0].Reason)
	}

	// Should now have 2 total flags (1 resolved + 1 active)
	all, _ := store.ListFlags("bench", "test", false)
	if len(all) != 2 {
		t.Errorf("expected 2 total flags, got %d", len(all))
	}
	active, _ := store.ListFlags("bench", "test", true)
	if len(active) != 1 {
		t.Errorf("expected 1 active flag, got %d", len(active))
	}
}

func TestListFlags_Filtering(t *testing.T) {
	store := newFlagStore(t)

	store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench-a", Split: "test", ItemID: "row-1", Reason: "wrong 1"},
		{Benchmark: "bench-a", Split: "dev", ItemID: "row-2", Reason: "wrong 2"},
		{Benchmark: "bench-b", Split: "test", ItemID: "row-3", Reason: "wrong 3"},
	})

	// All flags
	all, err := store.ListFlags("", "", false)
	if err != nil {
		t.Fatalf("list all failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 flags, got %d", len(all))
	}

	// Filter by benchmark
	benchA, _ := store.ListFlags("bench-a", "", false)
	if len(benchA) != 2 {
		t.Errorf("expected 2 bench-a flags, got %d", len(benchA))
	}

	// Filter by benchmark+split
	benchATest, _ := store.ListFlags("bench-a", "test", false)
	if len(benchATest) != 1 {
		t.Errorf("expected 1 bench-a/test flag, got %d", len(benchATest))
	}
}

func TestListFlagsByItems(t *testing.T) {
	store := newFlagStore(t)

	store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong 1"},
		{Benchmark: "bench", Split: "test", ItemID: "row-2", Reason: "wrong 2"},
		{Benchmark: "bench", Split: "test", ItemID: "row-3", Reason: "wrong 3"},
	})

	// Look up subset
	flags, err := store.ListFlagsByItems("bench", "test", []string{"row-1", "row-3", "row-99"})
	if err != nil {
		t.Fatalf("list by items failed: %v", err)
	}
	if len(flags) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(flags))
	}
	ids := map[string]bool{}
	for _, f := range flags {
		ids[f.ItemID] = true
	}
	if !ids["row-1"] || !ids["row-3"] {
		t.Errorf("expected row-1 and row-3, got %v", ids)
	}
}

func TestListFlagsByItems_ExcludesResolved(t *testing.T) {
	store := newFlagStore(t)

	store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong"},
		{Benchmark: "bench", Split: "test", ItemID: "row-2", Reason: "wrong"},
	})
	store.ResolveFlagByItem("bench", "test", "row-1", "tester", "ok")

	flags, _ := store.ListFlagsByItems("bench", "test", []string{"row-1", "row-2"})
	if len(flags) != 1 {
		t.Fatalf("expected 1 active flag, got %d", len(flags))
	}
	if flags[0].ItemID != "row-2" {
		t.Errorf("expected row-2, got %s", flags[0].ItemID)
	}
}

func TestListFlagsByItems_EmptySlice(t *testing.T) {
	store := newFlagStore(t)

	flags, err := store.ListFlagsByItems("bench", "test", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags != nil {
		t.Errorf("expected nil, got %v", flags)
	}
}

func TestDeleteFlag(t *testing.T) {
	store := newFlagStore(t)

	flags, _ := store.UpsertActiveFlags([]DatasetFlag{
		{Benchmark: "bench", Split: "test", ItemID: "row-1", Reason: "wrong"},
	})

	err := store.DeleteFlag(flags[0].ID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Should be gone entirely
	all, _ := store.ListFlags("bench", "test", false)
	if len(all) != 0 {
		t.Errorf("expected 0 flags after delete, got %d", len(all))
	}
}

func TestDeleteFlag_NotFound(t *testing.T) {
	store := newFlagStore(t)

	err := store.DeleteFlag(9999)
	if err == nil {
		t.Fatal("expected error for non-existent flag")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}
