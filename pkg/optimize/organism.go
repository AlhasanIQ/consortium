package optimize

import (
	"encoding/json"
	"fmt"
	"time"
)

// Organism is one materialized workflow variant in an optimization run.
type Organism struct {
	ID           string            `json:"id"`
	OptRunID     string            `json:"opt_run_id"`
	Generation   int               `json:"generation"`
	ParentIDs    []string          `json:"parent_ids"`
	ParamValues  map[string]string `json:"param_values"`
	WorkflowJSON json.RawMessage   `json:"workflow_json"`
	MutationType string            `json:"mutation_type"`
	MutationLog  string            `json:"mutation_log"`

	BenchRunID string   `json:"bench_run_id,omitempty"`
	Fitness    *Fitness `json:"fitness,omitempty"`

	CreatedAt   time.Time  `json:"created_at"`
	EvaluatedAt *time.Time `json:"evaluated_at,omitempty"`
}

// Fitness stores benchmark metrics and derived optimization values.
type Fitness struct {
	Accuracy         float64 `json:"accuracy"`
	AdjustedAccuracy float64 `json:"adjusted_accuracy"`
	ParseRate        float64 `json:"parse_rate"`
	TotalCost        float64 `json:"total_cost"`
	CostPerItem      float64 `json:"cost_per_item"`
	AvgLatencyMS     float64 `json:"avg_latency_ms"`
	P95LatencyMS     float64 `json:"p95_latency_ms"`
	FailedItems      int     `json:"failed_items"`
	TotalItems       int     `json:"total_items"`
	FlaggedItems     int     `json:"flagged_items"`

	CompositeScore       float64  `json:"composite_score"`
	Feasible             bool     `json:"feasible"`
	ConstraintViolations []string `json:"constraint_violations"`
}

func (f *Fitness) MetricValue(metric string) float64 {
	if f == nil {
		return 0
	}
	switch metric {
	case "accuracy", "adjusted_accuracy":
		return f.AdjustedAccuracy
	case "raw_accuracy":
		return f.Accuracy
	case "parse_rate":
		return f.ParseRate
	case "cost_per_item":
		return f.CostPerItem
	case "avg_latency_ms":
		return f.AvgLatencyMS
	case "p95_latency_ms":
		return f.P95LatencyMS
	default:
		return 0
	}
}

// ComputeCompositeScore calculates weighted objective score where higher is always better.
// For "minimize" objectives, the raw value is inverted (1/val) so that lower raw values
// produce higher scores. Edge case: a zero-cost metric scores 0 for that objective rather
// than infinity — this is conservative and avoids rewarding potentially-broken zero-cost runs.
func ComputeCompositeScore(f *Fitness, objectives []Objective) float64 {
	if f == nil {
		return 0
	}
	score := 0.0
	for _, objective := range objectives {
		val := f.MetricValue(objective.Metric)
		if objective.Direction == "minimize" {
			if val > 0 {
				val = 1.0 / val
			} else {
				val = 0 // Zero metric → score 0 (conservative; avoids rewarding broken runs)
			}
		}
		score += val * objective.Weight
	}
	return score
}

// CheckConstraints returns whether all constraints are satisfied and violation strings.
func CheckConstraints(f *Fitness, constraints []Constraint) (bool, []string) {
	if f == nil {
		if len(constraints) == 0 {
			return true, nil
		}
		return false, []string{"fitness is nil"}
	}

	violations := make([]string, 0)
	for _, constraint := range constraints {
		val := f.MetricValue(constraint.Metric)
		var satisfied bool
		switch constraint.Op {
		case "<=":
			satisfied = val <= constraint.Value
		case ">=":
			satisfied = val >= constraint.Value
		case "<":
			satisfied = val < constraint.Value
		case ">":
			satisfied = val > constraint.Value
		default:
			violations = append(violations, fmt.Sprintf("unsupported op %q for %s", constraint.Op, constraint.Metric))
			continue
		}
		if !satisfied {
			violations = append(violations, fmt.Sprintf("%s=%.6f %s %.6f", constraint.Metric, val, inverseConstraintOp(constraint.Op), constraint.Value))
		}
	}
	return len(violations) == 0, violations
}

func inverseConstraintOp(op string) string {
	switch op {
	case "<=":
		return ">"
	case ">=":
		return "<"
	case "<":
		return ">="
	case ">":
		return "<="
	default:
		return op
	}
}
