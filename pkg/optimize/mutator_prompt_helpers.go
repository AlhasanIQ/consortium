package optimize

import (
	"fmt"
	"strings"
)

// formatLearningLogSection renders a list of pre-selected learning entries as
// prompt text. Each entry is formatted as a single bullet line. The limit
// parameter controls how many entries to select from the raw learning log;
// pass the full log and the function handles selection internally.
func formatLearningLogSection(learning []LearningEntry, limit int) string {
	entries := selectPromptLearningEntries(learning, limit)
	if len(entries) == 0 {
		return "- none\n"
	}
	var b strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&b, "- Gen %d [%s]: %s -> %s (delta %.4f)\n",
			entry.Generation, entry.MutationType, entry.Description, entry.Outcome, entry.FitnessDelta)
	}
	return b.String()
}

// formatLearningLogSectionReflective renders the reflective variant used in
// GEPA single-step mutation prompts, with pipe-delimited outcome formatting.
func formatLearningLogSectionReflective(learning []LearningEntry, limit int) string {
	entries := selectPromptLearningEntries(learning, limit)
	if len(entries) == 0 {
		return "- none\n"
	}
	var b strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&b, "- Gen %d [%s] %s | outcome=%s | delta=%.4f\n",
			entry.Generation, entry.MutationType, entry.Description, entry.Outcome, entry.FitnessDelta)
	}
	return b.String()
}

// formatLearningLogSectionCompact renders a compact variant of learning log
// entries used in candidate-proposal prompts. Description is trimmed to
// descLimit characters.
func formatLearningLogSectionCompact(learning []LearningEntry, limit int, descLimit int) string {
	entries := selectPromptLearningEntries(learning, limit)
	if len(entries) == 0 {
		return "- none\n"
	}
	var b strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&b, "- Gen %d [%s] outcome=%s delta=%.4f desc=%s\n",
			entry.Generation, entry.MutationType, entry.Outcome, entry.FitnessDelta, trimForPrompt(entry.Description, descLimit))
	}
	return b.String()
}

// formatObjectivesSection renders the optimization objectives and constraints
// block used in mutation prompts. The header parameter controls the markdown
// heading text (e.g., "## Objectives", "## Optimization Objectives").
func formatObjectivesSection(header string, spec *OptimizeSpec) string {
	primaryWeight := objectiveWeight(spec, "accuracy")
	costWeight := objectiveWeight(spec, "cost_per_item")
	var b strings.Builder
	b.WriteString(header + "\n")
	fmt.Fprintf(&b, "- maximize adjusted_accuracy (weight %.2f)\n", primaryWeight)
	fmt.Fprintf(&b, "- minimize cost_per_item (weight %.2f)\n", costWeight)
	if spec != nil && len(spec.Constraints) > 0 {
		b.WriteString("Constraints:\n")
		for _, c := range spec.Constraints {
			fmt.Fprintf(&b, "- %s %s %.4f\n", c.Metric, c.Op, c.Value)
		}
	}
	return b.String()
}

// formatFailureDiagnoses renders the failure category frequency summary from
// pre-computed category counts. Returns empty string if no categories.
func formatFailureDiagnoses(categoryCounts map[string]int, header string) string {
	if len(categoryCounts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(header + "\n")
	for _, line := range sortedCategoryCounts(categoryCounts) {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// countFailureCategories tallies failure categories from a slice of FailureCase,
// normalizing empty categories to "uncategorized".
func countFailureCategories(failures []FailureCase) map[string]int {
	counts := map[string]int{}
	for _, fc := range failures {
		category := strings.TrimSpace(fc.Category)
		if category == "" {
			category = "uncategorized"
		}
		counts[category]++
	}
	return counts
}
