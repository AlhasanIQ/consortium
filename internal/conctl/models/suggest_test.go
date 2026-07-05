package models

import "testing"

func TestSuggestTracks(t *testing.T) {
	repo := &Repo{Models: []Model{
		{Slug: "cheap-mid", ModelID: "cheap-mid", OpenRouterModelID: "cheap-mid", ReasoningModel: true, IntelligenceIndex: floatPtr(35), CostToRunIndexTotalUS: floatPtr(10)},
		{Slug: "mid-smart", ModelID: "mid-smart", OpenRouterModelID: "mid-smart", ReasoningModel: true, IntelligenceIndex: floatPtr(45), CostToRunIndexTotalUS: floatPtr(50)},
		{Slug: "exp-best", ModelID: "exp-best", OpenRouterModelID: "exp-best", ReasoningModel: true, IntelligenceIndex: floatPtr(50), CostToRunIndexTotalUS: floatPtr(300)},
	}}

	result, err := Suggest(repo, SuggestOptions{Top: 2, Track: "cheap", Reasoning: "true", MinIntel: 0, MaxCost: 0})
	if err != nil {
		t.Fatalf("Suggest returned error: %v", err)
	}
	if len(result.Cheap) == 0 || len(result.Expensive) == 0 || len(result.MostIntelligent) == 0 {
		t.Fatalf("expected all tracks to be populated: %+v", result)
	}
	if got := TrackCandidates(result, "cheap"); len(got) == 0 {
		t.Fatal("expected cheap track candidates")
	}
	if got := TrackCandidates(result, "intelligence"); len(got) == 0 {
		t.Fatal("expected intelligence track candidates")
	}
	if got := TrackCandidates(result, "balanced"); len(got) == 0 {
		t.Fatal("expected balanced track candidates")
	}
}

func TestTrackCandidatesPrefersOpenRouterID(t *testing.T) {
	model := Model{ModelID: "provider/model", OpenRouterModelID: "openrouter/id"}
	if got := model.CandidateID(); got != "openrouter/id" {
		t.Fatalf("CandidateID() = %q, want openrouter/id", got)
	}
}

func floatPtr(v float64) *float64 {
	return &v
}
