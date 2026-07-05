package optimize

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- trimForPrompt ---

func TestTrimForPrompt(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{"empty string", "", 100, ""},
		{"whitespace only", "   ", 100, ""},
		{"zero limit", "hello", 0, ""},
		{"negative limit", "hello", -1, ""},
		{"short enough", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello"},
		{"trims leading whitespace", "  hi  ", 4, "hi"},
		{"unicode safe (byte truncation)", "abcdef", 3, "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := trimForPrompt(tc.text, tc.limit)
			if got != tc.want {
				t.Errorf("trimForPrompt(%q, %d) = %q, want %q", tc.text, tc.limit, got, tc.want)
			}
		})
	}
}

// --- sortedCategoryCounts ---

func TestSortedCategoryCounts(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := sortedCategoryCounts(nil)
		if result != nil {
			t.Errorf("expected nil for empty input, got %v", result)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		result := sortedCategoryCounts(map[string]int{})
		if result != nil {
			t.Errorf("expected nil for empty map, got %v", result)
		}
	})

	t.Run("single category", func(t *testing.T) {
		result := sortedCategoryCounts(map[string]int{"math": 3})
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if !strings.Contains(result[0], "math") || !strings.Contains(result[0], "3") {
			t.Errorf("unexpected result format: %q", result[0])
		}
	})

	t.Run("sorted by count descending", func(t *testing.T) {
		counts := map[string]int{
			"rare":   1,
			"common": 5,
			"middle": 3,
		}
		result := sortedCategoryCounts(counts)
		if len(result) != 3 {
			t.Fatalf("expected 3 results, got %d", len(result))
		}
		// First line must contain "common" (highest count).
		if !strings.Contains(result[0], "common") {
			t.Errorf("first entry should be 'common' (count=5), got %q", result[0])
		}
		// Last line must contain "rare" (lowest count).
		if !strings.Contains(result[2], "rare") {
			t.Errorf("last entry should be 'rare' (count=1), got %q", result[2])
		}
	})

	t.Run("tie broken by category name alphabetically", func(t *testing.T) {
		counts := map[string]int{
			"zebra": 2,
			"apple": 2,
		}
		result := sortedCategoryCounts(counts)
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
		if !strings.Contains(result[0], "apple") {
			t.Errorf("tie should break alphabetically: expected 'apple' first, got %q", result[0])
		}
	})
}

// --- selectPromptLearningEntries ---

func TestSelectPromptLearningEntries(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := selectPromptLearningEntries(nil, 10)
		if result != nil {
			t.Errorf("expected nil for nil input, got %v", result)
		}
	})

	t.Run("zero limit", func(t *testing.T) {
		entries := []LearningEntry{{MutationType: "llm_prompt", Outcome: "regression"}}
		result := selectPromptLearningEntries(entries, 0)
		if result != nil {
			t.Errorf("expected nil for zero limit, got %v", result)
		}
	})

	t.Run("skips improvements", func(t *testing.T) {
		entries := []LearningEntry{
			{MutationType: "llm_prompt", Outcome: "improvement"},
			{MutationType: "llm_prompt", Outcome: "regression"},
		}
		result := selectPromptLearningEntries(entries, 10)
		for _, e := range result {
			if e.Outcome == "improvement" {
				t.Error("improvements should be excluded from selection")
			}
		}
	})

	t.Run("prefers prompt entries over non-prompt", func(t *testing.T) {
		entries := []LearningEntry{
			{MutationType: "combinatorial", Outcome: "regression"},
			{MutationType: "llm_prompt", Outcome: "regression"},
		}
		result := selectPromptLearningEntries(entries, 10)
		if len(result) == 0 {
			t.Fatal("expected results")
		}
		for _, e := range result {
			if !strings.Contains(strings.ToLower(e.MutationType), "prompt") {
				t.Errorf("expected only prompt entries when available, got %q", e.MutationType)
			}
		}
	})

	t.Run("falls back to all non-improving when no prompt entries", func(t *testing.T) {
		entries := []LearningEntry{
			{MutationType: "combinatorial", Outcome: "regression"},
			{MutationType: "combinatorial", Outcome: "no_change"},
		}
		result := selectPromptLearningEntries(entries, 10)
		if len(result) != 2 {
			t.Errorf("expected 2 fallback entries, got %d", len(result))
		}
	})

	t.Run("limit applied", func(t *testing.T) {
		entries := make([]LearningEntry, 30)
		for i := range entries {
			entries[i] = LearningEntry{MutationType: "llm_prompt", Outcome: "regression"}
		}
		result := selectPromptLearningEntries(entries, 5)
		if len(result) != 5 {
			t.Errorf("expected 5 (limited), got %d", len(result))
		}
	})

	t.Run("returns most recent first (reverse iteration)", func(t *testing.T) {
		// Entries appended in order; selectPromptLearningEntries iterates from end to start.
		entries := []LearningEntry{
			{MutationType: "llm_prompt", Outcome: "regression", Generation: 1},
			{MutationType: "llm_prompt", Outcome: "regression", Generation: 2},
			{MutationType: "llm_prompt", Outcome: "regression", Generation: 3},
		}
		result := selectPromptLearningEntries(entries, 10)
		if len(result) != 3 {
			t.Fatalf("expected 3 results, got %d", len(result))
		}
		// First result should be the most recent (generation 3).
		if result[0].Generation != 3 {
			t.Errorf("expected most recent entry first (gen=3), got gen=%d", result[0].Generation)
		}
	})
}

// --- decodePromptValue ---

func TestDecodePromptValue(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		want    string
		wantErr bool
	}{
		{"empty string", "", "", true},
		{"whitespace only", "   ", "", true},
		{"valid JSON string", `"hello world"`, "hello world", false},
		{"invalid JSON", `not json`, "", true},
		{"number instead of string", `42`, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodePromptValue(tc.encoded)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- objectiveWeight ---

func TestObjectiveWeight(t *testing.T) {
	tests := []struct {
		name   string
		spec   *OptimizeSpec
		metric string
		want   float64
	}{
		{"nil spec defaults to 1", nil, "accuracy", 1.0},
		{
			"exact metric match",
			&OptimizeSpec{Objectives: []Objective{{Metric: "accuracy", Weight: 0.8}}},
			"accuracy",
			0.8,
		},
		{
			"adjusted_accuracy matches accuracy query",
			&OptimizeSpec{Objectives: []Objective{{Metric: "adjusted_accuracy", Weight: 0.75}}},
			"accuracy",
			0.75,
		},
		{
			"cost_per_item match",
			&OptimizeSpec{Objectives: []Objective{
				{Metric: "accuracy", Weight: 0.9},
				{Metric: "cost_per_item", Weight: 0.1},
			}},
			"cost_per_item",
			0.1,
		},
		{
			"metric not found defaults to 1",
			&OptimizeSpec{Objectives: []Objective{{Metric: "accuracy", Weight: 0.8}}},
			"latency",
			1.0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := objectiveWeight(tc.spec, tc.metric)
			if got != tc.want {
				t.Errorf("objectiveWeight(%q) = %v, want %v", tc.metric, got, tc.want)
			}
		})
	}
}

// --- coalesceString ---

func TestCoalesceString(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{"non-empty value used", "primary", "fallback", "primary"},
		{"empty value uses fallback", "", "fallback", "fallback"},
		{"whitespace-only value uses fallback", "   ", "fallback", "fallback"},
		{"value with spaces trimmed", "  hello  ", "fallback", "hello"},
		{"both empty returns empty fallback", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := coalesceString(tc.value, tc.fallback)
			if got != tc.want {
				t.Errorf("coalesceString(%q, %q) = %q, want %q", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

// --- extractWorkflowID ---

func TestExtractWorkflowID(t *testing.T) {
	tests := []struct {
		name string
		json json.RawMessage
		want string
	}{
		{"nil json", nil, ""},
		{"empty json", json.RawMessage{}, ""},
		{"invalid json", json.RawMessage(`not-json`), ""},
		{"json without id", json.RawMessage(`{"name":"wf"}`), ""},
		{"json with id", json.RawMessage(`{"id":"my-workflow","name":"wf"}`), "my-workflow"},
		{"id is not string", json.RawMessage(`{"id":123}`), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractWorkflowID(tc.json)
			if got != tc.want {
				t.Errorf("extractWorkflowID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- formatNodeTraces ---

func TestFormatNodeTraces(t *testing.T) {
	t.Run("empty traces returns empty string", func(t *testing.T) {
		got := formatNodeTraces(nil)
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("trace without model", func(t *testing.T) {
		traces := []FailureNodeTrace{{NodeID: "node-a", Output: "some output"}}
		got := formatNodeTraces(traces)
		if !strings.Contains(got, "node-a") {
			t.Errorf("output should contain node ID 'node-a', got %q", got)
		}
		if !strings.Contains(got, "some output") {
			t.Errorf("output should contain trace output, got %q", got)
		}
	})

	t.Run("trace with model", func(t *testing.T) {
		traces := []FailureNodeTrace{{NodeID: "node-b", Model: "gpt-4", Output: "result"}}
		got := formatNodeTraces(traces)
		if !strings.Contains(got, "model=gpt-4") {
			t.Errorf("output should contain model, got %q", got)
		}
	})

	t.Run("long output is truncated", func(t *testing.T) {
		longOutput := strings.Repeat("x", 500)
		traces := []FailureNodeTrace{{NodeID: "node-c", Output: longOutput}}
		got := formatNodeTraces(traces)
		// trimForPrompt uses 200 chars; total line should not embed 500 chars.
		if strings.Contains(got, longOutput) {
			t.Error("long output should have been truncated")
		}
	})

	t.Run("multiple traces rendered", func(t *testing.T) {
		traces := []FailureNodeTrace{
			{NodeID: "n1", Output: "out1"},
			{NodeID: "n2", Output: "out2"},
		}
		got := formatNodeTraces(traces)
		if !strings.Contains(got, "n1") || !strings.Contains(got, "n2") {
			t.Errorf("both node IDs should appear in output, got %q", got)
		}
	})
}
