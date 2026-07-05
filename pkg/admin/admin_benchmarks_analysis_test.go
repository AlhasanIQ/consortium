package admin

import (
	"math"
	"testing"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// ---------------------------------------------------------------------------
// parseAnswer
// ---------------------------------------------------------------------------

func TestAnalysis_ParseAnswer(t *testing.T) {
	tests := []struct {
		name    string
		format  bench.BenchmarkFormat
		raw     string
		wantAns string
		wantOK  bool
	}{
		{
			name:    "mcqa exact letter",
			format:  bench.BenchmarkFormatMCQA,
			raw:     "B",
			wantAns: "B",
			wantOK:  true,
		},
		{
			name:    "mcqa lowercase letter",
			format:  bench.BenchmarkFormatMCQA,
			raw:     "c",
			wantAns: "C",
			wantOK:  true,
		},
		{
			name:    "mcqa answer line",
			format:  bench.BenchmarkFormatMCQA,
			raw:     "The answer is A",
			wantAns: "A",
			wantOK:  true,
		},
		{
			name:    "mcqa empty string",
			format:  bench.BenchmarkFormatMCQA,
			raw:     "",
			wantAns: "",
			wantOK:  false,
		},
		{
			name:    "mcqa whitespace only",
			format:  bench.BenchmarkFormatMCQA,
			raw:     "   ",
			wantAns: "",
			wantOK:  false,
		},
		{
			name:    "mcqa unparseable text",
			format:  bench.BenchmarkFormatMCQA,
			raw:     "I'm not sure about this question",
			wantAns: "",
			wantOK:  false,
		},
		{
			name:    "math simple integer",
			format:  bench.BenchmarkFormatMathAnswer,
			raw:     "42",
			wantAns: "42",
			wantOK:  true,
		},
		{
			name:    "math with whitespace",
			format:  bench.BenchmarkFormatMathAnswer,
			raw:     "  42  ",
			wantAns: "42",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ans, ok := parseAnswer(tt.format, tt.raw)
			if ok != tt.wantOK {
				t.Errorf("parseAnswer(%q, %q) ok = %v, want %v", tt.format, tt.raw, ok, tt.wantOK)
			}
			if ans != tt.wantAns {
				t.Errorf("parseAnswer(%q, %q) answer = %q, want %q", tt.format, tt.raw, ans, tt.wantAns)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// answersMatch
// ---------------------------------------------------------------------------

func TestAnalysis_AnswersMatch(t *testing.T) {
	tests := []struct {
		name      string
		format    bench.BenchmarkFormat
		predicted string
		gold      string
		want      bool
	}{
		{
			name:      "mcqa exact match",
			format:    bench.BenchmarkFormatMCQA,
			predicted: "A",
			gold:      "A",
			want:      true,
		},
		{
			name:      "mcqa case insensitive",
			format:    bench.BenchmarkFormatMCQA,
			predicted: "a",
			gold:      "A",
			want:      true,
		},
		{
			name:      "mcqa mismatch",
			format:    bench.BenchmarkFormatMCQA,
			predicted: "B",
			gold:      "A",
			want:      false,
		},
		{
			name:      "math exact match",
			format:    bench.BenchmarkFormatMathAnswer,
			predicted: "42",
			gold:      "42",
			want:      true,
		},
		{
			name:      "math mismatch",
			format:    bench.BenchmarkFormatMathAnswer,
			predicted: "41",
			gold:      "42",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := answersMatch(tt.format, tt.predicted, tt.gold)
			if got != tt.want {
				t.Errorf("answersMatch(%q, %q, %q) = %v, want %v", tt.format, tt.predicted, tt.gold, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classifyWrongAnswer
// ---------------------------------------------------------------------------

func TestAnalysis_ClassifyWrongAnswer(t *testing.T) {
	format := bench.BenchmarkFormatMCQA

	tests := []struct {
		name               string
		goldLabel          string
		childResultAnswer  string
		childResultParseOK bool
		agentAnswers       []agentNodeAnswer
		want               wrongAnswerCategory
	}{
		{
			name:               "child right parent wrong",
			goldLabel:          "A",
			childResultAnswer:  "A",
			childResultParseOK: true,
			agentAnswers: []agentNodeAnswer{
				{NodeID: "n1", Answer: "B", Correct: false, ParseOK: true},
			},
			want: categoryChildRightParentWrong,
		},
		{
			name:               "all agents right but child wrong (all_right_child_wrong)",
			goldLabel:          "A",
			childResultAnswer:  "B",
			childResultParseOK: true,
			agentAnswers: []agentNodeAnswer{
				{NodeID: "n1", Answer: "A", Correct: true, ParseOK: true},
				{NodeID: "n2", Answer: "A", Correct: true, ParseOK: true},
			},
			want: categoryAllRightChildWrong,
		},
		{
			name:               "some agents right child wrong",
			goldLabel:          "A",
			childResultAnswer:  "B",
			childResultParseOK: true,
			agentAnswers: []agentNodeAnswer{
				{NodeID: "n1", Answer: "A", Correct: true, ParseOK: true},
				{NodeID: "n2", Answer: "C", Correct: false, ParseOK: true},
			},
			want: categorySomeRightChildWrong,
		},
		{
			name:               "all steps wrong",
			goldLabel:          "A",
			childResultAnswer:  "B",
			childResultParseOK: true,
			agentAnswers: []agentNodeAnswer{
				{NodeID: "n1", Answer: "B", Correct: false, ParseOK: true},
				{NodeID: "n2", Answer: "C", Correct: false, ParseOK: true},
			},
			want: categoryAllStepsWrong,
		},
		{
			name:               "no parsed agents is unclassified",
			goldLabel:          "A",
			childResultAnswer:  "B",
			childResultParseOK: true,
			agentAnswers: []agentNodeAnswer{
				{NodeID: "n1", Answer: "", Correct: false, ParseOK: false},
			},
			want: categoryUnclassified,
		},
		{
			name:               "empty agent answers is unclassified",
			goldLabel:          "A",
			childResultAnswer:  "B",
			childResultParseOK: true,
			agentAnswers:       nil,
			want:               categoryUnclassified,
		},
		{
			name:               "child not parsed still classifies agents",
			goldLabel:          "A",
			childResultAnswer:  "",
			childResultParseOK: false,
			agentAnswers: []agentNodeAnswer{
				{NodeID: "n1", Answer: "B", Correct: false, ParseOK: true},
			},
			want: categoryAllStepsWrong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyWrongAnswer(format, tt.goldLabel, tt.childResultAnswer, tt.childResultParseOK, tt.agentAnswers)
			if got != tt.want {
				t.Errorf("classifyWrongAnswer() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// percentileFloat64
// ---------------------------------------------------------------------------

func TestAnalysis_PercentileFloat64(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		p      int
		want   float64
	}{
		{
			name:   "empty returns zero",
			values: nil,
			p:      95,
			want:   0,
		},
		{
			name:   "single value",
			values: []float64{5.0},
			p:      50,
			want:   5.0,
		},
		{
			name:   "single value p95",
			values: []float64{5.0},
			p:      95,
			want:   5.0,
		},
		{
			name:   "two values p50",
			values: []float64{1.0, 2.0},
			p:      50,
			want:   1.0,
		},
		{
			name:   "two values p100",
			values: []float64{1.0, 2.0},
			p:      100,
			want:   2.0,
		},
		{
			name:   "10 values p95",
			values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			p:      95,
			want:   10.0,
		},
		{
			name:   "10 values p50",
			values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			p:      50,
			want:   5.0,
		},
		{
			name:   "unsorted input still works",
			values: []float64{10, 1, 5, 3, 7},
			p:      50,
			want:   5.0,
		},
		{
			name:   "does not mutate original",
			values: []float64{3, 1, 2},
			p:      50,
			want:   2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Keep a copy to verify no mutation.
			var original []float64
			if tt.values != nil {
				original = make([]float64, len(tt.values))
				copy(original, tt.values)
			}

			got := percentileFloat64(tt.values, tt.p)
			if got != tt.want {
				t.Errorf("percentileFloat64(%v, %d) = %v, want %v", tt.values, tt.p, got, tt.want)
			}

			// Verify the input slice was not mutated.
			if original != nil {
				for i, v := range tt.values {
					if v != original[i] {
						t.Errorf("percentileFloat64 mutated input: index %d changed from %v to %v", i, original[i], v)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractChildWorkflowAnswers
// ---------------------------------------------------------------------------

func TestAnalysis_ExtractChildWorkflowAnswers(t *testing.T) {
	format := bench.BenchmarkFormatMCQA

	t.Run("extracts agent answers and result", func(t *testing.T) {
		childNodes := []storage.WorkflowNode{
			{NodeID: "agent-1", NodeType: "prompt", Model: "model-a", Output: "A"},
			{NodeID: "agent-2", NodeType: "prompt", Model: "model-b", Output: "B"},
			{NodeID: "result", NodeType: "result", Output: "A"},
		}

		agents, childAns, childOK := extractChildWorkflowAnswers(format, childNodes, "A")
		if len(agents) != 2 {
			t.Fatalf("expected 2 agent answers, got %d", len(agents))
		}
		if !agents[0].Correct {
			t.Error("agent-1 should be correct")
		}
		if agents[1].Correct {
			t.Error("agent-2 should be incorrect")
		}
		if childAns != "A" || !childOK {
			t.Errorf("child result = (%q, %v), want (A, true)", childAns, childOK)
		}
	})

	t.Run("skips sub-nodes with parent_node_id", func(t *testing.T) {
		childNodes := []storage.WorkflowNode{
			{NodeID: "agent-1", NodeType: "prompt", Model: "model-a", Output: "A"},
			{NodeID: "agg-call-1", NodeType: "prompt", ParentNodeID: "result", Model: "model-x", Output: "C"},
			{NodeID: "result", NodeType: "result", Output: "A"},
		}

		agents, _, _ := extractChildWorkflowAnswers(format, childNodes, "A")
		if len(agents) != 1 {
			t.Fatalf("expected 1 agent answer (sub-nodes filtered), got %d", len(agents))
		}
		if agents[0].NodeID != "agent-1" {
			t.Errorf("expected agent-1, got %s", agents[0].NodeID)
		}
	})

	t.Run("empty nodes", func(t *testing.T) {
		agents, childAns, childOK := extractChildWorkflowAnswers(format, nil, "A")
		if len(agents) != 0 {
			t.Errorf("expected 0 agents, got %d", len(agents))
		}
		if childAns != "" || childOK {
			t.Errorf("expected empty result for nil nodes")
		}
	})
}

// ---------------------------------------------------------------------------
// benchmarkNodePerfAccumulator
// ---------------------------------------------------------------------------

func TestAnalysis_PerfAccumulator_AddSample(t *testing.T) {
	acc := &benchmarkNodePerfAccumulator{}

	acc.addSample(true, true, 0, 100.0, 0.01)
	acc.addSample(true, false, 2, 200.0, 0.02)
	acc.addSample(false, false, 0, 50.0, 0.005)

	if acc.samples != 3 {
		t.Errorf("samples = %d, want 3", acc.samples)
	}
	if acc.parseOK != 2 {
		t.Errorf("parseOK = %d, want 2", acc.parseOK)
	}
	if acc.correct != 1 {
		t.Errorf("correct = %d, want 1", acc.correct)
	}
	if acc.totalRetries != 2 {
		t.Errorf("totalRetries = %d, want 2", acc.totalRetries)
	}
	if acc.itemsWithRetries != 1 {
		t.Errorf("itemsWithRetries = %d, want 1", acc.itemsWithRetries)
	}
	if acc.totalLatencyMS != 350.0 {
		t.Errorf("totalLatencyMS = %f, want 350.0", acc.totalLatencyMS)
	}
	if math.Abs(acc.totalCostUSD-0.035) > 1e-9 {
		t.Errorf("totalCostUSD = %f, want 0.035", acc.totalCostUSD)
	}
}

func TestAnalysis_PerfAccumulator_ToView(t *testing.T) {
	t.Run("empty accumulator", func(t *testing.T) {
		acc := &benchmarkNodePerfAccumulator{}
		view := acc.toView()
		if view.Samples != 0 || view.Accuracy != 0 || view.AvgLatencyMS != 0 {
			t.Errorf("empty accumulator should produce zero view, got samples=%d accuracy=%f", view.Samples, view.Accuracy)
		}
	})

	t.Run("with samples", func(t *testing.T) {
		acc := &benchmarkNodePerfAccumulator{}
		acc.addSample(true, true, 0, 100.0, 0.01)
		acc.addSample(true, true, 1, 200.0, 0.02)
		acc.addSample(true, false, 0, 150.0, 0.015)
		acc.addSample(false, false, 0, 50.0, 0.005)

		view := acc.toView()
		if view.Samples != 4 {
			t.Errorf("samples = %d, want 4", view.Samples)
		}
		if view.ParseOK != 3 {
			t.Errorf("parseOK = %d, want 3", view.ParseOK)
		}
		if view.Correct != 2 {
			t.Errorf("correct = %d, want 2", view.Correct)
		}
		wantAccuracy := 2.0 / 4.0
		if math.Abs(view.Accuracy-wantAccuracy) > 1e-9 {
			t.Errorf("accuracy = %f, want %f", view.Accuracy, wantAccuracy)
		}
		wantParseRate := 3.0 / 4.0
		if math.Abs(view.ParseRate-wantParseRate) > 1e-9 {
			t.Errorf("parseRate = %f, want %f", view.ParseRate, wantParseRate)
		}
		wantAvgLatency := 500.0 / 4.0
		if math.Abs(view.AvgLatencyMS-wantAvgLatency) > 1e-9 {
			t.Errorf("avgLatencyMS = %f, want %f", view.AvgLatencyMS, wantAvgLatency)
		}
		if view.P95LatencyMS != 200.0 {
			t.Errorf("p95LatencyMS = %f, want 200.0", view.P95LatencyMS)
		}
	})
}

// ---------------------------------------------------------------------------
// benchmarkPerformanceCollector
// ---------------------------------------------------------------------------

func TestAnalysis_PerformanceCollector_IngestItem(t *testing.T) {
	t.Run("empty nodes is no-op", func(t *testing.T) {
		c := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 10)
		c.ingestItem("A", nil, nil, true)
		if c.itemsWithChildData != 0 {
			t.Errorf("expected 0 items with child data, got %d", c.itemsWithChildData)
		}
	})

	t.Run("ingests agent models and aggregation nodes", func(t *testing.T) {
		c := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 5)

		childNodes := []storage.WorkflowNode{
			{NodeID: "agent-1", NodeType: "prompt", Model: "model-a", Output: "A", LatencyMs: 100, Cost: 0.01, AttemptNumber: 1},
			{NodeID: "agent-2", NodeType: "prompt", Model: "model-b", Output: "B", LatencyMs: 200, Cost: 0.02, AttemptNumber: 1},
			{NodeID: "result", NodeType: "result", Output: "A", LatencyMs: 50, Cost: 0.005, AttemptNumber: 1},
			{NodeID: "agg-call", NodeType: "prompt", ParentNodeID: "result", Model: "model-c", Output: "A", LatencyMs: 50, Cost: 0.005, AttemptNumber: 1},
		}

		c.ingestItem("A", childNodes, nil, true)

		if c.itemsWithChildData != 1 {
			t.Errorf("itemsWithChildData = %d, want 1", c.itemsWithChildData)
		}

		// Two agent models: model-a and model-b.
		if len(c.agentModels) != 2 {
			t.Fatalf("expected 2 agent models, got %d", len(c.agentModels))
		}
		modelA := c.agentModels["model-a"]
		if modelA == nil || modelA.samples != 1 || modelA.correct != 1 {
			t.Errorf("model-a: samples=%d correct=%d", modelA.samples, modelA.correct)
		}
		modelB := c.agentModels["model-b"]
		if modelB == nil || modelB.samples != 1 || modelB.correct != 0 {
			t.Errorf("model-b: samples=%d correct=%d", modelB.samples, modelB.correct)
		}

		// One aggregation node: result.
		if len(c.aggregationNodes) != 1 {
			t.Fatalf("expected 1 aggregation node, got %d", len(c.aggregationNodes))
		}
		resultAcc := c.aggregationNodes["result"]
		if resultAcc == nil || resultAcc.samples != 1 {
			t.Fatalf("result node accumulator missing or wrong sample count")
		}
		// Aggregation node sums component latencies (result + agg-call).
		if resultAcc.totalLatencyMS != 100.0 {
			t.Errorf("result totalLatencyMS = %f, want 100.0 (50+50)", resultAcc.totalLatencyMS)
		}
		// model-c from the agg-call should be tracked.
		if _, ok := resultAcc.models["model-c"]; !ok {
			t.Error("expected model-c in aggregation models")
		}
	})

	t.Run("compiled aggregation internals are attributed to aggregation not agent rows", func(t *testing.T) {
		c := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 5)

		childNodes := []storage.WorkflowNode{
			{NodeID: "agent-a", NodeType: "prompt", Model: "solver-a", Output: "A", LatencyMs: 100, Cost: 0.01, AttemptNumber: 1},
			{NodeID: "agent-b", NodeType: "prompt", Model: "solver-b", Output: "B", LatencyMs: 120, Cost: 0.02, AttemptNumber: 1},
			{NodeID: "agg--judge", ParentNodeID: "agg--result", NodeType: "prompt", Model: "judge-model", Output: `{"winner":"A"}`, LatencyMs: 80, Cost: 0.03, AttemptNumber: 1},
			{NodeID: "agg--repair-selection", ParentNodeID: "agg--result", NodeType: "conditional", Output: "A", LatencyMs: 0, Cost: 0, AttemptNumber: 1},
			{NodeID: "agg--repair-selection_true", ParentNodeID: "agg--result", NodeType: "prompt", Model: "judge-model", Output: "A", LatencyMs: 60, Cost: 0.04, AttemptNumber: 1},
			{NodeID: "agg--parse-selection", ParentNodeID: "agg--result", NodeType: "operation", Output: "A", LatencyMs: 5, AttemptNumber: 1},
			{NodeID: "agg--result", NodeType: "result", Output: "A", LatencyMs: 3, AttemptNumber: 1},
		}
		childAttempts := []storage.NodeExecutionAttempt{
			{NodeID: "agg--judge", Attempt: 1, LatencyMs: 80, Cost: 0.03},
			{NodeID: "agg--repair-selection", Attempt: 1, LatencyMs: 0, Cost: 0},
			{NodeID: "agg--repair-selection_true", Attempt: 1, LatencyMs: 60, Cost: 0.04},
			{NodeID: "agg--parse-selection", Attempt: 1, LatencyMs: 5, Cost: 0},
			{NodeID: "agg--result", Attempt: 1, LatencyMs: 3, Cost: 0},
		}

		c.ingestItem("A", childNodes, childAttempts, true)

		if _, ok := c.agentModels["judge-model"]; ok {
			t.Fatalf("compiled aggregation judge model was counted as a solver model")
		}
		if len(c.agentModels) != 2 {
			t.Fatalf("agentModels len = %d, want 2", len(c.agentModels))
		}
		agg := c.aggregationNodes["agg--result"]
		if agg == nil {
			t.Fatalf("compiled aggregation result accumulator missing")
		}
		if _, ok := agg.models["judge-model"]; !ok {
			t.Fatalf("judge model missing from aggregation attribution: %+v", agg.models)
		}
		if agg.totalLatencyMS != 148 {
			t.Fatalf("aggregation totalLatencyMS = %f, want 148", agg.totalLatencyMS)
		}
		if math.Abs(agg.totalCostUSD-0.07) > 1e-9 {
			t.Fatalf("aggregation totalCostUSD = %f, want 0.07", agg.totalCostUSD)
		}
	})

	t.Run("unknown model gets placeholder", func(t *testing.T) {
		c := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 1)
		childNodes := []storage.WorkflowNode{
			{NodeID: "agent-1", NodeType: "prompt", Model: "", Output: "A", LatencyMs: 10, AttemptNumber: 1},
		}
		c.ingestItem("A", childNodes, nil, true)
		if _, ok := c.agentModels["(unknown)"]; !ok {
			t.Error("expected (unknown) model placeholder for empty model")
		}
	})

	t.Run("parent fallback data does not increment child data coverage", func(t *testing.T) {
		c := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 1)
		parentNodes := []storage.WorkflowNode{
			{NodeID: "solver", NodeType: "prompt", Model: "baseline-model", Output: "A", LatencyMs: 10, AttemptNumber: 1},
		}
		c.ingestItem("A", parentNodes, nil, false)
		if c.itemsWithChildData != 0 {
			t.Fatalf("itemsWithChildData = %d, want 0 for direct parent fallback", c.itemsWithChildData)
		}
		if c.agentModels["baseline-model"] == nil {
			t.Fatalf("parent fallback should still populate agent model rows")
		}
	})

	t.Run("uses attempt usage when available", func(t *testing.T) {
		c := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 1)
		childNodes := []storage.WorkflowNode{
			{NodeID: "agent-1", NodeType: "prompt", Model: "model-a", Output: "A", LatencyMs: 999, Cost: 999, AttemptNumber: 1},
		}
		attempts := []storage.NodeExecutionAttempt{
			{NodeID: "agent-1", Attempt: 1, LatencyMs: 50, Cost: 0.001},
			{NodeID: "agent-1", Attempt: 2, LatencyMs: 60, Cost: 0.002},
		}
		c.ingestItem("A", childNodes, attempts, true)

		modelA := c.agentModels["model-a"]
		if modelA == nil {
			t.Fatal("model-a accumulator missing")
		}
		// Should use attempt data (50+60=110), not node data (999).
		if modelA.totalLatencyMS != 110.0 {
			t.Errorf("totalLatencyMS = %f, want 110.0 (from attempts)", modelA.totalLatencyMS)
		}
		if modelA.totalRetries != 1 {
			t.Errorf("totalRetries = %d, want 1 (maxAttempt=2, retries=2-1=1)", modelA.totalRetries)
		}
	})
}

func TestAnalysis_PerformanceCollector_ToView(t *testing.T) {
	c := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 10)
	childNodes := []storage.WorkflowNode{
		{NodeID: "agent-1", NodeType: "prompt", Model: "model-b", Output: "A", LatencyMs: 100, AttemptNumber: 1},
		{NodeID: "agent-2", NodeType: "prompt", Model: "model-a", Output: "A", LatencyMs: 200, AttemptNumber: 1},
		{NodeID: "result", NodeType: "result", Output: "A", LatencyMs: 50, AttemptNumber: 1},
	}
	c.ingestItem("A", childNodes, nil, true)

	view := c.toView()
	if view.TotalItems != 10 {
		t.Errorf("totalItems = %d, want 10", view.TotalItems)
	}
	if view.ItemsWithChildData != 1 {
		t.Errorf("itemsWithChildData = %d, want 1", view.ItemsWithChildData)
	}
	// Agent models should be sorted alphabetically.
	if len(view.AgentModels) != 2 {
		t.Fatalf("expected 2 agent models, got %d", len(view.AgentModels))
	}
	if view.AgentModels[0].Model != "model-a" {
		t.Errorf("first agent model = %q, want model-a (sorted)", view.AgentModels[0].Model)
	}
	if view.AgentModels[1].Model != "model-b" {
		t.Errorf("second agent model = %q, want model-b (sorted)", view.AgentModels[1].Model)
	}
	if len(view.AggregationNodes) != 1 {
		t.Fatalf("expected 1 aggregation node, got %d", len(view.AggregationNodes))
	}
	if view.AggregationNodes[0].NodeID != "result" {
		t.Errorf("aggregation node = %q, want result", view.AggregationNodes[0].NodeID)
	}
}

// ---------------------------------------------------------------------------
// benchmarkAnalysisDiagnosticsCollector
// ---------------------------------------------------------------------------

func TestAnalysis_DiagnosticsCollector_DefaultTopN(t *testing.T) {
	c := newBenchmarkAnalysisDiagnosticsCollector(0)
	if c.topN != 10 {
		t.Errorf("default topN = %d, want 10", c.topN)
	}
}

func TestAnalysis_DiagnosticsCollector_ToView(t *testing.T) {
	c := newBenchmarkAnalysisDiagnosticsCollector(2)

	c.add(benchmarkAnalysisItemDiagnostics{ItemID: "slow", TotalLatency: 1000, TotalRetries: 0})
	c.add(benchmarkAnalysisItemDiagnostics{ItemID: "fast", TotalLatency: 10, TotalRetries: 5})
	c.add(benchmarkAnalysisItemDiagnostics{ItemID: "mid", TotalLatency: 500, TotalRetries: 3})

	view := c.toView()

	if view.TopN != 2 {
		t.Errorf("topN = %d, want 2", view.TopN)
	}

	// Slowest: top 2 by total latency descending.
	if len(view.SlowestItems) != 2 {
		t.Fatalf("expected 2 slowest items, got %d", len(view.SlowestItems))
	}
	if view.SlowestItems[0].ItemID != "slow" {
		t.Errorf("slowest[0] = %q, want slow", view.SlowestItems[0].ItemID)
	}
	if view.SlowestItems[1].ItemID != "mid" {
		t.Errorf("slowest[1] = %q, want mid", view.SlowestItems[1].ItemID)
	}

	// Most retries: top 2 by total retries descending.
	if len(view.MostRetryItems) != 2 {
		t.Fatalf("expected 2 most retry items, got %d", len(view.MostRetryItems))
	}
	if view.MostRetryItems[0].ItemID != "fast" {
		t.Errorf("mostRetries[0] = %q, want fast", view.MostRetryItems[0].ItemID)
	}
	if view.MostRetryItems[1].ItemID != "mid" {
		t.Errorf("mostRetries[1] = %q, want mid", view.MostRetryItems[1].ItemID)
	}
}

func TestAnalysis_DiagnosticsCollector_TopNExceedsItems(t *testing.T) {
	c := newBenchmarkAnalysisDiagnosticsCollector(100)
	c.add(benchmarkAnalysisItemDiagnostics{ItemID: "only-one", TotalLatency: 50, TotalRetries: 1})

	view := c.toView()
	if len(view.SlowestItems) != 1 {
		t.Errorf("expected 1 slowest item, got %d", len(view.SlowestItems))
	}
	if len(view.MostRetryItems) != 1 {
		t.Errorf("expected 1 most retry item, got %d", len(view.MostRetryItems))
	}
}

func TestAnalysis_DiagnosticsCollector_EmptyItems(t *testing.T) {
	c := newBenchmarkAnalysisDiagnosticsCollector(5)
	view := c.toView()
	if len(view.SlowestItems) != 0 {
		t.Errorf("expected 0 slowest items, got %d", len(view.SlowestItems))
	}
	if len(view.MostRetryItems) != 0 {
		t.Errorf("expected 0 most retry items, got %d", len(view.MostRetryItems))
	}
}

// ---------------------------------------------------------------------------
// summarizeNodeAttemptUsage
// ---------------------------------------------------------------------------

func TestAnalysis_SummarizeNodeAttemptUsage(t *testing.T) {
	attempts := []storage.NodeExecutionAttempt{
		{NodeID: "n1", Attempt: 1, LatencyMs: 100, Cost: 0.01},
		{NodeID: "n1", Attempt: 2, LatencyMs: 150, Cost: 0.02},
		{NodeID: "n2", Attempt: 1, LatencyMs: 200, Cost: 0.03},
	}

	usage := summarizeNodeAttemptUsage(attempts)

	n1 := usage["n1"]
	if !n1.hasAttempts {
		t.Error("n1 should have attempts")
	}
	if n1.totalLatency != 250 {
		t.Errorf("n1 totalLatency = %f, want 250", n1.totalLatency)
	}
	if n1.totalCost != 0.03 {
		t.Errorf("n1 totalCost = %f, want 0.03", n1.totalCost)
	}
	if n1.maxAttempt != 2 {
		t.Errorf("n1 maxAttempt = %d, want 2", n1.maxAttempt)
	}

	n2 := usage["n2"]
	if n2.maxAttempt != 1 {
		t.Errorf("n2 maxAttempt = %d, want 1", n2.maxAttempt)
	}

	// Non-existent node.
	n3 := usage["n3"]
	if n3.hasAttempts {
		t.Error("n3 should not have attempts")
	}
}

func TestAnalysis_SummarizeNodeAttemptUsage_Empty(t *testing.T) {
	usage := summarizeNodeAttemptUsage(nil)
	if len(usage) != 0 {
		t.Errorf("expected empty map, got %d entries", len(usage))
	}
}

// ---------------------------------------------------------------------------
// summarizeNodeUsageTotals
// ---------------------------------------------------------------------------

func TestAnalysis_SummarizeNodeUsageTotals(t *testing.T) {
	usage := map[string]benchmarkNodeAttemptUsage{
		"n1": {totalLatency: 100, maxAttempt: 3, hasAttempts: true},
		"n2": {totalLatency: 200, maxAttempt: 1, hasAttempts: true},
		"n3": {hasAttempts: false}, // Should be skipped.
	}

	latency, retries := summarizeNodeUsageTotals(usage)
	if latency != 300 {
		t.Errorf("latency = %f, want 300", latency)
	}
	// n1: max(3-1, 0) = 2, n2: max(1-1, 0) = 0.
	if retries != 2 {
		t.Errorf("retries = %d, want 2", retries)
	}
}

// ---------------------------------------------------------------------------
// summarizeNodeFallbackTotals
// ---------------------------------------------------------------------------

func TestAnalysis_SummarizeNodeFallbackTotals(t *testing.T) {
	nodes := []storage.WorkflowNode{
		{LatencyMs: 100, AttemptNumber: 3},
		{LatencyMs: 200, AttemptNumber: 1},
	}
	latency, retries := summarizeNodeFallbackTotals(nodes)
	if latency != 300 {
		t.Errorf("latency = %f, want 300", latency)
	}
	// Node 1: max(3-1, 0) = 2, Node 2: max(1-1, 0) = 0.
	if retries != 2 {
		t.Errorf("retries = %d, want 2", retries)
	}
}

// ---------------------------------------------------------------------------
// nodeUsageFor
// ---------------------------------------------------------------------------

func TestAnalysis_NodeUsageFor(t *testing.T) {
	t.Run("uses attempt data when available", func(t *testing.T) {
		node := storage.WorkflowNode{NodeID: "n1", LatencyMs: 999, Cost: 999, AttemptNumber: 1}
		usage := map[string]benchmarkNodeAttemptUsage{
			"n1": {totalLatency: 50, totalCost: 0.01, maxAttempt: 2, hasAttempts: true},
		}
		lat, cost, retries := nodeUsageFor(node, usage)
		if lat != 50 || cost != 0.01 || retries != 1 {
			t.Errorf("got (%f, %f, %d), want (50, 0.01, 1)", lat, cost, retries)
		}
	})

	t.Run("falls back to node data", func(t *testing.T) {
		node := storage.WorkflowNode{NodeID: "n1", LatencyMs: 100, Cost: 0.05, AttemptNumber: 3}
		usage := map[string]benchmarkNodeAttemptUsage{}
		lat, cost, retries := nodeUsageFor(node, usage)
		if lat != 100 || cost != 0.05 || retries != 2 {
			t.Errorf("got (%f, %f, %d), want (100, 0.05, 2)", lat, cost, retries)
		}
	})
}

// ---------------------------------------------------------------------------
// keysFromSet
// ---------------------------------------------------------------------------

func TestAnalysis_KeysFromSet(t *testing.T) {
	t.Run("nil map", func(t *testing.T) {
		got := keysFromSet(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		got := keysFromSet(map[string]struct{}{})
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("sorted output", func(t *testing.T) {
		got := keysFromSet(map[string]struct{}{
			"charlie": {},
			"alpha":   {},
			"bravo":   {},
		})
		if len(got) != 3 || got[0] != "alpha" || got[1] != "bravo" || got[2] != "charlie" {
			t.Errorf("expected [alpha bravo charlie], got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// maxNodeRetries
// ---------------------------------------------------------------------------

func TestAnalysis_MaxNodeRetries(t *testing.T) {
	if maxNodeRetries(3, 1) != 3 {
		t.Error("maxNodeRetries(3,1) should be 3")
	}
	if maxNodeRetries(1, 3) != 3 {
		t.Error("maxNodeRetries(1,3) should be 3")
	}
	if maxNodeRetries(0, 0) != 0 {
		t.Error("maxNodeRetries(0,0) should be 0")
	}
	if maxNodeRetries(-1, 0) != 0 {
		t.Error("maxNodeRetries(-1,0) should be 0")
	}
}
