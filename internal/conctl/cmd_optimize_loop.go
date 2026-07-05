package conctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	"github.com/alhasaniq/consortium/pkg/optimize"
)

type optimizeLoopConfig struct {
	VerifyMutations        bool
	VerifyMode             string
	VerifyReplayMode       string
	QuickCheckItems        int
	IncludeFlaggedFailures bool
}

func runOptimizeLoop(gf app.GlobalFlags, runID string, cfg optimizeLoopConfig) int {
	timeout, err := parseTimeout(gf.Timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid timeout %q: %v\n", gf.Timeout, err)
		return app.ExitUsage
	}
	client := optimize.NewHTTPClient(gf.URL, timeout)
	store := optimize.NewHTTPPopulationStore(gf.URL, timeout)
	evaluator := optimize.NewHTTPBenchmarkEvaluator(gf.URL, timeout)
	workflowManager := optimize.NewHTTPWorkflowManager(gf.URL, timeout)

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	runCtx, cancelRun := context.WithCancel(signalCtx)
	defer cancelRun()

	run, err := store.GetRun(runCtx, runID)
	if err != nil {
		return handleOptimizeLoopError(err)
	}
	if run.IsTerminal() {
		fmt.Fprintf(os.Stderr, "Run %s is already %s\n", run.ID, run.Status)
		return app.ExitConflict
	}

	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = "unknown"
	}
	pid := os.Getpid()

	if err := acquireOptimizeLease(run, pid, hostname); err != nil {
		return handleOptimizeLoopError(err)
	}
	if err := store.UpdateRunLease(runCtx, run.ID, pid, hostname); err != nil {
		return handleOptimizeLoopError(err)
	}
	defer func() {
		_ = store.ClearRunLease(context.Background(), run.ID)
	}()

	if err := store.UpdateRunStatus(runCtx, run.ID, "running"); err != nil {
		return handleOptimizeLoopError(err)
	}
	run.Status = "running"

	heartbeat := newOptimizeHeartbeat(store, run.ID, pid, hostname, cancelRun)
	heartbeat.start(runCtx)
	defer heartbeat.stop()

	baseWorkflowJSON, err := fetchBaseWorkflowJSON(runCtx, client, run.WorkflowID)
	if err != nil {
		_ = store.UpdateRunStatus(context.Background(), run.ID, "failed")
		return handleOptimizeLoopError(err)
	}
	parentEval, err := resolveParentEvaluationConfig(runCtx, client, run.WorkflowID, run.Benchmark)
	if err != nil {
		_ = store.UpdateRunStatus(context.Background(), run.ID, "failed")
		return handleOptimizeLoopError(err)
	}

	if cfg.QuickCheckItems < 0 {
		cfg.QuickCheckItems = optimizeDefaultQuickChecks
	}

	progressStore := &optimizeProgressStore{
		PopulationStore: store,
		writer:          os.Stderr,
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // non-crypto loop randomness
	if run.RNGSeed != nil {
		rng = rand.New(rand.NewSource(*run.RNGSeed)) //nolint:gosec // deterministic seeded search
	}
	mutator, err := buildOptimizeMutator(run, rng)
	if err != nil {
		_ = store.UpdateRunStatus(context.Background(), run.ID, "failed")
		return handleOptimizeLoopError(err)
	}
	loop := &optimize.Loop{
		Evaluator:              evaluator,
		Store:                  progressStore,
		Workflows:              workflowManager,
		Mutator:                mutator,
		Selection:              optimize.DefaultSelectionConfig(),
		Rand:                   rng,
		VerifyMutations:        cfg.VerifyMutations,
		VerifyMode:             cfg.VerifyMode,
		VerifyReplayMode:       cfg.VerifyReplayMode,
		QuickCheckItems:        cfg.QuickCheckItems,
		IncludeFlaggedFailures: cfg.IncludeFlaggedFailures,
		ParentEvaluation:       parentEval,
	}

	if parentEval != nil {
		fmt.Fprintf(os.Stderr, "Evaluation wrapper: %s (child node=%s)\n", parentEval.WrapperWorkflowID, parentEval.ChildNodeID)
	}
	fmt.Fprintf(os.Stderr, "Running optimization loop for %s (workflow=%s benchmark=%s)\n", run.ID, run.WorkflowID, run.Benchmark)
	loopErr := loop.Execute(runCtx, run, baseWorkflowJSON)
	leaseErr := heartbeat.getErr()

	switch {
	case leaseErr != nil:
		_ = store.UpdateRunStatus(context.Background(), run.ID, "paused")
		return handleOptimizeLoopError(leaseErr)
	case signalCtx.Err() != nil:
		_ = store.UpdateRunStatus(context.Background(), run.ID, "paused")
		fmt.Fprintf(os.Stderr, "Optimization run %s paused\n", run.ID)
		return optimizeOutputFinalStatus(gf, run.ID)
	case errors.Is(loopErr, context.Canceled):
		_ = store.UpdateRunStatus(context.Background(), run.ID, "paused")
		fmt.Fprintf(os.Stderr, "Optimization run %s paused\n", run.ID)
		return optimizeOutputFinalStatus(gf, run.ID)
	case loopErr != nil:
		_ = store.UpdateRunStatus(context.Background(), run.ID, "failed")
		return handleOptimizeLoopError(loopErr)
	default:
		return optimizeOutputFinalStatus(gf, run.ID)
	}
}

func optimizeOutputFinalStatus(gf app.GlobalFlags, runID string) int {
	rc, err := NewRunContext(gf)
	if err != nil {
		return HandleError(err)
	}
	defer rc.Cancel()

	data, err := fetchOptimizeRunStatusData(rc.Ctx, rc.Client, runID)
	if err != nil {
		return HandleError(err)
	}
	return rc.Output(data, optimizeRunStatusTable)
}

func fetchBaseWorkflowJSON(ctx context.Context, client *optimize.HTTPClient, workflowID string) (json.RawMessage, error) {
	if strings.TrimSpace(workflowID) == "" {
		return nil, fmt.Errorf("workflow_id is required")
	}
	var workflow map[string]interface{}
	path := "/api/workflows/" + url.PathEscape(strings.TrimSpace(workflowID))
	if err := client.GetJSON(ctx, path, nil, &workflow); err != nil {
		return nil, fmt.Errorf("load workflow %s: %w", workflowID, err)
	}
	delete(workflow, "optimize")
	data, err := json.Marshal(workflow)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow JSON: %w", err)
	}
	return data, nil
}

func resolveParentEvaluationConfig(ctx context.Context, client *optimize.HTTPClient, workflowID string, benchmark string) (*optimize.ParentEvaluationConfig, error) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil, fmt.Errorf("workflow_id is required")
	}
	// If optimizing a benchmark wrapper directly, evaluate it directly.
	if strings.HasPrefix(workflowID, "benchmark-") {
		return nil, nil
	}
	// For non-reasoning workflows, preserve existing direct-eval behavior.
	if !strings.HasPrefix(workflowID, "reasoning-") {
		return nil, nil
	}

	suffix := strings.TrimPrefix(workflowID, "reasoning-")
	candidates := []string{}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(benchmark)), "math") {
		candidates = append(candidates, "benchmark-math-"+suffix)
	}
	candidates = append(candidates, "benchmark-"+suffix)
	candidates = dedupeStringsPreserveOrder(candidates)

	var lookupErrs []string
	for _, candidateID := range candidates {
		templateJSON, err := fetchBaseWorkflowJSON(ctx, client, candidateID)
		if err != nil {
			lookupErrs = append(lookupErrs, fmt.Sprintf("%s (%v)", candidateID, err))
			continue
		}
		childNodeID, ok, err := findChildNodeReferencingWorkflow(templateJSON, workflowID)
		if err != nil {
			lookupErrs = append(lookupErrs, fmt.Sprintf("%s (%v)", candidateID, err))
			continue
		}
		if !ok {
			lookupErrs = append(lookupErrs, fmt.Sprintf("%s (no child_workflow reference to %s)", candidateID, workflowID))
			continue
		}
		return &optimize.ParentEvaluationConfig{
			WrapperWorkflowID:   candidateID,
			WrapperTemplateJSON: templateJSON,
			ChildNodeID:         childNodeID,
		}, nil
	}

	return nil, fmt.Errorf(
		"no benchmark wrapper found for workflow %s and benchmark %s; attempted: %s",
		workflowID,
		benchmark,
		strings.Join(lookupErrs, "; "),
	)
}

func findChildNodeReferencingWorkflow(workflowJSON json.RawMessage, childWorkflowID string) (string, bool, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &raw); err != nil {
		return "", false, fmt.Errorf("parse workflow JSON: %w", err)
	}
	nodesRaw, ok := raw["nodes"].([]interface{})
	if !ok {
		return "", false, fmt.Errorf("workflow nodes is not an array")
	}
	target := strings.TrimSpace(childWorkflowID)
	for _, nodeRaw := range nodesRaw {
		node, ok := nodeRaw.(map[string]interface{})
		if !ok {
			continue
		}
		nodeID, _ := node["id"].(string)
		// runtime shape
		if strings.EqualFold(strings.TrimSpace(toString(node["type"])), "child_workflow") {
			if strings.TrimSpace(toString(node["child_workflow_id"])) == target {
				return nodeID, true, nil
			}
		}
		// workflow-file shape
		data, _ := node["data"].(map[string]interface{})
		if data == nil || !strings.EqualFold(strings.TrimSpace(toString(data["type"])), "child_workflow") {
			continue
		}
		cfg, _ := data["config"].(map[string]interface{})
		if cfg == nil {
			continue
		}
		if strings.TrimSpace(toString(cfg["childWorkflowId"])) == target {
			return nodeID, true, nil
		}
	}
	return "", false, nil
}

func buildOptimizeMutator(run *optimize.OptimizationRun, rng *rand.Rand) (optimize.Mutator, error) {
	if run != nil && optimize.NormalizeOptimizeStrategy(run.Strategy) == optimize.OptimizeStrategyDSPY {
		return buildDSPYMutator(run, rng)
	}
	return buildEvolutionaryMutator(run, rng), nil
}

func buildDSPYMutator(run *optimize.OptimizationRun, rng *rand.Rand) (optimize.Mutator, error) {
	if run == nil {
		return nil, fmt.Errorf("optimization run is required")
	}
	if err := optimize.ValidateRunConfiguration(optimize.OptimizeStrategyDSPY, run.MutatorMode, run.Spec); err != nil {
		return nil, err
	}
	mode, err := optimize.ResolveDSPYOptimizerMode(run.MutatorMode, run.Spec)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(run.ClaudeModel)
	switch mode {
	case optimize.MutatorModeMIPROv2:
		return &optimize.MIPROv2Mutator{Model: model, Rand: rng}, nil
	case optimize.MutatorModeGEPA:
		return &optimize.GEPAMutator{Model: model, Rand: rng}, nil
	default:
		return nil, fmt.Errorf("unsupported dspy optimizer mode %q", mode)
	}
}

func buildEvolutionaryMutator(run *optimize.OptimizationRun, rng *rand.Rand) optimize.Mutator {
	combinatorial := &optimize.CombinatorialMutator{Rand: rng}
	llm := &optimize.LLMMutator{Rand: rng}
	mipro := &optimize.MIPROv2Mutator{Rand: rng}
	gepa := &optimize.GEPAMutator{Rand: rng}
	if run != nil {
		model := strings.TrimSpace(run.ClaudeModel)
		llm.Model = model
		mipro.Model = model
		gepa.Model = model
	}
	mode := optimize.MutatorModeAuto
	if run != nil && strings.TrimSpace(run.MutatorMode) != "" {
		mode = optimize.NormalizeMutatorMode(run.MutatorMode)
	}
	if mode == optimize.MutatorModeAuto {
		return buildEvolutionaryAutoMutator(run, rng, combinatorial, llm, mipro, gepa)
	}
	switch mode {
	case optimize.MutatorModeCombinatorial:
		return combinatorial
	case optimize.MutatorModeLLM:
		return llm
	case optimize.MutatorModeMIPROv2:
		return mipro
	case optimize.MutatorModeGEPA:
		return gepa
	default:
		return buildEvolutionaryAutoMutator(run, rng, combinatorial, llm, mipro, gepa)
	}
}

func buildEvolutionaryAutoMutator(
	run *optimize.OptimizationRun,
	rng *rand.Rand,
	combinatorial *optimize.CombinatorialMutator,
	llm *optimize.LLMMutator,
	mipro *optimize.MIPROv2Mutator,
	gepa *optimize.GEPAMutator,
) optimize.Mutator {
	spec := (*optimize.OptimizeSpec)(nil)
	if run != nil {
		spec = run.Spec
	}
	hasPrompt := false
	hasCombinatorial := false
	if spec != nil {
		for _, declaration := range spec.Params {
			switch declaration.Type {
			case optimize.ParamTypePrompt:
				hasPrompt = true
			case optimize.ParamTypeModel, optimize.ParamTypeEnum, optimize.ParamTypeFloat, optimize.ParamTypeInt:
				hasCombinatorial = true
			}
		}
	}
	if hasPrompt && hasCombinatorial {
		return &optimize.AdaptiveMixMutator{
			Combinatorial:             combinatorial,
			LLM:                       mipro,
			PromptMutationProbability: 0.75,
			Rand:                      rng,
		}
	}
	switch optimize.ResolveAutoMutatorMode(spec) {
	case optimize.MutatorModeCombinatorial:
		return combinatorial
	case optimize.MutatorModeMIPROv2:
		return mipro
	case optimize.MutatorModeGEPA:
		return gepa
	case optimize.MutatorModeLLM:
		return llm
	default:
		return llm
	}
}

func acquireOptimizeLease(run *optimize.OptimizationRun, pid int, hostname string) error {
	if run == nil {
		return fmt.Errorf("optimization run is nil")
	}
	now := time.Now().UTC()
	if run.IsOwnedAndActive(now, optimizeLeaseTTL) {
		if run.OwnerPID != pid || !strings.EqualFold(run.OwnerHostname, hostname) {
			age := now.Sub(run.CreatedAt).Truncate(time.Second)
			if run.LastHeartbeatAt != nil {
				age = now.Sub(run.LastHeartbeatAt.UTC()).Truncate(time.Second)
				if age < 0 {
					age = 0
				}
			}
			return fmt.Errorf("run owned by PID %d on %s (last heartbeat %s ago)",
				run.OwnerPID,
				run.OwnerHostname,
				age,
			)
		}
	}
	return nil
}

type optimizeHeartbeat struct {
	store    optimize.PopulationStore
	runID    string
	pid      int
	hostname string
	cancel   context.CancelFunc

	mu  sync.Mutex
	err error
}

func newOptimizeHeartbeat(store optimize.PopulationStore, runID string, pid int, hostname string, cancel context.CancelFunc) *optimizeHeartbeat {
	return &optimizeHeartbeat{
		store:    store,
		runID:    runID,
		pid:      pid,
		hostname: hostname,
		cancel:   cancel,
	}
}

func (h *optimizeHeartbeat) start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(optimizeHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := h.store.UpdateRunLease(context.Background(), h.runID, h.pid, h.hostname)
				if err == nil {
					continue
				}
				if isHTTPStatusError(err, 409) {
					h.setErr(fmt.Errorf("optimization run lease lost: %w", err))
					h.cancel()
					return
				}
				fmt.Fprintf(os.Stderr, "warning: optimization heartbeat failed: %v\n", err)
			}
		}
	}()
}

func (h *optimizeHeartbeat) stop() {}

func (h *optimizeHeartbeat) setErr(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err == nil {
		h.err = err
	}
}

func (h *optimizeHeartbeat) getErr() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

type optimizeProgressStore struct {
	optimize.PopulationStore
	writer io.Writer
}

func (s *optimizeProgressStore) UpdateRunProgress(ctx context.Context, runID string, generation int, bestOrgID string, bestFitness *optimize.Fitness, spentUSD float64, totalOrganisms int, dspyMetricCallsUsed int) error {
	if err := s.PopulationStore.UpdateRunProgress(ctx, runID, generation, bestOrgID, bestFitness, spentUSD, totalOrganisms, dspyMetricCallsUsed); err != nil {
		return err
	}
	w := s.writer
	if w == nil {
		w = os.Stderr
	}
	if bestFitness == nil {
		fmt.Fprintf(w, "[optimize] gen=%d organisms=%d spent=%s best=%s\n",
			generation, totalOrganisms, formatCostVal(spentUSD), truncate(bestOrgID, 12))
		return nil
	}
	fmt.Fprintf(w, "[optimize] gen=%d organisms=%d spent=%s best=%s score=%.4f adj_acc=%.2f%% parse=%.2f%% cost/item=%s feasible=%t\n",
		generation,
		totalOrganisms,
		formatCostVal(spentUSD),
		truncate(bestOrgID, 12),
		bestFitness.CompositeScore,
		bestFitness.AdjustedAccuracy*100,
		bestFitness.ParseRate*100,
		formatCostVal(bestFitness.CostPerItem),
		bestFitness.Feasible,
	)
	return nil
}

func handleOptimizeLoopError(err error) int {
	if err == nil {
		return app.ExitSuccess
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	switch {
	case isHTTPStatusError(err, 401), isHTTPStatusError(err, 403):
		return app.ExitAuth
	case isHTTPStatusError(err, 404):
		return app.ExitNotFound
	case isHTTPStatusError(err, 409):
		return app.ExitConflict
	case isHTTPStatusError(err, 400), isHTTPStatusError(err, 422):
		return app.ExitUsage
	case isHTTPStatusError(err, 500), isHTTPStatusError(err, 503):
		return app.ExitServer
	default:
		if errors.Is(err, context.Canceled) {
			return app.ExitSuccess
		}
		return app.ExitServer
	}
}
