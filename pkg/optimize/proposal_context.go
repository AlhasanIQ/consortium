package optimize

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
)

// DSPy-inspired tips for instruction generation diversity.
// Randomly selecting a tip per candidate proposal matches DSPy's GroundedProposer
// approach to producing qualitatively different instruction styles.
var proposalTips = []string{
	"", // no tip (natural style)
	"Don't be afraid to be creative when crafting the new instruction!",
	"Keep the instruction clear and concise.",
	"Make sure your instruction is very informative and descriptive.",
	"The instruction should include a high-stakes scenario that motivates the LM to solve the task carefully.",
	"Include a persona that is relevant to the task in the instruction (e.g., \"You are a ...\").",
}

// randomProposalTip selects a random tip for instruction diversity.
func randomProposalTip(rng *rand.Rand) string {
	if rng == nil {
		return ""
	}
	return proposalTips[rng.Intn(len(proposalTips))]
}

// ProposalContext carries enriched context for instruction proposal prompts,
// inspired by DSPy's GroundedProposer which provides dataset summary,
// program/module descriptions, success examples, and scored instruction history.
type ProposalContext struct {
	// DatasetSummary is a brief description of the benchmark dataset's domain,
	// patterns, and difficulty distribution.
	DatasetSummary string
	// SuccessExamples are correctly-answered benchmark items shown alongside
	// failures to ground the proposer in what "good" looks like.
	SuccessExamples []SuccessExample
	// WorkflowDescription describes the workflow structure and the role of the
	// specific node being optimized.
	WorkflowDescription string
	// Tip is a randomly-selected diversity hint for the instruction proposal.
	Tip string
	// ImprovingEntries are learning log entries where mutations improved fitness,
	// shown alongside non-improving entries to provide positive signal.
	ImprovingEntries []LearningEntry
}

// SuccessExample represents a correctly-answered benchmark item used as
// positive grounding context in proposal prompts.
type SuccessExample struct {
	ItemID        string `json:"item_id"`
	Question      string `json:"question"`
	CorrectAnswer string `json:"correct_answer"`
	Predicted     string `json:"predicted"`
}

// formatProposalContextSection renders the enriched context sections for
// inclusion in proposal prompts.
func formatProposalContextSection(pc *ProposalContext) string {
	if pc == nil {
		return ""
	}
	var b strings.Builder

	if pc.DatasetSummary != "" {
		b.WriteString("## Dataset Summary\n")
		b.WriteString(pc.DatasetSummary)
		b.WriteString("\n\n")
	}

	if pc.WorkflowDescription != "" {
		b.WriteString("## Workflow Context\n")
		b.WriteString(pc.WorkflowDescription)
		b.WriteString("\n\n")
	}

	if len(pc.SuccessExamples) > 0 {
		fmt.Fprintf(&b, "## Successful Examples (%d samples)\n", len(pc.SuccessExamples))
		b.WriteString("These items were answered correctly — they show what good performance looks like:\n")
		for _, ex := range pc.SuccessExamples {
			fmt.Fprintf(&b, "- %s | answer=%q\n", ex.ItemID, ex.CorrectAnswer)
			if q := trimForPrompt(ex.Question, 200); q != "" {
				b.WriteString("  Question: " + q + "\n")
			}
		}
		b.WriteString("\n")
	}

	if len(pc.ImprovingEntries) > 0 {
		b.WriteString("## Previous Improving Mutations\n")
		b.WriteString("These mutations improved fitness — build on what worked:\n")
		for _, entry := range pc.ImprovingEntries {
			fmt.Fprintf(&b, "- Gen %d [%s] delta=+%.4f desc=%s\n",
				entry.Generation, entry.MutationType, entry.FitnessDelta,
				trimForPrompt(entry.Description, 160))
		}
		b.WriteString("\n")
	}

	if pc.Tip != "" {
		b.WriteString("## Tip\n")
		b.WriteString(pc.Tip)
		b.WriteString("\n\n")
	}

	return b.String()
}

// selectImprovingLearningEntries extracts learning log entries where mutations
// actually improved fitness, providing positive signal alongside the existing
// negative (non-improving) guidance.
func selectImprovingLearningEntries(learning []LearningEntry, limit int) []LearningEntry {
	if len(learning) == 0 || limit <= 0 {
		return nil
	}
	result := make([]LearningEntry, 0, limit)
	for i := len(learning) - 1; i >= 0; i-- {
		entry := learning[i]
		if strings.TrimSpace(entry.Outcome) == "improvement" && entry.FitnessDelta > 0 {
			if strings.Contains(strings.ToLower(strings.TrimSpace(entry.MutationType)), "prompt") {
				result = append(result, entry)
				if len(result) >= limit {
					break
				}
			}
		}
	}
	return result
}

// buildWorkflowDescription extracts a brief description of the workflow structure
// and the role of the specific node being optimized from the workflow JSON.
func buildWorkflowDescription(workflowJSON json.RawMessage, componentKey string) string {
	if len(workflowJSON) == 0 {
		return ""
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &raw); err != nil {
		return ""
	}

	var b strings.Builder
	if id, ok := raw["id"].(string); ok && id != "" {
		fmt.Fprintf(&b, "Workflow: %s", id)
		if name, ok := raw["name"].(string); ok && name != "" {
			fmt.Fprintf(&b, " (%s)", name)
		}
		b.WriteString("\n")
	}

	nodes, ok := raw["nodes"].([]interface{})
	if !ok || len(nodes) == 0 {
		return b.String()
	}

	fmt.Fprintf(&b, "Nodes (%d total):\n", len(nodes))
	for _, n := range nodes {
		node, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		nodeID, _ := node["id"].(string)
		data, _ := node["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		nodeType, _ := data["type"].(string)
		label, _ := data["label"].(string)
		config, _ := data["config"].(map[string]interface{})

		marker := "  "
		if componentKey != "" && strings.Contains(componentKey, nodeID) {
			marker = "* " // mark the node being optimized
		}
		desc := fmt.Sprintf("%s- %s [%s]", marker, nodeID, coalesceString(nodeType, "unknown"))
		if label != "" {
			desc += fmt.Sprintf(" \"%s\"", label)
		}
		if config != nil {
			if model, ok := config["model"].(string); ok && model != "" {
				desc += fmt.Sprintf(" model=%s", model)
			}
		}
		b.WriteString(desc + "\n")
	}
	if componentKey != "" {
		fmt.Fprintf(&b, "(* = node being optimized: %s)\n", componentKey)
	}
	return b.String()
}

// buildDatasetSummary generates a brief description of the benchmark dataset
// by analyzing available failure and success examples. Unlike DSPy's multi-pass
// LLM summarization, this uses a deterministic heuristic approach since we
// don't want to spend LLM budget on summarization during optimization.
func buildDatasetSummary(failures []FailureCase, successes []SuccessExample) string {
	totalSamples := len(failures) + len(successes)
	if totalSamples == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Dataset sample: %d items observed (%d failures, %d successes).\n",
		totalSamples, len(failures), len(successes))

	// Compute category distribution
	categories := map[string]int{}
	for _, fc := range failures {
		cat := strings.TrimSpace(fc.Category)
		if cat == "" {
			cat = "uncategorized"
		}
		categories[cat]++
	}
	if len(categories) > 0 {
		b.WriteString("Failure categories: ")
		sorted := sortedCategoryCounts(categories)
		b.WriteString(strings.Join(sorted, ", "))
		b.WriteString(".\n")
	}

	// Estimate average question length for domain hints
	totalLen := 0
	sampleCount := 0
	for _, fc := range failures {
		if q := strings.TrimSpace(fc.Question); q != "" {
			totalLen += len(q)
			sampleCount++
		}
	}
	for _, ex := range successes {
		if q := strings.TrimSpace(ex.Question); q != "" {
			totalLen += len(q)
			sampleCount++
		}
	}
	if sampleCount > 0 {
		avgLen := totalLen / sampleCount
		complexity := "short"
		if avgLen > 500 {
			complexity = "long"
		} else if avgLen > 200 {
			complexity = "medium-length"
		}
		fmt.Fprintf(&b, "Questions are typically %s (avg ~%d chars).\n", complexity, avgLen)
	}

	return strings.TrimSpace(b.String())
}
