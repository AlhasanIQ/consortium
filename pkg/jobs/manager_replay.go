package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/compiler"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func (m *Manager) loadReplayRequestForJob(ctx context.Context, jobID string) (*workflowruntime.ReplayRequest, error) {
	replayJSON, err := m.storage.GetExecutionReplayRequest(ctx, jobID)
	if err != nil {
		return nil, err
	}
	replayJSON = strings.TrimSpace(replayJSON)
	if replayJSON == "" {
		return nil, nil
	}

	var replay workflowruntime.ReplayRequest
	if err := json.Unmarshal([]byte(replayJSON), &replay); err != nil {
		return nil, fmt.Errorf("parse replay request JSON for job %s: %w", jobID, err)
	}
	return &replay, nil
}

func (m *Manager) resolveChildReplayRequest(
	ctx context.Context,
	req *workflow.ChildWorkflowRequest,
	childWorkflow *workflow.Workflow,
	parentReplay *workflowruntime.ReplayRequest,
) (*workflowruntime.ReplayRequest, error) {
	if req == nil || childWorkflow == nil || parentReplay == nil || len(parentReplay.ChildByNode) == 0 {
		return nil, nil
	}

	template := parentReplay.ChildByNode[req.ParentNodeID]
	if template == nil {
		return nil, nil
	}
	return m.materializeReplayRequest(ctx, template, childWorkflow, nil)
}

func (m *Manager) materializeReplayRequest(
	ctx context.Context,
	template *workflowruntime.ReplayRequest,
	candidateWorkflow *workflow.Workflow,
	forceDirtyNodeIDs []string,
) (*workflowruntime.ReplayRequest, error) {
	if template == nil {
		return nil, nil
	}

	mode := normalizeReplayMode(template.Mode)
	if mode == workflowruntime.ReplayModeOff {
		return nil, nil
	}

	// Already materialized (plan + seeds present): use as-is.
	if template.Plan != nil || len(template.SeedNodes) > 0 {
		return template, nil
	}

	sourceJobID := strings.TrimSpace(template.BaseJobID)
	if sourceJobID == "" {
		// Backward compatibility: BaseRunID was previously used by callers.
		sourceJobID = strings.TrimSpace(template.BaseRunID)
	}
	if sourceJobID == "" {
		if mode == workflowruntime.ReplayModeRequired {
			return nil, fmt.Errorf("replay mode required but base_job_id is empty")
		}
		return nil, nil
	}

	replay, err := m.BuildReplayRequest(ctx, sourceJobID, candidateWorkflow, mode, forceDirtyNodeIDs)
	if err != nil {
		if mode == workflowruntime.ReplayModeRequired {
			return nil, err
		}
		log.Printf("Replay materialization skipped for source job %s: %v", sourceJobID, err)
		return nil, nil
	}
	return replay, nil
}

// BuildReplayRequest builds a replay plan + seed payload by comparing the
// candidate workflow against a baseline job's frozen DAG and node outputs.
func (m *Manager) BuildReplayRequest(
	ctx context.Context,
	sourceJobID string,
	candidateWorkflow *workflow.Workflow,
	mode workflowruntime.ReplayMode,
	forceDirtyNodeIDs []string,
) (*workflowruntime.ReplayRequest, error) {
	sourceJobID = strings.TrimSpace(sourceJobID)
	if sourceJobID == "" {
		return nil, fmt.Errorf("source job id is required")
	}
	if candidateWorkflow == nil {
		return nil, fmt.Errorf("candidate workflow is nil")
	}

	sourceJob, err := m.storage.GetExecution(sourceJobID)
	if err != nil {
		return nil, fmt.Errorf("load source job %s: %w", sourceJobID, err)
	}
	if strings.TrimSpace(sourceJob.DAGSnapshot) == "" {
		return nil, fmt.Errorf("source job %s has no dag snapshot", sourceJobID)
	}

	compiledCandidate, _, err := compiler.Compile(ctx, candidateWorkflow, workflowDefinitionResolver{store: m.storage})
	if err != nil {
		return nil, fmt.Errorf("compile candidate workflow references: %w", err)
	}
	candidateSnapshot, err := freezeWorkflowForSubmit(compiledCandidate)
	if err != nil {
		return nil, fmt.Errorf("freeze candidate workflow: %w", err)
	}
	baseSnapshot := &workflowruntime.FrozenSnapshot{
		DAGHash:    sourceJob.DAGHash,
		Definition: []byte(sourceJob.DAGSnapshot),
	}

	plan, err := workflowruntime.BuildReplayPlan(baseSnapshot, candidateSnapshot, &workflowruntime.ReplayPlanOptions{
		ForceDirtyNodeIDs: forceDirtyNodeIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("build replay plan: %w", err)
	}

	seedNodes, missingSeeds, err := buildReplaySeedNodesFromJob(m.storage, sourceJob, plan.ReuseNodeIDs)
	if err != nil {
		return nil, err
	}

	normalizedMode := normalizeReplayMode(mode)
	if normalizedMode == workflowruntime.ReplayModeRequired {
		if len(plan.ReuseNodeIDs) > 0 && len(seedNodes) == 0 {
			return nil, fmt.Errorf("replay mode required but no seed nodes are available from source job %s", sourceJobID)
		}
		if missingSeeds > 0 {
			return nil, fmt.Errorf("replay mode required but %d seed node(s) are missing from source job %s", missingSeeds, sourceJobID)
		}
	}
	if missingSeeds > 0 && normalizedMode == workflowruntime.ReplayModeBestEffort {
		log.Printf("Replay plan for source job %s missing %d seed node(s); best-effort mode will execute them live", sourceJobID, missingSeeds)
	}

	return &workflowruntime.ReplayRequest{
		Mode:      normalizedMode,
		BaseRunID: sourceJob.RunID,
		BaseJobID: sourceJobID,
		Plan:      plan,
		SeedNodes: seedNodes,
	}, nil
}

func buildReplaySeedNodesFromJob(
	store *storage.Storage,
	sourceJob *storage.WorkflowExecution,
	reuseNodeIDs []string,
) (map[string]workflowruntime.ReplaySeedNode, int, error) {
	if sourceJob == nil {
		return nil, 0, fmt.Errorf("source job is nil")
	}
	if len(reuseNodeIDs) == 0 {
		return map[string]workflowruntime.ReplaySeedNode{}, 0, nil
	}

	nodes, err := store.GetWorkflowNodes(sourceJob.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("load workflow nodes for source job %s: %w", sourceJob.ID, err)
	}
	nodeByID := make(map[string]storage.WorkflowNode, len(nodes))
	for _, node := range nodes {
		nodeByID[node.NodeID] = node
	}

	sourceRunID := sourceJob.RunID
	if strings.TrimSpace(sourceRunID) == "" {
		sourceRunID = sourceJob.ID
	}

	seedNodes := make(map[string]workflowruntime.ReplaySeedNode, len(reuseNodeIDs))
	missing := 0
	for _, nodeID := range reuseNodeIDs {
		node, ok := nodeByID[nodeID]
		if !ok {
			missing++
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(node.Status), events.JobStatusCompleted) {
			missing++
			continue
		}

		var metadata map[string]interface{}
		if raw := strings.TrimSpace(node.Metadata); raw != "" {
			_ = json.Unmarshal([]byte(raw), &metadata)
		}

		outputChecksum := checksumString(node.Output)
		seedNodes[nodeID] = workflowruntime.ReplaySeedNode{
			NodeID:         nodeID,
			Output:         node.Output,
			TokensInput:    node.TokensInput,
			TokensOutput:   node.TokensOutput,
			Cost:           node.Cost,
			Metadata:       metadata,
			SourceJobID:    sourceJob.ID,
			SourceRunID:    sourceRunID,
			SourceAttempt:  node.AttemptNumber,
			OutputChecksum: outputChecksum,
		}
	}

	return seedNodes, missing, nil
}

func normalizeReplayMode(mode workflowruntime.ReplayMode) workflowruntime.ReplayMode {
	switch mode {
	case workflowruntime.ReplayModeOff, workflowruntime.ReplayModeBestEffort, workflowruntime.ReplayModeRequired:
		return mode
	default:
		return workflowruntime.ReplayModeBestEffort
	}
}

func checksumString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
