package benchloop

import (
	"fmt"
	"math"
)

// Gate evaluation constants.
const (
	EpsilonAccuracy  = 0.001 // minimum accuracy improvement to count as "better"
	EpsilonParseRate = 0.002 // minimum parse rate improvement for tiebreak
	EpsilonCostRatio = 0.05  // minimum cost reduction ratio for tiebreak
	MinParseRate     = 0.95  // absolute floor for parse rate
	ParseRateSlack   = 0.01  // allowed parse rate regression from baseline
)

// GateResult is the outcome of promotion gate evaluation.
type GateResult struct {
	Accept   bool
	Override bool   // true if controller overrode the agent's verdict
	Reason   string // human-readable explanation
}

// EvaluateGate checks whether a decision should be promoted or rejected.
// The state represents the current best (baseline to beat).
func EvaluateGate(decision *Decision, state *State) GateResult {
	// Safety floor: parse rate.
	minRequired := math.Max(MinParseRate, state.CurrentParseRate-ParseRateSlack)
	if decision.ParseRate < minRequired {
		return GateResult{
			Accept: false,
			Reason: fmt.Sprintf("parse rate %.3f below floor %.3f", decision.ParseRate, minRequired),
		}
	}

	// Safety floor: failed items.
	if decision.FailedItems > state.CurrentFailedItems {
		return GateResult{
			Accept: false,
			Reason: fmt.Sprintf("failed items %d exceeds baseline %d", decision.FailedItems, state.CurrentFailedItems),
		}
	}

	// Agent requested rollback — honor it.
	if decision.Verdict == VerdictRollback {
		return GateResult{
			Accept: false,
			Reason: "agent requested rollback",
		}
	}

	// Agent says stop with no progress — rollback but don't override (stop logic is in the loop).
	if decision.Verdict == VerdictStopNoProgress {
		return GateResult{
			Accept: false,
			Reason: "agent reports no progress",
		}
	}

	// Matrix change request — can't promote, needs matrix expansion first.
	if decision.Verdict == VerdictRequestMatrixChange {
		return GateResult{
			Accept: false,
			Reason: "agent requests matrix change before promotion",
		}
	}

	// Promotion comparator: accuracy > parse rate > cost (priority order).
	improved := isImproved(decision, state)

	// Override: agent says continue/stop_improved but there's a regression.
	accuracyDelta := decision.Accuracy - state.CurrentAccuracy
	if accuracyDelta < -EpsilonAccuracy {
		return GateResult{
			Accept:   false,
			Override: decision.Verdict == VerdictContinue || decision.Verdict == VerdictStopImproved,
			Reason:   fmt.Sprintf("accuracy regression %.4f (%.4f -> %.4f)", accuracyDelta, state.CurrentAccuracy, decision.Accuracy),
		}
	}

	if !improved {
		return GateResult{
			Accept: false,
			Reason: "no meaningful improvement on any metric",
		}
	}

	// All gates pass — accept.
	reason := fmt.Sprintf("accuracy %.4f -> %.4f", state.CurrentAccuracy, decision.Accuracy)
	if decision.AvgLatencyMS > 0 {
		reason += fmt.Sprintf(" (latency %.0fms)", decision.AvgLatencyMS)
	}
	if decision.Verdict == VerdictStopImproved {
		reason += " (agent recommends stopping)"
	}
	return GateResult{
		Accept: true,
		Reason: reason,
	}
}

// isImproved checks whether the decision represents a meaningful improvement
// using the priority comparator: accuracy > parse rate > cost.
func isImproved(decision *Decision, state *State) bool {
	// Priority 1: accuracy.
	accDelta := decision.Accuracy - state.CurrentAccuracy
	if accDelta > EpsilonAccuracy {
		return true
	}
	if accDelta < -EpsilonAccuracy {
		return false
	}

	// Accuracy tied within epsilon — check parse rate.
	parseDelta := decision.ParseRate - state.CurrentParseRate
	if parseDelta > EpsilonParseRate {
		return true
	}
	if parseDelta < -EpsilonParseRate {
		return false
	}

	// Parse rate also tied — check cost reduction.
	if state.CurrentCostPerItem > 0 {
		costRatio := (state.CurrentCostPerItem - decision.CostPerItem) / state.CurrentCostPerItem
		if costRatio > EpsilonCostRatio {
			return true
		}
	}

	return false
}
