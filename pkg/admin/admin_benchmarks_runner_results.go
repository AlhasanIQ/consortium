package admin

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

func (s *Server) persistBenchmarkRunResults(plans []benchmarkRunPlan, outcome benchmarkRunOutcome) (int, []string) {
	importedRuns := 0
	saveErrors := make([]string, 0)

	for i := range plans {
		run := &plans[i]
		completedAt := time.Now()
		if completedNanos := atomic.LoadInt64(&run.CompletedAtUnix); completedNanos > 0 {
			completedAt = time.Unix(0, completedNanos)
		}

		startedAt := run.StartedAt
		if run.MergeIntoRunID != "" {
			// Merge mode: load the full item + attempt history from DB
			// (originals preserved, rerun attempts appended), then apply
			// cost enrichment on the complete set.
			merged, mergeStart, mergeErr := s.buildMergedItems(run)
			if mergeErr != nil {
				saveErrors = append(saveErrors, fmt.Sprintf("merge %s: %v", run.RunID, mergeErr))
			} else {
				run.ItemResults = merged
				startedAt = mergeStart
			}
		}

		// Cost enrichment: for normal runs enriches execution results; for
		// merge runs enriches the full merged list (original attempt costs
		// stay from DB, rerun attempt costs get enriched from job records).
		s.applyExecutionUsageToRunResults(run)
		allItems := run.ItemResults

		summary := bench.BuildSummary(
			run.RunID,
			run.Benchmark,
			run.Split,
			run.WorkflowID,
			run.WorkflowName,
			run.DatasetPath,
			startedAt,
			completedAt,
			allItems,
		)
		summary.ExecutionEngine = "admin.backend.native"
		if outcome.FatalGuardTriggered {
			summary.ExecutionEngineNotes = "fatal_guard_triggered=true"
		} else if outcome.CancelEffective {
			summary.ExecutionEngineNotes = "cancelled_by_user=true"
		}
		if run.MergeIntoRunID != "" {
			note := "rerun_merge=true"
			if summary.ExecutionEngineNotes != "" {
				note = summary.ExecutionEngineNotes + ";" + note
			}
			summary.ExecutionEngineNotes = note
		}

		runResult := bench.RunResult{
			Summary: summary,
			Items:   allItems,
		}

		fullPath, _, saveErr := bench.SaveRunResult(bench.DefaultResultDir, runResult)
		if saveErr != nil {
			saveErrors = append(saveErrors, fmt.Sprintf("save %s: %v", run.RunID, saveErr))
		}

		converted := convertBenchRunResult(runResult)
		converted.Summary.ItemLimit = max(run.ItemLimit, 0)
		artifactPath := fullPath
		if artifactPath == "" {
			artifactPath = "save_failed"
		}
		if upsertErr := s.storage.UpsertBenchmarkRunResult(&converted, storage.BenchmarkRunPersistMeta{
			Status:        outcome.Status,
			Source:        run.Source,
			ArtifactPath:  artifactPath,
			OptRunID:      run.OptRunID,
			OptOrganismID: run.OptOrganismID,
		}); upsertErr != nil {
			saveErrors = append(saveErrors, fmt.Sprintf("upsert %s: %v", run.RunID, upsertErr))
		} else {
			importedRuns++
		}

		s.benchmarkRunnerMu.Lock()
		if s.benchmarkRunner != nil {
			s.benchmarkRunner.CompletedRuns++
			s.benchmarkRunner.ImportedRuns = importedRuns
			s.benchmarkRunner.LastUpdateAt = time.Now()
		}
		s.benchmarkRunnerMu.Unlock()
	}

	return importedRuns, saveErrors
}

func (s *Server) finalizeBenchmarkRunner(runErr error, importedRuns int, guard *benchmarkFatalGuard, outcome benchmarkRunOutcome) {
	s.benchmarkRunnerMu.Lock()
	defer s.benchmarkRunnerMu.Unlock()

	if s.benchmarkRunner != nil {
		now := time.Now()
		s.benchmarkRunner.Running = false
		s.benchmarkRunner.FinishedAt = now
		s.benchmarkRunner.LastUpdateAt = now
		s.benchmarkRunner.CurrentRunID = ""
		s.benchmarkRunner.CurrentBenchmark = ""
		s.benchmarkRunner.CurrentWorkflow = ""
		s.benchmarkRunner.CurrentItemID = ""
		s.benchmarkRunner.ImportedRuns = importedRuns
		s.benchmarkRunner.Error = benchmarkRunnerError(runErr, guard, outcome)
	}
	if s.benchmarkSessionID != "" {
		sessionStatus := benchmarkSessionStatus(runErr, outcome.FatalGuardTriggered, outcome.CancelEffective)
		sessionError := ""
		if s.benchmarkRunner != nil && s.benchmarkRunner.Error != "" {
			sessionError = s.benchmarkRunner.Error
		}
		now := time.Now()
		_ = s.storage.UpdateBenchmarkRunnerSession(s.benchmarkSessionID, storage.BenchmarkRunnerSessionUpdate{
			Status:           &sessionStatus,
			Error:            &sessionError,
			CompletedRuns:    &importedRuns,
			ImportedRuns:     &importedRuns,
			FinishedAt:       &now,
			CurrentRunID:     strPtr(""),
			CurrentItemID:    strPtr(""),
			CurrentBenchmark: strPtr(""),
			CurrentWorkflow:  strPtr(""),
		})
		s.benchmarkSessionID = ""
	}
	s.benchmarkCancel = nil
}

func (s *Server) preflightValidateBenchmarkWorkflow(
	def *storage.WorkflowDefinition,
	sampleItem bench.DatasetItem,
) error {
	if def == nil {
		return fmt.Errorf("workflow definition is nil")
	}
	prompt, err := bench.BuildQuestionPrompt(sampleItem)
	if err != nil {
		return fmt.Errorf("build sample benchmark prompt: %w", err)
	}
	probeRun := &benchmarkRunPlan{
		WorkflowID:         def.ID,
		WorkflowName:       def.Name,
		WorkflowDefinition: def.Definition,
	}
	execWorkflow, err := buildBenchmarkExecutionWorkflow(probeRun, sampleItem, prompt)
	if err != nil {
		return fmt.Errorf("convert workflow definition: %w", err)
	}
	result := workflow.NewValidator(s.registry).Validate(execWorkflow)
	if result.Valid {
		return nil
	}
	return fmt.Errorf("workflow DAG validation failed: %s", summarizeValidationErrors(result.Errors, 5))
}

func summarizeValidationErrors(errors []workflow.ValidationError, maxErrors int) string {
	if len(errors) == 0 {
		return "unknown validation error"
	}
	if maxErrors < 1 {
		maxErrors = 1
	}

	capHint := maxErrors
	if len(errors) < capHint {
		capHint = len(errors)
	}
	parts := make([]string, 0, capHint)
	for i, validationErr := range errors {
		if i >= maxErrors {
			break
		}
		field := strings.TrimSpace(validationErr.Field)
		if field == "" {
			field = "workflow"
		}
		if nodeID := strings.TrimSpace(validationErr.NodeID); nodeID != "" {
			field = fmt.Sprintf("%s[%s]", field, nodeID)
		}
		msg := strings.TrimSpace(validationErr.Message)
		if msg == "" {
			msg = "validation failed"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", field, msg))
	}
	if len(errors) > maxErrors {
		parts = append(parts, fmt.Sprintf("... and %d more validation error(s)", len(errors)-maxErrors))
	}
	return strings.Join(parts, "; ")
}
