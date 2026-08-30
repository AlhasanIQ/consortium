package workflow

import "testing"

func TestPeerMatrixCertificateRejectsDuplicateAndSelfReviewPairs(t *testing.T) {
	inputs := certifiedTestInputs("a", "b", "c")

	duplicate := []evalResult{
		{ReviewerID: "a", CandidateID: "b", Score: 8, Valid: true},
		{ReviewerID: "a", CandidateID: "b", Score: 9, Valid: true},
	}
	if _, err := buildPeerMatrixCertificate(duplicate, inputs, 1); err == nil {
		t.Fatal("duplicate reviewer-candidate pair must fail closed")
	}

	self := []evalResult{{ReviewerID: "a", CandidateID: "a", Score: 8, Valid: true}}
	if _, err := buildPeerMatrixCertificate(self, inputs, 1); err == nil {
		t.Fatal("self review must fail closed")
	}

	unknownReviewer := []evalResult{{ReviewerID: "missing", CandidateID: "a", Score: 8, Valid: true}}
	if _, err := buildPeerMatrixCertificate(unknownReviewer, inputs, 1); err == nil {
		t.Fatal("unknown reviewer must fail closed")
	}
}

func TestPeerMatrixSkippedPairsAreExactAndDeterministic(t *testing.T) {
	inputs := certifiedTestInputs("c", "a", "b")
	results := []evalResult{
		{ReviewerID: "a", CandidateID: "b", Score: 8, Valid: true},
		{ReviewerID: "b", CandidateID: "c", Valid: false},
		{ReviewerID: "c", CandidateID: "a", Score: 7, Valid: true},
	}

	skipped := peerMatrixSkippedPairs(results, inputs)
	want := []PeerMatrixEvaluationPair{
		{ReviewerID: "a", CandidateID: "c"},
		{ReviewerID: "b", CandidateID: "a"},
		{ReviewerID: "c", CandidateID: "b"},
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped pair count = %d, want %d: %+v", len(skipped), len(want), skipped)
	}
	for i := range want {
		if skipped[i] != want[i] {
			t.Fatalf("skipped[%d] = %+v, want %+v", i, skipped[i], want[i])
		}
	}
}
