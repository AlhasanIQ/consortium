package optimize

import "testing"

func TestFitness_MetricValue(t *testing.T) {
	f := &Fitness{
		Accuracy:         0.85,
		AdjustedAccuracy: 0.80,
		ParseRate:        0.95,
		CostPerItem:      0.02,
		AvgLatencyMS:     150,
		P95LatencyMS:     300,
	}
	tests := []struct {
		metric string
		want   float64
	}{
		{"accuracy", 0.80},          // maps to AdjustedAccuracy
		{"adjusted_accuracy", 0.80}, // maps to AdjustedAccuracy
		{"raw_accuracy", 0.85},
		{"parse_rate", 0.95},
		{"cost_per_item", 0.02},
		{"avg_latency_ms", 150},
		{"p95_latency_ms", 300},
		{"unknown_metric", 0},
	}
	for _, tt := range tests {
		got := f.MetricValue(tt.metric)
		if got != tt.want {
			t.Errorf("MetricValue(%q) = %v, want %v", tt.metric, got, tt.want)
		}
	}
}

func TestFitness_MetricValue_Nil(t *testing.T) {
	var f *Fitness
	if got := f.MetricValue("accuracy"); got != 0 {
		t.Errorf("nil fitness MetricValue = %v, want 0", got)
	}
}

func TestComputeCompositeScore_MinimizeZeroCost(t *testing.T) {
	f := &Fitness{CostPerItem: 0}
	score := ComputeCompositeScore(f, []Objective{
		{Metric: "cost_per_item", Weight: 1.0, Direction: "minimize"},
	})
	if score != 0 {
		t.Errorf("zero cost minimize should score 0 (conservative), got %v", score)
	}
}

func TestComputeCompositeScore_MultiObjective(t *testing.T) {
	f := &Fitness{
		AdjustedAccuracy: 0.80,
		CostPerItem:      0.04,
	}
	score := ComputeCompositeScore(f, []Objective{
		{Metric: "accuracy", Weight: 0.7, Direction: "maximize"},
		{Metric: "cost_per_item", Weight: 0.3, Direction: "minimize"},
	})
	// 0.80*0.7 + (1/0.04)*0.3 = 0.56 + 7.5 = 8.06
	if score < 8.0 || score > 8.1 {
		t.Errorf("expected ~8.06, got %v", score)
	}
}

func TestCheckConstraints_AllOps(t *testing.T) {
	f := &Fitness{AdjustedAccuracy: 0.80}

	tests := []struct {
		op     string
		value  float64
		wantOK bool
	}{
		{">=", 0.70, true},
		{">=", 0.90, false},
		{"<=", 0.90, true},
		{"<=", 0.70, false},
		{">", 0.70, true},
		{">", 0.80, false},
		{"<", 0.90, true},
		{"<", 0.80, false},
	}
	for _, tt := range tests {
		ok, _ := CheckConstraints(f, []Constraint{
			{Metric: "accuracy", Op: tt.op, Value: tt.value},
		})
		if ok != tt.wantOK {
			t.Errorf("accuracy=0.80 %s %.2f: ok=%v, want %v", tt.op, tt.value, ok, tt.wantOK)
		}
	}
}

func TestCheckConstraints_UnsupportedOp(t *testing.T) {
	f := &Fitness{AdjustedAccuracy: 0.80}
	ok, violations := CheckConstraints(f, []Constraint{
		{Metric: "accuracy", Op: "!=", Value: 0.5},
	})
	if ok {
		t.Error("expected not ok for unsupported op")
	}
	if len(violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(violations))
	}
}
