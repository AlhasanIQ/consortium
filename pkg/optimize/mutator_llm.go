package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

type LLMMutator struct {
	Model string
	Rand  *rand.Rand
}

func (m *LLMMutator) Mutate(ctx context.Context, req *MutationRequest) ([]*MutationResult, error) {
	rng := m.Rand
	return mutatePromptWithStrategy(ctx, m.Model, rng, req, promptMutationStrategy{
		MutationType:  "llm_prompt",
		DefaultReason: "LLM revised prompt based on sampled failures",
		BuildPrompt:   buildLLMMutationPrompt,
	})
}

func buildLLMMutationPrompt(currentPrompt string, failures []FailureCase, learning []LearningEntry, spec *OptimizeSpec, pc *ProposalContext) string {
	var b strings.Builder
	b.WriteString("You are a prompt optimization agent for a multi-model AI reasoning workflow.\n\n")

	if section := formatProposalContextSection(pc); section != "" {
		b.WriteString(section)
	}

	b.WriteString("## Current System Prompt\n")
	b.WriteString(currentPrompt)
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "## Benchmark Failures (%d samples)\n", len(failures))
	categoryCounts := countFailureCategories(failures)
	for _, fc := range failures {
		category := coalesceString(strings.TrimSpace(fc.Category), "uncategorized")
		fmt.Fprintf(&b, "- Item %s: expected %q, predicted %q\n", fc.ItemID, fc.CorrectAnswer, fc.Predicted)
		if child := strings.TrimSpace(fc.ChildPredicted); child != "" {
			fmt.Fprintf(&b, "  Child predicted: %q\n", child)
		}
		if question := trimForPrompt(fc.Question, 500); question != "" {
			fmt.Fprintf(&b, "  Question excerpt: %s\n", question)
		}
		if fc.FailureReason != "" || category != "" {
			fmt.Fprintf(&b, "  Failure: %s | Category: %s\n", fc.FailureReason, category)
		}
		if votes := summarizeAgentAnswers(fc.AgentAnswers, 6); votes != "" {
			fmt.Fprintf(&b, "  Agent votes (model:answer:mark): %s\n", votes)
		}
		if diagnosis := strings.TrimSpace(fc.Diagnosis); diagnosis != "" {
			fmt.Fprintf(&b, "  Diagnosis hint: %s\n", trimForPrompt(diagnosis, 280))
		}
		if fc.Flagged {
			if reason := strings.TrimSpace(fc.FlagReason); reason != "" {
				fmt.Fprintf(&b, "  Flagged dataset item: %s\n", trimForPrompt(reason, 180))
			} else {
				b.WriteString("  Flagged dataset item\n")
			}
		}
		if text := strings.TrimSpace(fc.RawOutput); text != "" {
			text = trimForPrompt(text, 500)
			fmt.Fprintf(&b, "  Raw output excerpt: %s\n", text)
		}
		if traces := formatNodeTraces(fc.NodeTraces); traces != "" {
			b.WriteString(traces)
		}
	}
	b.WriteString("\n")
	if s := formatFailureDiagnoses(categoryCounts, "Top failure categories:"); s != "" {
		b.WriteString(s)
		b.WriteString("\n")
	}

	b.WriteString("## Previous Mutations That Failed\n")
	b.WriteString(formatLearningLogSection(learning, 20))
	b.WriteString("\n")

	primaryWeight := objectiveWeight(spec, "accuracy")
	costWeight := objectiveWeight(spec, "cost_per_item")
	b.WriteString("## Optimization Objectives\n")
	fmt.Fprintf(&b, "Primary: maximize accuracy (weight %.2f)\n", primaryWeight)
	fmt.Fprintf(&b, "Secondary: minimize cost_per_item (weight %.2f)\n", costWeight)
	if spec != nil && len(spec.Constraints) > 0 {
		b.WriteString("Constraints:\n")
		for _, c := range spec.Constraints {
			fmt.Fprintf(&b, "- %s %s %.4f\n", c.Metric, c.Op, c.Value)
		}
	}
	b.WriteString("\n")

	b.WriteString("## Instructions\n")
	b.WriteString("Analyze failure patterns and revise the prompt.\n")
	b.WriteString("- Focus on recurring failure patterns and categories\n")
	b.WriteString("- Prioritize one or two high-leverage prompt changes instead of broad rewrites\n")
	b.WriteString("- Do NOT add benchmark-specific content (no A/B/C/D instructions)\n")
	b.WriteString("- Keep prompt general-purpose for reasoning tasks\n")
	b.WriteString("- Explain your reasoning\n\n")
	b.WriteString("Return ONLY valid JSON:\n")
	b.WriteString("{\n")
	b.WriteString("  \"revised_prompt\": \"...\",\n")
	b.WriteString("  \"reasoning\": \"...\",\n")
	b.WriteString("  \"changes_summary\": \"...\"\n")
	b.WriteString("}\n")
	return b.String()
}

// formatNodeTraces renders per-node execution trace lines for a failure case.
func formatNodeTraces(traces []FailureNodeTrace) string {
	if len(traces) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("  Execution trace:\n")
	for _, t := range traces {
		fmt.Fprintf(&b, "    [%s]", t.NodeID)
		if t.Model != "" {
			fmt.Fprintf(&b, " model=%s", t.Model)
		}
		fmt.Fprintf(&b, ": %s\n", trimForPrompt(t.Output, 200))
	}
	return b.String()
}

func trimForPrompt(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func sortedCategoryCounts(categoryCounts map[string]int) []string {
	if len(categoryCounts) == 0 {
		return nil
	}
	type pair struct {
		Category string
		Count    int
	}
	pairs := make([]pair, 0, len(categoryCounts))
	for category, count := range categoryCounts {
		pairs = append(pairs, pair{Category: category, Count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count == pairs[j].Count {
			return pairs[i].Category < pairs[j].Category
		}
		return pairs[i].Count > pairs[j].Count
	})
	lines := make([]string, 0, len(pairs))
	for _, p := range pairs {
		lines = append(lines, fmt.Sprintf("%s (%d)", p.Category, p.Count))
	}
	return lines
}

func selectPromptLearningEntries(learning []LearningEntry, limit int) []LearningEntry {
	if len(learning) == 0 || limit <= 0 {
		return nil
	}
	promptOnly := make([]LearningEntry, 0, len(learning))
	allNonImproving := make([]LearningEntry, 0, len(learning))
	for i := len(learning) - 1; i >= 0; i-- {
		entry := learning[i]
		if strings.TrimSpace(entry.Outcome) == "improvement" {
			continue
		}
		allNonImproving = append(allNonImproving, entry)
		if strings.Contains(strings.ToLower(strings.TrimSpace(entry.MutationType)), "prompt") {
			promptOnly = append(promptOnly, entry)
		}
	}
	selected := promptOnly
	if len(selected) == 0 {
		selected = allNonImproving
	}
	if len(selected) > limit {
		selected = selected[:limit]
	}
	return selected
}

func decodePromptValue(encoded string) (string, error) {
	if strings.TrimSpace(encoded) == "" {
		return "", ErrMissingPromptValue
	}
	var val string
	if err := json.Unmarshal([]byte(encoded), &val); err != nil {
		return "", err
	}
	return val, nil
}

func objectiveWeight(spec *OptimizeSpec, metric string) float64 {
	if spec == nil {
		return 1
	}
	for _, objective := range spec.Objectives {
		if objective.Metric == metric || (metric == "accuracy" && objective.Metric == "adjusted_accuracy") {
			return objective.Weight
		}
	}
	return 1
}

func coalesceString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func extractWorkflowID(workflowJSON json.RawMessage) string {
	if len(workflowJSON) == 0 {
		return ""
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &raw); err != nil {
		return ""
	}
	id, _ := raw["id"].(string)
	return id
}
