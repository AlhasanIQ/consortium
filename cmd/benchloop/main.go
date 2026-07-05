// Package main provides the benchloop CLI — an iterative benchmark tuning loop
// that uses a Claude Code agent to optimize Consortium reasoning workflows.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/internal/benchloop"
	"github.com/joho/godotenv"
)

type rootCommand struct {
	Name    string
	Aliases []string
	Usage   string
	Summary string
	Run     func([]string)
}

var rootCommands = []rootCommand{
	{
		Name:    "run",
		Usage:   "benchloop [run] [flags]",
		Summary: "Run the benchmark tuning loop (default)",
		Run:     runCommand,
	},
	{
		Name:    "archive",
		Usage:   "benchloop archive [flags]",
		Summary: "Archive loop state for a clean-slate run",
		Run:     archiveCommand,
	},
	{
		Name:    "status",
		Usage:   "benchloop status [flags]",
		Summary: "Show in-flight loop status from state.json",
		Run:     statusCommand,
	},
	{
		Name:    "prompts",
		Usage:   "benchloop prompts [flags]",
		Summary: "Render benchloop system/user prompts for inspection",
		Run:     promptsCommand,
	},
	{
		Name:    "sessions",
		Usage:   "benchloop sessions [flags]",
		Summary: "List benchloop -> Claude session mappings",
		Run:     sessionsCommand,
	},
	{
		Name:    "transcript",
		Aliases: []string{"debug"},
		Usage:   "benchloop transcript [flags]",
		Summary: "Show high-level transcript messages from a mapped session",
		Run:     transcriptCommand,
	},
}

func main() {
	// Load .env when present without overriding existing env vars.
	_ = godotenv.Load()

	args := os.Args[1:]

	if len(args) > 0 {
		first := strings.TrimSpace(args[0])
		switch first {
		case "help", "-h", "--help":
			printRootUsage()
			return
		default:
			if !strings.HasPrefix(first, "-") {
				command, ok := findRootCommand(first)
				if !ok {
					log.Fatalf("Unknown subcommand %q. Use one of: %s", first, rootCommandNames())
				}
				command.Run(args[1:])
				return
			}
		}
	}

	run, _ := findRootCommand("run")
	run.Run(args)
}

func findRootCommand(name string) (rootCommand, bool) {
	for _, cmd := range rootCommands {
		if cmd.Name == name {
			return cmd, true
		}
		for _, alias := range cmd.Aliases {
			if alias == name {
				return cmd, true
			}
		}
	}
	return rootCommand{}, false
}

func rootCommandNames() string {
	names := make([]string, 0, len(rootCommands))
	for _, cmd := range rootCommands {
		names = append(names, cmd.Name)
	}
	return strings.Join(names, ", ")
}

func printRootUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	for _, cmd := range rootCommands {
		fmt.Fprintf(os.Stderr, "  %s\n", cmd.Usage)
	}
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	for _, cmd := range rootCommands {
		if len(cmd.Aliases) == 0 {
			fmt.Fprintf(os.Stderr, "  %-11s %s\n", cmd.Name, cmd.Summary)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-11s %s (aliases: %s)\n", cmd.Name, cmd.Summary, strings.Join(cmd.Aliases, ", "))
	}
}

func runCommand(args []string) {
	var (
		workflows   string
		benchmark   string
		runSet      string
		split       string
		itemLimit   int
		concurrency int
		bestRunID   string

		maxIterations    int
		stopAfterPlateau int
		iterationTimeout string

		totalBudgetUSD float64
		agentBudgetUSD float64

		claudeBin         string
		model             string
		agentOutputFormat string
		allowModelSwaps   bool

		resume  bool
		dryRun  bool
		verbose bool
	)

	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.StringVar(&workflows, "workflows", "", "Comma-separated target workflow IDs (required for fresh runs)")
	fs.StringVar(&benchmark, "benchmark", "global-mmlu-lite", "Benchmark name")
	fs.StringVar(&runSet, "run-set", "lite", "Run set: lite|custom")
	fs.StringVar(&split, "split", "", "Split name (required for fresh runs; also required for --run-set custom)")
	fs.IntVar(&itemLimit, "item-limit", 0, "Items per benchmark run (required for fresh runs; 0 = full split)")
	fs.IntVar(&concurrency, "concurrency", 0, "Benchmark concurrency (required for fresh runs)")
	fs.StringVar(&bestRunID, "best-run-id", "", "Baseline run ID (optional — iteration 1 bootstraps if omitted)")

	fs.IntVar(&maxIterations, "max-iterations", 10, "Maximum loop iterations")
	fs.IntVar(&stopAfterPlateau, "stop-after-plateau", 3, "Stop after N consecutive no-progress iterations")
	fs.StringVar(&iterationTimeout, "iteration-timeout", "30m", "Per-iteration timeout")

	fs.Float64Var(&totalBudgetUSD, "total-budget-usd", 0, "Total benchloop budget in USD (benchmark + agent, 0 = unlimited)")
	fs.Float64Var(&agentBudgetUSD, "agent-budget-usd", 0, "Per-iteration Claude API budget in USD (0 = unlimited)")

	fs.StringVar(&claudeBin, "claude-bin", "claude", "Path to claude binary")
	fs.StringVar(&model, "model", "opus", "Claude model for agent")
	fs.StringVar(&agentOutputFormat, "agent-output-format", "json", "Claude output format for benchloop agent: json|stream-json")
	fs.BoolVar(&allowModelSwaps, "allow-model-swaps", false, "Allow agent to change model/model_* fields (default false)")

	fs.BoolVar(&resume, "resume", false, "Resume from existing state.json + matrix.lock.json")
	fs.BoolVar(&dryRun, "dry-run", false, "Show resolved config without running")
	fs.BoolVar(&verbose, "verbose", false, "Tee agent stdout to terminal in real-time")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: benchloop [run] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Run an iterative benchmark tuning loop using a Claude Code agent.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	explicitFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})

	timeout, err := time.ParseDuration(iterationTimeout)
	if err != nil {
		log.Fatalf("Invalid --iteration-timeout %q: %v", iterationTimeout, err)
	}

	var wfList []string
	for _, wf := range strings.Split(workflows, ",") {
		wf = strings.TrimSpace(wf)
		if wf != "" {
			wfList = append(wfList, wf)
		}
	}

	cfg := &benchloop.Config{
		Workflows:         wfList,
		Benchmark:         benchmark,
		RunSet:            runSet,
		Split:             split,
		ItemLimit:         itemLimit,
		Concurrency:       concurrency,
		BestRunID:         bestRunID,
		ExplicitFlags:     explicitFlags,
		MaxIterations:     maxIterations,
		StopAfterPlateau:  stopAfterPlateau,
		IterationTimeout:  timeout,
		TotalBudgetUSD:    totalBudgetUSD,
		AgentBudgetUSD:    agentBudgetUSD,
		ClaudeBin:         claudeBin,
		Model:             model,
		AgentOutputFormat: agentOutputFormat,
		AllowModelSwaps:   allowModelSwaps,
		Resume:            resume,
		DryRun:            dryRun,
		Verbose:           verbose,
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	if cfg.DryRun {
		printDryRun(cfg)
		return
	}

	loop := benchloop.NewLoop(cfg)
	if err := loop.Run(); err != nil {
		log.Fatalf("Loop failed: %v", err)
	}
}

func archiveCommand(args []string) {
	var (
		workdir string
		force   bool
	)
	fs := flag.NewFlagSet("archive", flag.ExitOnError)
	fs.StringVar(&workdir, "workdir", "", "Project root (default: current directory)")
	fs.BoolVar(&force, "force", false, "Archive even if decision.json is invalid")
	_ = fs.Parse(args)

	resolvedWorkdir, err := resolveWorkdir(workdir)
	if err != nil {
		log.Fatalf("Resolve workdir: %v", err)
	}

	// Reuse the run lock to prevent archiving while an active loop is running.
	lock, err := benchloop.AcquireRunLock(resolvedWorkdir, false)
	if err != nil {
		log.Fatalf("Cannot archive while loop is active: %v", err)
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			log.Printf("Warning: failed to release archive lock: %v", releaseErr)
		}
	}()

	sessionID, err := benchloop.ArchiveCleanSlate(resolvedWorkdir, force)
	if err != nil {
		log.Fatalf("Archive failed: %v", err)
	}
	if sessionID == "" {
		fmt.Println("No active benchloop session state found to archive.")
		return
	}

	fmt.Printf("Archived loop state to benchmarks/loop/archive/%s/\n", sessionID)
	fmt.Println("Benchloop workspace is ready for a clean-slate run.")
}

func statusCommand(args []string) {
	var (
		workdir  string
		asJSON   bool
		watch    bool
		interval string
	)
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.StringVar(&workdir, "workdir", "", "Project root (default: current directory)")
	fs.BoolVar(&asJSON, "json", false, "Print JSON payload")
	fs.BoolVar(&watch, "watch", false, "Poll status continuously")
	fs.StringVar(&interval, "interval", "5s", "Poll interval with --watch")
	_ = fs.Parse(args)

	resolvedWorkdir, err := resolveWorkdir(workdir)
	if err != nil {
		log.Fatalf("Resolve workdir: %v", err)
	}
	pollInterval, err := time.ParseDuration(interval)
	if err != nil {
		log.Fatalf("Invalid --interval %q: %v", interval, err)
	}

	printOnce := func() {
		payload := struct {
			Workdir string                     `json:"workdir"`
			Lock    *benchloop.RunLockMetadata `json:"lock,omitempty"`
			LockOK  *bool                      `json:"lock_alive,omitempty"`
			State   *benchloop.State           `json:"state,omitempty"`
			Time    string                     `json:"time"`
		}{
			Workdir: resolvedWorkdir,
			Time:    time.Now().Format(time.RFC3339),
		}

		if lockMeta, lockErr := benchloop.ReadRunLockMetadata(resolvedWorkdir); lockErr == nil {
			payload.Lock = lockMeta
			alive := benchloop.IsRunLockProcessAlive(lockMeta)
			payload.LockOK = &alive
		}
		if _, statErr := os.Stat(benchloop.StatePath(resolvedWorkdir)); statErr == nil {
			if state, loadErr := benchloop.LoadState(resolvedWorkdir); loadErr == nil {
				payload.State = state
			}
		}

		if asJSON {
			out, _ := json.MarshalIndent(payload, "", "  ")
			fmt.Println(string(out))
			return
		}

		fmt.Printf("Time:    %s\n", payload.Time)
		fmt.Printf("Workdir: %s\n", payload.Workdir)
		if payload.Lock != nil {
			lockState := "stale"
			if payload.LockOK != nil && *payload.LockOK {
				lockState = "alive"
			}
			fmt.Printf("Lock:    pid=%d session=%q created=%s (%s)\n", payload.Lock.PID, payload.Lock.SessionID, payload.Lock.CreatedAt.Format(time.RFC3339), lockState)
		} else {
			fmt.Printf("Lock:    (none)\n")
		}
		if payload.State == nil {
			fmt.Printf("State:   (none)\n")
			return
		}

		state := payload.State
		fmt.Printf("Status:  %s (session=%s)\n", state.Status, state.SessionID)
		fmt.Printf("Iter:    %d/%d (attempts=%d crashes=%d)\n", state.Iteration, state.MaxIterations, state.TotalAttempts, state.AgentCrashCount)
		if state.BaselineRunID != "" {
			fmt.Printf("Base:    %.1f%% run=%s\n", state.BaselineAccuracy*100, state.BaselineRunID)
		}
		if state.CurrentRunID != "" {
			fmt.Printf("Current: %.1f%% run=%s\n", state.CurrentAccuracy*100, state.CurrentRunID)
		}
		if state.Live != nil {
			fmt.Printf("Live:    phase=%s step=%s iter=%d attempt=%d\n", state.Live.Phase, state.Live.Step, state.Live.Iteration, state.Live.Attempt)
			if strings.TrimSpace(state.Live.Message) != "" {
				fmt.Printf("Message: %s\n", state.Live.Message)
			}
			if strings.TrimSpace(state.Live.AgentSessionID) != "" {
				fmt.Printf("Agent:   session=%s pid=%d\n", state.Live.AgentSessionID, state.Live.AgentPID)
			}
			if strings.TrimSpace(state.Live.AgentLogPath) != "" {
				fmt.Printf("Log:     %s\n", state.Live.AgentLogPath)
			}
			if strings.TrimSpace(state.Live.TranscriptPath) != "" {
				fmt.Printf("Trace:   %s\n", state.Live.TranscriptPath)
			}
		}
		if strings.TrimSpace(state.LastClaudeSessionID) != "" {
			fmt.Printf("Last session: %s\n", state.LastClaudeSessionID)
		}
		if strings.TrimSpace(state.LastTranscriptPath) != "" {
			fmt.Printf("Last trace:   %s\n", state.LastTranscriptPath)
		}
	}

	for {
		printOnce()
		if !watch {
			return
		}
		time.Sleep(pollInterval)
		fmt.Println("---")
	}
}

func promptsCommand(args []string) {
	var (
		workdir         string
		workflows       string
		benchmark       string
		runSet          string
		split           string
		itemLimit       int
		concurrency     int
		model           string
		allowModelSwaps bool
		phase           string
		asJSON          bool
		ignoreLock      bool
		memory          string
		memoryFile      string
	)
	fs := flag.NewFlagSet("prompts", flag.ExitOnError)
	fs.StringVar(&workdir, "workdir", "", "Project root (default: current directory)")
	fs.StringVar(&workflows, "workflows", "", "Comma-separated workflow IDs (fallback when matrix lock is missing)")
	fs.StringVar(&benchmark, "benchmark", "global-mmlu-lite", "Benchmark name")
	fs.StringVar(&runSet, "run-set", "lite", "Run set: lite|custom")
	fs.StringVar(&split, "split", "dev", "Split name")
	fs.IntVar(&itemLimit, "item-limit", 50, "Item limit for matrix sample")
	fs.IntVar(&concurrency, "concurrency", 20, "Concurrency for matrix sample")
	fs.StringVar(&model, "model", "opus", "Claude model label in rendered context")
	fs.BoolVar(&allowModelSwaps, "allow-model-swaps", false, "Render prompts with model swapping enabled")
	fs.StringVar(&phase, "phase", "all", "Prompt to render: all|base-system|iteration-system|iteration")
	fs.BoolVar(&asJSON, "json", false, "Print JSON payload")
	fs.BoolVar(&ignoreLock, "ignore-lock", false, "Ignore existing matrix.lock.json/state.json and render from CLI flags")
	fs.StringVar(&memory, "memory", "", "Inline memory text for iteration prompt")
	fs.StringVar(&memoryFile, "memory-file", "", "Load memory text from file")
	_ = fs.Parse(args)

	resolvedWorkdir, err := resolveWorkdir(workdir)
	if err != nil {
		log.Fatalf("Resolve workdir: %v", err)
	}
	if memoryFile != "" {
		data, readErr := os.ReadFile(memoryFile)
		if readErr != nil {
			log.Fatalf("Read --memory-file: %v", readErr)
		}
		memory = string(data)
	}

	var lock *benchloop.MatrixLock
	if !ignoreLock {
		if _, statErr := os.Stat(benchloop.MatrixLockPath(resolvedWorkdir)); statErr == nil {
			loaded, readErr := benchloop.ReadMatrixLock(resolvedWorkdir)
			if readErr != nil {
				log.Fatalf("Read matrix lock: %v", readErr)
			}
			lock = loaded
		}
	}

	var state *benchloop.State
	if !ignoreLock {
		if _, statErr := os.Stat(benchloop.StatePath(resolvedWorkdir)); statErr == nil {
			loaded, readErr := benchloop.LoadState(resolvedWorkdir)
			if readErr != nil {
				log.Fatalf("Read state: %v", readErr)
			}
			state = loaded
		}
	}

	wfList := splitCSV(workflows)
	if len(wfList) == 0 && lock != nil {
		wfList = append(wfList, lock.TargetWorkflows...)
	}
	if len(wfList) == 0 {
		log.Fatalf("--workflows is required when no matrix lock exists")
	}

	workflowOrder, targetWorkflows := derivePromptMatrixWorkflows(wfList)
	if lock == nil {
		lock = &benchloop.MatrixLock{
			MatrixProposal: benchloop.MatrixProposal{
				Benchmark:       benchmark,
				RunSet:          runSet,
				Split:           split,
				ItemLimit:       itemLimit,
				Concurrency:     concurrency,
				WorkflowOrder:   workflowOrder,
				TargetWorkflows: targetWorkflows,
			},
		}
		lock.Normalize()
	}
	if state == nil {
		state = &benchloop.State{
			Iteration:       0,
			MaxIterations:   1,
			Benchmark:       lock.Benchmark,
			Split:           lock.Split,
			ItemLimit:       lock.ItemLimit,
			Concurrency:     lock.Concurrency,
			TargetWorkflows: append([]string(nil), lock.TargetWorkflows...),
		}
	}
	if ignoreLock {
		// Ensure emulation mode is deterministic from CLI flags even if state/lock files exist.
		state.Benchmark = lock.Benchmark
		state.Split = lock.Split
		state.ItemLimit = lock.ItemLimit
		state.Concurrency = lock.Concurrency
		state.TargetWorkflows = append([]string(nil), lock.TargetWorkflows...)
	}

	cfg := &benchloop.Config{
		Workflows:       append([]string(nil), wfList...),
		Benchmark:       lock.Benchmark,
		RunSet:          lock.RunSet,
		Split:           lock.Split,
		ItemLimit:       lock.ItemLimit,
		Concurrency:     lock.Concurrency,
		Model:           model,
		AllowModelSwaps: allowModelSwaps,
		Workdir:         resolvedWorkdir,
	}

	outputs := map[string]string{}
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "all":
		outputs["base_system"] = benchloop.BuildBaseSystemPrompt(cfg)
		outputs["iteration_system"] = benchloop.BuildIterationSystemPrompt(cfg)
		outputs["iteration"] = benchloop.BuildIterationPrompt(cfg, state, lock, memory)
	case "base-system":
		outputs["base_system"] = benchloop.BuildBaseSystemPrompt(cfg)
	case "iteration-system":
		outputs["iteration_system"] = benchloop.BuildIterationSystemPrompt(cfg)
	case "iteration":
		outputs["iteration"] = benchloop.BuildIterationPrompt(cfg, state, lock, memory)
	default:
		log.Fatalf("invalid --phase %q (use all|base-system|iteration-system|iteration)", phase)
	}

	if asJSON {
		payload := struct {
			Workdir string            `json:"workdir"`
			Phase   string            `json:"phase"`
			Outputs map[string]string `json:"outputs"`
		}{
			Workdir: resolvedWorkdir,
			Phase:   phase,
			Outputs: outputs,
		}
		out, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(out))
		return
	}

	for key, value := range outputs {
		fmt.Printf("=== %s ===\n\n%s\n\n", key, value)
	}
}

func derivePromptMatrixWorkflows(wfList []string) ([]string, []string) {
	workflowOrder := make([]string, 0, len(wfList))
	targetSet := make(map[string]struct{}, len(wfList)*2)
	for _, wf := range wfList {
		wf = strings.TrimSpace(wf)
		if wf == "" {
			continue
		}
		targetSet[wf] = struct{}{}
		if strings.HasPrefix(wf, "benchmark-") {
			workflowOrder = append(workflowOrder, wf)
			child := "reasoning-" + strings.TrimPrefix(wf, "benchmark-")
			targetSet[child] = struct{}{}
		}
	}
	if len(workflowOrder) == 0 {
		workflowOrder = append(workflowOrder, wfList...)
	}
	targetWorkflows := make([]string, 0, len(targetSet))
	for wf := range targetSet {
		targetWorkflows = append(targetWorkflows, wf)
	}
	slices.Sort(targetWorkflows)
	return workflowOrder, targetWorkflows
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func sessionsCommand(args []string) {
	var (
		workdir     string
		loopSession string
		all         bool
		limit       int
		asJSON      bool
	)
	fs := flag.NewFlagSet("sessions", flag.ExitOnError)
	fs.StringVar(&workdir, "workdir", "", "Project root (default: current directory)")
	fs.StringVar(&loopSession, "loop-session", "", "Filter by benchloop session_id (default: current active session)")
	fs.BoolVar(&all, "all", false, "Show records from all loop sessions in the index")
	fs.IntVar(&limit, "limit", 20, "Max records to print (0 = all)")
	fs.BoolVar(&asJSON, "json", false, "Print JSON instead of table")
	_ = fs.Parse(args)

	resolvedWorkdir, err := resolveWorkdir(workdir)
	if err != nil {
		log.Fatalf("Resolve workdir: %v", err)
	}

	records, err := benchloop.ReadSessionRecords(resolvedWorkdir)
	if err != nil {
		log.Fatalf("Read session index: %v", err)
	}
	liveRecord, liveErr := benchloop.ReadLiveSessionRecord(resolvedWorkdir)
	if liveErr != nil {
		log.Printf("Warning: read live session: %v", liveErr)
	} else {
		records = benchloop.MergeLiveSessionRecord(records, liveRecord)
	}
	if !all && loopSession == "" {
		current, err := benchloop.CurrentLoopSessionID(resolvedWorkdir)
		if err != nil {
			log.Fatalf("Read current loop session: %v", err)
		}
		loopSession = current
	}
	if loopSession != "" {
		filtered := make([]benchloop.SessionRecord, 0, len(records))
		for _, r := range records {
			if r.LoopSessionID == loopSession {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}

	if asJSON {
		out, _ := json.MarshalIndent(records, "", "  ")
		fmt.Println(string(out))
		return
	}

	if len(records) == 0 {
		fmt.Println("No session records found.")
		return
	}

	fmt.Printf("Session index: %s\n", benchloop.SessionIndexPath(resolvedWorkdir))
	if loopSession != "" {
		fmt.Printf("Loop session: %s\n", loopSession)
	}
	fmt.Printf("%-8s %-9s %-10s %-8s %-6s %-36s\n", "attempt", "iter", "phase", "state", "exit", "claude_session_id")
	for _, r := range records {
		stateLabel := "done"
		if r.InFlight {
			stateLabel = "running"
		}
		fmt.Printf("%-8d %-9d %-10s %-8s %-6s %-36s\n", r.Attempt, r.Iteration, r.Phase, stateLabel, formatExitCode(r), r.ClaudeSessionID)
	}
	fmt.Println("\nTip: benchloop transcript --attempt <N>")
}

func transcriptCommand(args []string) {
	var (
		workdir      string
		loopSession  string
		all          bool
		claudeID     string
		iteration    int
		attempt      int
		includeUser  bool
		includeTools bool
		maxMessages  int
		asJSON       bool
		raw          bool
	)
	fs := flag.NewFlagSet("transcript", flag.ExitOnError)
	fs.StringVar(&workdir, "workdir", "", "Project root (default: current directory)")
	fs.StringVar(&loopSession, "loop-session", "", "Filter by benchloop session_id (default: current active session)")
	fs.BoolVar(&all, "all", false, "Search across all loop sessions in the index")
	fs.StringVar(&claudeID, "session-id", "", "Claude session UUID")
	fs.IntVar(&iteration, "iteration", -1, "Logical loop iteration number")
	fs.IntVar(&attempt, "attempt", -1, "Monotonic benchloop attempt number")
	fs.BoolVar(&includeUser, "include-user", false, "Include user messages")
	fs.BoolVar(&includeTools, "include-tools", false, "Include tool calls and their results")
	fs.IntVar(&maxMessages, "max-messages", 20, "Maximum number of high-level messages to print (0 = all)")
	fs.BoolVar(&asJSON, "json", false, "Print JSON instead of text")
	fs.BoolVar(&raw, "raw", false, "Stream the raw Claude JSONL transcript to stdout")
	_ = fs.Parse(args)

	resolvedWorkdir, err := resolveWorkdir(workdir)
	if err != nil {
		log.Fatalf("Resolve workdir: %v", err)
	}

	records, err := benchloop.ReadSessionRecords(resolvedWorkdir)
	if err != nil {
		log.Fatalf("Read session index: %v", err)
	}
	liveRecord, liveErr := benchloop.ReadLiveSessionRecord(resolvedWorkdir)
	if liveErr != nil {
		log.Printf("Warning: read live session: %v", liveErr)
	} else {
		records = benchloop.MergeLiveSessionRecord(records, liveRecord)
	}
	if !all && loopSession == "" {
		current, err := benchloop.CurrentLoopSessionID(resolvedWorkdir)
		if err != nil {
			log.Fatalf("Read current loop session: %v", err)
		}
		loopSession = current
	}
	if loopSession != "" {
		filtered := make([]benchloop.SessionRecord, 0, len(records))
		for _, r := range records {
			if r.LoopSessionID == loopSession {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}

	record, err := selectSessionRecord(records, claudeID, iteration, attempt)
	if err != nil {
		if claudeID != "" {
			path, pathErr := benchloop.ClaudeTranscriptPath(resolvedWorkdir, claudeID)
			if pathErr != nil {
				log.Fatalf("Resolve transcript target: %v", err)
			}
			record = benchloop.SessionRecord{
				LoopSessionID:   loopSession,
				Phase:           "iteration",
				ClaudeSessionID: claudeID,
				TranscriptPath:  path,
				ExitCode:        -1,
				InFlight:        true,
			}
		} else {
			log.Fatalf("Resolve transcript target: %v", err)
		}
	}
	record.LogPath = benchloop.ResolveSessionLogPath(resolvedWorkdir, record)

	path := record.TranscriptPath
	if path == "" {
		path, err = benchloop.ClaudeTranscriptPath(resolvedWorkdir, record.ClaudeSessionID)
		if err != nil {
			log.Fatalf("Resolve transcript path: %v", err)
		}
	}
	if raw {
		f, err := os.Open(path)
		if err != nil {
			log.Fatalf("Open transcript %s: %v", path, err)
		}
		defer func() { _ = f.Close() }()
		if _, err := fmt.Fprintf(os.Stderr, "# %s\n", path); err != nil {
			log.Fatalf("Write stderr: %v", err)
		}
		if _, err := io.Copy(os.Stdout, f); err != nil {
			log.Fatalf("Stream transcript: %v", err)
		}
		return
	}

	messages, err := benchloop.ReadTranscriptMessages(path, benchloop.TranscriptReadOptions{
		IncludeUser:  includeUser,
		IncludeTools: includeTools,
		MaxMessages:  maxMessages,
	})
	if err != nil {
		log.Fatalf("Read transcript %s: %v", path, err)
	}

	if asJSON {
		payload := struct {
			Record   benchloop.SessionRecord       `json:"record"`
			Messages []benchloop.TranscriptMessage `json:"messages"`
		}{
			Record:   record,
			Messages: messages,
		}
		out, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(out))
		return
	}

	fmt.Printf("Loop session:   %s\n", record.LoopSessionID)
	fmt.Printf("Phase/iter:     %s / %d\n", record.Phase, record.Iteration)
	fmt.Printf("Attempt:        %d\n", record.Attempt)
	fmt.Printf("Claude session: %s\n", record.ClaudeSessionID)
	fmt.Printf("Transcript:     %s\n", path)
	if record.LogPath != "" {
		fmt.Printf("Benchloop log:  %s\n", record.LogPath)
	}
	if record.InFlight {
		fmt.Printf("Exit code:      running (in-flight)\n")
	} else {
		fmt.Printf("Exit code:      %d\n", record.ExitCode)
	}
	if !record.StartedAt.IsZero() {
		fmt.Printf("Started:        %s\n", record.StartedAt.Format(time.RFC3339))
	} else {
		fmt.Printf("Started:        (unknown)\n")
	}
	if !record.FinishedAt.IsZero() {
		fmt.Printf("Finished:       %s\n", record.FinishedAt.Format(time.RFC3339))
	}
	fmt.Println()

	if len(messages) == 0 {
		fmt.Println("No high-level text messages found in transcript.")
		return
	}
	fmt.Printf("Messages (%d):\n", len(messages))
	showPrefix := includeUser || includeTools
	for i, msg := range messages {
		prefix := ""
		if showPrefix {
			prefix = "[" + msg.Role + "] "
		}
		text := msg.Text
		if msg.Role != "tool_result" {
			text = compactText(text)
		} else {
			text = compactText(text)
		}
		fmt.Printf("%d. %s%s\n", i+1, prefix, text)
	}
}

func selectSessionRecord(records []benchloop.SessionRecord, claudeID string, iteration, attempt int) (benchloop.SessionRecord, error) {
	if len(records) == 0 {
		return benchloop.SessionRecord{}, fmt.Errorf("no matching session records")
	}

	// Pick the newest matching record by scanning from end.
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if claudeID != "" && r.ClaudeSessionID != claudeID {
			continue
		}
		if attempt >= 0 && r.Attempt != attempt {
			continue
		}
		if iteration >= 0 && r.Iteration != iteration {
			continue
		}
		return r, nil
	}

	return benchloop.SessionRecord{}, fmt.Errorf("no record matched session-id=%q iteration=%d attempt=%d", claudeID, iteration, attempt)
}

func resolveWorkdir(workdir string) (string, error) {
	if strings.TrimSpace(workdir) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return wd, nil
	}
	return filepath.Abs(workdir)
}

func compactText(s string) string {
	oneLine := strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	const maxLen = 320
	if len(oneLine) > maxLen {
		return oneLine[:maxLen-3] + "..."
	}
	return oneLine
}

func formatExitCode(record benchloop.SessionRecord) string {
	if record.InFlight {
		return "-"
	}
	return fmt.Sprintf("%d", record.ExitCode)
}

func printDryRun(cfg *benchloop.Config) {
	fmt.Println("=== Benchloop Dry Run ===")
	fmt.Printf("Workflows:          %s\n", strings.Join(cfg.Workflows, ", "))
	fmt.Printf("Benchmark:          %s\n", cfg.Benchmark)
	fmt.Printf("Run set:            %s\n", cfg.RunSet)
	if cfg.IsExplicit("split") {
		fmt.Printf("Split:              %s\n", cfg.Split)
	} else {
		fmt.Printf("Split:              (from existing matrix lock on --resume)\n")
	}
	if cfg.IsExplicit("item-limit") {
		fmt.Printf("Item limit:         %d\n", cfg.ItemLimit)
	} else {
		fmt.Printf("Item limit:         (from existing matrix lock on --resume)\n")
	}
	if cfg.IsExplicit("concurrency") {
		fmt.Printf("Concurrency:        %d\n", cfg.Concurrency)
	} else {
		fmt.Printf("Concurrency:        (from existing matrix lock on --resume)\n")
	}
	if cfg.BestRunID != "" {
		fmt.Printf("Baseline run:       %s\n", cfg.BestRunID)
	} else {
		fmt.Printf("Baseline run:       (iteration 1 will bootstrap)\n")
	}
	fmt.Printf("Max iterations:     %d\n", cfg.MaxIterations)
	fmt.Printf("Stop after plateau: %d\n", cfg.StopAfterPlateau)
	fmt.Printf("Iteration timeout:  %s\n", cfg.IterationTimeout)
	if cfg.TotalBudgetUSD > 0 {
		fmt.Printf("Total budget:       $%.2f (benchmark + agent)\n", cfg.TotalBudgetUSD)
	} else {
		fmt.Printf("Total budget:       unlimited\n")
	}
	if cfg.AgentBudgetUSD > 0 {
		fmt.Printf("Agent budget/iter:  $%.2f\n", cfg.AgentBudgetUSD)
	} else {
		fmt.Printf("Agent budget/iter:  unlimited\n")
	}
	fmt.Printf("Claude binary:      %s\n", cfg.ClaudeBin)
	fmt.Printf("Model:              %s\n", cfg.Model)
	fmt.Printf("Agent output:       %s\n", cfg.EffectiveAgentOutputFormat())
	fmt.Printf("Permissions:        bypass (fixed)\n")
	fmt.Printf("Allow model swaps:  %v\n", cfg.AllowModelSwaps)
	fmt.Printf("Workdir:            %s\n", cfg.Workdir)
	fmt.Println("\nNo actions taken (dry run).")
}
