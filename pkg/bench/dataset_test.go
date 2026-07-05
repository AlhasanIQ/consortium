package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDataset(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.jsonl")
	content := "" +
		`{"id":"1","benchmark":"global-mmlu-lite","split":"test","subject":"math","language":"en","question":"2+2?","choices":["1","2","4","8"],"answer_index":2,"answer_label":"C"}` + "\n" +
		`{"id":"2","benchmark":"global-mmlu-lite","split":"test","subject":"science","language":"en","question":"H2O is?","choices":["water","salt"],"answer_index":0,"answer_label":"A"}` + "\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	items, err := LoadDataset(path, 0)
	if err != nil {
		t.Fatalf("LoadDataset failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].AnswerLabel != "C" {
		t.Fatalf("expected first answer label C, got %q", items[0].AnswerLabel)
	}
}

func TestDatasetPath(t *testing.T) {
	path, err := DatasetPath("benchmarks/data", "mmlu-pro", "test")
	if err != nil {
		t.Fatalf("DatasetPath failed: %v", err)
	}
	want := filepath.Join("benchmarks/data", "mmlu_pro", "test.jsonl")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}

	globalPath, err := DatasetPath("benchmarks/data", "global-mmlu", "validation")
	if err != nil {
		t.Fatalf("DatasetPath(global-mmlu) failed: %v", err)
	}
	globalWant := filepath.Join("benchmarks/data", "global_mmlu", "validation.jsonl")
	if globalPath != globalWant {
		t.Fatalf("expected %q, got %q", globalWant, globalPath)
	}
}

func TestNormalizeBenchmark(t *testing.T) {
	got, err := NormalizeBenchmark("global_mmlu")
	if err != nil {
		t.Fatalf("NormalizeBenchmark failed: %v", err)
	}
	if got != BenchmarkGlobalMMLU {
		t.Fatalf("expected %q, got %q", BenchmarkGlobalMMLU, got)
	}

	gotMath, err := NormalizeBenchmark("math500")
	if err != nil {
		t.Fatalf("NormalizeBenchmark math500 failed: %v", err)
	}
	if gotMath != BenchmarkMath500 {
		t.Fatalf("expected %q, got %q", BenchmarkMath500, gotMath)
	}
}

func TestGlobalMMLULanguageDefault(t *testing.T) {
	item, err := toDatasetItem(datasetRow{
		ID:          "x",
		Benchmark:   "global-mmlu",
		Split:       "test",
		Subject:     "science",
		Language:    "",
		Question:    "Q?",
		Choices:     []string{"A1", "A2"},
		AnswerIndex: 1,
		AnswerLabel: "B",
	}, 1)
	if err != nil {
		t.Fatalf("toDatasetItem failed: %v", err)
	}
	if item.Language != "en" {
		t.Fatalf("expected default language en, got %q", item.Language)
	}
}

func TestNormalizeRunSize(t *testing.T) {
	full, err := NormalizeRunSize("")
	if err != nil {
		t.Fatalf("NormalizeRunSize empty failed: %v", err)
	}
	if full != RunSizeFull {
		t.Fatalf("expected default run size full, got %q", full)
	}

	small, err := NormalizeRunSize("small")
	if err != nil {
		t.Fatalf("NormalizeRunSize small failed: %v", err)
	}
	if small != RunSizeSmall {
		t.Fatalf("expected run size small, got %q", small)
	}
}

func TestResolveDatasetPathForRun(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global_mmlu")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// dev split: 2 rows
	dev := "" +
		`{"id":"d1","benchmark":"global-mmlu","split":"dev","subject":"math","language":"en","question":"Q1","choices":["a","b"],"answer_index":0,"answer_label":"A"}` + "\n" +
		`{"id":"d2","benchmark":"global-mmlu","split":"dev","subject":"math","language":"en","question":"Q2","choices":["a","b"],"answer_index":1,"answer_label":"B"}` + "\n"
	if err := os.WriteFile(filepath.Join(globalDir, "dev.jsonl"), []byte(dev), 0o644); err != nil {
		t.Fatalf("write dev fixture failed: %v", err)
	}

	// test split: 4 rows
	test := dev + dev
	if err := os.WriteFile(filepath.Join(globalDir, "test.jsonl"), []byte(test), 0o644); err != nil {
		t.Fatalf("write test fixture failed: %v", err)
	}

	// Repro split is intentionally tiny but should not be auto-selected by run-size=small.
	repro := `{"id":"r1","benchmark":"global-mmlu","split":"repro","subject":"math","language":"en","question":"Q3","choices":["a","b"],"answer_index":0,"answer_label":"A"}` + "\n"
	if err := os.WriteFile(filepath.Join(globalDir, "repro_issue.jsonl"), []byte(repro), 0o644); err != nil {
		t.Fatalf("write repro fixture failed: %v", err)
	}

	smallSplit, smallPath, err := ResolveDatasetPathForRun(tmpDir, "global-mmlu", "small", "")
	if err != nil {
		t.Fatalf("ResolveDatasetPathForRun small failed: %v", err)
	}
	if smallSplit != "dev" {
		t.Fatalf("expected dev for small run, got %q", smallSplit)
	}
	if smallPath != filepath.Join(globalDir, "dev.jsonl") {
		t.Fatalf("unexpected small path %q", smallPath)
	}

	fullSplit, fullPath, err := ResolveDatasetPathForRun(tmpDir, "global-mmlu", "full", "")
	if err != nil {
		t.Fatalf("ResolveDatasetPathForRun full failed: %v", err)
	}
	if fullSplit != "test" {
		t.Fatalf("expected test for full run, got %q", fullSplit)
	}
	if fullPath != filepath.Join(globalDir, "test.jsonl") {
		t.Fatalf("unexpected full path %q", fullPath)
	}

	customSplit, customPath, err := ResolveDatasetPathForRun(tmpDir, "global-mmlu", "custom", "repro_issue")
	if err != nil {
		t.Fatalf("ResolveDatasetPathForRun custom failed: %v", err)
	}
	if customSplit != "repro_issue" {
		t.Fatalf("expected custom split repro_issue, got %q", customSplit)
	}
	if customPath != filepath.Join(globalDir, "repro_issue.jsonl") {
		t.Fatalf("unexpected custom path %q", customPath)
	}
}

func TestResolveDatasetPathForRunMMLUPro(t *testing.T) {
	tmpDir := t.TempDir()
	proDir := filepath.Join(tmpDir, "mmlu_pro")
	if err := os.MkdirAll(proDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	validation := `{"id":"v1","benchmark":"mmlu-pro","split":"validation","subject":"math","language":"en","question":"Q1","choices":["a","b"],"answer_index":0,"answer_label":"A"}` + "\n"
	if err := os.WriteFile(filepath.Join(proDir, "validation.jsonl"), []byte(validation), 0o644); err != nil {
		t.Fatalf("write validation fixture failed: %v", err)
	}
	test := validation + validation
	if err := os.WriteFile(filepath.Join(proDir, "test.jsonl"), []byte(test), 0o644); err != nil {
		t.Fatalf("write test fixture failed: %v", err)
	}

	smallSplit, smallPath, err := ResolveDatasetPathForRun(tmpDir, "mmlu-pro", "small", "")
	if err != nil {
		t.Fatalf("ResolveDatasetPathForRun small failed: %v", err)
	}
	if smallSplit != "validation" {
		t.Fatalf("expected validation for mmlu-pro small run, got %q", smallSplit)
	}
	if smallPath != filepath.Join(proDir, "validation.jsonl") {
		t.Fatalf("unexpected mmlu-pro small path %q", smallPath)
	}

	fullSplit, fullPath, err := ResolveDatasetPathForRun(tmpDir, "mmlu-pro", "full", "")
	if err != nil {
		t.Fatalf("ResolveDatasetPathForRun full failed: %v", err)
	}
	if fullSplit != "test" {
		t.Fatalf("expected test for mmlu-pro full run, got %q", fullSplit)
	}
	if fullPath != filepath.Join(proDir, "test.jsonl") {
		t.Fatalf("unexpected mmlu-pro full path %q", fullPath)
	}
}

func TestResolveDatasetPathForRunMath500(t *testing.T) {
	tmpDir := t.TempDir()
	mathDir := filepath.Join(tmpDir, "math_500")
	if err := os.MkdirAll(mathDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	dev := `{"id":"m1","benchmark":"math-500","split":"dev","subject":"algebra","language":"en","question":"Q1","gold_answer":"7","gold_solution":"...","answer_label":"7"}` + "\n"
	test := dev + dev
	if err := os.WriteFile(filepath.Join(mathDir, "dev.jsonl"), []byte(dev), 0o644); err != nil {
		t.Fatalf("write dev fixture failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mathDir, "test.jsonl"), []byte(test), 0o644); err != nil {
		t.Fatalf("write test fixture failed: %v", err)
	}

	smallSplit, smallPath, err := ResolveDatasetPathForRun(tmpDir, "math-500", "small", "")
	if err != nil {
		t.Fatalf("ResolveDatasetPathForRun math-500 small failed: %v", err)
	}
	if smallSplit != "dev" {
		t.Fatalf("expected dev for math-500 small run, got %q", smallSplit)
	}
	if smallPath != filepath.Join(mathDir, "dev.jsonl") {
		t.Fatalf("unexpected math-500 small path %q", smallPath)
	}

	fullSplit, fullPath, err := ResolveDatasetPathForRun(tmpDir, "math-500", "full", "")
	if err != nil {
		t.Fatalf("ResolveDatasetPathForRun math-500 full failed: %v", err)
	}
	if fullSplit != "test" {
		t.Fatalf("expected test for math-500 full run, got %q", fullSplit)
	}
	if fullPath != filepath.Join(mathDir, "test.jsonl") {
		t.Fatalf("unexpected math-500 full path %q", fullPath)
	}
}

func TestToDatasetItemMath500(t *testing.T) {
	item, err := toDatasetItem(datasetRow{
		ID:           "m-1",
		Benchmark:    "math-500",
		Split:        "test",
		Subject:      "algebra",
		Question:     "Compute 1+1",
		GoldAnswer:   "2",
		GoldSolution: "Because arithmetic",
	}, 1)
	if err != nil {
		t.Fatalf("toDatasetItem math-500 failed: %v", err)
	}
	if item.GoldAnswer != "2" {
		t.Fatalf("GoldAnswer=%q, want 2", item.GoldAnswer)
	}
	if item.Language != "en" {
		t.Fatalf("Language=%q, want en", item.Language)
	}
	if item.AnswerLabel != "2" {
		t.Fatalf("AnswerLabel=%q, want 2", item.AnswerLabel)
	}
	if len(item.Choices) != 0 {
		t.Fatalf("Choices=%v, want empty", item.Choices)
	}
}
