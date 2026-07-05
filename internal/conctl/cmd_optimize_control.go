package conctl

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	"github.com/alhasaniq/consortium/internal/conctl/models"
	"github.com/alhasaniq/consortium/pkg/optimize"
)

func optimizeStartCmd() *app.Command {
	return &app.Command{
		Name: "start",
		Desc: "Start a new optimization run and execute the loop in foreground (cost-inducing).\n\n" +
			"Press Ctrl-C to pause gracefully. Resume with `conctl optimize resume --id <run-id> --yes`.",
		UsageLine: "conctl optimize start --workflow <id> --benchmark <name> [flags] --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize start", flag.ContinueOnError)
			var includeKnob repeatedStringFlag
			var excludeKnob repeatedStringFlag
			var freezeKnob repeatedStringFlag
			var setKnob repeatedStringFlag
			fs.String("workflow", "", "Workflow ID to optimize (required)")
			fs.String("benchmark", "", "Benchmark name (required)")
			fs.String("split", "dev", "Benchmark split")
			fs.Int("item-limit", 0, "Items per organism benchmark run (0 = full split; recommended for dspy auto modes)")
			fs.Int("concurrency", 20, "Benchmark run concurrency")
			fs.Float64("budget", 10.0, "Total optimization budget in USD")
			fs.String("strategy", "evolutionary", "Optimization path: evolutionary|darwinian|dspy")
			fs.Int("population-size", 5, "Organisms per generation")
			fs.Int("children-per-parent", 1, "Children produced per selected parent")
			fs.Int("max-children-per-generation", 0, "Hard cap of children evaluated each generation (0 = population size)")
			fs.Bool("adaptive-fanout", false, "Increase children-per-parent to 2 when budget headroom is high")
			fs.String("claude-model", "opus", "Claude CLI model for prompt mutations")
			fs.String("mutator-mode", "auto", "Mutation mode: combinatorial|llm|miprov2|gepa|auto (dspy resolves to miprov2/gepa)")
			fs.String("rng-seed", "", "Optional RNG seed for deterministic selection/combinatorial mutation")
			fs.Bool("compact-artifacts", false, "Store hashes only for LLM mutation artifacts (when artifact persistence is enabled)")
			fs.Bool("verify-mutations", true, "Run quick sanity benchmark before full eval")
			fs.String("verify-mode", "replay", "Mutation verification mode: replay|full")
			fs.String("verify-replay-mode", "best_effort", "Replay verification strictness: best_effort|required")
			fs.Int("quick-check-items", optimizeDefaultQuickChecks, "Replay failure items to quick-check (0 = all parent failures)")
			fs.Bool("include-flagged-failures", false, "Include flagged dataset items in mutation failure samples")
			fs.Bool("allow-model-swaps", false, "Augment model candidates using benchmark model suggestions")
			fs.Int("model-suggest-top", 5, "Top suggested models per track")
			fs.String("model-track", "cheap", "Model suggestion track: cheap|balanced|intelligence")
			fs.Float64("model-min-intel", models.DefaultMinIntel, "Minimum intelligence index for model suggestions")
			fs.Float64("model-max-cost", models.DefaultMaxCost, "Maximum cost cap for model suggestions")
			fs.Var(&includeKnob, "include-knob", "Repeatable selector to keep optimizable params (path:/glob:/type:/group:)")
			fs.Var(&excludeKnob, "exclude-knob", "Repeatable selector to remove optimizable params")
			fs.Var(&freezeKnob, "freeze-knob", "Repeatable selector to freeze params for this run")
			fs.Var(&setKnob, "set-knob", "Repeatable selector override as <selector>=<json-value>")
			fs.Bool("freeze-temperature", false, "Freeze temperature knobs for deterministic startup")
			fs.Float64("temperature-value", 0, "Value applied to temperature knobs when --freeze-temperature is set")
			fs.Bool("yes", false, "Confirm cost-inducing operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize start", flag.ContinueOnError)
			var includeKnob repeatedStringFlag
			var excludeKnob repeatedStringFlag
			var freezeKnob repeatedStringFlag
			var setKnob repeatedStringFlag
			fs.SetOutput(os.Stderr)
			workflowID := fs.String("workflow", "", "Workflow ID (required)")
			benchmark := fs.String("benchmark", "", "Benchmark (required)")
			split := fs.String("split", "dev", "Split")
			itemLimit := fs.Int("item-limit", 0, "Item limit")
			concurrency := fs.Int("concurrency", 20, "Concurrency")
			budgetUSD := fs.Float64("budget", 10.0, "Budget USD")
			strategy := fs.String("strategy", "evolutionary", "Optimization path: evolutionary|darwinian|dspy")
			populationSize := fs.Int("population-size", 5, "Population size")
			childrenPerParent := fs.Int("children-per-parent", 1, "Children per parent")
			maxChildrenPerGeneration := fs.Int("max-children-per-generation", 0, "Max children per generation")
			adaptiveFanout := fs.Bool("adaptive-fanout", false, "Adaptive fanout")
			claudeModel := fs.String("claude-model", "opus", "Claude model")
			mutatorMode := fs.String("mutator-mode", "auto", "Mutation mode: combinatorial|llm|miprov2|gepa|auto (dspy resolves to miprov2/gepa)")
			rngSeedRaw := fs.String("rng-seed", "", "RNG seed")
			compactArtifacts := fs.Bool("compact-artifacts", false, "Compact artifacts")
			verifyMutations := fs.Bool("verify-mutations", true, "Quick-check mutations")
			verifyMode := fs.String("verify-mode", "replay", "Verify mode")
			verifyReplayMode := fs.String("verify-replay-mode", "best_effort", "Verify replay mode")
			quickCheckItems := fs.Int("quick-check-items", optimizeDefaultQuickChecks, "Quick-check items")
			includeFlaggedFailures := fs.Bool("include-flagged-failures", false, "Include flagged dataset items in mutation failure samples")
			allowModelSwaps := fs.Bool("allow-model-swaps", false, "Augment model candidates using benchmark model suggestions")
			modelSuggestTop := fs.Int("model-suggest-top", 5, "Top suggested models per track")
			modelTrack := fs.String("model-track", "cheap", "Model suggestion track")
			modelMinIntel := fs.Float64("model-min-intel", models.DefaultMinIntel, "Minimum intelligence index for model suggestions")
			modelMaxCost := fs.Float64("model-max-cost", models.DefaultMaxCost, "Maximum cost cap for model suggestions")
			fs.Var(&includeKnob, "include-knob", "Repeatable selector to keep optimizable params")
			fs.Var(&excludeKnob, "exclude-knob", "Repeatable selector to remove optimizable params")
			fs.Var(&freezeKnob, "freeze-knob", "Repeatable selector to freeze params for this run")
			fs.Var(&setKnob, "set-knob", "Repeatable selector override as <selector>=<json-value>")
			freezeTemperature := fs.Bool("freeze-temperature", false, "Freeze temperature knobs for deterministic startup")
			temperatureValue := fs.Float64("temperature-value", 0, "Value applied to temperature knobs when --freeze-temperature is set")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*workflowID) == "" || strings.TrimSpace(*benchmark) == "" {
				fmt.Fprintln(os.Stderr, "Error: --workflow and --benchmark are required")
				return app.ExitUsage
			}
			if *itemLimit < 0 || *concurrency < 1 || *populationSize < 1 || *budgetUSD <= 0 || *childrenPerParent < 1 || *maxChildrenPerGeneration < 0 {
				fmt.Fprintln(os.Stderr, "Error: --item-limit >= 0, --concurrency >= 1, --population-size >= 1, --children-per-parent >= 1, --max-children-per-generation >= 0, and --budget > 0 are required")
				return app.ExitUsage
			}
			if *quickCheckItems < 0 {
				fmt.Fprintln(os.Stderr, "Error: --quick-check-items must be >= 0")
				return app.ExitUsage
			}
			verifyModeValue := strings.ToLower(strings.TrimSpace(*verifyMode))
			if verifyModeValue != "replay" && verifyModeValue != "full" {
				fmt.Fprintln(os.Stderr, "Error: --verify-mode must be replay or full")
				return app.ExitUsage
			}
			verifyReplayModeValue := strings.ToLower(strings.TrimSpace(*verifyReplayMode))
			if verifyReplayModeValue != "best_effort" && verifyReplayModeValue != "required" {
				fmt.Fprintln(os.Stderr, "Error: --verify-replay-mode must be best_effort or required")
				return app.ExitUsage
			}
			mutatorModeValue := strings.ToLower(strings.TrimSpace(*mutatorMode))
			if !optimize.IsSupportedMutatorMode(mutatorModeValue) {
				fmt.Fprintln(os.Stderr, "Error: --mutator-mode must be combinatorial, llm, miprov2, gepa, or auto")
				return app.ExitUsage
			}
			strategyValue := optimize.NormalizeOptimizeStrategy(*strategy)
			if !optimize.IsSupportedOptimizeStrategy(strategyValue) {
				fmt.Fprintln(os.Stderr, "Error: --strategy must be evolutionary, darwinian, or dspy")
				return app.ExitUsage
			}

			var rngSeedPtr *int64
			if strings.TrimSpace(*rngSeedRaw) != "" {
				value, parseErr := strconv.ParseInt(strings.TrimSpace(*rngSeedRaw), 10, 64)
				if parseErr != nil {
					fmt.Fprintf(os.Stderr, "Error: invalid --rng-seed: %v\n", parseErr)
					return app.ExitUsage
				}
				rngSeedPtr = &value
			}
			if looksLikeClaudeAlias(*claudeModel) {
				fmt.Fprintf(os.Stderr, "Warning: --claude-model %q looks like an alias; use an exact model ID for better reproducibility.\n", strings.TrimSpace(*claudeModel))
			}

			if code, ok := RequireYes(*yes, "optimize start"); !ok {
				return code
			}

			includeSelectors := includeKnob.Values()
			excludeSelectors := excludeKnob.Values()
			freezeSelectors := freezeKnob.Values()
			setOperations := setKnob.Values()
			if *freezeTemperature {
				// Keep deterministic startup explicit for temperature-like knobs.
				freezeSelectors = append(freezeSelectors, "type:float", "glob:**.temperature")
				setOperations = append([]string{fmt.Sprintf("glob:**.temperature=%s", strconv.FormatFloat(*temperatureValue, 'f', -1, 64))}, setOperations...)
			}

			needsSpec := *allowModelSwaps || (len(includeSelectors)+len(excludeSelectors)+len(freezeSelectors)+len(setOperations) > 0)
			var (
				parsedSeed  *optimize.ParsedSeed
				specForRun  *optimize.OptimizeSpec
				startErrMsg string
				err         error
			)
			if needsSpec {
				parsedSeed, err = loadParsedSeed(strings.TrimSpace(*workflowID))
				if err != nil {
					startErrMsg = fmt.Sprintf("failed to load seed optimize spec for overlays/model-swaps: %v", err)
				} else {
					specForRun = parsedSeed.OptimizeSpec
				}
			}
			if startErrMsg == "" && len(includeSelectors)+len(excludeSelectors)+len(freezeSelectors)+len(setOperations) > 0 {
				specForRun, err = optimize.ApplySpecOverlay(specForRun, optimize.SpecOverlayOptions{
					IncludeSelectors: includeSelectors,
					ExcludeSelectors: excludeSelectors,
					FreezeSelectors:  freezeSelectors,
					SetOperations:    setOperations,
				})
				if err != nil {
					startErrMsg = fmt.Sprintf("invalid optimize overlay: %v", err)
				}
			}
			if startErrMsg == "" && *allowModelSwaps {
				if err := augmentSpecModelCandidates(specForRun, parsedSeed.WorkflowJSON, modelSwapOptions{
					Top:      *modelSuggestTop,
					Track:    strings.TrimSpace(*modelTrack),
					MinIntel: *modelMinIntel,
					MaxCost:  *modelMaxCost,
				}); err != nil {
					startErrMsg = fmt.Sprintf("failed to apply model suggestions: %v", err)
				}
			}
			if startErrMsg == "" && strategyValue == optimize.OptimizeStrategyDSPY {
				if specForRun == nil {
					parsedSeed, err = loadParsedSeed(strings.TrimSpace(*workflowID))
					if err != nil {
						startErrMsg = fmt.Sprintf("failed to load seed optimize spec for dspy strategy validation: %v", err)
					} else {
						specForRun = parsedSeed.OptimizeSpec
					}
				}
				if startErrMsg == "" {
					if err := optimize.ValidateRunConfiguration(strategyValue, mutatorModeValue, specForRun); err != nil {
						startErrMsg = err.Error()
					}
				}
			}
			if startErrMsg != "" {
				fmt.Fprintf(os.Stderr, "Error: %s\n", startErrMsg)
				return app.ExitUsage
			}

			payload := map[string]interface{}{
				"workflow_id":                 strings.TrimSpace(*workflowID),
				"benchmark":                   strings.TrimSpace(*benchmark),
				"split":                       strings.TrimSpace(*split),
				"item_limit":                  *itemLimit,
				"concurrency":                 *concurrency,
				"budget_usd":                  *budgetUSD,
				"strategy":                    strategyValue,
				"population_size":             *populationSize,
				"claude_model":                strings.TrimSpace(*claudeModel),
				"mutator_mode":                mutatorModeValue,
				"children_per_parent":         *childrenPerParent,
				"max_children_per_generation": *maxChildrenPerGeneration,
				"adaptive_fanout":             *adaptiveFanout,
				"compact_artifacts":           *compactArtifacts,
			}
			if rngSeedPtr != nil {
				payload["rng_seed"] = *rngSeedPtr
			}
			if specForRun != nil {
				payload["spec"] = specForRun
			}

			if *dryRun {
				return DryRunOutput("POST", "/api/admin/optimize/runs", payload)
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var run optimize.OptimizationRun
			if err := rc.Client.PostJSON(rc.Ctx, "/api/admin/optimize/runs", payload, &run); err != nil {
				return HandleError(err)
			}
			fmt.Fprintf(os.Stderr, "Started optimization run %s\n", run.ID)

			return runOptimizeLoop(gf, run.ID, optimizeLoopConfig{
				VerifyMutations:        *verifyMutations,
				QuickCheckItems:        *quickCheckItems,
				VerifyMode:             verifyModeValue,
				VerifyReplayMode:       verifyReplayModeValue,
				IncludeFlaggedFailures: *includeFlaggedFailures,
			})
		},
	}
}

func optimizeResumeCmd() *app.Command {
	return &app.Command{
		Name: "resume",
		Desc: "Resume a paused/interrupted optimization run in foreground.\n\n" +
			"Press Ctrl-C to pause gracefully again.",
		UsageLine: "conctl optimize resume --id <run-id> --yes [--verify-mutations] [--quick-check-items N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize resume", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Bool("verify-mutations", true, "Run quick sanity benchmark before full eval")
			fs.String("verify-mode", "replay", "Mutation verification mode: replay|full")
			fs.String("verify-replay-mode", "best_effort", "Replay verification strictness: best_effort|required")
			fs.Int("quick-check-items", optimizeDefaultQuickChecks, "Replay failure items to quick-check (0 = all parent failures)")
			fs.Bool("include-flagged-failures", false, "Include flagged dataset items in mutation failure samples")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize resume", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			runID := fs.String("id", "", "Run ID (required)")
			verifyMutations := fs.Bool("verify-mutations", true, "Quick-check mutations")
			verifyMode := fs.String("verify-mode", "replay", "Verify mode")
			verifyReplayMode := fs.String("verify-replay-mode", "best_effort", "Verify replay mode")
			quickCheckItems := fs.Int("quick-check-items", optimizeDefaultQuickChecks, "Quick-check items")
			includeFlaggedFailures := fs.Bool("include-flagged-failures", false, "Include flagged dataset items in mutation failure samples")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*runID) == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if *quickCheckItems < 0 {
				fmt.Fprintln(os.Stderr, "Error: --quick-check-items must be >= 0")
				return app.ExitUsage
			}
			verifyModeValue := strings.ToLower(strings.TrimSpace(*verifyMode))
			if verifyModeValue != "replay" && verifyModeValue != "full" {
				fmt.Fprintln(os.Stderr, "Error: --verify-mode must be replay or full")
				return app.ExitUsage
			}
			verifyReplayModeValue := strings.ToLower(strings.TrimSpace(*verifyReplayMode))
			if verifyReplayModeValue != "best_effort" && verifyReplayModeValue != "required" {
				fmt.Fprintln(os.Stderr, "Error: --verify-replay-mode must be best_effort or required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "optimize resume"); !ok {
				return code
			}
			if *dryRun {
				return DryRunOutput("loop", "/api/admin/optimize/runs/"+strings.TrimSpace(*runID), map[string]interface{}{
					"verify_mutations":         *verifyMutations,
					"verify_mode":              verifyModeValue,
					"verify_replay_mode":       verifyReplayModeValue,
					"quick_check_items":        *quickCheckItems,
					"include_flagged_failures": *includeFlaggedFailures,
				})
			}

			return runOptimizeLoop(gf, strings.TrimSpace(*runID), optimizeLoopConfig{
				VerifyMutations:        *verifyMutations,
				VerifyMode:             verifyModeValue,
				VerifyReplayMode:       verifyReplayModeValue,
				QuickCheckItems:        *quickCheckItems,
				IncludeFlaggedFailures: *includeFlaggedFailures,
			})
		},
	}
}

func optimizePauseCmd() *app.Command {
	return optimizeMutateByIDCmd(
		"pause",
		"Pause an optimization run",
		"/api/admin/optimize/runs/{id}/pause",
	)
}

func optimizeCancelCmd() *app.Command {
	return optimizeMutateByIDCmd(
		"cancel",
		"Cancel an optimization run",
		"/api/admin/optimize/runs/{id}/cancel",
	)
}

func optimizePromoteCmd() *app.Command {
	return &app.Command{
		Name: "promote",
		Desc: "Promote the best organism into the workflow seed + definition with no-regression checks.\n\n" +
			"Uses optimistic locking and fails on workflow version conflict.",
		UsageLine: "conctl optimize promote --id <run-id> --yes [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize promote", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize promote", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			runID := fs.String("id", "", "Run ID (required)")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*runID) == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "optimize promote"); !ok {
				return code
			}
			path := "/api/admin/optimize/runs/" + url.PathEscape(strings.TrimSpace(*runID)) + "/promote"
			if *dryRun {
				return DryRunOutput("POST", path, map[string]interface{}{"dry_run": true})
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.PostJSON(rc.Ctx, path, map[string]interface{}{"dry_run": false}, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

// augmentSpecModelCandidates merges model suggestions from the local repo
// into the optimize spec's model parameter candidates.
func augmentSpecModelCandidates(spec *optimize.OptimizeSpec, seedWorkflowJSON json.RawMessage, opts modelSwapOptions) error {
	if spec == nil {
		return fmt.Errorf("optimize spec is required")
	}
	repo, err := models.LoadModelsRepo(models.DefaultRepoPath)
	if err != nil {
		return err
	}
	suggestions, err := models.Suggest(repo, models.SuggestOptions{
		Top:               opts.Top,
		Track:             opts.Track,
		Reasoning:         "true",
		IncludeDeprecated: false,
		MinIntel:          opts.MinIntel,
		MaxCost:           opts.MaxCost,
	})
	if err != nil {
		return err
	}
	trackCandidates := models.TrackCandidates(suggestions, opts.Track)
	suggestedIDs := make([]string, 0, len(trackCandidates))
	for _, candidate := range trackCandidates {
		if id := strings.TrimSpace(candidate.CandidateID()); id != "" {
			suggestedIDs = append(suggestedIDs, id)
		}
	}
	if len(suggestedIDs) == 0 {
		return fmt.Errorf("model suggestion track %q produced no candidate IDs", opts.Track)
	}

	modelParams := 0
	counts := make(map[string]int)
	for i, declaration := range spec.Params {
		if declaration.Type != optimize.ParamTypeModel {
			continue
		}
		modelParams++
		merged := make([]string, 0, len(declaration.Candidates)+len(suggestedIDs)+1)
		if current, found, valueErr := optimize.GetWorkflowPathValue(seedWorkflowJSON, declaration.Path); valueErr == nil && found {
			if currentModel, ok := current.(string); ok && strings.TrimSpace(currentModel) != "" {
				merged = append(merged, currentModel)
			}
		}
		merged = append(merged, declaration.Candidates...)
		merged = append(merged, suggestedIDs...)
		merged = dedupeStringsPreserveOrder(merged)
		spec.Params[i].Candidates = merged
		counts[declaration.Path] = len(merged)
	}
	if modelParams == 0 {
		return fmt.Errorf("allow-model-swaps requested but optimize spec has no model params")
	}
	if spec.ModelSwap == nil {
		spec.ModelSwap = &optimize.ModelSwapMetadata{}
	}
	spec.ModelSwap.Enabled = true
	spec.ModelSwap.Track = strings.ToLower(strings.TrimSpace(opts.Track))
	spec.ModelSwap.Top = opts.Top
	spec.ModelSwap.MinIntel = opts.MinIntel
	spec.ModelSwap.MaxCost = opts.MaxCost
	spec.ModelSwap.RepoSnapshotAt = suggestions.SnapshotAt
	spec.ModelSwap.TotalSuggested = len(suggestedIDs)
	spec.ModelSwap.CandidateCounts = counts
	return nil
}
