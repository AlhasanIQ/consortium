package workflow

// CandidateLabel returns stable spreadsheet-style labels for candidate-facing
// prompts: A..Z, AA..AZ, BA...
func CandidateLabel(index int) string {
	if index < 0 {
		return ""
	}
	n := index + 1
	label := ""
	for n > 0 {
		n--
		label = string(rune('A'+(n%26))) + label
		n /= 26
	}
	return label
}

func CandidateLabels(count int) []string {
	if count <= 0 {
		return nil
	}
	labels := make([]string, count)
	for i := range count {
		labels[i] = CandidateLabel(i)
	}
	return labels
}
