package optimize

import (
	"fmt"
	"math/rand"
	"strings"
)

// DemoExample represents a single few-shot demonstration example bootstrapped
// from successful benchmark items, analogous to DSPy's BootstrapFewShot demos.
type DemoExample struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// bootstrapDemoPools creates multiple demo set variants from successful benchmark
// items, analogous to DSPy's BootstrapFewShot which creates few-shot examples
// from labeled data where the model got the answer correct.
func bootstrapDemoPools(successes []SuccessExample, numSets int, maxPerSet int, rng *rand.Rand) [][]DemoExample {
	if len(successes) == 0 || numSets <= 0 || maxPerSet <= 0 {
		return nil
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec
	}
	// Filter to examples with both question and answer.
	eligible := make([]SuccessExample, 0, len(successes))
	for _, ex := range successes {
		if strings.TrimSpace(ex.Question) != "" && strings.TrimSpace(ex.CorrectAnswer) != "" {
			eligible = append(eligible, ex)
		}
	}
	if len(eligible) == 0 {
		return nil
	}

	sets := make([][]DemoExample, 0, numSets)
	for i := 0; i < numSets; i++ {
		size := min(maxPerSet, len(eligible))
		perm := rng.Perm(len(eligible))
		demos := make([]DemoExample, 0, size)
		for j := 0; j < size; j++ {
			ex := eligible[perm[j]]
			demos = append(demos, DemoExample{
				Question: strings.TrimSpace(ex.Question),
				Answer:   strings.TrimSpace(ex.CorrectAnswer),
			})
		}
		sets = append(sets, demos)
	}
	return sets
}

// formatDemoSection renders a few-shot demo set as a prompt section that gets
// prepended or appended to the base instruction.
func formatDemoSection(demos []DemoExample) string {
	if len(demos) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n## Few-Shot Examples (%d)\n", len(demos))
	b.WriteString("The following are examples of correct answers:\n\n")
	for i, demo := range demos {
		q := trimForPrompt(demo.Question, 400)
		fmt.Fprintf(&b, "Example %d:\n", i+1)
		fmt.Fprintf(&b, "Q: %s\n", q)
		fmt.Fprintf(&b, "A: %s\n\n", demo.Answer)
	}
	return b.String()
}

// augmentCandidatesWithDemos creates additional prompt candidates by appending
// bootstrapped few-shot demo sets to existing instruction candidates. This
// implements the joint instruction+demo optimization from DSPy's MIPROv2 where
// both dimensions are searched via TPE.
func augmentCandidatesWithDemos(
	candidates []ClaudePromptCandidate,
	demoPools [][]DemoExample,
	limit int,
) []ClaudePromptCandidate {
	if len(candidates) == 0 || len(demoPools) == 0 {
		return candidates
	}
	augmented := make([]ClaudePromptCandidate, 0, len(candidates)+len(demoPools))
	// Keep all original candidates (instruction-only).
	augmented = append(augmented, candidates...)
	// Add demo-augmented variants for the top instruction candidates.
	for _, demos := range demoPools {
		demoSection := formatDemoSection(demos)
		if demoSection == "" {
			continue
		}
		// Augment the first non-baseline candidate (or baseline if only one).
		baseIdx := 0
		if len(candidates) > 1 {
			baseIdx = 1 // skip baseline at index 0
		}
		base := candidates[baseIdx]
		augmented = append(augmented, ClaudePromptCandidate{
			RevisedPrompt:  base.RevisedPrompt + demoSection,
			Reasoning:      fmt.Sprintf("demo-augmented: %s", base.Reasoning),
			ChangesSummary: fmt.Sprintf("added %d few-shot demos to instruction", len(demos)),
		})
		if limit > 0 && len(augmented) >= limit {
			break
		}
	}
	if limit > 0 && len(augmented) > limit {
		augmented = augmented[:limit]
	}
	return augmented
}
