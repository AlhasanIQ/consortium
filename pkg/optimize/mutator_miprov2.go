package optimize

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
)

// MIPROv2Mutator is a prompt mutator inspired by DSPy's MIPROv2:
// synthesize task-aware instruction improvements from minibatch failures.
type MIPROv2Mutator struct {
	Model string
	Rand  *rand.Rand
}

func (m *MIPROv2Mutator) Mutate(ctx context.Context, req *MutationRequest) ([]*MutationResult, error) {
	count, rng, err := prepareMutation(req, m.Rand)
	if err != nil {
		return nil, err
	}

	groups := collectPromptGroups(req.Spec)
	if len(groups) == 0 {
		return nil, ErrNoPromptParams
	}
	baseValues, err := buildParentParamValues(req.Parent, req.Spec)
	if err != nil {
		return nil, err
	}

	settings := ResolveDSPYRuntimeSettings(req.Spec, MutatorModeMIPROv2, 0)
	numCandidates := max(settings.NumCandidates, max(count, 2))
	demoSets := buildMIPRODemoSets(req.FailureCases, numCandidates, 4, rng)

	candidatesByGroup := make(map[string][]ClaudePromptCandidate, len(groups))
	artifactsByGroup := make(map[string]MutationArtifact, len(groups))
	for _, group := range groups {
		currentPrompt, err := decodePromptValue(baseValues[group.Declarations[0].Path])
		if err != nil {
			return nil, fmt.Errorf("decode current prompt %s: %w", group.Declarations[0].Path, err)
		}
		proposalPrompt := buildMIPROv2CandidateProposalPrompt(
			group.Key,
			currentPrompt,
			req.FailureCases,
			req.LearningLog,
			req.Spec,
			demoSets,
			numCandidates,
			req.ProposalContext,
		)
		response, err := InvokeClaudePromptCandidates(ctx, m.Model, proposalPrompt, numCandidates)
		if err != nil {
			// Fallback to single-step prompt rewrite if candidate proposal fails.
			single, singleErr := mutatePromptWithStrategy(ctx, m.Model, rng, req, promptMutationStrategy{
				MutationType:  "miprov2_prompt",
				DefaultReason: "MIPROv2-style instruction rewrite from sampled failures",
				BuildPrompt:   buildMIPROv2MutationPrompt,
			})
			if singleErr != nil {
				return nil, singleErr
			}
			return single, nil
		}
		candidates := dedupePromptCandidates(response.Candidates, currentPrompt, numCandidates)
		if len(candidates) == 0 {
			candidates = []ClaudePromptCandidate{{RevisedPrompt: currentPrompt}}
		}
		candidatesByGroup[group.Key] = candidates
		artifactsByGroup[group.Key] = MutationArtifact{
			InputPromptHash:  sha256Hex(proposalPrompt),
			InputPrompt:      proposalPrompt,
			RawOutputHash:    sha256Hex(response.RawOutput),
			RawOutput:        response.RawOutput,
			ClaudeModel:      coalesceString(response.Model, strings.TrimSpace(m.Model)),
			ClaudeCLIVersion: response.CLIVersion,
		}
	}

	results := make([]*MutationResult, 0, count)
	for trial := 0; trial < count; trial++ {
		childValues := copyStringMap(baseValues)
		changes := make([]ParamChange, 0, len(groups))
		selected := make([]string, 0, len(groups))
		artifacts := make([]MutationArtifact, 0, len(groups))
		for _, group := range groups {
			candidates := candidatesByGroup[group.Key]
			idx := chooseMIPROCandidateIndex(trial, len(candidates), rng)
			candidate := candidates[idx]
			encoded, err := encodeJSONValue(candidate.RevisedPrompt)
			if err != nil {
				return nil, fmt.Errorf("encode revised prompt: %w", err)
			}
			for _, declaration := range group.Declarations {
				oldEncoded := childValues[declaration.Path]
				childValues[declaration.Path] = encoded
				changes = append(changes, ParamChange{
					Path:     declaration.Path,
					OldValue: oldEncoded,
					NewValue: encoded,
					Reason:   coalesceString(candidate.ChangesSummary, "miprov2_prompt"),
				})
			}
			selected = append(selected, fmt.Sprintf("%s#%d", group.Key, idx))
			if artifact, ok := artifactsByGroup[group.Key]; ok {
				artifacts = append(artifacts, artifact)
			}
		}
		reason := fmt.Sprintf("MIPROv2 trial %d/%d using candidate combination %s", trial+1, count, strings.Join(selected, ", "))
		organism := newChildOrganism(req.Parent, req.Generation, "miprov2_prompt", reason, childValues)
		results = append(results, &MutationResult{
			Organism:  organism,
			Changes:   changes,
			Artifacts: artifacts,
			Reasoning: reason,
		})
	}
	return results, nil
}

func buildMIPROv2MutationPrompt(currentPrompt string, failures []FailureCase, learning []LearningEntry, spec *OptimizeSpec, pc *ProposalContext) string {
	var b strings.Builder
	b.WriteString("You are running a MIPROv2-style prompt optimization step.\n\n")

	if section := formatProposalContextSection(pc); section != "" {
		b.WriteString(section)
	}

	b.WriteString("## Current Prompt\n")
	b.WriteString(currentPrompt)
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "## Failure Minibatch (%d samples)\n", len(failures))
	categoryCounts := countFailureCategories(failures)
	for _, fc := range failures {
		fmt.Fprintf(&b, "- Item %s | expected=%q predicted=%q\n", fc.ItemID, fc.CorrectAnswer, fc.Predicted)
		if child := strings.TrimSpace(fc.ChildPredicted); child != "" {
			fmt.Fprintf(&b, "  Child predicted: %q\n", child)
		}
		if question := trimForPrompt(fc.Question, 400); question != "" {
			fmt.Fprintf(&b, "  Question excerpt: %s\n", question)
		}
		if reason := strings.TrimSpace(fc.FailureReason); reason != "" {
			fmt.Fprintf(&b, "  Failure reason: %s\n", reason)
		}
		if votes := summarizeAgentAnswers(fc.AgentAnswers, 6); votes != "" {
			fmt.Fprintf(&b, "  Agent votes (model:answer:mark): %s\n", votes)
		}
		if diagnosis := strings.TrimSpace(fc.Diagnosis); diagnosis != "" {
			fmt.Fprintf(&b, "  Diagnosis hint: %s\n", trimForPrompt(diagnosis, 240))
		}
		if traces := formatNodeTraces(fc.NodeTraces); traces != "" {
			b.WriteString(traces)
		}
	}
	if s := formatFailureDiagnoses(categoryCounts, "\nFailure-type frequency (for this minibatch):"); s != "" {
		b.WriteString(s)
	}
	b.WriteString("\n")

	b.WriteString("## Previous Prompt Mutation Outcomes\n")
	b.WriteString(formatLearningLogSection(learning, 12))
	b.WriteString("\n")

	b.WriteString(formatObjectivesSection("## Objective Weights", spec))
	b.WriteString("\n")

	b.WriteString("## Instructions\n")
	b.WriteString("Perform a MIPROv2-style instruction optimization step:\n")
	b.WriteString("1) Infer a concise task summary from failures.\n")
	b.WriteString("2) Formulate 2-3 candidate instruction edits in reasoning.\n")
	b.WriteString("3) Select the highest-utility candidate and return a single revised prompt.\n")
	b.WriteString("- Favor compact, high-leverage edits over broad rewrites.\n")
	b.WriteString("- Keep the prompt benchmark-agnostic (no option-letter tricks).\n")
	b.WriteString("- Preserve general reasoning quality and output format compliance.\n\n")
	b.WriteString("Return ONLY valid JSON:\n")
	b.WriteString("{\n")
	b.WriteString("  \"revised_prompt\": \"...\",\n")
	b.WriteString("  \"reasoning\": \"...\",\n")
	b.WriteString("  \"changes_summary\": \"...\"\n")
	b.WriteString("}\n")
	return b.String()
}

func buildMIPROv2CandidateProposalPrompt(
	componentKey string,
	currentPrompt string,
	failures []FailureCase,
	learning []LearningEntry,
	spec *OptimizeSpec,
	demoSets [][]FailureCase,
	numCandidates int,
	pc *ProposalContext,
) string {
	var b strings.Builder
	b.WriteString("You are running DSPy MIPROv2-style optimization.\n")
	b.WriteString("Goal: propose multiple high-quality instruction candidates for one component.\n\n")

	// Render enriched proposal context (dataset summary, workflow, successes, tips, improving entries)
	if section := formatProposalContextSection(pc); section != "" {
		b.WriteString(section)
	}

	fmt.Fprintf(&b, "## Component\n- key: %s\n\n", componentKey)
	b.WriteString("## Current Prompt\n")
	b.WriteString(currentPrompt)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "## Failure Minibatch (%d)\n", len(failures))
	for _, fc := range failures {
		fmt.Fprintf(&b, "- %s | expected=%q predicted=%q | category=%s\n", fc.ItemID, fc.CorrectAnswer, fc.Predicted, coalesceString(strings.TrimSpace(fc.Category), "uncategorized"))
		if q := trimForPrompt(fc.Question, 260); q != "" {
			b.WriteString("  Question: " + q + "\n")
		}
		if d := trimForPrompt(fc.Diagnosis, 200); d != "" {
			b.WriteString("  Diagnosis: " + d + "\n")
		}
		if traces := formatNodeTraces(fc.NodeTraces); traces != "" {
			b.WriteString(traces)
		}
	}
	b.WriteString("\n## Demo Candidate Sets\n")
	if len(demoSets) == 0 {
		b.WriteString("- none\n")
	} else {
		for i, set := range demoSets {
			fmt.Fprintf(&b, "- Set %d:\n", i+1)
			for _, item := range set {
				q := trimForPrompt(item.Question, 160)
				fmt.Fprintf(&b, "  - Q: %s\n    Gold: %q\n", q, item.CorrectAnswer)
			}
		}
	}
	b.WriteString("\n## Recent Prompt Mutation Outcomes\n")
	b.WriteString(formatLearningLogSectionCompact(learning, 10, 120))
	b.WriteString("\n")
	b.WriteString(formatObjectivesSection("## Objectives", spec))
	b.WriteString("\n## Instructions\n")
	b.WriteString("Produce diverse but benchmark-agnostic instruction candidates.\n")
	b.WriteString("- Keep candidates concise and operationally robust.\n")
	b.WriteString("- No benchmark-specific answer hacks.\n")
	fmt.Fprintf(&b, "- Return exactly %d candidates.\n\n", numCandidates)
	b.WriteString("Return ONLY valid JSON:\n")
	b.WriteString("{\n")
	b.WriteString("  \"candidates\": [\n")
	b.WriteString("    {\"revised_prompt\": \"...\", \"reasoning\": \"...\", \"changes_summary\": \"...\", \"score\": 0.0}\n")
	b.WriteString("  ]\n")
	b.WriteString("}\n")
	return b.String()
}

func buildMIPRODemoSets(failures []FailureCase, numSets int, perSet int, rng *rand.Rand) [][]FailureCase {
	if len(failures) == 0 || numSets <= 0 || perSet <= 0 {
		return nil
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}
	sets := make([][]FailureCase, 0, numSets)
	for i := 0; i < numSets; i++ {
		size := min(perSet, len(failures))
		perm := rng.Perm(len(failures))
		items := make([]FailureCase, 0, size)
		for j := 0; j < size; j++ {
			items = append(items, failures[perm[j]])
		}
		sets = append(sets, items)
	}
	return sets
}

func dedupePromptCandidates(candidates []ClaudePromptCandidate, currentPrompt string, limit int) []ClaudePromptCandidate {
	out := make([]ClaudePromptCandidate, 0, len(candidates)+1)
	seen := map[string]struct{}{}
	add := func(item ClaudePromptCandidate) {
		prompt := strings.TrimSpace(item.RevisedPrompt)
		if prompt == "" {
			return
		}
		if _, ok := seen[prompt]; ok {
			return
		}
		item.RevisedPrompt = prompt
		out = append(out, item)
		seen[prompt] = struct{}{}
	}
	add(ClaudePromptCandidate{
		RevisedPrompt:  currentPrompt,
		Reasoning:      "baseline",
		ChangesSummary: "baseline",
		Score:          0,
	})
	for _, item := range candidates {
		add(item)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func chooseMIPROCandidateIndex(trial int, total int, rng *rand.Rand) int {
	_ = trial
	if total <= 1 {
		return 0
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}
	return rng.Intn(total)
}
