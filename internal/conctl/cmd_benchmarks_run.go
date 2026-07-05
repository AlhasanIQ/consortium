package conctl

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

func benchmarksImportCmd() *app.Command {
	return simpleMutateFor("benchmarks", "import", "Import benchmark datasets", "/api/admin/benchmarks/import")
}

func benchmarksRunCmd() *app.Command {
	return &app.Command{
		Name:      "run",
		Desc:      "Start a benchmark run (cost-inducing).\n\nSplit mapping: --run-set full → test split, --run-set lite → dev/validation split,\n--run-set custom --split <name> for explicit split.",
		UsageLine: "conctl benchmarks run --yes --benchmarks <list> --workflows <list> [flags] [--wait]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks run", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm cost-inducing operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			fs.String("benchmarks", "", "Comma-separated benchmark names (required)")
			fs.String("workflows", "", "Comma-separated workflow IDs (required)")
			fs.String("run-set", "full", "Run set: full, lite (=small), custom")
			fs.String("split", "", "Split name (required for custom run-set)")
			fs.Int("limit", 20, "Item limit per run")
			fs.Int("concurrency", 20, "Concurrent workers")
			fs.Int("max-non-letter-retries", 2, "Max non-letter retries")
			fs.Int("max-transient-retries", 3, "Max transient retries")
			fs.String("source", "manual", "Run source metadata: manual, benchloop, optimizer, imported, replay")
			fs.Bool("pause-on-fatal", true, "Pause on fatal errors")
			fs.Int("fatal-repeat-threshold", 3, "Fatal repeat threshold")
			fs.Bool("wait", false, "Wait for runner to return to idle before exiting")
			fs.String("interval", "5s", "Polling interval used with --wait")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks run", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			benchmarks := fs.String("benchmarks", "", "Benchmarks (required)")
			workflows := fs.String("workflows", "", "Workflows (required)")
			runSet := fs.String("run-set", "full", "Run set: full, lite (=small), custom")
			split := fs.String("split", "", "Split name")
			limit := fs.Int("limit", 20, "Limit")
			concurrency := fs.Int("concurrency", 20, "Concurrency")
			maxNonLetterRetries := fs.Int("max-non-letter-retries", 2, "Max non-letter retries")
			maxTransientRetries := fs.Int("max-transient-retries", 3, "Max transient retries")
			source := fs.String("source", "manual", "Run source metadata")
			pauseOnFatal := fs.Bool("pause-on-fatal", true, "Pause on fatal")
			fatalRepeatThreshold := fs.Int("fatal-repeat-threshold", 3, "Fatal repeat threshold")
			wait := fs.Bool("wait", false, "Wait until runner idle")
			interval := fs.String("interval", "5s", "Poll interval for --wait")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}

			if *benchmarks == "" || *workflows == "" {
				fmt.Fprintln(os.Stderr, "Error: --benchmarks and --workflows are required")
				return app.ExitUsage
			}
			sourceValue := strings.ToLower(strings.TrimSpace(*source))
			switch sourceValue {
			case "", "manual", "benchloop", "optimizer", "imported", "replay":
			default:
				fmt.Fprintln(os.Stderr, "Error: --source must be one of manual, benchloop, optimizer, imported, replay")
				return app.ExitUsage
			}

			if code, ok := RequireYes(*yes, "benchmarks run"); !ok {
				return code
			}

			// CLI alias: "lite" → "small" (backend accepts small|full|custom).
			resolvedRunSet := *runSet
			if resolvedRunSet == "lite" {
				resolvedRunSet = "small"
			}

			// Build form data (matches backend's ParseForm encoding).
			form := url.Values{}
			form.Set("benchmarks", *benchmarks)
			form.Set("workflows", *workflows)
			form.Set("run_set", resolvedRunSet)
			if *split != "" {
				form.Set("split", *split)
			}
			form.Set("limit", strconv.Itoa(*limit))
			form.Set("concurrency", strconv.Itoa(*concurrency))
			form.Set("max_non_letter_retries", strconv.Itoa(*maxNonLetterRetries))
			form.Set("max_transient_retries", strconv.Itoa(*maxTransientRetries))
			if sourceValue != "" {
				form.Set("source", sourceValue)
			}
			if !*pauseOnFatal {
				form.Set("pause_on_fatal", "false")
			}
			form.Set("fatal_repeat_threshold", strconv.Itoa(*fatalRepeatThreshold))

			if *dryRun {
				return DryRunOutput("POST", "/api/admin/benchmarks/run", form)
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.PostForm(rc.Ctx, "/api/admin/benchmarks/run", form, &data); err != nil {
				return HandleError(err)
			}
			if !*wait {
				return rc.Output(data, nil)
			}

			fmt.Fprintln(os.Stderr, "Run submitted. Waiting for benchmark runner to become idle...")
			fetchStatus := func() (interface{}, error) {
				var status map[string]interface{}
				if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/runner-status", nil, &status); err != nil {
					return nil, err
				}
				return status, nil
			}
			isIdle := func(payload interface{}) bool {
				m, ok := payload.(map[string]interface{})
				if !ok {
					return false
				}
				running, ok := m["running"].(bool)
				if !ok {
					return false
				}
				return !running
			}
			return runWaitUntil(rc, *interval, fetchStatus, isIdle, benchmarkRunnerStatusTable)
		},
	}
}

func benchmarksCancelRunCmd() *app.Command {
	return simpleMutateFor("benchmarks", "cancel-run", "Cancel the active benchmark run", "/api/admin/benchmarks/run/cancel")
}

func benchmarksRerunFailuresCmd() *app.Command {
	return &app.Command{
		Name:      "rerun-failures",
		Desc:      "Rerun failed items in a benchmark run (cost-inducing)",
		UsageLine: "conctl benchmarks rerun-failures --id <run-id> --yes [--item <item-id>] [--admission-bypass] [--concurrency N] [--max-non-letter-retries N] [--max-transient-retries N] [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks rerun-failures", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Bool("yes", false, "Confirm")
			fs.Bool("dry-run", false, "Dry run")
			fs.String("item", "", "Optional single failed item ID to rerun")
			fs.Bool("admission-bypass", false, "Allow one-item probe while admission is paused")
			fs.Int("concurrency", 20, "Concurrency")
			fs.Int("max-non-letter-retries", 2, "Max non-letter retries")
			fs.Int("max-transient-retries", 3, "Max transient retries")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks rerun-failures", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.String("id", "", "Run ID (required)")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			item := fs.String("item", "", "Optional single failed item ID")
			admissionBypass := fs.Bool("admission-bypass", false, "Allow one-item probe while admission is paused")
			concurrency := fs.Int("concurrency", 20, "Concurrency")
			maxNonLetterRetries := fs.Int("max-non-letter-retries", 2, "Max non-letter retries")
			maxTransientRetries := fs.Int("max-transient-retries", 3, "Max transient retries")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *id == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "benchmarks rerun-failures"); !ok {
				return code
			}

			form := url.Values{}
			form.Set("concurrency", strconv.Itoa(*concurrency))
			form.Set("max_non_letter_retries", strconv.Itoa(*maxNonLetterRetries))
			form.Set("max_transient_retries", strconv.Itoa(*maxTransientRetries))
			if strings.TrimSpace(*item) != "" {
				form.Set("item", strings.TrimSpace(*item))
			}
			if *admissionBypass {
				form.Set("admission_bypass", "true")
			}

			return dryRunOrPostForm(gf, *dryRun, "/api/admin/benchmarks/"+*id+"/rerun-failures", form)
		},
	}
}

func benchmarksReplayItemsCmd() *app.Command {
	return &app.Command{
		Name: "replay-items",
		Desc: "Run targeted benchmark items with replay seeding from a baseline run (cost-inducing).\n\n" +
			"Use this for fast hypothesis checks after workflow edits: replay only the specified item IDs,\n" +
			"reuse unchanged upstream nodes from the baseline run, then execute changed/downstream nodes.",
		UsageLine: "conctl benchmarks replay-items --id <base-run-id> --items <csv> --yes [--workflow <id>] [--changed-workflows <csv>] [--mode best_effort|required] [--admission-bypass] [--concurrency N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks replay-items", flag.ContinueOnError)
			fs.String("id", "", "Baseline run ID (required)")
			fs.String("items", "", "Comma-separated item IDs (required)")
			fs.String("workflow", "", "Workflow ID override (default: baseline run workflow)")
			fs.String("changed-workflows", "", "Comma-separated workflow IDs that changed (used to force child_workflow node re-execution)")
			fs.String("mode", "best_effort", "Replay mode: best_effort|required|off")
			fs.Int("concurrency", 1, "Concurrency")
			fs.Int("max-non-letter-retries", 1, "Max non-letter retries")
			fs.Int("max-transient-retries", 1, "Max transient retries")
			fs.Bool("admission-bypass", false, "Allow one-item probe while admission is paused")
			fs.Bool("yes", false, "Confirm")
			fs.Bool("dry-run", false, "Dry run")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks replay-items", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.String("id", "", "Baseline run ID (required)")
			items := fs.String("items", "", "Comma-separated item IDs (required)")
			workflowID := fs.String("workflow", "", "Workflow ID override")
			changedWorkflows := fs.String("changed-workflows", "", "Changed workflow IDs")
			mode := fs.String("mode", "best_effort", "Replay mode")
			concurrency := fs.Int("concurrency", 1, "Concurrency")
			maxNonLetterRetries := fs.Int("max-non-letter-retries", 1, "Max non-letter retries")
			maxTransientRetries := fs.Int("max-transient-retries", 1, "Max transient retries")
			admissionBypass := fs.Bool("admission-bypass", false, "Allow one-item probe while admission is paused")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}

			if *id == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if strings.TrimSpace(*items) == "" {
				fmt.Fprintln(os.Stderr, "Error: --items is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "benchmarks replay-items"); !ok {
				return code
			}

			form := url.Values{}
			form.Set("items", *items)
			form.Set("mode", *mode)
			form.Set("concurrency", strconv.Itoa(*concurrency))
			form.Set("max_non_letter_retries", strconv.Itoa(*maxNonLetterRetries))
			form.Set("max_transient_retries", strconv.Itoa(*maxTransientRetries))
			if strings.TrimSpace(*workflowID) != "" {
				form.Set("workflow", strings.TrimSpace(*workflowID))
			}
			if strings.TrimSpace(*changedWorkflows) != "" {
				form.Set("changed_workflows", strings.TrimSpace(*changedWorkflows))
			}
			if *admissionBypass {
				form.Set("admission_bypass", "true")
			}

			return dryRunOrPostForm(gf, *dryRun, "/api/admin/benchmarks/"+*id+"/replay-items", form)
		},
	}
}
