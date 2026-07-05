package conctl

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// BenchmarksResource returns the benchmarks resource.
func BenchmarksResource() *app.Resource {
	return &app.Resource{
		Name: "benchmarks",
		Desc: "Benchmark runs, comparison, analysis, and runner control",
		Commands: []*app.Command{
			benchmarksListCmd(),
			benchmarksRunnerStatusCmd(),
			benchmarksModelsCmd(),
			benchmarksImportCmd(),
			benchmarksRunCmd(),
			benchmarksCancelRunCmd(),
			benchmarksGetCmd(),
			benchmarksAnalyzeCmd(),
			benchmarksAnalysisCmd(),
			benchmarksRetriesCmd(),
			benchmarksItemCmd(),
			benchmarksDrillCmd(),
			benchmarksExplainCmd(),
			benchmarksExportCmd(),
			benchmarksRerunFailuresCmd(),
			benchmarksReplayItemsCmd(),
			benchmarksCompareCmd(),
			benchmarksCompareItemsCmd(),
			benchmarksDagCmd(),
			benchmarksFlagCmd(),
			benchmarksUnflagCmd(),
			benchmarksFlagsCmd(),
		},
	}
}

// BenchmarkResource returns a singular alias for BenchmarksResource.
func BenchmarkResource() *app.Resource {
	alias := BenchmarksResource()
	return &app.Resource{
		Name:     "benchmark",
		Desc:     alias.Desc + " (alias of benchmarks)",
		Commands: alias.Commands,
	}
}

func benchmarksListCmd() *app.Command {
	return &app.Command{
		Name:      "list",
		Desc:      "List benchmark runs with optional filters.\n\n--benchmark and --workflow use substring matching: \"mmlu\" matches \"global-mmlu-lite\", \"peer\" matches \"benchmark-peer-matrix\".",
		UsageLine: "conctl benchmarks list [--benchmark <name>] [--workflow <id>] [--status <status>] [--limit N] [--include-optimizer] [--watch]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks list", flag.ContinueOnError)
			fs.String("benchmark", "", "Filter by benchmark name")
			fs.String("workflow", "", "Filter by workflow ID")
			fs.String("status", "", "Filter by status")
			fs.Int("limit", 25, "Max runs to return")
			fs.Bool("include-optimizer", false, "Include optimizer-generated runs")
			fs.Bool("watch", false, "Poll continuously")
			fs.String("interval", "3s", "Poll interval")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks list", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			benchmark := fs.String("benchmark", "", "Filter by benchmark")
			workflow := fs.String("workflow", "", "Filter by workflow")
			status := fs.String("status", "", "Filter by status")
			limit := fs.Int("limit", 25, "Max runs")
			includeOptimizer := fs.Bool("include-optimizer", false, "Include optimizer-generated runs")
			watch := fs.Bool("watch", false, "Poll continuously")
			interval := fs.String("interval", "3s", "Poll interval")

			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			if *benchmark != "" {
				q.Set("benchmark", *benchmark)
			}
			if *workflow != "" {
				q.Set("workflow", *workflow)
			}
			if *status != "" {
				q.Set("status", *status)
			}
			if *includeOptimizer {
				q.Set("include_optimizer", "true")
			}
			q.Set("limit", strconv.Itoa(*limit))

			fetch := func() (interface{}, error) {
				var data interface{}
				if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks", q, &data); err != nil {
					return nil, err
				}
				return data, nil
			}

			if *watch {
				return runWatch(rc, *interval, fetch, benchmarksTable)
			}

			data, err := fetch()
			if err != nil {
				return HandleError(err)
			}
			exitCode := rc.Output(data, benchmarksTable)
			hintTruncated(fs, *limit, countItems(data, "Runs"), "benchmark runs")
			return exitCode
		},
	}
}

func benchmarksRunnerStatusCmd() *app.Command {
	return &app.Command{
		Name:      "runner-status",
		Desc:      "Get benchmark runner status",
		UsageLine: "conctl benchmarks runner-status [--watch | --wait-until <idle|running>] [--interval <dur>] [--timeout <dur>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks runner-status", flag.ContinueOnError)
			fs.Bool("watch", false, "Poll continuously")
			fs.String("wait-until", "", "Block until condition met: idle (running==false) or running (running==true)")
			fs.String("interval", "3s", "Poll interval")
			fs.String("timeout", "", "Max wait time for --wait-until (e.g. 5m, 30m). Overrides global --timeout")
			fs.Bool("diagnostics", false, "Include running job diagnostics (slowest active jobs)")
			fs.Int("jobs-limit", 5, "Max running jobs to include with --diagnostics")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks runner-status", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			watch := fs.Bool("watch", false, "Poll continuously")
			waitUntil := fs.String("wait-until", "", "Block until condition: idle or running")
			interval := fs.String("interval", "3s", "Poll interval")
			localTimeout := fs.String("timeout", "", "Max wait time for --wait-until")
			diagnostics := fs.Bool("diagnostics", false, "Include running job diagnostics")
			jobsLimit := fs.Int("jobs-limit", 5, "Max running jobs for diagnostics")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}

			// Local --timeout overrides global --timeout for wait-until.
			if *localTimeout != "" {
				gf.Timeout = *localTimeout
				gf.TimeoutExplicit = true
			}

			if *watch && *waitUntil != "" {
				fmt.Fprintln(os.Stderr, "Error: --watch and --wait-until are mutually exclusive")
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			fetch := func() (interface{}, error) {
				var data map[string]interface{}
				if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/runner-status", nil, &data); err != nil {
					return nil, err
				}
				if *diagnostics && boolVal(data, "running") {
					q := url.Values{}
					q.Set("status", "running")
					q.Set("show", "all")
					q.Set("limit", strconv.Itoa(*jobsLimit))
					var jobsData map[string]interface{}
					if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/jobs", q, &jobsData); err == nil {
						if jobs, ok := jobsData["Jobs"]; ok {
							data["_running_jobs"] = jobs
						} else if jobs, ok := jobsData["jobs"]; ok {
							data["_running_jobs"] = jobs
						}
					}
				}
				return data, nil
			}

			if *waitUntil != "" {
				wantRunning := false
				switch *waitUntil {
				case "idle":
					wantRunning = false
				case "running":
					wantRunning = true
				default:
					fmt.Fprintf(os.Stderr, "Error: --wait-until must be 'idle' or 'running', got %q\n", *waitUntil)
					return app.ExitUsage
				}
				// Default to 5s interval for wait-until (more appropriate than 3s watch default).
				if !isFlagSet(fs, "interval") {
					*interval = "5s"
				}
				check := func(data interface{}) bool {
					m, ok := data.(map[string]interface{})
					if !ok {
						return false
					}
					running, ok := m["running"].(bool)
					if !ok {
						return false
					}
					return running == wantRunning
				}
				return runWaitUntil(rc, *interval, fetch, check, benchmarkRunnerStatusTable)
			}

			if *watch {
				return runWatch(rc, *interval, fetch, benchmarkRunnerStatusTable)
			}

			data, err := fetch()
			if err != nil {
				return HandleError(err)
			}
			return rc.Output(data, benchmarkRunnerStatusTable)
		},
	}
}
