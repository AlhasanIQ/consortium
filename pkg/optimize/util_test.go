package optimize

import (
	"encoding/json"
	"testing"
)

func TestCopyStringMap(t *testing.T) {
	t.Run("nil input returns empty map", func(t *testing.T) {
		result := copyStringMap(nil)
		if result == nil {
			t.Fatal("expected non-nil empty map")
		}
		if len(result) != 0 {
			t.Errorf("expected empty map, got %d entries", len(result))
		}
	})

	t.Run("copies all entries", func(t *testing.T) {
		src := map[string]string{"a": "1", "b": "2"}
		dst := copyStringMap(src)
		if len(dst) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(dst))
		}
		if dst["a"] != "1" || dst["b"] != "2" {
			t.Errorf("unexpected values: %v", dst)
		}
	})

	t.Run("mutation does not affect original", func(t *testing.T) {
		src := map[string]string{"key": "val"}
		dst := copyStringMap(src)
		dst["key"] = "changed"
		dst["new"] = "added"
		if src["key"] != "val" {
			t.Error("original was mutated")
		}
		if _, exists := src["new"]; exists {
			t.Error("original gained new key")
		}
	})
}

func TestEncodeJSONValue(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"string", "hello", `"hello"`},
		{"number", 42.0, "42"},
		{"boolean", true, "true"},
		{"null", nil, "null"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeJSONValue(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("encodeJSONValue(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBuildParentParamValues_NilParent(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.model", Type: ParamTypeModel},
		},
	}
	_, err := buildParentParamValues(nil, spec)
	if err == nil {
		t.Fatal("expected error for nil parent")
	}
}

func TestBuildParentParamValues_Complete(t *testing.T) {
	parent := &Organism{
		ParamValues: map[string]string{
			"nodes[a].data.config.model": `"gpt-4"`,
			"nodes[a].data.config.temp":  "0.5",
		},
	}
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.model", Type: ParamTypeModel},
			{Path: "nodes[a].data.config.temp", Type: ParamTypeFloat},
		},
	}
	values, err := buildParentParamValues(parent, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
	if values["nodes[a].data.config.model"] != `"gpt-4"` {
		t.Errorf("model = %q, want %q", values["nodes[a].data.config.model"], `"gpt-4"`)
	}
}

func TestBuildParentParamValues_FillsFromWorkflowJSON(t *testing.T) {
	parent := &Organism{
		ParamValues: map[string]string{
			"nodes[a].data.config.model": `"gpt-4"`,
		},
		WorkflowJSON: json.RawMessage(`{
			"nodes": [
				{"id": "a", "data": {"config": {"model": "gpt-4", "temperature": 0.7}}}
			]
		}`),
	}
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.model", Type: ParamTypeModel},
			{Path: "nodes[a].data.config.temperature", Type: ParamTypeFloat},
		},
	}
	values, err := buildParentParamValues(parent, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
	// Temperature should be filled from workflow JSON.
	if values["nodes[a].data.config.temperature"] != "0.7" {
		t.Errorf("temperature = %q, want %q", values["nodes[a].data.config.temperature"], "0.7")
	}
}

func TestNewChildOrganism(t *testing.T) {
	parent := &Organism{
		ID:       "parent-id",
		OptRunID: "run-123",
	}
	values := map[string]string{"nodes[a].data.config.model": `"claude-3"`}
	child := newChildOrganism(parent, 2, "llm", "  test mutation  ", values)

	if child.ID == "" {
		t.Error("child ID should be generated")
	}
	if child.OptRunID != "run-123" {
		t.Errorf("OptRunID = %q, want %q", child.OptRunID, "run-123")
	}
	if child.Generation != 2 {
		t.Errorf("Generation = %d, want 2", child.Generation)
	}
	if len(child.ParentIDs) != 1 || child.ParentIDs[0] != "parent-id" {
		t.Errorf("ParentIDs = %v, want [parent-id]", child.ParentIDs)
	}
	if child.MutationType != "llm" {
		t.Errorf("MutationType = %q, want %q", child.MutationType, "llm")
	}
	if child.MutationLog != "test mutation" {
		t.Errorf("MutationLog = %q, want trimmed", child.MutationLog)
	}
}

func TestNewChildOrganism_NilParent(t *testing.T) {
	child := newChildOrganism(nil, 0, "seed", "baseline", nil)
	if child.OptRunID != "" {
		t.Errorf("OptRunID should be empty for nil parent, got %q", child.OptRunID)
	}
	if len(child.ParentIDs) != 0 {
		t.Errorf("ParentIDs should be empty for nil parent, got %v", child.ParentIDs)
	}
}
