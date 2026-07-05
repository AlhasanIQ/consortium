package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/storage"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// --- Runner types ---

type benchmarkRunnerState struct {
	Running          bool      `json:"running"`
	StartedAt        time.Time `json:"started_at,omitempty"`
	FinishedAt       time.Time `json:"finished_at,omitempty"`
	Command          string    `json:"command,omitempty"`
	Error            string    `json:"error,omitempty"`
	ImportedRuns     int       `json:"imported_runs,omitempty"`
	TotalRuns        int       `json:"total_runs,omitempty"`
	CompletedRuns    int       `json:"completed_runs,omitempty"`
	TotalItems       int       `json:"total_items,omitempty"`
	CompletedItems   int       `json:"completed_items,omitempty"`
	CorrectItems     int       `json:"correct_items,omitempty"`
	IncorrectItems   int       `json:"incorrect_items,omitempty"`
	CurrentRunID     string    `json:"current_run_id,omitempty"`
	CurrentBenchmark string    `json:"current_benchmark,omitempty"`
	CurrentWorkflow  string    `json:"current_workflow,omitempty"`
	CurrentItemID    string    `json:"current_item_id,omitempty"`
	CancelRequested  bool      `json:"cancel_requested,omitempty"`
	LastUpdateAt     time.Time `json:"last_update_at,omitempty"`

	// Run config (echoed back so the UI can pre-fill the form).
	Benchmarks          []string `json:"benchmarks,omitempty"`
	Workflows           []string `json:"workflows,omitempty"`
	RunSet              string   `json:"run_set,omitempty"`
	Split               string   `json:"split,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	Concurrency         int      `json:"concurrency,omitempty"`
	MaxNonLetterRetries int      `json:"max_non_letter_retries,omitempty"`
	MaxTransientRetries int      `json:"max_transient_retries,omitempty"`
}

type benchmarkRunPlan struct {
	Benchmark          string
	Split              string
	DatasetPath        string
	ItemLimit          int
	Source             string
	OptRunID           string
	OptOrganismID      string
	WorkflowID         string
	WorkflowName       string
	WorkflowDefinition string
	RunID              string
	StartedAt          time.Time
	Items              []bench.DatasetItem
	ItemResults        []bench.ItemResult
	AdmissionBypass    bool
	Replay             *benchmarkReplaySpec

	// MergeIntoRunID, when set, means this is a rerun-failures plan whose
	// results should be stitched back into the original run rather than
	// creating a separate run record. RunID == MergeIntoRunID in this case.
	MergeIntoRunID string

	// CompletedItems and CompletedAtUnix are updated via atomic operations
	// from concurrent goroutines within RunConcurrently. They are read
	// (via atomic.LoadInt64) only after RunConcurrently returns, which
	// provides happens-before ordering via sync.WaitGroup.
	CompletedItems  int64
	CompletedAtUnix int64
}

type benchmarkReplaySpec struct {
	Mode                  workflowruntime.ReplayMode
	BaseRunID             string
	ChangedWorkflowIDs    map[string]struct{}
	SourceParentJobByItem map[string]string
}

type benchmarkWorkItem struct {
	RunIndex  int
	ItemIndex int
}

type benchmarkRunRequest struct {
	Benchmarks           []string
	Workflows            []string
	RunSet               string
	Split                string
	Limit                int
	Source               string
	OptRunID             string
	OptOrganismID        string
	OptScope             string
	SampleMode           string
	SampleSeed           int64
	Concurrency          int
	MaxNonLetterRetries  int
	MaxTransientRetries  int
	AdmissionBypass      bool
	PauseOnFatal         bool
	FatalRepeatThreshold int
}

const (
	benchmarkSampleModeHead   = "head"
	benchmarkSampleModeRandom = "random"
)

// --- Fatal guard types ---

// benchmarkFatalCause captures diagnostic context when a fatal (unrecoverable
// or repeated non-retryable) error is detected during benchmark execution.
type benchmarkFatalCause struct {
	Benchmark     string
	WorkflowID    string
	ItemID        string
	JobID         string
	Attempt       int
	Code          string
	Message       string
	Reason        string
	FailureReason string
	Signature     string
	Occurrences   int
	Hard          bool
}

// benchmarkFatalGuard tracks error signatures and pauses the benchmark
// when a hard-fatal error is detected or a soft error signature repeats
// beyond the configured threshold.
type benchmarkFatalGuard struct {
	cancel context.CancelFunc

	triggered atomic.Bool

	mu              sync.Mutex
	cause           *benchmarkFatalCause
	signatureCounts map[string]int
	repeatThreshold int
}

func (g *benchmarkFatalGuard) isTriggered() bool {
	return g != nil && g.triggered.Load()
}

func (g *benchmarkFatalGuard) snapshotMessage() string {
	if g == nil {
		return "benchmark paused due to fatal execution error"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cause == nil {
		return "benchmark paused due to fatal execution error"
	}
	return fmt.Sprintf(
		"benchmark paused due to fatal execution error (code=%s reason=%s): %s",
		g.cause.Code,
		g.cause.Reason,
		g.cause.Message,
	)
}

func (g *benchmarkFatalGuard) note(cause benchmarkFatalCause) (bool, int) {
	if g == nil {
		return false, 0
	}

	shouldTrigger := false
	count := 0
	g.mu.Lock()
	if g.cause != nil {
		if g.cause.Signature != "" {
			count = g.signatureCounts[g.cause.Signature]
			if count == 0 {
				count = g.cause.Occurrences
			}
		}
		g.mu.Unlock()
		return false, count
	}

	if cause.Hard {
		if cause.Signature == "" {
			cause.Signature = strings.ToUpper(strings.TrimSpace(cause.Code))
		}
		cause.Occurrences = 1
		causeCopy := cause
		g.cause = &causeCopy
		shouldTrigger = true
		count = 1
	} else if cause.Signature != "" {
		g.signatureCounts[cause.Signature]++
		count = g.signatureCounts[cause.Signature]
		if count >= g.repeatThreshold {
			cause.Occurrences = count
			causeCopy := cause
			g.cause = &causeCopy
			shouldTrigger = true
		}
	}
	g.mu.Unlock()

	if shouldTrigger && g.triggered.CompareAndSwap(false, true) {
		if g.cancel != nil {
			g.cancel()
		}
		return true, count
	}
	return false, count
}

// --- Orchestration ---

func (s *Server) launchBenchmarkRun(command string, totalRuns, totalItems int, cfg benchmarkRunRequest, plans []benchmarkRunPlan) error {
	s.benchmarkRunnerMu.Lock()
	if s.benchmarkRunner != nil && s.benchmarkRunner.Running {
		s.benchmarkRunnerMu.Unlock()
		return fmt.Errorf("a benchmark run is already active")
	}
	ctx, cancel := context.WithCancel(context.Background())
	state := &benchmarkRunnerState{
		Running:             true,
		StartedAt:           time.Now(),
		Command:             command,
		TotalRuns:           totalRuns,
		TotalItems:          totalItems,
		LastUpdateAt:        time.Now(),
		Benchmarks:          cfg.Benchmarks,
		Workflows:           cfg.Workflows,
		RunSet:              cfg.RunSet,
		Split:               cfg.Split,
		Limit:               cfg.Limit,
		Concurrency:         cfg.Concurrency,
		MaxNonLetterRetries: cfg.MaxNonLetterRetries,
		MaxTransientRetries: cfg.MaxTransientRetries,
	}
	s.benchmarkRunner = state
	s.benchmarkCancel = cancel
	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	s.benchmarkSessionID = sessionID
	s.benchmarkRunnerMu.Unlock()

	if err := s.storage.CreateBenchmarkRunnerSession(&storage.BenchmarkRunnerSession{
		ID:         sessionID,
		Status:     "running",
		Command:    command,
		TotalRuns:  totalRuns,
		TotalItems: totalItems,
		StartedAt:  state.StartedAt,
	}); err != nil {
		log.Printf("Warning: failed to persist benchmark session: %v", err)
	}

	go s.runBenchmarkPlans(ctx, cfg, plans)
	return nil
}

func parseBenchmarkRunRequest(r *http.Request) (benchmarkRunRequest, error) {
	if err := r.ParseForm(); err != nil {
		return benchmarkRunRequest{}, fmt.Errorf("invalid form payload")
	}

	benchmarksRaw := strings.TrimSpace(r.FormValue("benchmarks"))
	workflowsRaw := strings.TrimSpace(r.FormValue("workflows"))
	runSetRaw := strings.TrimSpace(r.FormValue("run_set"))
	splitRaw := strings.TrimSpace(r.FormValue("split"))
	sourceRaw := strings.TrimSpace(strings.ToLower(r.FormValue("source")))
	optRunIDRaw := strings.TrimSpace(r.FormValue("opt_run_id"))
	optOrganismRaw := strings.TrimSpace(r.FormValue("opt_organism_id"))
	optScopeRaw := strings.TrimSpace(strings.ToLower(r.FormValue("opt_scope")))
	sampleModeRaw := strings.TrimSpace(strings.ToLower(r.FormValue("sample_mode")))
	sampleSeedRaw := strings.TrimSpace(r.FormValue("sample_seed"))
	if optOrganismRaw == "" {
		optOrganismRaw = strings.TrimSpace(r.FormValue("organism_id"))
	}

	if benchmarksRaw == "" || !safeCSVValue.MatchString(benchmarksRaw) {
		return benchmarkRunRequest{}, fmt.Errorf("invalid benchmarks value")
	}
	if workflowsRaw == "" || !safeCSVValue.MatchString(workflowsRaw) {
		return benchmarkRunRequest{}, fmt.Errorf("invalid workflows value")
	}
	if sourceRaw != "" && !safeSplitValue.MatchString(sourceRaw) {
		return benchmarkRunRequest{}, fmt.Errorf("invalid source value")
	}
	if optRunIDRaw != "" && !safeSplitValue.MatchString(optRunIDRaw) {
		return benchmarkRunRequest{}, fmt.Errorf("invalid opt_run_id value")
	}
	if optOrganismRaw != "" && !safeSplitValue.MatchString(optOrganismRaw) {
		return benchmarkRunRequest{}, fmt.Errorf("invalid opt_organism_id value")
	}
	if optScopeRaw != "" && !safeSplitValue.MatchString(optScopeRaw) {
		return benchmarkRunRequest{}, fmt.Errorf("invalid opt_scope value")
	}
	if sampleModeRaw != "" && sampleModeRaw != benchmarkSampleModeHead && sampleModeRaw != benchmarkSampleModeRandom {
		return benchmarkRunRequest{}, fmt.Errorf("sample_mode must be head or random")
	}
	sampleSeed := int64(0)
	if sampleSeedRaw != "" {
		parsed, parseErr := strconv.ParseInt(sampleSeedRaw, 10, 64)
		if parseErr != nil {
			return benchmarkRunRequest{}, fmt.Errorf("invalid sample_seed value")
		}
		sampleSeed = parsed
	}

	runSet, err := bench.NormalizeRunSize(runSetRaw)
	if err != nil {
		return benchmarkRunRequest{}, fmt.Errorf("invalid run_set value")
	}

	split := ""
	if runSet == bench.RunSizeCustom {
		if splitRaw == "" || !safeSplitValue.MatchString(splitRaw) {
			return benchmarkRunRequest{}, fmt.Errorf("split is required and must be safe for custom run_set")
		}
		split = bench.NormalizeSplit(splitRaw)
	}

	benchmarks := bench.ParseCSVValues(benchmarksRaw)
	workflows := bench.ParseCSVValues(workflowsRaw)
	if len(benchmarks) == 0 {
		return benchmarkRunRequest{}, fmt.Errorf("at least one benchmark is required")
	}
	if len(workflows) == 0 {
		return benchmarkRunRequest{}, fmt.Errorf("at least one workflow is required")
	}

	for i := range benchmarks {
		norm, normErr := bench.NormalizeBenchmark(benchmarks[i])
		if normErr != nil {
			return benchmarkRunRequest{}, fmt.Errorf("invalid benchmark %q", benchmarks[i])
		}
		benchmarks[i] = norm
	}

	pauseOnFatal := true
	if v := strings.TrimSpace(r.FormValue("pause_on_fatal")); v == "false" || v == "0" {
		pauseOnFatal = false
	}
	admissionBypass := false
	if v := strings.TrimSpace(r.FormValue("admission_bypass")); v == "true" || v == "1" {
		admissionBypass = true
	}
	source := "manual"
	if sourceRaw != "" {
		switch sourceRaw {
		case "manual", "benchloop", "optimizer", "imported", "replay":
			source = sourceRaw
		default:
			return benchmarkRunRequest{}, fmt.Errorf("source must be one of manual|benchloop|optimizer|imported|replay")
		}
	}
	sampleMode := benchmarkSampleModeHead
	if sampleModeRaw != "" {
		sampleMode = sampleModeRaw
	}
	// DSPy minibatch evaluations should sample random subsets each trial to
	// mirror DSPy's create_minibatch behavior.
	if source == "optimizer" && optScopeRaw == "dspy_minibatch" {
		sampleMode = benchmarkSampleModeRandom
		if sampleSeed == 0 {
			sampleSeed = time.Now().UnixNano()
		}
	}

	return benchmarkRunRequest{
		Benchmarks:           dedupeOrderedStrings(benchmarks),
		Workflows:            dedupeOrderedStrings(workflows),
		RunSet:               runSet,
		Split:                split,
		Limit:                max(parseIntDefault(r.FormValue("limit"), 20), 0),
		Source:               source,
		OptRunID:             optRunIDRaw,
		OptOrganismID:        optOrganismRaw,
		OptScope:             optScopeRaw,
		SampleMode:           sampleMode,
		SampleSeed:           sampleSeed,
		Concurrency:          max(parseIntDefault(r.FormValue("concurrency"), 20), 1),
		MaxNonLetterRetries:  max(parseIntDefault(r.FormValue("max_non_letter_retries"), 2), 0),
		MaxTransientRetries:  max(parseIntDefault(r.FormValue("max_transient_retries"), 3), 0),
		AdmissionBypass:      admissionBypass,
		PauseOnFatal:         pauseOnFatal,
		FatalRepeatThreshold: max(parseIntDefault(r.FormValue("fatal_repeat_threshold"), 3), 1),
	}, nil
}

func (s *Server) prepareBenchmarkRunPlans(cfg benchmarkRunRequest) ([]benchmarkRunPlan, error) {
	workflowDefs := make(map[string]*storage.WorkflowDefinition, len(cfg.Workflows))
	for _, workflowID := range cfg.Workflows {
		wf, err := s.storage.GetWorkflow(workflowID)
		if err != nil {
			return nil, fmt.Errorf("load workflow %q: %w", workflowID, err)
		}
		workflowDefs[workflowID] = wf
	}

	plans := make([]benchmarkRunPlan, 0, len(cfg.Benchmarks)*len(cfg.Workflows))
	seenRunIDs := make(map[string]int)
	for _, benchmarkID := range cfg.Benchmarks {
		resolvedSplit, datasetPath, err := bench.ResolveDatasetPathForRun(bench.DefaultDataDir, benchmarkID, cfg.RunSet, cfg.Split)
		if err != nil {
			return nil, fmt.Errorf("resolve dataset benchmark=%s: %w", benchmarkID, err)
		}

		items, err := bench.LoadDataset(datasetPath, 0)
		if err != nil {
			return nil, fmt.Errorf("load dataset benchmark=%s split=%s path=%s: %w", benchmarkID, resolvedSplit, datasetPath, err)
		}
		items = filterBenchmarkItems(items, benchmarkID, true)
		items = applyLimit(items, cfg.Limit, cfg.SampleMode, cfg.SampleSeed)
		if len(items) == 0 {
			return nil, fmt.Errorf("no benchmark items selected benchmark=%s split=%s", benchmarkID, resolvedSplit)
		}

		for _, workflowID := range cfg.Workflows {
			wf := workflowDefs[workflowID]
			sampleItem := items[0]
			if err := s.preflightValidateBenchmarkWorkflow(wf, sampleItem); err != nil {
				return nil, fmt.Errorf("preflight validate workflow=%s benchmark=%s split=%s: %w", workflowID, benchmarkID, resolvedSplit, err)
			}
			startedAt := time.Now()
			runID := bench.BuildRunID(benchmarkID, workflowID, startedAt)
			if count, exists := seenRunIDs[runID]; exists {
				count++
				seenRunIDs[runID] = count
				runID = fmt.Sprintf("%s-%d", runID, count)
			} else {
				seenRunIDs[runID] = 0
			}
			plans = append(plans, benchmarkRunPlan{
				Benchmark:          benchmarkID,
				Split:              resolvedSplit,
				DatasetPath:        datasetPath,
				ItemLimit:          max(cfg.Limit, 0),
				Source:             cfg.Source,
				OptRunID:           cfg.OptRunID,
				OptOrganismID:      cfg.OptOrganismID,
				WorkflowID:         workflowID,
				WorkflowName:       wf.Name,
				WorkflowDefinition: wf.Definition,
				RunID:              runID,
				StartedAt:          startedAt,
				Items:              items,
				ItemResults:        make([]bench.ItemResult, len(items)),
				AdmissionBypass:    cfg.AdmissionBypass,
			})
		}
	}
	return plans, nil
}

func (s *Server) runBenchmarkPlans(ctx context.Context, cfg benchmarkRunRequest, plans []benchmarkRunPlan) {
	guard := s.newBenchmarkFatalGuard(cfg)
	workItems := buildInterleavedBenchmarkWork(plans)
	s.ensureBenchmarkRunRecords(plans)

	tasks := s.buildBenchmarkTasks(ctx, cfg, guard, plans, workItems)
	jobs.RunConcurrently(tasks, cfg.Concurrency, nil)
	// After RunConcurrently returns, all per-run atomic writes (CompletedItems,
	// CompletedAtUnix) are visible due to WaitGroup happens-before ordering.

	outcome := determineBenchmarkRunOutcome(ctx, guard, plans)
	importedRuns, saveErrors := s.persistBenchmarkRunResults(plans, outcome)
	runErr := benchmarkRunError(saveErrors)
	s.finalizeBenchmarkRunner(runErr, importedRuns, guard, outcome)
}

type benchmarkRunOutcome struct {
	Status              string
	FatalGuardTriggered bool
	CancelEffective     bool
}

func (s *Server) newBenchmarkFatalGuard(cfg benchmarkRunRequest) *benchmarkFatalGuard {
	if !cfg.PauseOnFatal {
		return nil
	}

	s.benchmarkRunnerMu.Lock()
	cancelFn := s.benchmarkCancel
	s.benchmarkRunnerMu.Unlock()

	return &benchmarkFatalGuard{
		cancel:          cancelFn,
		signatureCounts: make(map[string]int),
		repeatThreshold: cfg.FatalRepeatThreshold,
	}
}

func (s *Server) ensureBenchmarkRunRecords(plans []benchmarkRunPlan) {
	// Ensure benchmark run records exist for incremental item persistence.
	// Skip for merge-mode plans where the run already exists.
	for _, run := range plans {
		if run.MergeIntoRunID != "" {
			continue
		}
		if err := s.storage.EnsureBenchmarkRunExists(
			run.RunID, run.Benchmark, run.Split,
			run.WorkflowID, run.WorkflowName, run.DatasetPath,
			len(run.Items), max(run.ItemLimit, 0), run.Source, run.OptRunID, run.OptOrganismID,
		); err != nil {
			log.Printf("Warning: failed to ensure benchmark run %s exists: %v", run.RunID, err)
		}
	}
}

func (s *Server) buildBenchmarkTasks(
	ctx context.Context,
	cfg benchmarkRunRequest,
	guard *benchmarkFatalGuard,
	plans []benchmarkRunPlan,
	workItems []benchmarkWorkItem,
) []func() {
	tasks := make([]func(), 0, len(workItems))
	for _, workItem := range workItems {
		runIndex := workItem.RunIndex
		itemIndex := workItem.ItemIndex
		tasks = append(tasks, func() {
			s.executeBenchmarkWorkItem(ctx, cfg, guard, plans, runIndex, itemIndex)
		})
	}
	return tasks
}

// --- Runner HTTP handlers ---

func (s *Server) handleBenchmarkRunStart(w http.ResponseWriter, r *http.Request) {
	cfg, err := parseBenchmarkRunRequest(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.AdmissionBypass {
		writeJSONError(w, "admission_bypass is only supported for single-item rerun/replay probes", http.StatusBadRequest)
		return
	}
	if !s.ensureBenchmarkAdmission(w, false, 0) {
		return
	}

	runPlans, err := s.prepareBenchmarkRunPlans(cfg)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to prepare benchmark run: %v", err), http.StatusBadRequest)
		return
	}

	totalItems := 0
	for _, run := range runPlans {
		totalItems += len(run.Items)
	}

	if err := s.launchBenchmarkRun("backend-native", len(runPlans), totalItems, cfg, runPlans); err != nil {
		writeJSONError(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"started":     true,
		"command":     "backend-native",
		"total_runs":  len(runPlans),
		"total_items": totalItems,
	})
}

func (s *Server) handleBenchmarkRunCancel(w http.ResponseWriter, r *http.Request) {
	s.benchmarkRunnerMu.Lock()
	defer s.benchmarkRunnerMu.Unlock()

	if s.benchmarkRunner == nil || !s.benchmarkRunner.Running || s.benchmarkCancel == nil {
		writeJSONError(w, "No running benchmark to cancel", http.StatusConflict)
		return
	}
	s.benchmarkRunner.CancelRequested = true
	s.benchmarkRunner.LastUpdateAt = time.Now()
	if s.benchmarkSessionID != "" {
		cancelTrue := true
		_ = s.storage.UpdateBenchmarkRunnerSession(s.benchmarkSessionID, storage.BenchmarkRunnerSessionUpdate{
			CancelRequested: &cancelTrue,
		})
	}
	s.benchmarkCancel()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"cancel_requested": true,
	})
}

func (s *Server) handleBenchmarkRunnerStatus(w http.ResponseWriter, r *http.Request) {
	state := s.currentBenchmarkRunnerState()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

func (s *Server) currentBenchmarkRunnerState() benchmarkRunnerState {
	s.benchmarkRunnerMu.Lock()
	defer s.benchmarkRunnerMu.Unlock()
	if s.benchmarkRunner != nil {
		return *s.benchmarkRunner
	}
	// Fall back to DB for sessions that survived a server restart.
	sess, err := s.storage.GetActiveBenchmarkRunnerSession()
	if err != nil || sess == nil {
		return benchmarkRunnerState{}
	}
	return benchmarkRunnerState{
		Running:          sess.Status == "running",
		StartedAt:        sess.StartedAt,
		Command:          sess.Command,
		Error:            sess.Error,
		TotalRuns:        sess.TotalRuns,
		CompletedRuns:    sess.CompletedRuns,
		ImportedRuns:     sess.ImportedRuns,
		TotalItems:       sess.TotalItems,
		CompletedItems:   sess.CompletedItems,
		CorrectItems:     sess.CorrectItems,
		IncorrectItems:   sess.IncorrectItems,
		CurrentRunID:     sess.CurrentRunID,
		CurrentBenchmark: sess.CurrentBenchmark,
		CurrentWorkflow:  sess.CurrentWorkflow,
		CurrentItemID:    sess.CurrentItemID,
		CancelRequested:  sess.CancelRequested,
		LastUpdateAt:     sess.LastHeartbeatAt,
	}
}

func writeAdmissionPausedJSON(w http.ResponseWriter, reason *jobs.AdmissionPauseReason) {
	payload := map[string]interface{}{
		"error": "Admission paused",
		"code":  "ADMISSION_PAUSED",
	}
	if reason != nil {
		payload["details"] = reason
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) ensureBenchmarkAdmission(w http.ResponseWriter, admissionBypass bool, itemCount int) bool {
	paused, reason := s.jobManager.AdmissionState()
	if !paused {
		return true
	}
	if !admissionBypass {
		writeAdmissionPausedJSON(w, reason)
		return false
	}
	if itemCount != 1 {
		writeJSONError(w, "admission_bypass requires exactly one item", http.StatusBadRequest)
		return false
	}
	return true
}
