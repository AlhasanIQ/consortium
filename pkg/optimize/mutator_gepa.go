package optimize

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
)

// GEPAMutator is a reflective prompt mutator inspired by GEPA:
// reflect on failure trajectories and mutate targeted prompt components.
type GEPAMutator struct {
	Model string
	Rand  *rand.Rand
}

func (m *GEPAMutator) Mutate(ctx context.Context, req *MutationRequest) ([]*MutationResult, error) {
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

	settings := ResolveDSPYRuntimeSettings(req.Spec, MutatorModeGEPA, 0)
	numCandidates := max(settings.NumCandidates, 3)
	reflectionFailures := req.FailureCases
	if settings.ReflectionMinibatchSize > 0 && len(reflectionFailures) > settings.ReflectionMinibatchSize {
		reflectionFailures = sampleFailureCasesRandom(reflectionFailures, settings.ReflectionMinibatchSize, rng)
	}
	selectedComponents := selectGEPAComponents(groups, settings.ComponentSelector, req.Generation, req.LearningLog, rng)
	if len(selectedComponents) == 0 {
		return nil, ErrNoComponentsSelected
	}
	if settings.ComponentSelector != "all" && len(selectedComponents) > 1 {
		selectedComponents = selectedComponents[:1]
	}

	candidatesByGroup := make(map[string][]ClaudePromptCandidate, len(selectedComponents))
	artifactsByGroup := make(map[string]MutationArtifact, len(selectedComponents))
	for _, idx := range selectedComponents {
		group := groups[idx]
		currentPrompt, err := decodePromptValue(baseValues[group.Declarations[0].Path])
		if err != nil {
			return nil, fmt.Errorf("decode current prompt %s: %w", group.Declarations[0].Path, err)
		}
		proposalPrompt := buildGEPACandidateProposalPrompt(
			group.Key,
			currentPrompt,
			reflectionFailures,
			req.LearningLog,
			req.Spec,
			settings,
			numCandidates,
			req.ProposalContext,
		)
		response, err := InvokeClaudePromptCandidates(ctx, m.Model, proposalPrompt, numCandidates)
		if err != nil {
			// Fallback to single-step reflective prompt mutation if candidate proposal fails.
			single, singleErr := mutatePromptWithStrategy(ctx, m.Model, rng, req, promptMutationStrategy{
				MutationType:  "gepa_reflective_prompt",
				DefaultReason: "GEPA-style reflective prompt mutation from sampled failures",
				BuildPrompt:   buildGEPAMutationPrompt,
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
	mergeBudget := settings.MaxMergeInvocations
	for trial := 0; trial < count; trial++ {
		childValues := copyStringMap(baseValues)
		changes := make([]ParamChange, 0, len(selectedComponents))
		artifacts := make([]MutationArtifact, 0, len(selectedComponents))
		selected := make([]string, 0, len(selectedComponents))
		for _, idx := range selectedComponents {
			group := groups[idx]
			candidates := candidatesByGroup[group.Key]
			candidateIdx := chooseGEPACandidateIndex(settings.CandidateSelectionStrategy, trial, candidates, rng)
			candidate := candidates[candidateIdx]
			if settings.UseMerge && mergeBudget > 0 && len(candidates) > 2 && trial > 0 {
				a := candidates[candidateIdx]
				b := candidates[(candidateIdx+1)%len(candidates)]
				merged, mergeErr := mergeGEPAReflectiveCandidates(ctx, m.Model, a, b, group.Key, req.FailureCases)
				if mergeErr == nil && strings.TrimSpace(merged.RevisedPrompt) != "" {
					candidate = merged
					mergeBudget--
				}
			}
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
					Reason:   coalesceString(candidate.ChangesSummary, "gepa_reflective_prompt"),
				})
			}
			selected = append(selected, fmt.Sprintf("%s#%d", group.Key, candidateIdx))
			if artifact, ok := artifactsByGroup[group.Key]; ok {
				artifacts = append(artifacts, artifact)
			}
		}
		reason := fmt.Sprintf("GEPA reflective trial %d/%d using components %s", trial+1, count, strings.Join(selected, ", "))
		organism := newChildOrganism(req.Parent, req.Generation, "gepa_reflective_prompt", reason, childValues)
		results = append(results, &MutationResult{
			Organism:  organism,
			Changes:   changes,
			Artifacts: artifacts,
			Reasoning: reason,
		})
	}
	return results, nil
}

func buildGEPAMutationPrompt(currentPrompt string, failures []FailureCase, learning []LearningEntry, spec *OptimizeSpec, pc *ProposalContext) string {
	var b strings.Builder
	b.WriteString("You are running a GEPA-style reflective prompt evolution step.\n\n")

	if section := formatProposalContextSection(pc); section != "" {
		b.WriteString(section)
	}

	b.WriteString("## Current Prompt\n")
	b.WriteString(currentPrompt)
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("## Reflection Batch (%d failures)\n", len(failures)))
	categoryCounts := countFailureCategories(failures)
	for _, fc := range failures {
		category := coalesceString(strings.TrimSpace(fc.Category), "uncategorized")
		b.WriteString(fmt.Sprintf("- %s | expected=%q predicted=%q | category=%s\n", fc.ItemID, fc.CorrectAnswer, fc.Predicted, category))
		if child := strings.TrimSpace(fc.ChildPredicted); child != "" {
			b.WriteString(fmt.Sprintf("  Child predicted: %q\n", child))
		}
		if question := trimForPrompt(fc.Question, 360); question != "" {
			b.WriteString(fmt.Sprintf("  Prompted task excerpt: %s\n", question))
		}
		if reason := strings.TrimSpace(fc.FailureReason); reason != "" {
			b.WriteString(fmt.Sprintf("  Failure reason: %s\n", reason))
		}
		if votes := summarizeAgentAnswers(fc.AgentAnswers, 6); votes != "" {
			b.WriteString(fmt.Sprintf("  Agent votes (model:answer:mark): %s\n", votes))
		}
		if diagnosis := strings.TrimSpace(fc.Diagnosis); diagnosis != "" {
			b.WriteString(fmt.Sprintf("  Diagnosis hint: %s\n", trimForPrompt(diagnosis, 240)))
		}
		if traces := formatNodeTraces(fc.NodeTraces); traces != "" {
			b.WriteString(traces)
		}
	}
	if s := formatFailureDiagnoses(categoryCounts, "\nBatch category profile:"); s != "" {
		b.WriteString(s)
	}
	b.WriteString("\n")

	b.WriteString("## Trajectory Feedback (Recent Prompt Mutations)\n")
	b.WriteString(formatLearningLogSectionReflective(learning, 16))
	b.WriteString("\n")

	b.WriteString(formatObjectivesSection("## Objectives", spec))
	b.WriteString("\n")

	b.WriteString("## Instructions\n")
	b.WriteString("Perform reflective evolution:\n")
	b.WriteString("1) Identify one high-impact failure pattern from the batch.\n")
	b.WriteString("2) Reflect on why prior prompt changes failed or regressed.\n")
	b.WriteString("3) Apply a targeted edit to the prompt section most responsible.\n")
	b.WriteString("4) Keep unchanged sections stable unless needed for the chosen fix.\n")
	b.WriteString("- Do not insert benchmark-specific answer hacks.\n")
	b.WriteString("- Keep final prompt concise and operationally robust.\n\n")
	b.WriteString("Return ONLY valid JSON:\n")
	b.WriteString("{\n")
	b.WriteString("  \"revised_prompt\": \"...\",\n")
	b.WriteString("  \"reasoning\": \"...\",\n")
	b.WriteString("  \"changes_summary\": \"...\"\n")
	b.WriteString("}\n")
	return b.String()
}

func buildGEPACandidateProposalPrompt(
	componentKey string,
	currentPrompt string,
	failures []FailureCase,
	learning []LearningEntry,
	spec *OptimizeSpec,
	settings DSPYRuntimeSettings,
	numCandidates int,
	pc *ProposalContext,
) string {
	var b strings.Builder
	b.WriteString("You are running DSPy GEPA-style reflective prompt evolution.\n")

	// Render enriched proposal context (dataset summary, workflow, successes, tips, improving entries)
	if section := formatProposalContextSection(pc); section != "" {
		b.WriteString(section)
	}

	b.WriteString(fmt.Sprintf("Component key: %s\n", componentKey))
	b.WriteString(fmt.Sprintf("Selection strategy: %s\n", settings.CandidateSelectionStrategy))
	b.WriteString(fmt.Sprintf("Component selector: %s\n", settings.ComponentSelector))
	b.WriteString(fmt.Sprintf("Use merge: %t\n\n", settings.UseMerge))
	b.WriteString("## Current Prompt\n")
	b.WriteString(currentPrompt)
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("## Reflective Dataset (%d failures)\n", len(failures)))
	for _, fc := range failures {
		b.WriteString(fmt.Sprintf("- id=%s category=%s expected=%q predicted=%q\n", fc.ItemID, coalesceString(strings.TrimSpace(fc.Category), "uncategorized"), fc.CorrectAnswer, fc.Predicted))
		if q := trimForPrompt(fc.Question, 220); q != "" {
			b.WriteString("  Inputs: " + q + "\n")
		}
		outputs := trimForPrompt(fc.RawOutput, 220)
		if outputs == "" {
			outputs = fc.Predicted
		}
		b.WriteString("  Generated Outputs: " + outputs + "\n")
		feedback := trimForPrompt(coalesceString(strings.TrimSpace(fc.Diagnosis), strings.TrimSpace(fc.FailureReason)), 220)
		if feedback == "" {
			feedback = "This trajectory failed on the benchmark item."
		}
		b.WriteString("  Feedback: " + feedback + "\n")
		if votes := summarizeAgentAnswers(fc.AgentAnswers, 6); votes != "" {
			b.WriteString("  Predictor Trace Hint: " + votes + "\n")
		}
		if traces := formatNodeTraces(fc.NodeTraces); traces != "" {
			b.WriteString(traces)
		}
	}
	b.WriteString("\n## Trajectory Feedback History\n")
	b.WriteString(formatLearningLogSectionCompact(learning, 14, 120))
	b.WriteString("\n")
	b.WriteString(formatObjectivesSection("## Objectives", spec))
	b.WriteString("\n## Instructions\n")
	b.WriteString("Reflect over feedback and propose targeted instruction mutations.\n")
	b.WriteString("- Preserve stable sections unless reflection indicates change.\n")
	b.WriteString("- Avoid benchmark-specific hacks.\n")
	b.WriteString(fmt.Sprintf("- Return exactly %d candidates.\n\n", numCandidates))
	b.WriteString("Return ONLY valid JSON:\n")
	b.WriteString("{\n")
	b.WriteString("  \"candidates\": [\n")
	b.WriteString("    {\"revised_prompt\": \"...\", \"reasoning\": \"...\", \"changes_summary\": \"...\", \"score\": 0.0}\n")
	b.WriteString("  ]\n")
	b.WriteString("}\n")
	return b.String()
}

func selectGEPAComponents(
	groups []promptParamGroup,
	selector string,
	generation int,
	learning []LearningEntry,
	rng *rand.Rand,
) []int {
	_ = learning
	_ = rng
	if len(groups) == 0 {
		return nil
	}
	selector = strings.ToLower(strings.TrimSpace(selector))
	switch selector {
	case "all":
		indices := make([]int, 0, len(groups))
		for i := range groups {
			indices = append(indices, i)
		}
		return indices
	default:
		// Pure round-robin selection, matching DSPy's default module selector.
		start := 0
		if generation > 0 && len(groups) > 0 {
			start = (generation - 1) % len(groups)
		}
		return []int{start}
	}
}

func chooseGEPACandidateIndex(strategy string, trial int, candidates []ClaudePromptCandidate, rng *rand.Rand) int {
	if len(candidates) <= 1 {
		return 0
	}
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	switch strategy {
	case "current_best":
		// Reserve index 0 for baseline/current prompt. Prefer first non-baseline
		// candidate as "current best" proxy; measured eval then picks true best.
		return min(1, len(candidates)-1)
	default:
		// In pareto mode, avoid pretending Claude candidate scores are Pareto-valid.
		// Cycle across non-baseline candidates; downstream evaluation does Pareto
		// prioritization using measured benchmark metrics.
		offset := 0
		if rng != nil {
			offset = rng.Intn(len(candidates) - 1)
		}
		return 1 + ((offset + trial) % (len(candidates) - 1))
	}
}

func mergeGEPAReflectiveCandidates(
	ctx context.Context,
	model string,
	a ClaudePromptCandidate,
	b ClaudePromptCandidate,
	componentKey string,
	failures []FailureCase,
) (ClaudePromptCandidate, error) {
	var prompt strings.Builder
	prompt.WriteString("Merge two reflective instruction candidates into one superior prompt.\n")
	prompt.WriteString("Your goal is to combine the strengths of both candidates, producing a prompt\n")
	prompt.WriteString("that handles more failure patterns than either candidate alone.\n\n")
	prompt.WriteString(fmt.Sprintf("Component key: %s\n\n", componentKey))
	prompt.WriteString("Candidate A:\n")
	prompt.WriteString(a.RevisedPrompt)
	if a.ChangesSummary != "" {
		prompt.WriteString(fmt.Sprintf("\n(Focus: %s)", a.ChangesSummary))
	}
	prompt.WriteString("\n\nCandidate B:\n")
	prompt.WriteString(b.RevisedPrompt)
	if b.ChangesSummary != "" {
		prompt.WriteString(fmt.Sprintf("\n(Focus: %s)", b.ChangesSummary))
	}
	// R12: Provide failure context so the merge is validation-aware — the LLM
	// can see what failure types exist and craft a merged prompt that addresses
	// complementary weaknesses from each candidate.
	if len(failures) > 0 {
		prompt.WriteString("\n\n## Active Failure Patterns\n")
		prompt.WriteString("The merged prompt should address as many of these as possible:\n")
		cats := countFailureCategories(failures)
		for _, line := range sortedCategoryCounts(cats) {
			prompt.WriteString("- " + line + "\n")
		}
	}
	prompt.WriteString("\n\nReturn ONLY valid JSON:\n")
	prompt.WriteString("{\"revised_prompt\":\"...\",\"reasoning\":\"...\",\"changes_summary\":\"...\",\"score\":0.0}\n")
	response, err := InvokeClaudePromptCandidates(ctx, model, prompt.String(), 1)
	if err != nil {
		return ClaudePromptCandidate{}, err
	}
	if len(response.Candidates) == 0 {
		return ClaudePromptCandidate{}, ErrNoMergedCandidate
	}
	return response.Candidates[0], nil
}
