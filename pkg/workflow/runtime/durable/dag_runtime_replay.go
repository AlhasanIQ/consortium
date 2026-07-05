package durable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func (r *DAGRuntime) applyReplaySeeds(
	ctx context.Context,
	params *runtime.StartParams,
	runID string,
	jobID string,
	nodeIDs []string,
	nodeMap map[string]*workflow.Node,
	totalNodes int,
) (int, error) {
	if params == nil || params.Replay == nil {
		return 0, nil
	}

	replay := params.Replay
	mode := replayModeOrDefault(replay.Mode)
	if mode == runtime.ReplayModeOff {
		return 0, nil
	}
	if len(replay.SeedNodes) == 0 {
		return 0, nil
	}

	if replay.Plan != nil && strings.TrimSpace(replay.Plan.CandidateDAGHash) != "" {
		candidateHash := strings.TrimSpace(replay.Plan.CandidateDAGHash)
		actualHash := strings.TrimSpace(params.Identity.DAGHash)
		if candidateHash != actualHash {
			msg := fmt.Sprintf("replay candidate dag hash mismatch: request=%s runtime=%s", candidateHash, actualHash)
			if mode == runtime.ReplayModeRequired {
				return 0, fmt.Errorf("%s", msg)
			}
			log.Printf("Replay skipped: %s", msg)
			return 0, nil
		}
	}

	seeded := 0
	for idx, nodeID := range nodeIDs {
		seed, ok := replay.SeedNodes[nodeID]
		if !ok {
			continue
		}

		node := nodeMap[nodeID]
		if node == nil {
			msg := fmt.Sprintf("replay seed node %s missing from workflow definition", nodeID)
			if mode == runtime.ReplayModeRequired {
				return seeded, fmt.Errorf("%s", msg)
			}
			log.Printf("Replay seed skipped: %s", msg)
			continue
		}

		normalizedSeedID := strings.TrimSpace(seed.NodeID)
		if normalizedSeedID != "" && normalizedSeedID != nodeID {
			msg := fmt.Sprintf("replay seed node mismatch: map key=%s payload=%s", nodeID, normalizedSeedID)
			if mode == runtime.ReplayModeRequired {
				return seeded, fmt.Errorf("%s", msg)
			}
			log.Printf("Replay seed skipped: %s", msg)
			continue
		}

		// Build metadata with replay provenance (copy to avoid mutating seed).
		replayMeta := make(map[string]interface{}, len(seed.Metadata)+2)
		for k, v := range seed.Metadata {
			replayMeta[k] = v
		}
		replayMeta["replayed"] = true
		if seed.SourceJobID != "" {
			replayMeta["source_job_id"] = seed.SourceJobID
		}

		// Replayed nodes have zero cost/tokens — no actual API call was made.
		// The output is reused from the baseline; original costs are preserved
		// in history attributes as seed_* fields for audit purposes.
		output := &runtime.ActivityOutput{
			NodeID:   nodeID,
			Success:  true,
			Output:   seed.Output,
			Metadata: replayMeta,
		}

		activityID := fmt.Sprintf("%s:%s:%d", jobID, nodeID, 1)
		if err := r.appendHistoryBatch(ctx, runID, []historyBatchEntry{
			{
				eventType:  runtime.HistoryScheduleActivity,
				nodeID:     nodeID,
				activityID: activityID,
				attrs: map[string]interface{}{
					"attempt":       float64(1),
					"activity_type": string(runtime.NodeTypeToActivityType(node.Type)),
					"replayed":      true,
					"source_job_id": seed.SourceJobID,
				},
			},
			{
				eventType:  runtime.HistoryActivityStarted,
				nodeID:     nodeID,
				activityID: activityID,
				attrs: map[string]interface{}{
					"attempt":  float64(1),
					"replayed": true,
				},
			},
			{
				eventType:  runtime.HistoryActivityCompleted,
				nodeID:     nodeID,
				activityID: activityID,
				attrs: map[string]interface{}{
					"output":             seed.Output,
					"tokens_input":       0,
					"tokens_output":      0,
					"cost":               float64(0),
					"output_checksum":    replayOutputChecksum(seed),
					"replayed":           true,
					"source_job_id":      seed.SourceJobID,
					"source_run_id":      seed.SourceRunID,
					"seed_tokens_input":  seed.TokensInput,
					"seed_tokens_output": seed.TokensOutput,
					"seed_cost":          seed.Cost,
				},
			},
		}); err != nil {
			return seeded, fmt.Errorf("append replay seed history for node %s: %w", nodeID, err)
		}

		now := time.Now()
		if err := r.store.UpsertNodeExecutionAttempt(context.Background(), &storage.NodeExecutionAttempt{
			JobID:       jobID,
			ExecutionID: params.Identity.WorkflowExecutionID,
			RunID:       runID,
			NodeID:      nodeID,
			NodeType:    string(node.Type),
			Attempt:     1,
			Status:      "completed",
			ActivityID:  activityID,
			StartedAt:   &now,
			CompletedAt: &now,
			Metadata:    replayMeta,
		}); err != nil {
			log.Printf("Warning: failed to persist replay node attempt for %s: %v", nodeID, err)
		}

		// Keep projection/storage + stream event behavior consistent with normal completion.
		r.emitNodeComplete(params, node, output, jobID)
		r.saveWorkflowNode(jobID, runID, params.Identity.WorkflowExecutionID, node, output, 1, activityID, idx+1, totalNodes)

		seeded++
	}

	if seeded == 0 && mode == runtime.ReplayModeRequired {
		return 0, fmt.Errorf("replay mode required but no seed nodes were applied")
	}
	return seeded, nil
}

func applyReplayWorkflowContext(workflowCtx map[string]interface{}, replay *runtime.ReplayRequest) {
	if workflowCtx == nil || replay == nil || len(replay.SeedNodes) == 0 {
		return
	}

	for nodeID, seed := range replay.SeedNodes {
		workflowCtx[nodeID] = seed.Output
		if len(seed.Metadata) == 0 {
			continue
		}
		outputName, _ := seed.Metadata["output_name"].(string)
		outputName = strings.TrimSpace(outputName)
		if outputName == "" {
			continue
		}
		outputValue := seed.Output
		if raw, ok := seed.Metadata["output_value"]; ok {
			outputValue = fmt.Sprintf("%v", raw)
		}
		workflowCtx["__output_"+outputName] = outputValue
	}
}

func replayModeOrDefault(mode runtime.ReplayMode) runtime.ReplayMode {
	switch mode {
	case runtime.ReplayModeOff, runtime.ReplayModeBestEffort, runtime.ReplayModeRequired:
		return mode
	default:
		return runtime.ReplayModeBestEffort
	}
}

func replayOutputChecksum(seed runtime.ReplaySeedNode) string {
	if trimmed := strings.TrimSpace(seed.OutputChecksum); trimmed != "" {
		return trimmed
	}
	sum := sha256.Sum256([]byte(seed.Output))
	return hex.EncodeToString(sum[:])
}
