package conctl

import (
	"math"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestBuildBenchmarkModelsRepo_PrefersComputedPerformanceHostModel(t *testing.T) {
	rowA := map[string]interface{}{
		"id":                      "host-a",
		"slug":                    "provider-a_model-x",
		"host_id":                 "provider-a",
		"price_1m_input_tokens":   2.0,
		"price_1m_output_tokens":  8.0,
		"price_1m_blended_3_to_1": 3.5,
		"host": map[string]interface{}{
			"short_name": "Provider A",
		},
		"model": map[string]interface{}{
			"id":                                 "model-x",
			"slug":                               "model-x",
			"name":                               "Model X",
			"short_name":                         "Model X",
			"reasoning_model":                    true,
			"deprecated":                         false,
			"intelligence_index":                 20.0,
			"computed_performance_host_model_id": "host-b",
			"intelligence_index_token_counts": map[string]interface{}{
				"input_tokens":  1000000.0,
				"output_tokens": 1000000.0,
			},
		},
	}
	rowB := map[string]interface{}{
		"id":                      "host-b",
		"slug":                    "provider-b_model-x",
		"host_id":                 "provider-b",
		"price_1m_input_tokens":   5.0,
		"price_1m_output_tokens":  5.0,
		"price_1m_blended_3_to_1": 5.0,
		"host": map[string]interface{}{
			"short_name": "Provider B",
		},
		"model": map[string]interface{}{
			"id":                                 "model-x",
			"slug":                               "model-x",
			"name":                               "Model X",
			"short_name":                         "Model X",
			"reasoning_model":                    true,
			"deprecated":                         false,
			"intelligence_index":                 20.0,
			"computed_performance_host_model_id": "host-b",
			"intelligence_index_token_counts": map[string]interface{}{
				"input_tokens":  1000000.0,
				"output_tokens": 1000000.0,
			},
		},
	}

	repo := buildBenchmarkModelsRepo([]map[string]interface{}{rowA, rowB}, "source-url", "2026-02-16T00:00:00Z")
	if got, want := repo.UniqueModels, 1; got != want {
		t.Fatalf("UniqueModels = %d, want %d", got, want)
	}
	if got, want := repo.Models[0].SourceHostModelID, "host-b"; got != want {
		t.Fatalf("selected SourceHostModelID = %q, want %q", got, want)
	}
}

func TestBuildBenchmarkModelsRepo_FallbackPrefersMedian(t *testing.T) {
	makeRow := func(id, hostID string, blended float64) map[string]interface{} {
		return map[string]interface{}{
			"id":                      id,
			"slug":                    hostID + "_model-y",
			"host_id":                 hostID,
			"price_1m_input_tokens":   blended * 0.5,
			"price_1m_output_tokens":  blended * 2.0,
			"price_1m_blended_3_to_1": blended,
			"host": map[string]interface{}{
				"short_name": hostID,
			},
			"model": map[string]interface{}{
				"id":                 "model-y",
				"slug":               "model-y",
				"name":               "Model Y",
				"short_name":         "Model Y",
				"reasoning_model":    false,
				"deprecated":         false,
				"intelligence_index": 15.0,
				"intelligence_index_token_counts": map[string]interface{}{
					"input_tokens":  2000000.0,
					"output_tokens": 1000000.0,
				},
			},
		}
	}

	// 3 hosts: cheap (0.10), medium (1.00), expensive (5.00) — median should be medium
	rows := []map[string]interface{}{
		makeRow("host-expensive", "provider-expensive", 5.0),
		makeRow("host-cheap", "provider-cheap", 0.10),
		makeRow("host-medium", "provider-medium", 1.0),
	}
	repo := buildBenchmarkModelsRepo(rows, "source-url", "2026-02-16T00:00:00Z")
	if got, want := repo.Models[0].SourceHostModelID, "host-medium"; got != want {
		t.Fatalf("selected SourceHostModelID = %q, want %q", got, want)
	}
}

func TestBenchmarkModelRecordFromRow_DerivesCostAndValue(t *testing.T) {
	row := map[string]interface{}{
		"id":                      "host-z",
		"slug":                    "provider-z_model-z",
		"host_id":                 "provider-z",
		"price_1m_input_tokens":   2.0,
		"price_1m_output_tokens":  4.0,
		"price_1m_blended_3_to_1": 2.5,
		"host": map[string]interface{}{
			"short_name": "Provider Z",
		},
		"model": map[string]interface{}{
			"id":                 "model-z",
			"slug":               "model-z",
			"name":               "Model Z",
			"short_name":         "Model Z",
			"reasoning_model":    true,
			"deprecated":         false,
			"intelligence_index": 40.0,
			"intelligence_index_token_counts": map[string]interface{}{
				"input_tokens":     3000000.0,
				"output_tokens":    2000000.0,
				"answer_tokens":    1000000.0,
				"reasoning_tokens": 1000000.0,
			},
		},
	}

	record, ok := benchmarkModelRecordFromRow(row, "2026-02-16T00:00:00Z")
	if !ok {
		t.Fatal("expected record to parse")
	}
	if record.CostToRunIndexInputUSD == nil || record.CostToRunIndexOutputUSD == nil || record.CostToRunIndexTotalUSD == nil || record.ValueScore == nil {
		t.Fatal("expected derived cost/value fields to be present")
	}

	assertAlmostEqual(t, "input_cost", *record.CostToRunIndexInputUSD, 6.0)
	assertAlmostEqual(t, "output_cost", *record.CostToRunIndexOutputUSD, 8.0)
	assertAlmostEqual(t, "total_cost", *record.CostToRunIndexTotalUSD, 14.0)
	assertAlmostEqual(t, "value_score", *record.ValueScore, 40.0/14.0)
}

func TestBenchmarkModelRecordFromRow_DerivesCostAndValueFromOutputCostFallback(t *testing.T) {
	row := map[string]interface{}{
		"id":                     "host-glm",
		"slug":                   "makora_glm-5-2",
		"host_id":                "makora",
		"price_1m_output_tokens": 3.9855,
		"host": map[string]interface{}{
			"short_name": "Makora",
		},
		"model": map[string]interface{}{
			"id":                 "glm-5-2",
			"slug":               "glm-5-2",
			"name":               "GLM-5.2 (max)",
			"short_name":         "GLM-5.2 (max)",
			"reasoning_model":    true,
			"deprecated":         false,
			"intelligence_index": 51.0858347714416,
			"intelligence_index_token_counts": map[string]interface{}{
				"output_tokens": 136789844.0,
			},
		},
	}

	record, ok := benchmarkModelRecordFromRow(row, "2026-06-23T15:44:12Z")
	if !ok {
		t.Fatal("expected record to parse")
	}
	if record.CostToRunIndexOutputUSD == nil {
		t.Fatal("expected output cost to be present")
	}
	if record.CostToRunIndexTotalUSD == nil {
		t.Fatal("expected total cost to fall back to output cost")
	}
	if record.ValueScore == nil {
		t.Fatal("expected value score to be derived from fallback total cost")
	}

	assertAlmostEqual(t, "output_cost", *record.CostToRunIndexOutputUSD, 545.175923262)
	assertAlmostEqual(t, "total_cost", *record.CostToRunIndexTotalUSD, 545.175923262)
	assertAlmostEqual(t, "value_score", *record.ValueScore, 51.0858347714416/545.175923262)
}

func TestFilterBenchmarkModels_ReasoningValidation(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "a", ShortName: "A", ReasoningModel: true},
		{Slug: "b", ShortName: "B", ReasoningModel: false},
	}

	out, err := filterBenchmarkModels(models, "", false, "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(out), 1; got != want || out[0].Slug != "a" {
		t.Fatalf("reasoning=true filter mismatch: got=%v", out)
	}

	if _, err := filterBenchmarkModels(models, "", false, "maybe"); err == nil {
		t.Fatal("expected invalid reasoning value to return error")
	}
}

func TestApplyOpenRouterMappings_PrefersProviderOnSlugCollision(t *testing.T) {
	repo := &benchmarkModelsRepo{
		Models: []benchmarkModelRecord{
			{
				Slug:        "gpt-5-2",
				ShortName:   "GPT-5.2",
				Name:        "GPT-5.2 (xhigh)",
				CreatorName: "OpenAI",
			},
		},
	}

	orModels := []providers.Model{
		{ID: "foo/gpt-5.2", Name: "GPT-5.2"},
		{ID: "openai/gpt-5.2", Name: "GPT-5.2"},
	}

	mapped := applyOpenRouterMappings(repo, orModels)
	if mapped != 1 {
		t.Fatalf("mapped = %d, want 1", mapped)
	}
	got := repo.Models[0]
	if got.OpenRouterModelID != "openai/gpt-5.2" {
		t.Fatalf("OpenRouterModelID = %q, want %q", got.OpenRouterModelID, "openai/gpt-5.2")
	}
	if got.OpenRouterMatchMethod != "slug_provider_exact" {
		t.Fatalf("OpenRouterMatchMethod = %q, want %q", got.OpenRouterMatchMethod, "slug_provider_exact")
	}
}

func TestApplyOpenRouterMappings_UsesFuzzyFallback(t *testing.T) {
	repo := &benchmarkModelsRepo{
		Models: []benchmarkModelRecord{
			{
				Slug:        "claude-opus-4-5-thinking",
				ShortName:   "Claude Opus 4.5 (Thinking)",
				Name:        "Claude Opus 4.5 (Thinking)",
				CreatorName: "Anthropic",
			},
		},
	}

	orModels := []providers.Model{
		{ID: "anthropic/claude-opus-4.5", Name: "Claude Opus 4.5"},
		{ID: "openai/gpt-4o-mini", Name: "GPT-4o Mini"},
	}

	mapped := applyOpenRouterMappings(repo, orModels)
	if mapped != 1 {
		t.Fatalf("mapped = %d, want 1", mapped)
	}
	got := repo.Models[0]
	if got.OpenRouterModelID != "anthropic/claude-opus-4.5" {
		t.Fatalf("OpenRouterModelID = %q, want %q", got.OpenRouterModelID, "anthropic/claude-opus-4.5")
	}
	if got.OpenRouterMatchMethod != "fuzzy" {
		t.Fatalf("OpenRouterMatchMethod = %q, want %q", got.OpenRouterMatchMethod, "fuzzy")
	}
}

func TestBenchmarkModelsRepoEqualIgnoringSnapshot(t *testing.T) {
	a := benchmarkModelsRepo{
		SourceURL:    "https://example.com",
		SourceRows:   2,
		UniqueModels: 1,
		SnapshotAt:   "2026-02-16T01:00:00Z",
		Models: []benchmarkModelRecord{
			{
				ModelID:           "model-1",
				Slug:              "model-1",
				ShortName:         "Model 1",
				IntelligenceIndex: floatPtr(42),
				SnapshotAt:        "2026-02-16T01:00:00Z",
			},
		},
	}
	b := benchmarkModelsRepo{
		SourceURL:    "https://example.com",
		SourceRows:   2,
		UniqueModels: 1,
		SnapshotAt:   "2026-02-16T03:00:00Z",
		Models: []benchmarkModelRecord{
			{
				ModelID:           "model-1",
				Slug:              "model-1",
				ShortName:         "Model 1",
				IntelligenceIndex: floatPtr(42),
				SnapshotAt:        "2026-02-16T03:00:00Z",
			},
		},
	}

	if !benchmarkModelsRepoEqualIgnoringSnapshot(a, b) {
		t.Fatal("expected repos with only snapshot differences to be equal")
	}
}

func TestBenchmarkModelsRepoEqualIgnoringSnapshot_DetectsDataChanges(t *testing.T) {
	a := benchmarkModelsRepo{
		SourceURL:    "https://example.com",
		SourceRows:   2,
		UniqueModels: 1,
		SnapshotAt:   "2026-02-16T01:00:00Z",
		Models: []benchmarkModelRecord{
			{
				ModelID:           "model-1",
				Slug:              "model-1",
				ShortName:         "Model 1",
				IntelligenceIndex: floatPtr(42),
				SnapshotAt:        "2026-02-16T01:00:00Z",
			},
		},
	}
	b := benchmarkModelsRepo{
		SourceURL:    "https://example.com",
		SourceRows:   2,
		UniqueModels: 1,
		SnapshotAt:   "2026-02-16T03:00:00Z",
		Models: []benchmarkModelRecord{
			{
				ModelID:           "model-1",
				Slug:              "model-1",
				ShortName:         "Model 1",
				IntelligenceIndex: floatPtr(43), // actual data change
				SnapshotAt:        "2026-02-16T03:00:00Z",
			},
		},
	}

	if benchmarkModelsRepoEqualIgnoringSnapshot(a, b) {
		t.Fatal("expected repos with non-snapshot differences to be unequal")
	}
}

func TestSetBenchmarkModelsRepoSnapshot(t *testing.T) {
	repo := &benchmarkModelsRepo{
		Models: []benchmarkModelRecord{
			{Slug: "a", SnapshotAt: "old"},
			{Slug: "b", SnapshotAt: "old"},
		},
	}

	setBenchmarkModelsRepoSnapshot(repo, "2026-02-16T04:00:00Z")
	if repo.SnapshotAt != "2026-02-16T04:00:00Z" {
		t.Fatalf("repo snapshot not updated: %q", repo.SnapshotAt)
	}
	for i, model := range repo.Models {
		if model.SnapshotAt != "2026-02-16T04:00:00Z" {
			t.Fatalf("model[%d] snapshot not updated: %q", i, model.SnapshotAt)
		}
	}
}

func TestDeduplicateOpenRouterMappings_KeepsHighestIntelligence(t *testing.T) {
	repo := &benchmarkModelsRepo{
		Models: []benchmarkModelRecord{
			{
				Slug:                  "mimo-v2-flash",
				ShortName:             "MiMo-V2-Flash",
				IntelligenceIndex:     floatPtr(30.56),
				OpenRouterModelID:     "xiaomi/mimo-v2-flash",
				OpenRouterMatchMethod: "slug_provider_exact",
			},
			{
				Slug:                  "mimo-v2-flash-reasoning",
				ShortName:             "MiMo-V2-Flash (Reasoning)",
				IntelligenceIndex:     floatPtr(39.24),
				OpenRouterModelID:     "xiaomi/mimo-v2-flash",
				OpenRouterMatchMethod: "fuzzy",
			},
			{
				Slug:              "mimo-v2-0206",
				ShortName:         "MiMo-V2-Flash (Feb 2026)",
				IntelligenceIndex: floatPtr(41.42),
				// No OpenRouter mapping.
			},
		},
	}

	deduplicateOpenRouterMappings(repo)

	// The non-reasoning entry (lower intel) should lose the mapping.
	if repo.Models[0].OpenRouterModelID != "" {
		t.Fatalf("expected non-reasoning entry to lose OpenRouter mapping, got %q", repo.Models[0].OpenRouterModelID)
	}
	// The reasoning entry (higher intel) should keep it.
	if repo.Models[1].OpenRouterModelID != "xiaomi/mimo-v2-flash" {
		t.Fatalf("expected reasoning entry to keep OpenRouter mapping, got %q", repo.Models[1].OpenRouterModelID)
	}
	// The Feb 2026 entry should remain unmapped.
	if repo.Models[2].OpenRouterModelID != "" {
		t.Fatalf("expected Feb 2026 entry to remain unmapped, got %q", repo.Models[2].OpenRouterModelID)
	}
}

func TestDeduplicateOpenRouterMappings_NoConflict(t *testing.T) {
	repo := &benchmarkModelsRepo{
		Models: []benchmarkModelRecord{
			{
				Slug:              "model-a",
				IntelligenceIndex: floatPtr(40.0),
				OpenRouterModelID: "provider/model-a",
			},
			{
				Slug:              "model-b",
				IntelligenceIndex: floatPtr(35.0),
				OpenRouterModelID: "provider/model-b",
			},
		},
	}

	deduplicateOpenRouterMappings(repo)

	// Both should keep their mappings.
	if repo.Models[0].OpenRouterModelID != "provider/model-a" {
		t.Fatalf("model-a lost mapping unexpectedly")
	}
	if repo.Models[1].OpenRouterModelID != "provider/model-b" {
		t.Fatalf("model-b lost mapping unexpectedly")
	}
}

func TestSortByCompositeRank(t *testing.T) {
	// Model A: highest intel but most expensive.
	// Model B: strong across both dimensions (balanced winner).
	// Model C: cheapest but low intel.
	// Model D: medium on both.
	models := []benchmarkModelRecord{
		{
			Slug:                   "model-a",
			ShortName:              "Model A",
			IntelligenceIndex:      floatPtr(50.0),
			CostToRunIndexTotalUSD: floatPtr(500.0),
			E2ETotalTimeSec:        floatPtr(100.0),
		},
		{
			Slug:                   "model-b",
			ShortName:              "Model B",
			IntelligenceIndex:      floatPtr(40.0),
			CostToRunIndexTotalUSD: floatPtr(30.0),
			E2ETotalTimeSec:        floatPtr(10.0),
		},
		{
			Slug:                   "model-c",
			ShortName:              "Model C",
			IntelligenceIndex:      floatPtr(10.0),
			CostToRunIndexTotalUSD: floatPtr(5.0),
			E2ETotalTimeSec:        floatPtr(50.0),
		},
		{
			Slug:                   "model-d",
			ShortName:              "Model D",
			IntelligenceIndex:      floatPtr(20.0),
			CostToRunIndexTotalUSD: floatPtr(200.0),
			E2ETotalTimeSec:        floatPtr(3.0),
		},
	}

	sortByCompositeRank(models)

	// Model B should rank first — strong in both intelligence and cost.
	if models[0].Slug != "model-b" {
		t.Fatalf("expected model-b first (most balanced), got %q", models[0].Slug)
	}
}

func TestSortByCompositeRank_IgnoresSpeedDimension(t *testing.T) {
	models := []benchmarkModelRecord{
		{
			Slug:                   "fast-model",
			IntelligenceIndex:      floatPtr(30.0),
			CostToRunIndexTotalUSD: floatPtr(50.0),
			E2ETotalTimeSec:        floatPtr(5.0),
		},
		{
			Slug:                   "no-speed",
			IntelligenceIndex:      floatPtr(45.0),
			CostToRunIndexTotalUSD: floatPtr(20.0),
		},
	}

	sortByCompositeRank(models)

	// Rank uses only intelligence + cost, so missing speed should not matter.
	if models[0].Slug != "no-speed" {
		t.Fatalf("expected no-speed first, got %q", models[0].Slug)
	}
}

func TestBenchmarkModelRecordFromRow_ExtractsSpeedFields(t *testing.T) {
	row := map[string]interface{}{
		"id":                      "host-s",
		"slug":                    "provider-s_model-s",
		"host_id":                 "provider-s",
		"price_1m_input_tokens":   1.0,
		"price_1m_output_tokens":  2.0,
		"price_1m_blended_3_to_1": 1.25,
		"host": map[string]interface{}{
			"short_name": "Provider S",
		},
		"model": map[string]interface{}{
			"id":                 "model-s",
			"slug":               "model-s",
			"name":               "Model S",
			"short_name":         "Model S",
			"reasoning_model":    true,
			"intelligence_index": 35.0,
			"intelligence_index_token_counts": map[string]interface{}{
				"input_tokens":  1000000.0,
				"output_tokens": 500000.0,
			},
		},
		"end_to_end_response_time_metrics": map[string]interface{}{
			"total_time":     15.5,
			"p95_total_time": 20.3,
		},
		"time_to_first_answer_token_metrics": map[string]interface{}{
			"total_time": 12.1,
		},
		"timescaleData": map[string]interface{}{
			"median_output_speed": 173.5,
		},
	}

	record, ok := benchmarkModelRecordFromRow(row, "2026-02-16T00:00:00Z")
	if !ok {
		t.Fatal("expected record to parse")
	}
	if record.E2ETotalTimeSec == nil {
		t.Fatal("expected E2ETotalTimeSec to be present")
	}
	assertAlmostEqual(t, "e2e_total", *record.E2ETotalTimeSec, 15.5)
	assertAlmostEqual(t, "e2e_p95", *record.E2EP95TimeSec, 20.3)
	assertAlmostEqual(t, "ttfa_total", *record.TTFATotalTimeSec, 12.1)
	assertAlmostEqual(t, "output_speed", *record.OutputSpeedTokSec, 173.5)
}

func TestParseSortKeys(t *testing.T) {
	tests := []struct {
		input   string
		want    []string
		wantErr bool
	}{
		{"value", []string{"value"}, false},
		{"cost,intelligence", []string{"cost", "intelligence"}, false},
		{"COST, Intelligence", []string{"cost", "intelligence"}, false},
		{"", []string{"value"}, false},
		{"rank", []string{"rank"}, false},
		{"bogus", nil, true},
		{"cost,bogus", nil, true},
		{"speed", nil, true},
		// Multi-key constraints: name and rank cannot appear in multi-key.
		{"cost,name", nil, true},
		{"rank,cost", nil, true},
		// Single-key name/rank are fine.
		{"name", []string{"name"}, false},
	}
	for _, tt := range tests {
		keys, err := parseSortKeys(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseSortKeys(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseSortKeys(%q) unexpected error: %v", tt.input, err)
		}
		if len(keys) != len(tt.want) {
			t.Fatalf("parseSortKeys(%q) = %v, want %v", tt.input, keys, tt.want)
		}
		for i := range keys {
			if keys[i] != tt.want[i] {
				t.Fatalf("parseSortKeys(%q)[%d] = %q, want %q", tt.input, i, keys[i], tt.want[i])
			}
		}
	}
}

func TestMultiKeySort_CostThenIntelligence(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "expensive-smart", ShortName: "A", CostToRunIndexTotalUSD: floatPtr(500), IntelligenceIndex: floatPtr(50)},
		{Slug: "cheap-dumb", ShortName: "B", CostToRunIndexTotalUSD: floatPtr(10), IntelligenceIndex: floatPtr(20)},
		{Slug: "cheap-smart", ShortName: "C", CostToRunIndexTotalUSD: floatPtr(10), IntelligenceIndex: floatPtr(45)},
	}

	sortBenchmarkModels(models, "cost,intelligence")

	// Weighted composite (cost gets much stronger positional priority than intelligence).
	// cheap-smart wins: same best cost as cheap-dumb, with much higher intelligence.
	// cheap-dumb has same cost pct but low intel. expensive-smart has high intel but terrible cost.
	if models[0].Slug != "cheap-smart" {
		t.Fatalf("expected cheap-smart first, got %q", models[0].Slug)
	}
	if models[1].Slug != "cheap-dumb" {
		t.Fatalf("expected cheap-dumb second, got %q", models[1].Slug)
	}
	if models[2].Slug != "expensive-smart" {
		t.Fatalf("expected expensive-smart last, got %q", models[2].Slug)
	}
}

func TestMultiKeySort_CostThenIntelligence_StrongCostPriority(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "cheapest-strong", ShortName: "Cheapest Strong", CostToRunIndexTotalUSD: floatPtr(70.6), IntelligenceIndex: floatPtr(41.6)},
		{Slug: "mid-better", ShortName: "Mid Better", CostToRunIndexTotalUSD: floatPtr(278.3), IntelligenceIndex: floatPtr(46.4)},
		{Slug: "expensive-best", ShortName: "Expensive Best", CostToRunIndexTotalUSD: floatPtr(547.0), IntelligenceIndex: floatPtr(49.6)},
	}

	sortBenchmarkModels(models, "cost,intelligence")

	// For "cost,intelligence", a modest intelligence gain should not push
	// substantially more expensive models above the strongest cheap option.
	if models[0].Slug != "cheapest-strong" {
		t.Fatalf("expected cheapest-strong first for cost-priority sort, got %q", models[0].Slug)
	}
}

func TestMultiKeySort_IntelligenceThenCost(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "smart-expensive-slow", ShortName: "A", IntelligenceIndex: floatPtr(45), CostToRunIndexTotalUSD: floatPtr(500), E2ETotalTimeSec: floatPtr(100)},
		{Slug: "smart-cheap-fast", ShortName: "B", IntelligenceIndex: floatPtr(45), CostToRunIndexTotalUSD: floatPtr(50), E2ETotalTimeSec: floatPtr(10)},
		{Slug: "smartest", ShortName: "C", IntelligenceIndex: floatPtr(50), CostToRunIndexTotalUSD: floatPtr(200), E2ETotalTimeSec: floatPtr(30)},
	}

	sortBenchmarkModels(models, "intelligence,cost")

	// Weighted composite with positional priority (intel >> cost).
	// smartest wins despite mid-range cost because intelligence is dominant.
	if models[0].Slug != "smartest" {
		t.Fatalf("expected smartest first, got %q", models[0].Slug)
	}
	if models[1].Slug != "smart-cheap-fast" {
		t.Fatalf("expected smart-cheap-fast second, got %q", models[1].Slug)
	}
	if models[2].Slug != "smart-expensive-slow" {
		t.Fatalf("expected smart-expensive-slow last, got %q", models[2].Slug)
	}
}

func TestMultiKeySort_BalancedBeatsExtreme(t *testing.T) {
	// Validates that weighted composite avoids the "cheap but useless" problem.
	// A dirt-cheap model with terrible intelligence should NOT rank first when
	// sorting by cost,intelligence — the balanced model should win.
	models := []benchmarkModelRecord{
		{Slug: "free-garbage", ShortName: "FG", CostToRunIndexTotalUSD: floatPtr(0), IntelligenceIndex: floatPtr(5), E2ETotalTimeSec: floatPtr(15)},
		{Slug: "cheap-decent", ShortName: "CD", CostToRunIndexTotalUSD: floatPtr(40), IntelligenceIndex: floatPtr(39), E2ETotalTimeSec: floatPtr(8)},
		{Slug: "moderate-good", ShortName: "MG", CostToRunIndexTotalUSD: floatPtr(125), IntelligenceIndex: floatPtr(42), E2ETotalTimeSec: floatPtr(46)},
		{Slug: "expensive-best", ShortName: "EB", CostToRunIndexTotalUSD: floatPtr(280), IntelligenceIndex: floatPtr(46), E2ETotalTimeSec: floatPtr(14)},
	}

	sortBenchmarkModels(models, "cost,intelligence")

	// cheap-decent should beat free-garbage because intel=39 >> intel=5.
	// The cost weight advantage of free-garbage is not enough to overcome the
	// massive intelligence penalty in the composite score.
	if models[0].Slug != "cheap-decent" {
		t.Fatalf("expected cheap-decent first (balanced), got %q", models[0].Slug)
	}
	if models[len(models)-1].Slug == "cheap-decent" {
		t.Fatal("cheap-decent should not be last")
	}
}

func TestMinMaxNormalize(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "a", CostToRunIndexTotalUSD: floatPtr(10)},
		{Slug: "b", CostToRunIndexTotalUSD: floatPtr(10)},
		{Slug: "c", CostToRunIndexTotalUSD: floatPtr(50)},
	}
	norm := minMaxNormalize(models,
		func(m benchmarkModelRecord) float64 { return ptrFloatOr(m.CostToRunIndexTotalUSD, 0) },
		func(m benchmarkModelRecord) bool { return m.CostToRunIndexTotalUSD != nil },
		true, // lower is better
	)

	// Models a and b are tied at cost=10 (cheapest) → normalized to 1.0.
	if norm[0] != 1.0 || norm[1] != 1.0 {
		t.Fatalf("cheapest models should normalize to 1.0: a=%.4f, b=%.4f", norm[0], norm[1])
	}
	// Model c (most expensive, cost=50) → normalized to 0.0.
	if norm[2] != 0.0 {
		t.Fatalf("most expensive model should normalize to 0.0: c=%.4f", norm[2])
	}
}

func TestMinMaxNormalize_HigherIsBetter(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "low", IntelligenceIndex: floatPtr(10)},
		{Slug: "mid", IntelligenceIndex: floatPtr(30)},
		{Slug: "high", IntelligenceIndex: floatPtr(50)},
	}
	norm := minMaxNormalize(models,
		func(m benchmarkModelRecord) float64 { return ptrFloatOr(m.IntelligenceIndex, 0) },
		func(m benchmarkModelRecord) bool { return m.IntelligenceIndex != nil },
		false, // higher is better
	)

	if norm[0] != 0.0 {
		t.Fatalf("lowest intel should normalize to 0.0: got %.4f", norm[0])
	}
	if norm[1] != 0.5 {
		t.Fatalf("mid intel should normalize to 0.5: got %.4f", norm[1])
	}
	if norm[2] != 1.0 {
		t.Fatalf("highest intel should normalize to 1.0: got %.4f", norm[2])
	}
}

func TestParetoFrontier(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "cheap-dumb", IntelligenceIndex: floatPtr(10), CostToRunIndexTotalUSD: floatPtr(20)},
		{Slug: "cheap-smart", IntelligenceIndex: floatPtr(40), CostToRunIndexTotalUSD: floatPtr(40)},
		{Slug: "mid-smarter", IntelligenceIndex: floatPtr(45), CostToRunIndexTotalUSD: floatPtr(100)},
		{Slug: "expensive-best", IntelligenceIndex: floatPtr(50), CostToRunIndexTotalUSD: floatPtr(300)},
		{Slug: "expensive-worse", IntelligenceIndex: floatPtr(42), CostToRunIndexTotalUSD: floatPtr(250)},
	}

	frontier := paretoFrontier(models, 0, 0)

	// cheap-dumb: non-dominated (cheapest)
	// cheap-smart: non-dominated (smarter than cheap-dumb, next cheapest)
	// mid-smarter: non-dominated (smarter than cheap-smart)
	// expensive-best: non-dominated (smartest overall)
	// expensive-worse: DOMINATED by mid-smarter (mid-smarter is cheaper AND smarter)
	slugs := make([]string, len(frontier))
	for i, m := range frontier {
		slugs[i] = m.Slug
	}
	want := []string{"cheap-dumb", "cheap-smart", "mid-smarter", "expensive-best"}
	if len(slugs) != len(want) {
		t.Fatalf("frontier = %v, want %v", slugs, want)
	}
	for i := range want {
		if slugs[i] != want[i] {
			t.Fatalf("frontier[%d] = %q, want %q", i, slugs[i], want[i])
		}
	}
}

func TestParetoFrontier_MinIntelFilter(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "weak", IntelligenceIndex: floatPtr(10), CostToRunIndexTotalUSD: floatPtr(5)},
		{Slug: "strong", IntelligenceIndex: floatPtr(40), CostToRunIndexTotalUSD: floatPtr(50)},
		{Slug: "strongest", IntelligenceIndex: floatPtr(48), CostToRunIndexTotalUSD: floatPtr(200)},
	}

	frontier := paretoFrontier(models, 35, 0)

	// "weak" filtered out by min-intel=35.
	// strong and strongest are both on the frontier.
	if len(frontier) != 2 {
		t.Fatalf("expected 2 models, got %d", len(frontier))
	}
	if frontier[0].Slug != "strong" {
		t.Fatalf("expected strong first (cheapest), got %q", frontier[0].Slug)
	}
	if frontier[1].Slug != "strongest" {
		t.Fatalf("expected strongest second, got %q", frontier[1].Slug)
	}
}

func TestParetoFrontier_MaxCostFilter(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "cheap", IntelligenceIndex: floatPtr(30), CostToRunIndexTotalUSD: floatPtr(20)},
		{Slug: "mid", IntelligenceIndex: floatPtr(40), CostToRunIndexTotalUSD: floatPtr(80)},
		{Slug: "expensive", IntelligenceIndex: floatPtr(50), CostToRunIndexTotalUSD: floatPtr(500)},
	}

	frontier := paretoFrontier(models, 0, 100)

	// "expensive" filtered out by max-cost=100.
	if len(frontier) != 2 {
		t.Fatalf("expected 2 models, got %d", len(frontier))
	}
	if frontier[0].Slug != "cheap" || frontier[1].Slug != "mid" {
		t.Fatalf("expected [cheap, mid], got [%s, %s]", frontier[0].Slug, frontier[1].Slug)
	}
}

func TestParetoFrontier_MissingData(t *testing.T) {
	models := []benchmarkModelRecord{
		{Slug: "no-intel", CostToRunIndexTotalUSD: floatPtr(10)},
		{Slug: "no-cost", IntelligenceIndex: floatPtr(40)},
		{Slug: "valid", IntelligenceIndex: floatPtr(35), CostToRunIndexTotalUSD: floatPtr(50)},
	}

	frontier := paretoFrontier(models, 0, 0)

	// Models without both intel and cost are excluded.
	if len(frontier) != 1 || frontier[0].Slug != "valid" {
		t.Fatalf("expected [valid], got %v", frontier)
	}
}

func assertAlmostEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %.12f, want %.12f", name, got, want)
	}
}

func floatPtr(v float64) *float64 {
	return &v
}
