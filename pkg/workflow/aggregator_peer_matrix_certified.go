package workflow

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

const (
	peerMatrixScoreMin                = 1.0
	peerMatrixScoreMax                = 10.0
	peerMatrixCertificationTolerance = 1e-9
	peerMatrixProofVersion            = "bounded-average-v1"
)

// PeerMatrixEvaluationPair identifies one ordered peer-evaluation cell.
type PeerMatrixEvaluationPair struct {
	ReviewerID  string `json:"reviewer_id"`
	CandidateID string `json:"candidate_id"`
}

// PeerMatrixScoreBound is the proof state for one candidate during certified
// progressive peer-matrix evaluation. LowerBound is only present once the
// candidate has at least one valid review. UpperBound remains present while a
// candidate can still receive a valid review.
type PeerMatrixScoreBound struct {
	ObservedAverage  *float64 `json:"observed_average,omitempty"`
	LowerBound       *float64 `json:"lower_bound,omitempty"`
	UpperBound       *float64 `json:"upper_bound,omitempty"`
	ValidReviews     int      `json:"valid_reviews"`
	InvalidReviews   int      `json:"invalid_reviews"`
	RemainingReviews int      `json:"remaining_reviews"`
	Eliminated       bool     `json:"eliminated,omitempty"`
	DominatedBy      string   `json:"dominated_by,omitempty"`
}

// PeerMatrixCertificate explains why progressive evaluation could stop without
// changing the winner that exhaustive peer-matrix evaluation could produce.
// The certificate is intentionally based only on the hard 1..10 score domain,
// not on probabilistic confidence or model-specific assumptions.
type PeerMatrixCertificate struct {
	Mode                          string                          `json:"mode"`
	ProofVersion                  string                          `json:"proof_version"`
	Certified                     bool                            `json:"certified"`
	Winner                        string                          `json:"winner,omitempty"`
	ScoreMin                      float64                         `json:"score_min"`
	ScoreMax                      float64                         `json:"score_max"`
	Normalization                 string                          `json:"normalization"`
	TieBreak                      string                          `json:"tie_break"`
	TotalEvaluations              int                             `json:"total_evaluations"`
	CompletedEvaluations          int                             `json:"completed_evaluations"`
	SkippedEvaluations            int                             `json:"skipped_evaluations"`
	SavingsRatio                  float64                         `json:"savings_ratio"`
	RoundsCompleted               int                             `json:"rounds_completed"`
	WinnerLowerBound              *float64                        `json:"winner_lower_bound,omitempty"`
	StrongestChallengerUpperBound *float64                        `json:"strongest_challenger_upper_bound,omitempty"`
	GuaranteedMargin              *float64                        `json:"guaranteed_margin,omitempty"`
	Bounds                        map[string]PeerMatrixScoreBound `json:"bounds"`
	SkippedPairs                  []PeerMatrixEvaluationPair      `json:"skipped_pairs,omitempty"`
}

type peerMatrixBoundState struct {
	ID        string
	Sum       float64
	Valid     int
	Invalid   int
	Attempted int
}

// validateCertifiedPeerMatrixConfig verifies the invariants required by the
// proof. The current exhaustive implementation treats normalization as a
// pass-through, but certified mode pins that behavior explicitly so a future
// normalization strategy cannot silently invalidate the bounds.
func validateCertifiedPeerMatrixConfig(cfg PeerMatrixConfig) error {
	if !strings.EqualFold(strings.TrimSpace(cfg.Normalization), "none") {
		return fmt.Errorf("peer_matrix certified_early_stop requires normalization=none: %w", ErrAggregationConfig)
	}
	if len(cfg.Rubric) == 0 {
		return fmt.Errorf("peer_matrix certified_early_stop requires a non-empty rubric: %w", ErrAggregationConfig)
	}

	var positiveWeight float64
	for _, criterion := range cfg.Rubric {
		if math.IsNaN(criterion.Weight) || math.IsInf(criterion.Weight, 0) || criterion.Weight < 0 {
			return fmt.Errorf("peer_matrix certified_early_stop requires finite non-negative rubric weights: %w", ErrAggregationConfig)
		}
		positiveWeight += criterion.Weight
	}
	if positiveWeight <= 0 || math.IsNaN(positiveWeight) || math.IsInf(positiveWeight, 0) {
		return fmt.Errorf("peer_matrix certified_early_stop requires positive total rubric weight: %w", ErrAggregationConfig)
	}
	return nil
}

// peerScoresWithinCertifiedRange guards the fallback score parser. A malformed
// provider response must never create a score outside the domain used by the
// mathematical certificate.
func peerScoresWithinCertifiedRange(scores map[string]float64) bool {
	if len(scores) == 0 {
		return false
	}
	for _, score := range scores {
		if math.IsNaN(score) || math.IsInf(score, 0) || score < peerMatrixScoreMin || score > peerMatrixScoreMax {
			return false
		}
	}
	return true
}

// buildCertifiedEvalRounds creates deterministic round-robin derangements.
// Every round gives each candidate at most one new review and each reviewer at
// most one task. Across N-1 rounds all N*(N-1) ordered pairs appear exactly once.
func buildCertifiedEvalRounds(inputs []AgentOutput) [][]evalTask {
	if len(inputs) < 2 {
		return nil
	}

	ordered := append([]AgentOutput(nil), inputs...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].AgentID < ordered[j].AgentID
	})

	rounds := make([][]evalTask, 0, len(ordered)-1)
	for offset := 1; offset < len(ordered); offset++ {
		round := make([]evalTask, 0, len(ordered))
		for reviewerIndex, reviewer := range ordered {
			candidate := ordered[(reviewerIndex+offset)%len(ordered)]
			reviewerName := reviewer.AgentID
			if name, ok := reviewer.Metadata["name"].(string); ok && name != "" {
				reviewerName = name
			}
			round = append(round, evalTask{
				ReviewerID:     reviewer.AgentID,
				ReviewerName:   reviewerName,
				ReviewerModel:  reviewer.Model,
				CandidateID:    candidate.AgentID,
				Response:       candidate.Output,
				ReviewerAnswer: reviewer.Output,
			})
		}
		rounds = append(rounds, round)
	}
	return rounds
}

// executeCertifiedEvaluations evaluates balanced rounds and permanently prunes
// candidates once another candidate's guaranteed lower bound is above their
// best possible upper bound. Pruning is safe because lower bounds can only rise
// (or stay fixed) as additional bounded reviews arrive.
func (p *PeerMatrixAggregator) executeCertifiedEvaluations(
	ctx context.Context,
	inputs []AgentOutput,
	cfg PeerMatrixConfig,
	originalPrompt string,
	llmClient *providers.Client,
	aggCtx *AggregationContext,
) ([]evalResult, *PeerMatrixCertificate, error) {
	rounds := buildCertifiedEvalRounds(inputs)
	totalEvaluations := len(inputs) * (len(inputs) - 1)
	results := make([]evalResult, 0, totalEvaluations)
	eliminated := make(map[string]struct{}, len(inputs))
	roundsCompleted := 0

	for _, round := range rounds {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		batch := make([]evalTask, 0, len(round))
		for _, task := range round {
			if _, skip := eliminated[task.CandidateID]; skip {
				continue
			}
			batch = append(batch, task)
		}
		if len(batch) == 0 {
			break
		}

		batchResults := p.executeEvaluations(ctx, batch, cfg, originalPrompt, llmClient, aggCtx)
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		results = append(results, batchResults...)
		roundsCompleted++

		certificate, err := buildPeerMatrixCertificate(results, inputs, roundsCompleted)
		if err != nil {
			return nil, nil, err
		}
		for candidateID, bound := range certificate.Bounds {
			if bound.Eliminated {
				eliminated[candidateID] = struct{}{}
			}
		}
		if certificate.Certified {
			certificate.SkippedPairs = peerMatrixSkippedPairs(results, inputs)
			return results, certificate, nil
		}
	}

	certificate, err := buildPeerMatrixCertificate(results, inputs, roundsCompleted)
	if err != nil {
		return nil, nil, err
	}
	if certificate.Certified {
		certificate.SkippedPairs = peerMatrixSkippedPairs(results, inputs)
	}
	return results, certificate, nil
}

// buildPeerMatrixCertificate computes conservative final-average intervals.
//
// For a candidate with observed sum S across C valid reviews and R unobserved
// reviews, every exhaustive final average must lie in:
//
//	lower = (S + R*1)  / (C + R)
//	upper = (S + R*10) / (C + R)
//
// Invalid future reviews are harmless to the proof: omitting a value in [1,10]
// can only move the final average back toward the already-observed average,
// which is itself inside the interval above.
func buildPeerMatrixCertificate(results []evalResult, inputs []AgentOutput, roundsCompleted int) (*PeerMatrixCertificate, error) {
	totalPerCandidate := len(inputs) - 1
	totalEvaluations := len(inputs) * totalPerCandidate
	states := make(map[string]*peerMatrixBoundState, len(inputs))
	ids := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if _, exists := states[input.AgentID]; exists {
			return nil, fmt.Errorf("peer_matrix certified_early_stop requires unique agent IDs; duplicate %q: %w", input.AgentID, ErrAggregationConfig)
		}
		states[input.AgentID] = &peerMatrixBoundState{ID: input.AgentID}
		ids = append(ids, input.AgentID)
	}
	sort.Strings(ids)

	seenPairs := make(map[string]struct{}, len(results))
	for _, result := range results {
		if states[result.ReviewerID] == nil {
			return nil, fmt.Errorf("peer_matrix certificate saw unknown reviewer %q", result.ReviewerID)
		}
		state := states[result.CandidateID]
		if state == nil {
			return nil, fmt.Errorf("peer_matrix certificate saw unknown candidate %q", result.CandidateID)
		}
		if result.ReviewerID == result.CandidateID {
			return nil, fmt.Errorf("peer_matrix certificate saw self-review for %q", result.CandidateID)
		}
		pairKey := peerMatrixPairKey(result.ReviewerID, result.CandidateID)
		if _, duplicate := seenPairs[pairKey]; duplicate {
			return nil, fmt.Errorf("peer_matrix certificate saw duplicate review pair %s -> %s", result.ReviewerID, result.CandidateID)
		}
		seenPairs[pairKey] = struct{}{}

		state.Attempted++
		if !result.Valid {
			state.Invalid++
			continue
		}
		if math.IsNaN(result.Score) || math.IsInf(result.Score, 0) || result.Score < peerMatrixScoreMin || result.Score > peerMatrixScoreMax {
			return nil, fmt.Errorf("peer_matrix certified score %.6f for %s is outside [1,10]", result.Score, result.CandidateID)
		}
		state.Valid++
		state.Sum += result.Score
	}

	bounds := make(map[string]PeerMatrixScoreBound, len(ids))
	for _, id := range ids {
		state := states[id]
		if state.Attempted > totalPerCandidate {
			return nil, fmt.Errorf("peer_matrix certificate has %d reviews for %s; maximum is %d", state.Attempted, id, totalPerCandidate)
		}
		remaining := totalPerCandidate - state.Attempted
		bound := PeerMatrixScoreBound{
			ValidReviews:     state.Valid,
			InvalidReviews:   state.Invalid,
			RemainingReviews: remaining,
		}

		if state.Valid > 0 {
			observed := state.Sum / float64(state.Valid)
			lower := observed
			upper := observed
			if remaining > 0 {
				denominator := float64(state.Valid + remaining)
				lower = (state.Sum + float64(remaining)*peerMatrixScoreMin) / denominator
				upper = (state.Sum + float64(remaining)*peerMatrixScoreMax) / denominator
			}
			bound.ObservedAverage = float64Ptr(observed)
			bound.LowerBound = float64Ptr(lower)
			bound.UpperBound = float64Ptr(upper)
		} else if remaining > 0 {
			// The candidate has no guaranteed lower score yet because every
			// remaining evaluation could be invalid. It can still achieve 10.
			bound.UpperBound = float64Ptr(peerMatrixScoreMax)
		}
		bounds[id] = bound
	}

	// A candidate is permanently eliminated when any scored candidate's worst
	// possible final average still beats its best possible final average. While
	// unseen reviews remain we require a small strict numerical margin; exact
	// alphabetical tie-breaking is only used once both sides are fully observed.
	for _, targetID := range ids {
		target := bounds[targetID]
		if target.UpperBound == nil {
			target.Eliminated = true
			bounds[targetID] = target
			continue
		}

		bestDominator := ""
		bestLower := math.Inf(-1)
		for _, challengerID := range ids {
			if challengerID == targetID {
				continue
			}
			challenger := bounds[challengerID]
			if challenger.LowerBound == nil {
				continue
			}
			if !peerBoundDominates(challengerID, challenger, targetID, target) {
				continue
			}
			lower := *challenger.LowerBound
			if lower > bestLower || (lower == bestLower && (bestDominator == "" || challengerID < bestDominator)) {
				bestLower = lower
				bestDominator = challengerID
			}
		}
		if bestDominator != "" {
			target.Eliminated = true
			target.DominatedBy = bestDominator
			bounds[targetID] = target
		}
	}

	active := make([]string, 0, len(ids))
	for _, id := range ids {
		if !bounds[id].Eliminated {
			active = append(active, id)
		}
	}

	certificate := &PeerMatrixCertificate{
		Mode:                 "certified_progressive",
		ProofVersion:         peerMatrixProofVersion,
		ScoreMin:             peerMatrixScoreMin,
		ScoreMax:             peerMatrixScoreMax,
		Normalization:        "none",
		TieBreak:             "agent_id_ascending",
		TotalEvaluations:     totalEvaluations,
		CompletedEvaluations: len(results),
		SkippedEvaluations:   totalEvaluations - len(results),
		RoundsCompleted:      roundsCompleted,
		Bounds:               bounds,
	}
	if totalEvaluations > 0 {
		certificate.SavingsRatio = float64(certificate.SkippedEvaluations) / float64(totalEvaluations)
	}

	if len(active) == 1 && bounds[active[0]].LowerBound != nil {
		winnerID := active[0]
		certificate.Certified = true
		certificate.Winner = winnerID
		winnerLower := *bounds[winnerID].LowerBound
		certificate.WinnerLowerBound = float64Ptr(winnerLower)

		var strongestUpper *float64
		for _, id := range ids {
			if id == winnerID || bounds[id].UpperBound == nil {
				continue
			}
			upper := *bounds[id].UpperBound
			if strongestUpper == nil || upper > *strongestUpper {
				strongestUpper = float64Ptr(upper)
			}
		}
		certificate.StrongestChallengerUpperBound = strongestUpper
		if strongestUpper != nil {
			margin := winnerLower - *strongestUpper
			certificate.GuaranteedMargin = float64Ptr(margin)
		}
	}

	return certificate, nil
}

func peerMatrixSkippedPairs(results []evalResult, inputs []AgentOutput) []PeerMatrixEvaluationPair {
	attempted := make(map[string]struct{}, len(results))
	for _, result := range results {
		attempted[peerMatrixPairKey(result.ReviewerID, result.CandidateID)] = struct{}{}
	}

	ids := make([]string, 0, len(inputs))
	for _, input := range inputs {
		ids = append(ids, input.AgentID)
	}
	sort.Strings(ids)

	skipped := make([]PeerMatrixEvaluationPair, 0)
	for _, reviewerID := range ids {
		for _, candidateID := range ids {
			if reviewerID == candidateID {
				continue
			}
			if _, ok := attempted[peerMatrixPairKey(reviewerID, candidateID)]; ok {
				continue
			}
			skipped = append(skipped, PeerMatrixEvaluationPair{ReviewerID: reviewerID, CandidateID: candidateID})
		}
	}
	return skipped
}

func peerMatrixPairKey(reviewerID, candidateID string) string {
	return reviewerID + "\x00" + candidateID
}

func peerBoundDominates(dominatorID string, dominator PeerMatrixScoreBound, targetID string, target PeerMatrixScoreBound) bool {
	if dominator.LowerBound == nil || target.UpperBound == nil {
		return false
	}
	lower := *dominator.LowerBound
	upper := *target.UpperBound
	if lower > upper+peerMatrixCertificationTolerance {
		return true
	}
	// Once both candidates are fully observed, exact ties are resolved by the
	// same alphabetical rule used by selectWinner.
	return dominator.RemainingReviews == 0 && target.RemainingReviews == 0 && lower == upper && dominatorID < targetID
}

func float64Ptr(v float64) *float64 {
	return &v
}
