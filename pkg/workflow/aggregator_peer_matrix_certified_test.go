package workflow

import (
	"math/rand"
	"testing"
)

func certifiedTestInputs(ids ...string) []AgentOutput {
	inputs := make([]AgentOutput, 0, len(ids))
	for _, id := range ids {
		inputs = append(inputs, AgentOutput{AgentID: id, Model: "mock-model", Output: "response-" + id})
	}
	return inputs
}

func TestBuildCertifiedEvalRoundsCoversEveryOrderedPairOnce(t *testing.T) {
	inputs := certifiedTestInputs("d", "b", "a", "c")
	rounds := buildCertifiedEvalRounds(inputs)
	if got, want := len(rounds), len(inputs)-1; got != want {
		t.Fatalf("round count = %d, want %d", got, want)
	}

	seen := make(map[string]int)
	for roundIndex, round := range rounds {
		if got, want := len(round), len(inputs); got != want {
			t.Fatalf("round %d has %d tasks, want %d", roundIndex, got, want)
		}
		reviewers := make(map[string]bool)
		candidates := make(map[string]bool)
		for _, task := range round {
			if task.ReviewerID == task.CandidateID {
				t.Fatalf("self review in round %d: %s", roundIndex, task.ReviewerID)
			}
			if reviewers[task.ReviewerID] {
				t.Fatalf("reviewer %s appears twice in round %d", task.ReviewerID, roundIndex)
			}
			if candidates[task.CandidateID] {
				t.Fatalf("candidate %s appears twice in round %d", task.CandidateID, roundIndex)
			}
			reviewers[task.ReviewerID] = true
			candidates[task.CandidateID] = true
			seen[task.ReviewerID+"->"+task.CandidateID]++
		}
	}

	for _, reviewer := range inputs {
		for _, candidate := range inputs {
			if reviewer.AgentID == candidate.AgentID {
				continue
			}
			key := reviewer.AgentID + "->" + candidate.AgentID
			if seen[key] != 1 {
				t.Errorf("pair %s appeared %d times, want exactly once", key, seen[key])
			}
		}
	}
}

func TestPeerMatrixCertificateStopsOnlyAfterWorstCaseCannotFlipWinner(t *testing.T) {
	inputs := certifiedTestInputs("a", "b", "c", "d", "e")
	rounds := buildCertifiedEvalRounds(inputs)
	results := make([]evalResult, 0, 15)

	for roundIndex := 0; roundIndex < 2; roundIndex++ {
		for _, task := range rounds[roundIndex] {
			score := 1.0
			if task.CandidateID == "a" {
				score = 10
			}
			results = append(results, evalResult{ReviewerID: task.ReviewerID, CandidateID: task.CandidateID, Score: score, Valid: true})
		}
	}
	cert, err := buildPeerMatrixCertificate(results, inputs, 2)
	if err != nil {
		t.Fatalf("unexpected certificate error: %v", err)
	}
	if cert.Certified {
		t.Fatal("certificate fired while a worst-case completion can still tie/flip the leader")
	}

	for _, task := range rounds[2] {
		score := 1.0
		if task.CandidateID == "a" {
			score = 10
		}
		results = append(results, evalResult{ReviewerID: task.ReviewerID, CandidateID: task.CandidateID, Score: score, Valid: true})
	}
	cert, err = buildPeerMatrixCertificate(results, inputs, 3)
	if err != nil {
		t.Fatalf("unexpected certificate error: %v", err)
	}
	if !cert.Certified || cert.Winner != "a" {
		t.Fatalf("certificate = %+v, want certified winner a", cert)
	}
	if cert.CompletedEvaluations != 15 || cert.TotalEvaluations != 20 || cert.SkippedEvaluations != 5 {
		t.Fatalf("evaluation counts = completed %d total %d skipped %d, want 15/20/5", cert.CompletedEvaluations, cert.TotalEvaluations, cert.SkippedEvaluations)
	}
	if cert.GuaranteedMargin == nil || *cert.GuaranteedMargin <= 0 {
		t.Fatalf("guaranteed margin = %v, want positive", cert.GuaranteedMargin)
	}
}

func TestPeerMatrixCertificateAccountsForInvalidFutureReviews(t *testing.T) {
	inputs := certifiedTestInputs("a", "b", "c", "d")
	results := []evalResult{
		{ReviewerID: "b", CandidateID: "a", Score: 10, Valid: true},
		{ReviewerID: "c", CandidateID: "a", Valid: false},
		{ReviewerID: "a", CandidateID: "b", Score: 2, Valid: true},
		{ReviewerID: "c", CandidateID: "b", Valid: false},
		{ReviewerID: "a", CandidateID: "c", Score: 2, Valid: true},
		{ReviewerID: "b", CandidateID: "c", Valid: false},
		{ReviewerID: "a", CandidateID: "d", Score: 2, Valid: true},
		{ReviewerID: "b", CandidateID: "d", Valid: false},
	}

	cert, err := buildPeerMatrixCertificate(results, inputs, 2)
	if err != nil {
		t.Fatalf("unexpected certificate error: %v", err)
	}
	if cert.Bounds["a"].InvalidReviews != 1 {
		t.Fatalf("a invalid reviews = %d, want 1", cert.Bounds["a"].InvalidReviews)
	}
	// One unseen 10 for a challenger can still produce avg(2,10)=6 while a's
	// unseen 1 can produce avg(10,1)=5.5, so stopping here would be unsafe.
	if cert.Certified {
		t.Fatal("certificate ignored invalid-review denominator semantics")
	}
}

func TestPeerMatrixCertificateUsesAlphabeticalTieBreakOnlyWhenExhausted(t *testing.T) {
	inputs := certifiedTestInputs("a", "b", "c")
	results := []evalResult{
		{ReviewerID: "b", CandidateID: "a", Score: 8, Valid: true},
		{ReviewerID: "c", CandidateID: "a", Score: 8, Valid: true},
		{ReviewerID: "a", CandidateID: "b", Score: 8, Valid: true},
		{ReviewerID: "c", CandidateID: "b", Score: 8, Valid: true},
		{ReviewerID: "a", CandidateID: "c", Score: 2, Valid: true},
		{ReviewerID: "b", CandidateID: "c", Score: 2, Valid: true},
	}

	cert, err := buildPeerMatrixCertificate(results, inputs, 2)
	if err != nil {
		t.Fatalf("unexpected certificate error: %v", err)
	}
	if !cert.Certified || cert.Winner != "a" {
		t.Fatalf("winner = %q certified=%v, want alphabetical tie winner a", cert.Winner, cert.Certified)
	}
	if cert.GuaranteedMargin == nil || *cert.GuaranteedMargin != 0 {
		t.Fatalf("tie margin = %v, want 0", cert.GuaranteedMargin)
	}
}

func TestCertifiedPeerMatrixConfigRejectsUnboundedRubricWeights(t *testing.T) {
	base := PeerMatrixConfig{
		Normalization: "none",
		Rubric:        []RubricCriterion{{Name: "quality", Weight: 1}},
	}
	if err := validateCertifiedPeerMatrixConfig(base); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	negative := base
	negative.Rubric = []RubricCriterion{{Name: "quality", Weight: -1}, {Name: "clarity", Weight: 2}}
	if err := validateCertifiedPeerMatrixConfig(negative); err == nil {
		t.Fatal("negative rubric weight must be rejected in certified mode")
	}

	zero := base
	zero.Rubric = []RubricCriterion{{Name: "quality", Weight: 0}}
	if err := validateCertifiedPeerMatrixConfig(zero); err == nil {
		t.Fatal("zero total rubric weight must be rejected in certified mode")
	}

	wrongNormalization := base
	wrongNormalization.Normalization = "zscore"
	if err := validateCertifiedPeerMatrixConfig(wrongNormalization); err == nil {
		t.Fatal("future/unknown normalization must fail closed in certified mode")
	}
}

func TestPeerMatrixCertificateRejectsOutOfDomainObservedScore(t *testing.T) {
	inputs := certifiedTestInputs("a", "b")
	_, err := buildPeerMatrixCertificate([]evalResult{{ReviewerID: "b", CandidateID: "a", Score: 11, Valid: true}}, inputs, 1)
	if err == nil {
		t.Fatal("expected out-of-domain score to fail closed")
	}
}

func TestPeerMatrixCertificateMatchesExhaustiveWinnerAcrossRandomMatrices(t *testing.T) {
	rng := rand.New(rand.NewSource(20260830))

	for iteration := 0; iteration < 2000; iteration++ {
		n := 3 + rng.Intn(5)
		ids := make([]string, n)
		for i := range ids {
			ids[i] = string(rune('a' + i))
		}
		inputs := certifiedTestInputs(ids...)
		rounds := buildCertifiedEvalRounds(inputs)

		type scripted struct {
			score float64
			valid bool
		}
		script := make(map[string]scripted, n*(n-1))
		for _, round := range rounds {
			for _, task := range round {
				script[task.ReviewerID+"->"+task.CandidateID] = scripted{
					score: float64(1 + rng.Intn(10)),
					valid: rng.Intn(10) != 0,
				}
			}
		}

		fullResults := make([]evalResult, 0, n*(n-1))
		for _, round := range rounds {
			for _, task := range round {
				value := script[task.ReviewerID+"->"+task.CandidateID]
				fullResults = append(fullResults, evalResult{
					ReviewerID:  task.ReviewerID,
					CandidateID: task.CandidateID,
					Score:       value.score,
					Valid:       value.valid,
				})
			}
		}
		fullMatrix := buildEvaluationMatrix(fullResults, inputs, "none")
		exhaustiveWinner, _ := selectWinner(fullMatrix.FinalScores, inputs)

		progressiveResults := make([]evalResult, 0, len(fullResults))
		eliminated := make(map[string]bool)
		var certifiedWinner string
		for roundIndex, round := range rounds {
			for _, task := range round {
				if eliminated[task.CandidateID] {
					continue
				}
				value := script[task.ReviewerID+"->"+task.CandidateID]
				progressiveResults = append(progressiveResults, evalResult{
					ReviewerID:  task.ReviewerID,
					CandidateID: task.CandidateID,
					Score:       value.score,
					Valid:       value.valid,
				})
			}
			cert, err := buildPeerMatrixCertificate(progressiveResults, inputs, roundIndex+1)
			if err != nil {
				t.Fatalf("iteration %d: certificate error: %v", iteration, err)
			}
			for id, bound := range cert.Bounds {
				if bound.Eliminated {
					eliminated[id] = true
				}
			}
			if cert.Certified {
				certifiedWinner = cert.Winner
				break
			}
		}

		if certifiedWinner != "" && certifiedWinner != exhaustiveWinner {
			t.Fatalf("iteration %d: certified winner %s != exhaustive winner %s", iteration, certifiedWinner, exhaustiveWinner)
		}
	}
}
