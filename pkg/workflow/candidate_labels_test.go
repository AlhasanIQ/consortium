package workflow

import "testing"

func TestCandidateLabelsExtendPastZ(t *testing.T) {
	labels := CandidateLabels(29)
	want := map[int]string{
		0:  "A",
		25: "Z",
		26: "AA",
		27: "AB",
		28: "AC",
	}
	for index, label := range want {
		if labels[index] != label {
			t.Fatalf("labels[%d] = %q, want %q", index, labels[index], label)
		}
	}
}
