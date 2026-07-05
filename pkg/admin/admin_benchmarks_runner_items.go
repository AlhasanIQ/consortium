package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func (s *Server) executeBenchmarkWorkItem(
	ctx context.Context,
	cfg benchmarkRunRequest,
	guard *benchmarkFatalGuard,
	plans []benchmarkRunPlan,
	runIndex, itemIndex int,
) {
	run := &plans[runIndex]
	item := run.Items[itemIndex]

	s.benchmarkRunnerMu.Lock()
	if s.benchmarkRunner != nil && s.benchmarkRunner.Running {
		s.benchmarkRunner.CurrentRunID = run.RunID
		s.benchmarkRunner.CurrentBenchmark = run.Benchmark
		s.benchmarkRunner.CurrentWorkflow = run.WorkflowID
		s.benchmarkRunner.CurrentItemID = item.ID
		s.benchmarkRunner.LastUpdateAt = time.Now()
	}
	s.benchmarkRunnerMu.Unlock()

	result := s.executeBenchmarkItem(ctx, guard, run, item, cfg.MaxNonLetterRetries, cfg.MaxTransientRetries)
	run.ItemResults[itemIndex] = result

	// Persist item result incrementally (crash recovery).
	// Merge mode appends attempts (preserving originals); normal mode replaces.
	if itemInput := convertSingleBenchmarkItem(run.RunID, result); itemInput != nil {
		var persistErr error
		if run.MergeIntoRunID != "" {
			persistErr = s.storage.AppendBenchmarkRunItemResult(run.RunID, itemInput)
		} else {
			persistErr = s.storage.UpsertBenchmarkRunItem(run.RunID, itemInput)
		}
		if persistErr != nil {
			log.Printf("Warning: failed to persist benchmark item %s/%s: %v", run.RunID, item.ID, persistErr)
		}
	}

	// Per-run atomics: safe because RunConcurrently's WaitGroup
	// provides happens-before ordering for post-loop reads.
	completed := int(atomic.AddInt64(&run.CompletedItems, 1))
	if completed == len(run.Items) {
		atomic.CompareAndSwapInt64(&run.CompletedAtUnix, 0, time.Now().UnixNano())
	}

	// Global runner state: protected by benchmarkRunnerMu (different
	// scope than per-run atomics above). Read by status endpoint.
	var sessionUpdate *storage.BenchmarkRunnerSessionUpdate
	s.benchmarkRunnerMu.Lock()
	if s.benchmarkRunner != nil && s.benchmarkRunner.Running {
		s.benchmarkRunner.CompletedItems++
		if result.Correct {
			s.benchmarkRunner.CorrectItems++
		} else {
			s.benchmarkRunner.IncorrectItems++
		}
		s.benchmarkRunner.LastUpdateAt = time.Now()
		if s.benchmarkSessionID != "" {
			ci := s.benchmarkRunner.CompletedItems
			co := s.benchmarkRunner.CorrectItems
			ic := s.benchmarkRunner.IncorrectItems
			sessionUpdate = &storage.BenchmarkRunnerSessionUpdate{
				CompletedItems:   &ci,
				CorrectItems:     &co,
				IncorrectItems:   &ic,
				CurrentRunID:     strPtr(run.RunID),
				CurrentItemID:    strPtr(item.ID),
				CurrentBenchmark: strPtr(run.Benchmark),
				CurrentWorkflow:  strPtr(run.WorkflowID),
			}
		}
	}
	s.benchmarkRunnerMu.Unlock()
	if sessionUpdate != nil {
		_ = s.storage.UpdateBenchmarkRunnerSession(s.benchmarkSessionID, *sessionUpdate)
	}
}

func (s *Server) executeBenchmarkItem(
	ctx context.Context,
	guard *benchmarkFatalGuard,
	run *benchmarkRunPlan,
	item bench.DatasetItem,
	maxNonLetterRetries int,
	maxTransientRetries int,
) bench.ItemResult {
	const maxAdmissionRetries = 120

	if paused, cancelled := pausedBenchmarkItemResultIfCancelled(ctx, guard, run, item); cancelled {
		return paused
	}

	result := newBenchmarkItemResult(run, item)
	format := bench.FormatForBenchmark(run.Benchmark)

	prompt, err := bench.BuildQuestionPrompt(item)
	if err != nil {
		return finalizeBenchmarkAttemptErrorResult(result, item, 1, err.Error())
	}

	transientRetries := 0
	for attempt := 0; ; attempt++ {
		if paused, cancelled := pausedBenchmarkItemResultIfCancelled(ctx, guard, run, item); cancelled {
			return paused
		}

		attemptDetail := bench.AttemptDetail{
			Attempt:      attempt + 1,
			OutputSource: bench.OutputSourceNone,
		}

		execWorkflow, err := buildBenchmarkExecutionWorkflow(run, item, prompt)
		if err != nil {
			return finalizeBenchmarkAttemptErrorResult(result, item, attempt+1, err.Error())
		}

		replayRequest, replayErr := s.buildBenchmarkReplayRequestForItem(ctx, run, item, execWorkflow)
		if replayErr != nil {
			return finalizeBenchmarkAttemptErrorResult(result, item, attempt+1, replayErr.Error())
		}

		execResult, err := s.executeBenchmarkWorkflowAttempt(ctx, execWorkflow, replayRequest, run.AdmissionBypass)
		if err != nil {
			if paused, cancelled := pausedBenchmarkItemResultIfCancelled(ctx, guard, run, item); cancelled {
				return paused
			}
			classifyAttemptError(&attemptDetail, err.Error())
			result.AttemptDetails = append(result.AttemptDetails, attemptDetail)

			maybeTriggerBenchmarkFatalGuard(guard, run.Benchmark, run.WorkflowID, item.ID, attemptDetail, nil)

			if shouldRetryTransientExecutionFailure(attemptDetail.FailureReason, attemptDetail.Error, "") &&
				transientRetries < maxTransientRetries {
				transientRetries++
				time.Sleep(transientRetryDelay(transientRetries))
				continue
			}
			finalizeBenchmarkItemFromAttempt(&result, item, attemptDetail)
			return result
		}
		if execResult == nil {
			classifyAttemptError(&attemptDetail, "nil execution result")
			result.AttemptDetails = append(result.AttemptDetails, attemptDetail)
			finalizeBenchmarkItemFromAttempt(&result, item, attemptDetail)
			return result
		}
		attemptDetail.JobID = execResult.JobID

		if !execResult.Success || execResult.Result == nil {
			classifyAttemptError(&attemptDetail, benchmarkExecutionFailureMessage(execResult))
			result.AttemptDetails = append(result.AttemptDetails, attemptDetail)

			maybeTriggerBenchmarkFatalGuard(guard, run.Benchmark, run.WorkflowID, item.ID, attemptDetail, execResult)

			if shouldRetryAdmissionExhaustion(execResult, attemptDetail.Error) && attempt+1 < maxAdmissionRetries {
				time.Sleep(admissionRetryDelay(attempt))
				continue
			}
			if shouldRetryTransientExecutionFailure(attemptDetail.FailureReason, attemptDetail.Error, execResult.ErrorCode) &&
				transientRetries < maxTransientRetries {
				transientRetries++
				time.Sleep(transientRetryDelay(transientRetries))
				continue
			}

			finalizeBenchmarkItemFromAttempt(&result, item, attemptDetail)
			return result
		}

		wfResult := execResult.Result
		accumulateBenchmarkAttemptMetrics(&result, &attemptDetail, wfResult)

		output, source, canonicalPresent := bench.ExtractCanonicalOutputForFormat(format, wfResult.Outputs, wfResult.FinalOutput)
		attemptDetail.RawOutput = output
		attemptDetail.OutputSource = source

		diag := extractContractNodeDiagnostics(wfResult.NodeResults)
		applyContractNodeDiagnostics(&attemptDetail, diag)

		classification := bench.FailureClassification{}
		if format == bench.BenchmarkFormatMathAnswer {
			classification = classifyMathAttemptOutput(output, canonicalPresent)
		} else {
			classification = bench.ClassifyFailure(bench.FailureClassificationInput{
				CanonicalOutput:  output,
				CanonicalPresent: canonicalPresent,
				ChoiceCount:      len(item.Choices),
				Outputs:          wfResult.Outputs,
			})
		}
		if classification.Reason == "" {
			attemptDetail.ParseOK = true
			attemptDetail.Predicted = classification.Predicted
			result.AttemptDetails = append(result.AttemptDetails, attemptDetail)
			finalizeBenchmarkItemFromAttempt(&result, item, attemptDetail)
			return result
		}

		attemptDetail.ParseOK = false
		attemptDetail.Predicted = classification.Predicted
		attemptDetail.FailureReason = classification.Reason

		// Reclassify as truncation when the contract node hit the token limit.
		if attemptDetail.ContractFinishReason == "length" {
			attemptDetail.FailureReason = bench.FailureReasonContractTruncated
		}

		enrichContractFailureDetails(&attemptDetail)
		result.AttemptDetails = append(result.AttemptDetails, attemptDetail)

		if shouldRetryBenchmarkContractFailure(attemptDetail.FailureReason) && result.NonLetterRetries < maxNonLetterRetries {
			result.NonLetterRetries++
			continue
		}

		finalizeBenchmarkItemFromAttempt(&result, item, attemptDetail)
		return result
	}
}

func buildBenchmarkExecutionWorkflow(run *benchmarkRunPlan, item bench.DatasetItem, prompt string) (*workflow.Workflow, error) {
	execWorkflow, err := jobs.WorkflowFromDefinition(run.WorkflowDefinition, map[string]interface{}{
		"user_prompt": prompt,
	})
	if err != nil {
		return nil, err
	}
	execWorkflow.ID = run.WorkflowID
	execWorkflow.Name = run.WorkflowName
	if err := applyPerOptionToolcallContract(execWorkflow, item); err != nil {
		return nil, err
	}
	return execWorkflow, nil
}

func (s *Server) executeBenchmarkWorkflowAttempt(
	ctx context.Context,
	execWorkflow *workflow.Workflow,
	replayRequest *workflowruntime.ReplayRequest,
	admissionBypass bool,
) (*jobs.WorkflowExecutionResult, error) {
	const perItemTimeout = 30 * time.Minute
	itemCtx, itemCancel := context.WithTimeout(ctx, perItemTimeout)
	defer itemCancel()
	return s.jobManager.ExecuteWorkflowWithReplayOptions(itemCtx, execWorkflow, replayRequest, jobs.ExecuteWorkflowOptions{
		AdmissionBypass: admissionBypass,
	})
}

func (s *Server) buildBenchmarkReplayRequestForItem(
	ctx context.Context,
	run *benchmarkRunPlan,
	item bench.DatasetItem,
	execWorkflow *workflow.Workflow,
) (*workflowruntime.ReplayRequest, error) {
	if run == nil || run.Replay == nil || execWorkflow == nil {
		return nil, nil
	}

	mode := run.Replay.Mode
	if mode == workflowruntime.ReplayModeOff {
		return nil, nil
	}

	sourceParentJobID := strings.TrimSpace(run.Replay.SourceParentJobByItem[item.ID])
	if sourceParentJobID == "" {
		if mode == workflowruntime.ReplayModeRequired {
			return nil, fmt.Errorf("replay required but no baseline parent job found for item %s", item.ID)
		}
		return nil, nil
	}

	forceDirty := forcedDirtyParentNodeIDs(execWorkflow, run.Replay.ChangedWorkflowIDs)
	replayReq, err := s.jobManager.BuildReplayRequest(ctx, sourceParentJobID, execWorkflow, mode, forceDirty)
	if err != nil {
		if mode == workflowruntime.ReplayModeRequired {
			return nil, err
		}
		log.Printf("Replay request skipped for item %s source job %s: %v", item.ID, sourceParentJobID, err)
		return nil, nil
	}
	if replayReq == nil {
		return nil, nil
	}

	if len(run.Replay.ChangedWorkflowIDs) == 0 {
		return replayReq, nil
	}

	childSourceByNode, err := s.loadChildSourceJobsByParentNode(sourceParentJobID)
	if err != nil {
		if mode == workflowruntime.ReplayModeRequired {
			return nil, err
		}
		log.Printf("Replay child source lookup skipped for parent %s: %v", sourceParentJobID, err)
		return replayReq, nil
	}

	for _, node := range execWorkflow.Nodes {
		if node == nil || node.Type != workflow.NodeTypeChildWorkflow {
			continue
		}
		childWorkflowID := strings.TrimSpace(node.ChildWorkflowID)
		if childWorkflowID == "" {
			continue
		}
		if _, changed := run.Replay.ChangedWorkflowIDs[childWorkflowID]; !changed {
			continue
		}

		sourceChildJobID := strings.TrimSpace(childSourceByNode[node.ID])
		if sourceChildJobID == "" {
			if mode == workflowruntime.ReplayModeRequired {
				return nil, fmt.Errorf("replay required but no source child job found for parent node %s", node.ID)
			}
			log.Printf("Replay child seed skipped: no source child job for parent node %s", node.ID)
			continue
		}
		if replayReq.ChildByNode == nil {
			replayReq.ChildByNode = make(map[string]*workflowruntime.ReplayRequest)
		}
		replayReq.ChildByNode[node.ID] = &workflowruntime.ReplayRequest{
			Mode:      mode,
			BaseRunID: sourceChildJobID,
			BaseJobID: sourceChildJobID,
		}
	}

	return replayReq, nil
}

func forcedDirtyParentNodeIDs(execWorkflow *workflow.Workflow, changedWorkflowIDs map[string]struct{}) []string {
	if execWorkflow == nil || len(changedWorkflowIDs) == 0 {
		return nil
	}
	forced := make([]string, 0)
	seen := make(map[string]struct{})
	for _, node := range execWorkflow.Nodes {
		if node == nil || node.Type != workflow.NodeTypeChildWorkflow {
			continue
		}
		childWorkflowID := strings.TrimSpace(node.ChildWorkflowID)
		if childWorkflowID == "" {
			continue
		}
		if _, changed := changedWorkflowIDs[childWorkflowID]; !changed {
			continue
		}
		if _, exists := seen[node.ID]; exists {
			continue
		}
		seen[node.ID] = struct{}{}
		forced = append(forced, node.ID)
	}
	sort.Strings(forced)
	return forced
}

func (s *Server) loadChildSourceJobsByParentNode(parentJobID string) (map[string]string, error) {
	nodes, err := s.storage.GetWorkflowNodes(parentJobID)
	if err != nil {
		return nil, fmt.Errorf("load parent workflow nodes %s: %w", parentJobID, err)
	}

	childByNode := make(map[string]string)
	for _, node := range nodes {
		if !strings.EqualFold(strings.TrimSpace(node.NodeType), string(workflow.NodeTypeChildWorkflow)) {
			continue
		}
		if strings.TrimSpace(node.Metadata) == "" {
			continue
		}

		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(node.Metadata), &metadata); err != nil {
			continue
		}
		sourceChildJobID, _ := metadata["child_job_id"].(string)
		sourceChildJobID = strings.TrimSpace(sourceChildJobID)
		if sourceChildJobID == "" {
			continue
		}
		childByNode[node.NodeID] = sourceChildJobID
	}
	return childByNode, nil
}
