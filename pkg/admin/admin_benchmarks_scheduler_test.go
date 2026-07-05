package admin

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/bench"
)

// --- isEnglishLanguage ---

func TestIsEnglishLanguage(t *testing.T) {
	tests := []struct {
		lang string
		want bool
	}{
		{"english", true},
		{"English", true},
		{"ENGLISH", true},
		{"en", true},
		{"en-US", true},
		{"en_US", true}, // underscores replaced with dashes, still has en prefix
		{"en-GB", true},
		{"fr", false},
		{"french", false},
		{"zh", false},
		{"de", false},
		{"", false},
		{"  ", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		if got := isEnglishLanguage(tt.lang); got != tt.want {
			t.Errorf("isEnglishLanguage(%q) = %v, want %v", tt.lang, got, tt.want)
		}
	}
}

// --- filterBenchmarkItems ---

func TestFilterBenchmarkItems_NoFilter(t *testing.T) {
	items := []bench.DatasetItem{
		{ID: "1", Language: "english"},
		{ID: "2", Language: "french"},
		{ID: "3", Language: ""},
	}
	got := filterBenchmarkItems(items, "some-benchmark", false)
	if len(got) != 3 {
		t.Errorf("expected all 3 items when englishOnly=false, got %d", len(got))
	}
}

func TestFilterBenchmarkItems_EnglishOnly(t *testing.T) {
	items := []bench.DatasetItem{
		{ID: "1", Language: "english"},
		{ID: "2", Language: "french"},
		{ID: "3", Language: "en-US"},
		{ID: "4", Language: "zh"},
	}
	got := filterBenchmarkItems(items, "some-benchmark", true)
	if len(got) != 2 {
		t.Errorf("expected 2 english items, got %d", len(got))
	}
	ids := map[string]bool{}
	for _, item := range got {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["3"] {
		t.Errorf("expected items 1 and 3, got %v", ids)
	}
}

func TestFilterBenchmarkItems_GlobalMMLUUnknownCarveout(t *testing.T) {
	items := []bench.DatasetItem{
		{ID: "1", Language: "english"},
		{ID: "2", Language: "unknown"},
		{ID: "3", Language: "french"},
	}
	// Global-MMLU benchmarks: unknown language is included
	got := filterBenchmarkItems(items, bench.BenchmarkGlobalMMLU, true)
	if len(got) != 2 {
		t.Errorf("expected 2 items (english + unknown) for Global MMLU, got %d", len(got))
	}
	ids := map[string]bool{}
	for _, item := range got {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["2"] {
		t.Errorf("expected items 1 and 2, got %v", ids)
	}
}

func TestFilterBenchmarkItems_GlobalMMLULiteUnknownCarveout(t *testing.T) {
	items := []bench.DatasetItem{
		{ID: "1", Language: "english"},
		{ID: "2", Language: "Unknown"}, // case-insensitive
		{ID: "3", Language: "spanish"},
	}
	got := filterBenchmarkItems(items, bench.BenchmarkGlobalMMLULite, true)
	if len(got) != 2 {
		t.Errorf("expected 2 items for Global MMLU Lite, got %d", len(got))
	}
}

func TestFilterBenchmarkItems_MMLUProUnknownCarveout(t *testing.T) {
	items := []bench.DatasetItem{
		{ID: "1", Language: "english"},
		{ID: "2", Language: "unknown"},
	}
	got := filterBenchmarkItems(items, bench.BenchmarkMMLUPro, true)
	if len(got) != 2 {
		t.Errorf("expected 2 items for MMLU Pro, got %d", len(got))
	}
}

func TestFilterBenchmarkItems_NonMMLUNoUnknownCarveout(t *testing.T) {
	items := []bench.DatasetItem{
		{ID: "1", Language: "english"},
		{ID: "2", Language: "unknown"},
		{ID: "3", Language: "french"},
	}
	// Non-MMLU benchmark: unknown language is NOT included
	got := filterBenchmarkItems(items, "math-500", true)
	if len(got) != 1 {
		t.Errorf("expected 1 item (english only) for non-MMLU benchmark, got %d", len(got))
	}
	if got[0].ID != "1" {
		t.Errorf("expected item 1, got %q", got[0].ID)
	}
}

func TestFilterBenchmarkItems_EmptyInput(t *testing.T) {
	got := filterBenchmarkItems(nil, "bench", true)
	if len(got) != 0 {
		t.Errorf("expected 0 items for nil input, got %d", len(got))
	}
}

// --- applyLimit ---

func TestApplyLimit_NoLimit(t *testing.T) {
	items := makeDatasetItems(5)
	got := applyLimit(items, 0, benchmarkSampleModeHead, 0)
	if len(got) != 5 {
		t.Errorf("expected all 5 items when limit=0, got %d", len(got))
	}
}

func TestApplyLimit_LimitLargerThanItems(t *testing.T) {
	items := makeDatasetItems(3)
	got := applyLimit(items, 10, benchmarkSampleModeHead, 0)
	if len(got) != 3 {
		t.Errorf("expected all 3 items when limit > count, got %d", len(got))
	}
}

func TestApplyLimit_HeadMode(t *testing.T) {
	items := makeDatasetItems(10)
	got := applyLimit(items, 4, benchmarkSampleModeHead, 0)
	if len(got) != 4 {
		t.Errorf("expected 4 items for head mode, got %d", len(got))
	}
	// Head mode should return first N items
	for i, item := range got {
		if item.ID != items[i].ID {
			t.Errorf("head mode: got[%d].ID = %q, want %q", i, item.ID, items[i].ID)
		}
	}
}

func TestApplyLimit_RandomMode_Deterministic(t *testing.T) {
	items := makeDatasetItems(10)
	const seed = int64(42)
	got1 := applyLimit(items, 5, benchmarkSampleModeRandom, seed)
	got2 := applyLimit(items, 5, benchmarkSampleModeRandom, seed)
	if len(got1) != 5 || len(got2) != 5 {
		t.Fatalf("expected 5 items each, got %d and %d", len(got1), len(got2))
	}
	// Same seed should produce same sample
	for i := range got1 {
		if got1[i].ID != got2[i].ID {
			t.Errorf("random mode not deterministic at index %d: %q != %q", i, got1[i].ID, got2[i].ID)
		}
	}
}

func TestApplyLimit_RandomMode_DifferentSeeds(t *testing.T) {
	items := makeDatasetItems(20)
	got1 := applyLimit(items, 10, benchmarkSampleModeRandom, int64(1))
	got2 := applyLimit(items, 10, benchmarkSampleModeRandom, int64(99))
	// Different seeds should (almost certainly) produce different order
	identical := true
	for i := range got1 {
		if got1[i].ID != got2[i].ID {
			identical = false
			break
		}
	}
	if identical {
		t.Error("expected different seeds to produce different ordering (this can theoretically fail but is very unlikely)")
	}
}

// makeDatasetItems creates n test dataset items with sequential IDs.
func makeDatasetItems(n int) []bench.DatasetItem {
	items := make([]bench.DatasetItem, n)
	for i := range items {
		items[i] = bench.DatasetItem{
			ID: string(rune('a' + i)),
		}
	}
	return items
}
