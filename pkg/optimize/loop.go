package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Loop struct {
	Evaluator        BenchmarkEvaluator
	Store            PopulationStore
	Workflows        WorkflowManager
	Mutator          Mutator
	Selection        SelectionConfig
	Rand             *rand.Rand
	VerifyMutations  bool
	QuickCheckItems  int
	VerifyMode       string // full|replay
	VerifyReplayMode string // required|best_effort

	// IncludeFlaggedFailures controls whether flagged dataset items are included
	// in mutation failure examples. Default false to avoid optimizing against bad gold labels.
	IncludeFlaggedFailures bool

	// ParentEvaluation configures evaluation through a benchmark wrapper workflow
	// that references the mutated candidate workflow via a child_workflow node.
	ParentEvaluation *ParentEvaluationConfig
}

// ParentEvaluationConfig describes how to run candidate workflows through a
// benchmark wrapper (contract parent) instead of directly benchmarking the
// candidate itself.
type ParentEvaluationConfig struct {
	WrapperWorkflowID   string          // Source wrapper ID (for observability)
	WrapperTemplateJSON json.RawMessage // Wrapper workflow JSON template
	ChildNodeID         string          // Child workflow node to rewrite
}

type stagedOrganism struct {
	Organism            *Organism
	Parent              *Organism
	CandidateWorkflowID string
	CandidateWorkflow   json.RawMessage
	EvaluationWorkflow  string
	CleanupWorkflowIDs  []string
	ReplayFitness       *Fitness // captured during verification; reused as trial score
}

type transientEvaluatedCandidate struct {
	RunID   string
	Fitness *Fitness
}

type comboAggregate struct {
	key       string
	meanScore float64
	best      scoredCandidate
}

type scoredCandidate struct {
	item    *stagedOrganism
	fitness *Fitness
}

func (l *Loop) Execute(ctx context.Context, run *OptimizationRun, baseWorkflowJSON json.RawMessage) error {
	if run == nil {
		return ErrRunRequired
	}
	if l.Evaluator == nil || l.Store == nil || l.Workflows == nil {
		return ErrMissingDependencies
	}
	if run.Spec == nil {
		return ErrMissingSpec
	}
	if err := run.Spec.Validate(); err != nil {
		return fmt.Errorf("invalid optimize spec: %w", err)
	}
	if run.PopulationSize < 1 {
		run.PopulationSize = 1
	}
	if run.Strategy == "" {
		run.Strategy = "evolutionary"
	}
	if err := ValidateRunConfiguration(run.Strategy, run.MutatorMode, run.Spec); err != nil {
		return fmt.Errorf("invalid strategy configuration: %w", err)
	}
	if NormalizeOptimizeStrategy(run.Strategy) != OptimizeStrategyDSPY && l.Mutator == nil {
		return fmt.Errorf("%w: strategy %s", ErrMutatorRequired, NormalizeOptimizeStrategy(run.Strategy))
	}
	if run.TotalBudgetUSD <= 0 {
		run.TotalBudgetUSD = run.Spec.StopPolicy.BudgetUSD
	}
	if run.Status == "" || run.Status == "pending" {
		run.Status = "running"
		now := time.Now().UTC()
		run.StartedAt = &now
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if l.Rand == nil {
		seed := time.Now().UnixNano()
		if run.RNGSeed != nil {
			seed = *run.RNGSeed
		}
		l.Rand = rand.New(rand.NewSource(seed)) //nolint:gosec // non-crypto search randomness
	}

	effectiveBaseWorkflowJSON, err := applyInitialValuesToWorkflow(baseWorkflowJSON, run.Spec.InitialValues)
	if err != nil {
		return err
	}

	if run.Generation == 0 {
		if err := l.initializeGenerationZero(ctx, run, effectiveBaseWorkflowJSON); err != nil {
			return err
		}
	}
	if run != nil && NormalizeOptimizeStrategy(run.Strategy) == OptimizeStrategyDSPY {
		return l.executeDSPYStyle(ctx, run, effectiveBaseWorkflowJSON)
	}

	withoutImprovement := 0
	var lastBestFitness *Fitness
	if run.BestFitness != nil {
		snapshot := *run.BestFitness
		lastBestFitness = &snapshot
	}

	for generation := run.Generation + 1; generation <= run.Spec.StopPolicy.MaxGenerations; generation++ {
		if err := ctx.Err(); err != nil {
			_ = l.Store.UpdateRunStatus(context.Background(), run.ID, "paused")
			return err
		}
		if run.SpentUSD >= run.TotalBudgetUSD {
			break
		}
		if run.Status != "running" {
			break
		}

		existing, err := l.Store.GetOrganismsByGeneration(ctx, run.ID, generation, 100000)
		if err != nil {
			return fmt.Errorf("load generation organisms: %w", err)
		}

		parentsByChild, err := l.loadParentMap(ctx, run.ID)
		if err != nil {
			return err
		}

		createdCount := 0
		if len(existing) == 0 {
			generated, generatedCount, genErr := l.generateGenerationChildren(ctx, run, generation, parentsByChild, withoutImprovement)
			if genErr != nil {
				return genErr
			}
			existing = generated
			createdCount = generatedCount
		}

		unevaluated := make([]*Organism, 0, len(existing))
		for _, organism := range existing {
			if organism == nil {
				continue
			}
			if organism.Fitness != nil && organism.EvaluatedAt != nil {
				continue
			}
			unevaluated = append(unevaluated, organism)
		}

		if len(unevaluated) > 0 {
			staged, err := l.stageOrganisms(ctx, run, effectiveBaseWorkflowJSON, unevaluated, parentsByChild)
			if err != nil {
				return err
			}

			survivors, err := l.verifyMutationsIfEnabled(ctx, run, staged)
			if err != nil {
				// Best-effort cleanup on error path.
				_ = l.cleanupStaged(context.Background(), staged)
				return err
			}
			if len(survivors) > 0 {
				if err := l.evaluateStagedBatch(ctx, run, survivors); err != nil {
					_ = l.cleanupStaged(context.Background(), staged)
					return err
				}
			}
			if err := l.cleanupStaged(ctx, staged); err != nil {
				return err
			}
		}

		best, err := l.currentBest(ctx, run)
		if err != nil {
			return err
		}
		improved := false
		if best != nil {
			if isFitnessBetter(best.Fitness, lastBestFitness) {
				improved = true
				snapshot := *best.Fitness
				lastBestFitness = &snapshot
			}
			run.BestOrganismID = best.ID
			run.BestFitness = best.Fitness
		}
		if improved {
			withoutImprovement = 0
		} else {
			withoutImprovement++
		}

		all, err := l.Store.GetAllOrganisms(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("load all organisms for progress: %w", err)
		}
		run.Generation = generation
		run.TotalOrganisms = len(all)
		if createdCount > 0 && run.TotalOrganisms < createdCount {
			run.TotalOrganisms = createdCount
		}
		if err := l.Store.UpdateRunProgress(ctx, run.ID, run.Generation, run.BestOrganismID, run.BestFitness, run.SpentUSD, run.TotalOrganisms, run.DSPYMetricCallsUsed); err != nil {
			return fmt.Errorf("update run progress: %w", err)
		}

		if run.Spec.StopPolicy.PlateauGenerations > 0 && withoutImprovement >= run.Spec.StopPolicy.PlateauGenerations {
			break
		}
	}

	run.Status = "completed"
	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	if err := l.Store.UpdateRunStatus(ctx, run.ID, "completed"); err != nil {
		return fmt.Errorf("set run completed: %w", err)
	}
	return nil
}

func (l *Loop) initializeGenerationZero(ctx context.Context, run *OptimizationRun, baseWorkflowJSON json.RawMessage) error {
	initialValues, err := extractParamValuesFromWorkflow(baseWorkflowJSON, run.Spec)
	if err != nil {
		return fmt.Errorf("extract baseline param values: %w", err)
	}

	baseline := &Organism{
		ID:           uuid.NewString(),
		OptRunID:     run.ID,
		Generation:   0,
		ParentIDs:    []string{},
		ParamValues:  initialValues,
		WorkflowJSON: baseWorkflowJSON,
		MutationType: "seed",
		MutationLog:  "baseline seed organism",
		CreatedAt:    time.Now().UTC(),
	}
	if err := l.Store.CreateOrganism(ctx, baseline); err != nil {
		return fmt.Errorf("create baseline organism: %w", err)
	}
	if _, err := l.evaluateOrganismSingle(ctx, run, baseWorkflowJSON, baseline, run.ItemLimit); err != nil {
		return err
	}
	run.BestOrganismID = baseline.ID
	run.BestFitness = baseline.Fitness
	run.TotalOrganisms = 1

	if run.PopulationSize > 1 {
		needed := run.PopulationSize - 1
		modelDiverse := l.buildModelDiverseGen0Children(baseline, run.Spec, needed)
		children := make([]*Organism, 0, needed)
		parentsByChild := map[string]*Organism{}
		createChild := func(mutation *MutationResult) error {
			if mutation == nil || mutation.Organism == nil {
				return nil
			}
			mutation.Organism.OptRunID = run.ID
			if err := l.Store.CreateOrganism(ctx, mutation.Organism); err != nil {
				return fmt.Errorf("create generation-0 child: %w", err)
			}
			if len(mutation.Changes) > 0 {
				if err := l.Store.CreateParamChanges(ctx, mutation.Organism.ID, mutation.Changes); err != nil {
					return fmt.Errorf("persist generation-0 child param changes: %w", err)
				}
			}
			if len(mutation.Artifacts) > 0 {
				if err := l.Store.CreateMutationArtifacts(ctx, mutation.Organism.ID, mutation.Artifacts, run.CompactArtifacts); err != nil {
					return fmt.Errorf("persist generation-0 mutation artifacts: %w", err)
				}
			}
			children = append(children, mutation.Organism)
			parentsByChild[mutation.Organism.ID] = baseline
			return nil
		}
		for _, mutation := range modelDiverse {
			if err := createChild(mutation); err != nil {
				return err
			}
		}
		if len(children) < needed {
			childReq := &MutationRequest{
				Parent:     baseline,
				Spec:       run.Spec,
				Generation: 0,
				Count:      needed - len(children),
			}
			mutations, err := l.Mutator.Mutate(ctx, childReq)
			if err != nil {
				return fmt.Errorf("seed generation mutation: %w", err)
			}
			for _, mutation := range mutations {
				if err := createChild(mutation); err != nil {
					return err
				}
			}
		}
		if len(children) > 0 {
			staged, err := l.stageOrganisms(ctx, run, baseWorkflowJSON, children, parentsByChild)
			if err != nil {
				return err
			}
			defer func() {
				_ = l.cleanupStaged(context.Background(), staged)
			}()
			if err := l.evaluateStagedBatch(ctx, run, staged); err != nil {
				return err
			}
			if err := l.cleanupStaged(ctx, staged); err != nil {
				return err
			}
		}
	}

	all, err := l.Store.GetAllOrganisms(ctx, run.ID)
	if err == nil {
		run.TotalOrganisms = len(all)
	}
	return l.Store.UpdateRunProgress(ctx, run.ID, 0, run.BestOrganismID, run.BestFitness, run.SpentUSD, run.TotalOrganisms, run.DSPYMetricCallsUsed)
}

func (l *Loop) buildModelDiverseGen0Children(parent *Organism, spec *OptimizeSpec, limit int) []*MutationResult {
	if parent == nil || spec == nil || limit <= 0 || spec.ModelSwap == nil || !spec.ModelSwap.Enabled {
		return nil
	}
	baseValues, err := buildParentParamValues(parent, spec)
	if err != nil {
		return nil
	}
	results := make([]*MutationResult, 0, limit)
	seen := make(map[string]struct{})
	for _, declaration := range spec.Params {
		if declaration.Type != ParamTypeModel || len(declaration.Candidates) == 0 {
			continue
		}
		current := strings.TrimSpace(baseValues[declaration.Path])
		for _, candidate := range declaration.Candidates {
			if len(results) >= limit {
				return results
			}
			encoded, err := encodeJSONValue(candidate)
			if err != nil {
				continue
			}
			if strings.TrimSpace(encoded) == current {
				continue
			}
			key := declaration.Path + "\x00" + candidate
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			childValues := copyStringMap(baseValues)
			childValues[declaration.Path] = encoded
			organism := newChildOrganism(parent, 0, "combinatorial", fmt.Sprintf("gen0 model-diverse seed: %s -> %s", declaration.Path, candidate), childValues)
			results = append(results, &MutationResult{
				Organism: organism,
				Changes: []ParamChange{{
					Path:     declaration.Path,
					OldValue: current,
					NewValue: encoded,
					Reason:   "model-diverse generation-0 seed",
				}},
				Reasoning: "model-diverse generation-0 seed",
			})
		}
	}
	return results
}

func (l *Loop) generateGenerationChildren(ctx context.Context, run *OptimizationRun, generation int, parentsByChild map[string]*Organism, withoutImprovement int) ([]*Organism, int, error) {
	parents, err := l.selectParents(ctx, run)
	if err != nil {
		return nil, 0, err
	}
	childrenPerParent := l.childrenPerParent(run, withoutImprovement)
	maxChildren := run.MaxChildrenPerGeneration
	if maxChildren <= 0 {
		maxChildren = run.PopulationSize
	}

	learning, _ := l.Store.GetLearningLog(ctx, run.ID, 300)
	children := make([]*Organism, 0, maxChildren)
	createdCount := 0
	for _, parent := range parents {
		if parent == nil {
			continue
		}
		if len(children) >= maxChildren {
			break
		}
		remaining := maxChildren - len(children)
		count := min(childrenPerParent, remaining)
		failures, _ := l.fetchParentFailures(ctx, run, parent, 12)
		pc := l.buildProposalContext(ctx, run, parent, parent.WorkflowJSON, "", failures, learning, l.loopRand())
		mutations, err := l.Mutator.Mutate(ctx, &MutationRequest{
			Parent:          parent,
			Spec:            run.Spec,
			FailureCases:    failures,
			LearningLog:     learning,
			Generation:      generation,
			Count:           count,
			ProposalContext: pc,
		})
		if err != nil {
			return nil, createdCount, fmt.Errorf("mutate parent %s: %w", parent.ID, err)
		}
		for _, mutation := range mutations {
			if mutation == nil || mutation.Organism == nil {
				continue
			}
			mutation.Organism.OptRunID = run.ID
			mutation.Organism.Generation = generation

			if err := l.Store.CreateOrganism(ctx, mutation.Organism); err != nil {
				return nil, createdCount, fmt.Errorf("create child organism: %w", err)
			}
			if len(mutation.Changes) > 0 {
				if err := l.Store.CreateParamChanges(ctx, mutation.Organism.ID, mutation.Changes); err != nil {
					return nil, createdCount, fmt.Errorf("persist child param changes: %w", err)
				}
			}
			if len(mutation.Artifacts) > 0 {
				if err := l.Store.CreateMutationArtifacts(ctx, mutation.Organism.ID, mutation.Artifacts, run.CompactArtifacts); err != nil {
					return nil, createdCount, fmt.Errorf("persist mutation artifacts: %w", err)
				}
			}
			children = append(children, mutation.Organism)
			createdCount++
			parentsByChild[mutation.Organism.ID] = parent
			if len(children) >= maxChildren {
				break
			}
		}
	}
	return children, createdCount, nil
}

func (l *Loop) loopRand() *rand.Rand {
	if l.Rand != nil {
		return l.Rand
	}
	l.Rand = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	return l.Rand
}

func extractParamValuesFromWorkflow(workflowJSON json.RawMessage, spec *OptimizeSpec) (map[string]string, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &raw); err != nil {
		return nil, fmt.Errorf("parse workflow JSON: %w", err)
	}
	values := make(map[string]string, len(spec.Params))
	for _, declaration := range spec.Params {
		val, found, err := getPathValue(raw, declaration.Path)
		if err != nil {
			return nil, fmt.Errorf("param path %s: %w", declaration.Path, err)
		}
		if !found {
			return nil, fmt.Errorf("param path %s not found in workflow", declaration.Path)
		}
		encoded, err := encodeJSONValue(val)
		if err != nil {
			return nil, fmt.Errorf("encode path %s: %w", declaration.Path, err)
		}
		values[declaration.Path] = encoded
	}
	return values, nil
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-")
	id = replacer.Replace(id)
	if id == "" {
		return "run"
	}
	return id
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func applyInitialValuesToWorkflow(baseWorkflowJSON json.RawMessage, initialValues map[string]string) (json.RawMessage, error) {
	if len(initialValues) == 0 {
		return baseWorkflowJSON, nil
	}
	var workflow map[string]interface{}
	if err := json.Unmarshal(baseWorkflowJSON, &workflow); err != nil {
		return nil, fmt.Errorf("parse base workflow JSON for initial_values: %w", err)
	}
	for path, encoded := range initialValues {
		value, err := decodeParamValue(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode initial_values[%s]: %w", path, err)
		}
		if err := setPathValue(workflow, path, value); err != nil {
			return nil, fmt.Errorf("apply initial_values[%s]: %w", path, err)
		}
	}
	updated, err := json.Marshal(workflow)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow with initial_values applied: %w", err)
	}
	return updated, nil
}
