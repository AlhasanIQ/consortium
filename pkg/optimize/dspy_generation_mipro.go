package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

type miproGroupPool struct {
	group      promptParamGroup
	candidates []ClaudePromptCandidate
	artifact   MutationArtifact
}

type miproComboStat struct {
	key           string
	selection     map[string]int
	sumScore      float64
	count         int
	bestOrganism  *Organism
	fullEvaluated bool
}

type miproTrialObservation struct {
	selection  map[string]int
	score      float64
	historyIdx int // position in append order, used for recency weighting
}

type miproBatchItem struct {
	organism  *Organism
	selection map[string]int
}

func (l *Loop) runDSPYMIPROGeneration(
	ctx context.Context,
	run *OptimizationRun,
	baseWorkflowJSON json.RawMessage,
	generation int,
	parent *Organism,
	runtime DSPYRuntimeSettings,
	metricCallsUsed *int,
) (int, error) {
	if run == nil || run.Spec == nil || parent == nil {
		return 0, fmt.Errorf("run, spec, and parent are required")
	}
	rng := l.loopRand()
	trialCount := max(runtime.NumTrials, 1)

	learning, _ := l.Store.GetLearningLog(ctx, run.ID, 300)
	failureLimit := max(12, runtime.MinibatchSize)
	failures, _ := l.fetchParentFailures(ctx, run, parent, failureLimit)
	baseValues, err := buildParentParamValues(parent, run.Spec)
	if err != nil {
		return 0, err
	}

	pc := l.buildProposalContext(ctx, run, parent, baseWorkflowJSON, "", failures, learning, rng)
	pools, err := l.buildMIPROPromptPools(ctx, run, baseValues, failures, learning, runtime, rng, pc)
	if err != nil {
		return 0, err
	}
	if len(pools) == 0 {
		return 0, fmt.Errorf("miprov2 dspy generation produced no prompt pools")
	}

	groupOrder := make([]string, 0, len(pools))
	for key := range pools {
		groupOrder = append(groupOrder, key)
	}
	sort.Strings(groupOrder)

	comboStats := make(map[string]*miproComboStat)
	history := make([]miproTrialObservation, 0, trialCount+1)
	createdCount := 0
	fullEvalSteps := max(runtime.MinibatchFullEvalSteps, 1)

	// R3: Inject baseline/parent observation so TPE starts with a known data point,
	// matching DSPy/Optuna's practice of adding the default trial before optimization.
	if parent != nil && parent.Fitness != nil {
		baselineSelection := make(map[string]int, len(groupOrder))
		for _, key := range groupOrder {
			baselineSelection[key] = 0 // index 0 is always the baseline prompt
		}
		history = append(history, miproTrialObservation{
			selection:  baselineSelection,
			score:      parent.Fitness.AdjustedAccuracy,
			historyIdx: 0,
		})
	}

	batchSize := max(runtime.BatchSize, 1)

	for trial := 0; trial < trialCount; {
		if err := ctx.Err(); err != nil {
			return createdCount, err
		}
		if run.Status != "running" || run.SpentUSD >= run.TotalBudgetUSD {
			break
		}

		// Determine chunk size for this iteration.
		chunkEnd := min(trial+batchSize, trialCount)
		chunkSize := chunkEnd - trial

		// 1. Select N combos from TPE and create organisms.
		batchItems := make([]miproBatchItem, 0, chunkSize)
		batchOrganisms := make([]*Organism, 0, chunkSize)
		parentsByChild := make(map[string]*Organism, chunkSize)

		for i := 0; i < chunkSize; i++ {
			selection := selectMIPROComboTPE(groupOrder, pools, history, rng)
			childValues := copyStringMap(baseValues)
			changes := make([]ParamChange, 0, len(groupOrder))
			artifacts := make([]MutationArtifact, 0, len(groupOrder))
			selected := make([]string, 0, len(groupOrder))

			for _, key := range groupOrder {
				pool := pools[key]
				idx := selection[key]
				if idx < 0 || idx >= len(pool.candidates) {
					idx = 0
				}
				candidate := pool.candidates[idx]
				encoded, err := encodeJSONValue(candidate.RevisedPrompt)
				if err != nil {
					return createdCount, fmt.Errorf("encode revised prompt: %w", err)
				}
				for _, declaration := range pool.group.Declarations {
					oldValue := childValues[declaration.Path]
					childValues[declaration.Path] = encoded
					changes = append(changes, ParamChange{
						Path:     declaration.Path,
						OldValue: oldValue,
						NewValue: encoded,
						Reason:   coalesceString(candidate.ChangesSummary, "miprov2_prompt"),
					})
				}
				selected = append(selected, fmt.Sprintf("%s#%d", key, idx))
				artifacts = append(artifacts, pool.artifact)
			}
			reason := fmt.Sprintf("MIPROv2 dspy trial %d/%d using combo %s", trial+i+1, trialCount, strings.Join(selected, ", "))
			organism := newChildOrganism(parent, generation, "miprov2_prompt", reason, childValues)
			organism.OptRunID = run.ID
			organism.Generation = generation
			if err := l.persistCandidateOrganism(ctx, run, organism, changes, artifacts); err != nil {
				return createdCount, err
			}
			createdCount++

			batchItems = append(batchItems, miproBatchItem{organism: organism, selection: copyIntMap(selection)})
			batchOrganisms = append(batchOrganisms, organism)
			parentsByChild[organism.ID] = parent
		}

		// 2. Stage all N at once.
		staged, err := l.stageOrganisms(ctx, run, baseWorkflowJSON, batchOrganisms, parentsByChild)
		if err != nil {
			return createdCount, err
		}

		// 3. Verify batch → survivors.
		survivors, err := l.verifyMutationsIfEnabled(ctx, run, staged)
		if err != nil {
			_ = l.cleanupStaged(ctx, staged)
			return createdCount, err
		}

		// 4. Score survivors that don't already have ReplayFitness.
		needsEval := filterStagedWithoutReplayFitness(survivors)
		var batchResults map[string]transientEvaluatedCandidate
		if len(needsEval) > 0 {
			evalLimit := runtime.MinibatchSize
			if !runtime.UseMinibatch {
				evalLimit = run.ItemLimit
			}
			var metricCalls int
			batchResults, metricCalls, err = l.evaluateStagedBatchTransient(ctx, run, needsEval, evalLimit)
			if err != nil {
				_ = l.cleanupStaged(ctx, staged)
				return createdCount, err
			}
			accumulateDSPYMetricCallsRaw(metricCallsUsed, metricCalls)
			// For full-eval (non-minibatch), persist fitness from batch results.
			if !runtime.UseMinibatch {
				for _, item := range needsEval {
					if res, ok := batchResults[item.Organism.ID]; ok && res.Fitness != nil {
						item.Organism.Fitness = res.Fitness
						item.Organism.BenchRunID = res.RunID
						now := time.Now().UTC()
						item.Organism.EvaluatedAt = &now
						_ = l.Store.UpdateOrganismFitness(ctx, item.Organism.ID, res.RunID, res.Fitness)
						_ = l.appendLearningForEvaluatedOrganism(ctx, run, item.Organism, parent)
					}
				}
			}
		}

		// 5. Update history + comboStats for all survivors.
		for _, item := range survivors {
			trialScore := pickTrialScore(item, batchResults)
			sel := findMIPROBatchSelection(batchItems, item.Organism.ID)
			history = append(history, miproTrialObservation{
				selection:  sel,
				score:      trialScore,
				historyIdx: len(history),
			})
			comboKey := miproSelectionKey(sel)
			stat := comboStats[comboKey]
			if stat == nil {
				stat = &miproComboStat{
					key:       comboKey,
					selection: copyIntMap(sel),
				}
				comboStats[comboKey] = stat
			}
			stat.sumScore += trialScore
			stat.count++
			stat.bestOrganism = item.Organism
			if !runtime.UseMinibatch && item.Organism.EvaluatedAt != nil {
				stat.fullEvaluated = true
			}
		}

		// 6. Cleanup all staged (survivors + rejected).
		_ = l.cleanupStaged(ctx, staged)

		// 7. Periodic full eval (unchanged logic).
		if runtime.UseMinibatch && shouldRunDSPYPeriodicFullEval(chunkEnd-1, trialCount, fullEvalSteps) {
			bestCombo := selectBestMIPROComboForFullEval(comboStats)
			if bestCombo != nil && bestCombo.bestOrganism != nil && bestCombo.bestOrganism.EvaluatedAt == nil {
				fitness, err := l.evaluateOrganismSingle(ctx, run, baseWorkflowJSON, bestCombo.bestOrganism, run.ItemLimit)
				if err != nil {
					return createdCount, err
				}
				accumulateDSPYMetricCalls(metricCallsUsed, fitness, run.ItemLimit)
				bestCombo.fullEvaluated = true
				// R4: Feed full-eval results back to TPE history so subsequent
				// Bayesian decisions benefit from higher-fidelity signal.
				history = append(history, miproTrialObservation{
					selection:  copyIntMap(bestCombo.selection),
					score:      fitness.AdjustedAccuracy,
					historyIdx: len(history),
				})
				if err := l.appendLearningForEvaluatedOrganism(ctx, run, bestCombo.bestOrganism, parent); err != nil {
					return createdCount, err
				}
			}
		}

		trial = chunkEnd
	}

	if runtime.UseMinibatch {
		// Ensure at least one combo is promoted to full evaluation.
		bestCombo := selectBestMIPROComboForFullEval(comboStats)
		if bestCombo != nil && bestCombo.bestOrganism != nil && bestCombo.bestOrganism.EvaluatedAt == nil {
			fitness, err := l.evaluateOrganismSingle(ctx, run, baseWorkflowJSON, bestCombo.bestOrganism, run.ItemLimit)
			if err != nil {
				return createdCount, err
			}
			accumulateDSPYMetricCalls(metricCallsUsed, fitness, run.ItemLimit)
			bestCombo.fullEvaluated = true
			if err := l.appendLearningForEvaluatedOrganism(ctx, run, bestCombo.bestOrganism, parent); err != nil {
				return createdCount, err
			}
		}
	}

	return createdCount, nil
}

func (l *Loop) buildMIPROPromptPools(
	ctx context.Context,
	run *OptimizationRun,
	baseValues map[string]string,
	failures []FailureCase,
	learning []LearningEntry,
	runtime DSPYRuntimeSettings,
	rng *rand.Rand,
	pc *ProposalContext,
) (map[string]miproGroupPool, error) {
	groups := collectPromptGroups(run.Spec)
	if len(groups) == 0 {
		return nil, fmt.Errorf("no prompt params declared")
	}
	numCandidates := max(runtime.NumCandidates, 2)
	demoSets := buildMIPRODemoSets(failures, numCandidates, 4, rng)
	pools := make(map[string]miproGroupPool, len(groups))
	model := strings.TrimSpace(run.ClaudeModel)

	// R10: Bootstrap few-shot demo pools from success examples for joint
	// instruction+demo optimization (DSPy's BootstrapFewShot + MIPROv2).
	var demoPools [][]DemoExample
	if pc != nil && len(pc.SuccessExamples) > 0 {
		demoPools = bootstrapDemoPools(pc.SuccessExamples, min(3, numCandidates), 3, rng)
	}

	// Build pools in parallel — each group's Claude call is independent.
	type poolResult struct {
		key  string
		pool miproGroupPool
		err  error
	}
	results := make([]poolResult, len(groups))
	var wg sync.WaitGroup
	for i, group := range groups {
		wg.Add(1)
		go func(idx int, g promptParamGroup) {
			defer wg.Done()
			currentPrompt, err := decodePromptValue(baseValues[g.Declarations[0].Path])
			if err != nil {
				results[idx] = poolResult{err: fmt.Errorf("decode current prompt %s: %w", g.Declarations[0].Path, err)}
				return
			}
			proposalPrompt := buildMIPROv2CandidateProposalPrompt(
				g.Key,
				currentPrompt,
				failures,
				learning,
				run.Spec,
				demoSets,
				numCandidates,
				pc,
			)
			response, err := InvokeClaudePromptCandidates(ctx, model, proposalPrompt, numCandidates)
			if err != nil {
				fallbackPrompt := buildMIPROv2MutationPrompt(currentPrompt, failures, learning, run.Spec, pc)
				single, singleErr := InvokeClaudePromptMutation(ctx, model, fallbackPrompt)
				if singleErr != nil {
					results[idx] = poolResult{err: fmt.Errorf("build miprov2 candidates for %s: %w", g.Key, err)}
					return
				}
				candidates := dedupePromptCandidates([]ClaudePromptCandidate{{
					RevisedPrompt:  single.Parsed.RevisedPrompt,
					Reasoning:      single.Parsed.Reasoning,
					ChangesSummary: single.Parsed.ChangesSummary,
				}}, currentPrompt, numCandidates)
				results[idx] = poolResult{
					key: g.Key,
					pool: miproGroupPool{
						group:      g,
						candidates: candidates,
						artifact: MutationArtifact{
							InputPromptHash:  sha256Hex(fallbackPrompt),
							InputPrompt:      fallbackPrompt,
							RawOutputHash:    sha256Hex(single.RawOutput),
							RawOutput:        single.RawOutput,
							ClaudeModel:      coalesceString(single.Model, model),
							ClaudeCLIVersion: single.CLIVersion,
						},
					},
				}
				return
			}
			candidates := dedupePromptCandidates(response.Candidates, currentPrompt, numCandidates)
			if len(candidates) == 0 {
				candidates = []ClaudePromptCandidate{{RevisedPrompt: currentPrompt}}
			}
			// R10: Augment instruction candidates with few-shot demo variants.
			candidates = augmentCandidatesWithDemos(candidates, demoPools, numCandidates+len(demoPools))
			results[idx] = poolResult{
				key: g.Key,
				pool: miproGroupPool{
					group:      g,
					candidates: candidates,
					artifact: MutationArtifact{
						InputPromptHash:  sha256Hex(proposalPrompt),
						InputPrompt:      proposalPrompt,
						RawOutputHash:    sha256Hex(response.RawOutput),
						RawOutput:        response.RawOutput,
						ClaudeModel:      coalesceString(response.Model, model),
						ClaudeCLIVersion: response.CLIVersion,
					},
				},
			}
		}(i, group)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		pools[r.key] = r.pool
	}

	return pools, nil
}

func findMIPROBatchSelection(items []miproBatchItem, orgID string) map[string]int {
	for _, item := range items {
		if item.organism != nil && item.organism.ID == orgID {
			return item.selection
		}
	}
	return nil
}

func selectMIPROComboTPE(
	groupOrder []string,
	pools map[string]miproGroupPool,
	history []miproTrialObservation,
	rng *rand.Rand,
) map[string]int {
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec
	}
	selection := randomMIPROSelection(groupOrder, pools, rng)
	if len(history) < max(4, len(groupOrder)) || rng.Float64() < 0.10 {
		return selection
	}

	// Approximate multivariate TPE: split historical combos into top gamma
	// ("good") and remaining ("bad"), then maximize l(x)/g(x) over sampled combos.
	// Gamma=0.10 matches Optuna's default TPE split (top 10%), capped at 25 like Optuna.
	scored := append([]miproTrialObservation(nil), history...)
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	gammaN := int(math.Ceil(float64(len(scored)) * 0.10))
	if gammaN < 1 {
		gammaN = 1
	}
	if gammaN > 25 {
		gammaN = 25
	}
	if gammaN >= len(scored) {
		return selection
	}
	good := scored[:gammaN]
	bad := scored[gammaN:]
	if len(good) == 0 || len(bad) == 0 {
		return selection
	}

	// Compute recency weights: older trials get less weight, matching Optuna's
	// default_weights() ramp. Trials are ordered newest-first (sorted by score,
	// but within history order the index reflects creation order). We apply
	// weights based on the original history index stored during append.
	weights := tpeRecencyWeights(len(history))

	goodByGroup := make(map[string][]float64, len(groupOrder))
	badByGroup := make(map[string][]float64, len(groupOrder))
	goodCombo := make(map[string]float64, len(good))
	badCombo := make(map[string]float64, len(bad))
	seenCombos := make(map[string]struct{}, len(history))
	totalGoodWeight := 0.0
	totalBadWeight := 0.0
	for _, key := range groupOrder {
		size := len(pools[key].candidates)
		if size < 1 {
			size = 1
		}
		goodByGroup[key] = make([]float64, size)
		badByGroup[key] = make([]float64, size)
	}
	for _, obs := range good {
		w := weights[obs.historyIdx]
		key := miproSelectionKey(obs.selection)
		goodCombo[key] += w
		totalGoodWeight += w
		seenCombos[key] = struct{}{}
		for _, groupKey := range groupOrder {
			idx := obs.selection[groupKey]
			if idx >= 0 && idx < len(goodByGroup[groupKey]) {
				goodByGroup[groupKey][idx] += w
			}
		}
	}
	for _, obs := range bad {
		w := weights[obs.historyIdx]
		key := miproSelectionKey(obs.selection)
		badCombo[key] += w
		totalBadWeight += w
		seenCombos[key] = struct{}{}
		for _, groupKey := range groupOrder {
			idx := obs.selection[groupKey]
			if idx >= 0 && idx < len(badByGroup[groupKey]) {
				badByGroup[groupKey][idx] += w
			}
		}
	}

	best := selection
	bestScore := math.Inf(-1)
	proposals := min(max(48, len(history)*2), 320)
	alpha := 1.0
	for i := 0; i < proposals; i++ {
		candidate := randomMIPROSelection(groupOrder, pools, rng)
		candidateKey := miproSelectionKey(candidate)
		score := 0.0
		for _, groupKey := range groupOrder {
			idx := candidate[groupKey]
			goodCounts := goodByGroup[groupKey]
			badCounts := badByGroup[groupKey]
			k := len(goodCounts)
			if k < 1 {
				k = 1
			}
			goodCount := 0.0
			badCount := 0.0
			if idx >= 0 && idx < len(goodCounts) {
				goodCount = goodCounts[idx]
			}
			if idx >= 0 && idx < len(badCounts) {
				badCount = badCounts[idx]
			}
			lx := (goodCount + alpha) / (totalGoodWeight + alpha*float64(k))
			gx := (badCount + alpha) / (totalBadWeight + alpha*float64(k))
			score += math.Log(lx) - math.Log(gx)
		}
		// Combo-level prior nudges selection toward historically promising full combos.
		comboLX := (goodCombo[candidateKey] + alpha) / (totalGoodWeight + alpha*2)
		comboGX := (badCombo[candidateKey] + alpha) / (totalBadWeight + alpha*2)
		score += math.Log(comboLX) - math.Log(comboGX)
		// Prefer unseen combos slightly to preserve exploration.
		if _, seen := seenCombos[candidateKey]; !seen {
			score += 0.05
		}
		if score > bestScore {
			bestScore = score
			best = candidate
		}
	}
	return best
}

// tpeRecencyWeights computes per-trial weights that ramp from 1/n to 1.0 for
// older trials and hold at 1.0 for the most recent 25 trials. This mirrors
// Optuna's default_weights() function, giving more influence to recent data.
func tpeRecencyWeights(n int) []float64 {
	weights := make([]float64, n)
	if n == 0 {
		return weights
	}
	rampEnd := n - 25
	if rampEnd < 0 {
		rampEnd = 0
	}
	for i := 0; i < n; i++ {
		if i >= rampEnd {
			weights[i] = 1.0
		} else {
			// Linear ramp from 1/rampEnd to 1.0
			weights[i] = float64(i+1) / float64(max(rampEnd, 1))
		}
	}
	return weights
}

func randomMIPROSelection(groupOrder []string, pools map[string]miproGroupPool, rng *rand.Rand) map[string]int {
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec
	}
	selection := make(map[string]int, len(groupOrder))
	for _, key := range groupOrder {
		n := len(pools[key].candidates)
		if n <= 1 {
			selection[key] = 0
			continue
		}
		selection[key] = rng.Intn(n)
	}
	return selection
}

func miproSelectionKey(selection map[string]int) string {
	if len(selection) == 0 {
		return ""
	}
	keys := make([]string, 0, len(selection))
	for key := range selection {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString("=")
		fmt.Fprintf(&b, "%d", selection[key])
		b.WriteString(";")
	}
	return b.String()
}

func selectBestMIPROComboForFullEval(comboStats map[string]*miproComboStat) *miproComboStat {
	var best *miproComboStat
	bestMean := math.Inf(-1)
	for _, stat := range comboStats {
		if stat == nil || stat.count == 0 || stat.fullEvaluated || stat.bestOrganism == nil {
			continue
		}
		mean := stat.sumScore / float64(stat.count)
		if best == nil || mean > bestMean+1e-9 {
			best = stat
			bestMean = mean
			continue
		}
		if math.Abs(mean-bestMean) <= 1e-9 && stat.bestOrganism.CreatedAt.Before(best.bestOrganism.CreatedAt) {
			best = stat
		}
	}
	return best
}
