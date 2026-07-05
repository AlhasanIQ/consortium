package optimize

import "testing"

func TestCollectPromptGroups_NilSpec(t *testing.T) {
	groups := collectPromptGroups(nil)
	if groups != nil {
		t.Errorf("expected nil for nil spec, got %v", groups)
	}
}

func TestCollectPromptGroups_NoPromptParams(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"x"}},
			{Path: "nodes[a].data.config.temp", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1}},
		},
	}
	groups := collectPromptGroups(spec)
	if groups != nil {
		t.Errorf("expected nil for no prompt params, got %v", groups)
	}
}

func TestCollectPromptGroups_SinglePromptNoGroup(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
	}
	groups := collectPromptGroups(spec)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	// When no group is set, key defaults to the path.
	if groups[0].Key != "nodes[a].data.config.prompt" {
		t.Errorf("group key = %q, want path", groups[0].Key)
	}
	if len(groups[0].Declarations) != 1 {
		t.Errorf("expected 1 declaration in group, got %d", len(groups[0].Declarations))
	}
}

func TestCollectPromptGroups_MultipleGrouped(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.system_prompt", Type: ParamTypePrompt, Group: "reasoning"},
			{Path: "nodes[a].data.config.user_prompt", Type: ParamTypePrompt, Group: "reasoning"},
			{Path: "nodes[b].data.config.prompt", Type: ParamTypePrompt, Group: "scoring"},
			{Path: "nodes[c].data.config.model", Type: ParamTypeModel, Candidates: []string{"x"}},
		},
	}
	groups := collectPromptGroups(spec)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// Groups should be sorted alphabetically by key.
	if groups[0].Key != "reasoning" {
		t.Errorf("first group key = %q, want %q", groups[0].Key, "reasoning")
	}
	if groups[1].Key != "scoring" {
		t.Errorf("second group key = %q, want %q", groups[1].Key, "scoring")
	}
	if len(groups[0].Declarations) != 2 {
		t.Errorf("reasoning group should have 2 declarations, got %d", len(groups[0].Declarations))
	}
	if len(groups[1].Declarations) != 1 {
		t.Errorf("scoring group should have 1 declaration, got %d", len(groups[1].Declarations))
	}
}

func TestCollectPromptGroups_SortedDeclarations(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[b].data.config.prompt", Type: ParamTypePrompt, Group: "g"},
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt, Group: "g"},
		},
	}
	groups := collectPromptGroups(spec)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	// Within the group, declarations should be sorted by path.
	if groups[0].Declarations[0].Path != "nodes[a].data.config.prompt" {
		t.Errorf("expected declarations sorted by path, got %q first", groups[0].Declarations[0].Path)
	}
}
