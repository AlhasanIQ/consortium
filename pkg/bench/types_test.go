package bench

import "testing"

func TestFormatForBenchmark(t *testing.T) {
	tests := []struct {
		name      string
		benchmark string
		want      BenchmarkFormat
	}{
		{
			name:      "math-500 returns MathAnswer format",
			benchmark: BenchmarkMath500,
			want:      BenchmarkFormatMathAnswer,
		},
		{
			name:      "global-mmlu returns MCQA format",
			benchmark: BenchmarkGlobalMMLU,
			want:      BenchmarkFormatMCQA,
		},
		{
			name:      "global-mmlu-lite returns MCQA format",
			benchmark: BenchmarkGlobalMMLULite,
			want:      BenchmarkFormatMCQA,
		},
		{
			name:      "mmlu-pro returns MCQA format",
			benchmark: BenchmarkMMLUPro,
			want:      BenchmarkFormatMCQA,
		},
		{
			name:      "unknown benchmark defaults to MCQA",
			benchmark: "some-future-benchmark",
			want:      BenchmarkFormatMCQA,
		},
		{
			name:      "empty string defaults to MCQA",
			benchmark: "",
			want:      BenchmarkFormatMCQA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatForBenchmark(tt.benchmark)
			if got != tt.want {
				t.Fatalf("FormatForBenchmark(%q) = %q, want %q", tt.benchmark, got, tt.want)
			}
		})
	}
}

func TestBenchmarkFormatConstants(t *testing.T) {
	// Ensure constants have expected string values (stable for DB storage and API contracts)
	if BenchmarkFormatMCQA != "mcqa" {
		t.Fatalf("BenchmarkFormatMCQA = %q, want %q", BenchmarkFormatMCQA, "mcqa")
	}
	if BenchmarkFormatMathAnswer != "math_answer" {
		t.Fatalf("BenchmarkFormatMathAnswer = %q, want %q", BenchmarkFormatMathAnswer, "math_answer")
	}
}

func TestBenchmarkNameConstants(t *testing.T) {
	// Ensure benchmark name constants match expected canonical strings
	expected := map[string]string{
		"BenchmarkGlobalMMLU":     "global-mmlu",
		"BenchmarkGlobalMMLULite": "global-mmlu-lite",
		"BenchmarkMMLUPro":        "mmlu-pro",
		"BenchmarkMath500":        "math-500",
	}

	got := map[string]string{
		"BenchmarkGlobalMMLU":     BenchmarkGlobalMMLU,
		"BenchmarkGlobalMMLULite": BenchmarkGlobalMMLULite,
		"BenchmarkMMLUPro":        BenchmarkMMLUPro,
		"BenchmarkMath500":        BenchmarkMath500,
	}

	for constName, wantVal := range expected {
		if got[constName] != wantVal {
			t.Fatalf("constant %s = %q, want %q", constName, got[constName], wantVal)
		}
	}
}
