package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (l *Loop) stageOrganisms(ctx context.Context, run *OptimizationRun, baseWorkflowJSON json.RawMessage, organisms []*Organism, parentsByChild map[string]*Organism) ([]*stagedOrganism, error) {
	staged := make([]*stagedOrganism, 0, len(organisms))
	for _, org := range organisms {
		if org == nil {
			continue
		}
		materialized, err := MaterializeWorkflow(baseWorkflowJSON, org.ParamValues, run.Spec)
		if err != nil {
			return nil, fmt.Errorf("materialize organism %s: %w", org.ID, err)
		}
		candidateID, candidateJSON, err := rewriteWorkflowIdentity(materialized, run, org)
		if err != nil {
			return nil, err
		}
		if err := l.Workflows.CreateTemporaryWorkflow(ctx, candidateID, candidateJSON, "Optimization Candidate", fmt.Sprintf("Optimization run %s organism %s", run.ID, org.ID)); err != nil {
			return nil, fmt.Errorf("create temporary workflow %s: %w", candidateID, err)
		}
		item := &stagedOrganism{
			Organism:            org,
			Parent:              parentsByChild[org.ID],
			CandidateWorkflowID: candidateID,
			CandidateWorkflow:   candidateJSON,
			EvaluationWorkflow:  candidateID,
			CleanupWorkflowIDs:  []string{candidateID},
		}
		if l.ParentEvaluation != nil && len(l.ParentEvaluation.WrapperTemplateJSON) > 0 {
			wrapperID := fmt.Sprintf("opt-parent-%s-g%d-%s", sanitizeID(run.ID), org.Generation, shortID(org.ID))
			wrapperJSON, err := rewriteWorkflowIdentityFixedID(l.ParentEvaluation.WrapperTemplateJSON, wrapperID, "Optimization Benchmark Wrapper")
			if err != nil {
				_ = l.Workflows.DeleteTemporaryWorkflow(context.Background(), candidateID)
				return nil, fmt.Errorf("rewrite wrapper identity for organism %s: %w", org.ID, err)
			}
			wrapperJSON, err = setChildWorkflowReference(wrapperJSON, l.ParentEvaluation.ChildNodeID, candidateID)
			if err != nil {
				_ = l.Workflows.DeleteTemporaryWorkflow(context.Background(), candidateID)
				return nil, fmt.Errorf("rewrite wrapper child reference for organism %s: %w", org.ID, err)
			}
			if err := l.Workflows.CreateTemporaryWorkflow(ctx, wrapperID, wrapperJSON, "Optimization Benchmark Wrapper", fmt.Sprintf("Optimization run %s organism %s evaluation wrapper", run.ID, org.ID)); err != nil {
				_ = l.Workflows.DeleteTemporaryWorkflow(context.Background(), candidateID)
				return nil, fmt.Errorf("create wrapper workflow %s: %w", wrapperID, err)
			}
			item.EvaluationWorkflow = wrapperID
			item.CleanupWorkflowIDs = []string{wrapperID, candidateID}
		}
		staged = append(staged, item)
	}
	return staged, nil
}

func (l *Loop) cleanupStaged(ctx context.Context, staged []*stagedOrganism) error {
	var firstErr error
	for _, item := range staged {
		if item == nil || len(item.CleanupWorkflowIDs) == 0 {
			continue
		}
		for _, workflowID := range item.CleanupWorkflowIDs {
			workflowID = strings.TrimSpace(workflowID)
			if workflowID == "" {
				continue
			}
			if err := l.Workflows.DeleteTemporaryWorkflow(ctx, workflowID); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (l *Loop) evaluateStagedBatch(ctx context.Context, run *OptimizationRun, staged []*stagedOrganism) error {
	if len(staged) == 0 {
		return nil
	}
	workflowIDs := make([]string, 0, len(staged))
	for _, item := range staged {
		if item != nil && strings.TrimSpace(item.EvaluationWorkflow) != "" {
			workflowIDs = append(workflowIDs, item.EvaluationWorkflow)
		}
	}
	runIDs, err := l.Evaluator.RunBatchBenchmark(ctx, RunBatchBenchmarkRequest{
		WorkflowIDs: workflowIDs,
		Benchmark:   run.Benchmark,
		Split:       run.Split,
		ItemLimit:   run.ItemLimit,
		Concurrency: run.Concurrency,
		Meta: map[string]string{
			"source":     "optimizer",
			"opt_run_id": run.ID,
		},
	})
	if err != nil {
		return fmt.Errorf("start batch benchmark evaluation: %w", err)
	}
	if err := l.Evaluator.WaitForCompletion(ctx); err != nil {
		return fmt.Errorf("wait batch benchmark completion: %w", err)
	}

	for _, item := range staged {
		if item == nil || item.Organism == nil {
			continue
		}
		runID := strings.TrimSpace(runIDs[item.EvaluationWorkflow])
		if runID == "" {
			return fmt.Errorf("batch benchmark missing run ID for workflow %s", item.EvaluationWorkflow)
		}
		fitness, err := l.Evaluator.GetRunSummary(ctx, runID)
		if err != nil {
			return fmt.Errorf("benchmark summary %s: %w", runID, err)
		}
		fitness.CompositeScore = ComputeCompositeScore(fitness, run.Spec.Objectives)
		fitness.Feasible, fitness.ConstraintViolations = CheckConstraints(fitness, run.Spec.Constraints)

		now := time.Now().UTC()
		item.Organism.BenchRunID = runID
		item.Organism.WorkflowJSON = item.CandidateWorkflow
		item.Organism.Fitness = fitness
		item.Organism.EvaluatedAt = &now
		if err := l.Store.UpdateOrganismFitness(ctx, item.Organism.ID, runID, fitness); err != nil {
			return fmt.Errorf("persist organism fitness: %w", err)
		}

		run.SpentUSD += fitness.TotalCost
		if isFitnessBetter(fitness, run.BestFitness) {
			run.BestFitness = fitness
			run.BestOrganismID = item.Organism.ID
		}

		delta := 0.0
		parentID := ""
		if item.Parent != nil {
			parentID = item.Parent.ID
			if item.Parent.Fitness != nil {
				delta = fitness.CompositeScore - item.Parent.Fitness.CompositeScore
			}
		}
		entry := &LearningEntry{
			Generation:   item.Organism.Generation,
			OrganismID:   item.Organism.ID,
			ParentID:     parentID,
			MutationType: item.Organism.MutationType,
			Description:  item.Organism.MutationLog,
			Outcome:      classifyOutcome(fitness, delta),
			FitnessDelta: delta,
			CreatedAt:    now,
		}
		if err := l.Store.AppendLearningEntry(ctx, run.ID, entry); err != nil {
			return fmt.Errorf("append learning entry: %w", err)
		}
	}
	return nil
}

func (l *Loop) evaluateStagedBatchTransient(
	ctx context.Context,
	run *OptimizationRun,
	staged []*stagedOrganism,
	itemLimit int,
) (map[string]transientEvaluatedCandidate, int, error) {
	results := make(map[string]transientEvaluatedCandidate, len(staged))
	if len(staged) == 0 {
		return results, 0, nil
	}
	workflowIDs := make([]string, 0, len(staged))
	for _, item := range staged {
		if item != nil && strings.TrimSpace(item.EvaluationWorkflow) != "" {
			workflowIDs = append(workflowIDs, item.EvaluationWorkflow)
		}
	}
	runIDs, err := l.Evaluator.RunBatchBenchmark(ctx, RunBatchBenchmarkRequest{
		WorkflowIDs: workflowIDs,
		Benchmark:   run.Benchmark,
		Split:       run.Split,
		ItemLimit:   itemLimit,
		Concurrency: run.Concurrency,
		Meta: map[string]string{
			"source":      "optimizer",
			"opt_run_id":  run.ID,
			"opt_scope":   "dspy_minibatch",
			"sample_seed": fmt.Sprintf("%d", l.loopRand().Int63()),
		},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("start transient batch benchmark evaluation: %w", err)
	}
	if err := l.Evaluator.WaitForCompletion(ctx); err != nil {
		return nil, 0, fmt.Errorf("wait transient batch benchmark completion: %w", err)
	}
	metricCalls := 0
	for _, item := range staged {
		if item == nil || item.Organism == nil {
			continue
		}
		runID := strings.TrimSpace(runIDs[item.EvaluationWorkflow])
		if runID == "" {
			return nil, 0, fmt.Errorf("transient batch benchmark missing run ID for workflow %s", item.EvaluationWorkflow)
		}
		fitness, err := l.Evaluator.GetRunSummary(ctx, runID)
		if err != nil {
			return nil, 0, fmt.Errorf("transient benchmark summary %s: %w", runID, err)
		}
		fitness.CompositeScore = ComputeCompositeScore(fitness, run.Spec.Objectives)
		fitness.Feasible, fitness.ConstraintViolations = CheckConstraints(fitness, run.Spec.Constraints)
		results[item.Organism.ID] = transientEvaluatedCandidate{
			RunID:   runID,
			Fitness: fitness,
		}
		run.SpentUSD += fitness.TotalCost
		metricCalls += metricCallsFromFitness(fitness, itemLimit)
	}
	return results, metricCalls, nil
}

// evaluateStagedSingle evaluates a pre-staged organism and persists fitness
// to the store. The caller owns staging and cleanup lifecycle.
func (l *Loop) evaluateStagedSingle(ctx context.Context, run *OptimizationRun, item *stagedOrganism, itemLimit int) (*Fitness, error) {
	if item == nil || item.Organism == nil {
		return nil, fmt.Errorf("staged organism is nil")
	}
	org := item.Organism

	runID, err := l.Evaluator.RunBenchmark(ctx, RunBenchmarkRequest{
		WorkflowID:  item.EvaluationWorkflow,
		Benchmark:   run.Benchmark,
		Split:       run.Split,
		ItemLimit:   itemLimit,
		Concurrency: run.Concurrency,
		Meta: map[string]string{
			"source":          "optimizer",
			"opt_run_id":      run.ID,
			"opt_organism_id": org.ID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("start benchmark run for organism %s: %w", org.ID, err)
	}
	if err := l.Evaluator.WaitForCompletion(ctx); err != nil {
		return nil, fmt.Errorf("wait benchmark runner completion: %w", err)
	}
	fitness, err := l.Evaluator.GetRunSummary(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("benchmark summary %s: %w", runID, err)
	}
	fitness.CompositeScore = ComputeCompositeScore(fitness, run.Spec.Objectives)
	fitness.Feasible, fitness.ConstraintViolations = CheckConstraints(fitness, run.Spec.Constraints)

	now := time.Now().UTC()
	org.BenchRunID = runID
	org.WorkflowJSON = item.CandidateWorkflow
	org.Fitness = fitness
	org.EvaluatedAt = &now
	if err := l.Store.UpdateOrganismFitness(ctx, org.ID, runID, fitness); err != nil {
		return nil, fmt.Errorf("persist organism fitness: %w", err)
	}

	run.SpentUSD += fitness.TotalCost
	if isFitnessBetter(fitness, run.BestFitness) {
		run.BestFitness = fitness
		run.BestOrganismID = org.ID
	}
	return fitness, nil
}

// evaluateOrganismSingle stages a single organism, evaluates it, persists fitness,
// and cleans up. Use evaluateStagedSingle when the organism is already staged.
func (l *Loop) evaluateOrganismSingle(ctx context.Context, run *OptimizationRun, baseWorkflowJSON json.RawMessage, org *Organism, itemLimit int) (*Fitness, error) {
	if org == nil {
		return nil, fmt.Errorf("organism is nil")
	}
	staged, err := l.stageOrganisms(ctx, run, baseWorkflowJSON, []*Organism{org}, map[string]*Organism{})
	if err != nil {
		return nil, err
	}
	if len(staged) != 1 || staged[0] == nil {
		return nil, fmt.Errorf("failed to stage organism %s", org.ID)
	}
	defer func() {
		_ = l.cleanupStaged(context.Background(), staged)
	}()
	return l.evaluateStagedSingle(ctx, run, staged[0], itemLimit)
}

func rewriteWorkflowIdentity(workflowJSON json.RawMessage, run *OptimizationRun, org *Organism) (string, json.RawMessage, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &raw); err != nil {
		return "", nil, fmt.Errorf("parse materialized workflow for identity rewrite: %w", err)
	}
	workflowID := fmt.Sprintf("opt-%s-g%d-%s", sanitizeID(run.ID), org.Generation, shortID(org.ID))
	raw["id"] = workflowID
	if name, ok := raw["name"].(string); ok {
		raw["name"] = fmt.Sprintf("%s [opt %s]", name, shortID(org.ID))
	} else {
		raw["name"] = fmt.Sprintf("Optimization Candidate %s", shortID(org.ID))
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return "", nil, fmt.Errorf("marshal workflow with rewritten identity: %w", err)
	}
	return workflowID, encoded, nil
}

func rewriteWorkflowIdentityFixedID(workflowJSON json.RawMessage, workflowID string, name string) (json.RawMessage, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &raw); err != nil {
		return nil, fmt.Errorf("parse workflow for identity rewrite: %w", err)
	}
	raw["id"] = workflowID
	if strings.TrimSpace(name) != "" {
		raw["name"] = name
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal rewritten workflow: %w", err)
	}
	return encoded, nil
}

func setChildWorkflowReference(workflowJSON json.RawMessage, childNodeID string, childWorkflowID string) (json.RawMessage, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &raw); err != nil {
		return nil, fmt.Errorf("parse wrapper workflow JSON: %w", err)
	}
	nodesRaw, ok := raw["nodes"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("wrapper workflow nodes is not an array")
	}
	targetNodeID := strings.TrimSpace(childNodeID)
	replaced := false
	for _, nodeRaw := range nodesRaw {
		node, ok := nodeRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if targetNodeID != "" {
			if id, _ := node["id"].(string); id != targetNodeID {
				continue
			}
		}

		nodeType, _ := node["type"].(string)
		nodeType = strings.TrimSpace(strings.ToLower(nodeType))
		if nodeType == "child_workflow" {
			node["child_workflow_id"] = childWorkflowID
			replaced = true
			if targetNodeID != "" {
				break
			}
			continue
		}

		// Also support workflow-file shape (nodes[].data.config.childWorkflowId)
		data, _ := node["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		dataType, _ := data["type"].(string)
		if strings.TrimSpace(strings.ToLower(dataType)) != "child_workflow" {
			continue
		}
		cfg, _ := data["config"].(map[string]interface{})
		if cfg == nil {
			cfg = map[string]interface{}{}
			data["config"] = cfg
		}
		cfg["childWorkflowId"] = childWorkflowID
		replaced = true
		if targetNodeID != "" {
			break
		}
	}
	if !replaced {
		if targetNodeID == "" {
			return nil, fmt.Errorf("wrapper workflow has no child_workflow node")
		}
		return nil, fmt.Errorf("wrapper workflow child node %s not found", targetNodeID)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal wrapper workflow JSON: %w", err)
	}
	return encoded, nil
}
