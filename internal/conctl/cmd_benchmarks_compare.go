package conctl

import (
	"flag"
	"net/url"
	"os"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

func benchmarksCompareCmd() *app.Command {
	return &app.Command{
		Name:      "compare",
		Desc:      "Compare benchmark runs across workflows.\n\nWithout --control-workflow, the run with highest accuracy is used as the implicit control\nfor delta calculations. --selected-run is repeatable to narrow comparison scope.",
		UsageLine: "conctl benchmarks compare [--benchmark <name>] [--split <split>] [--control-workflow <id>] [--selected-run <id>]...",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks compare", flag.ContinueOnError)
			fs.String("benchmark", "", "Benchmark name filter")
			fs.String("split", "", "Split filter")
			fs.String("control-workflow", "", "Control workflow ID")
			// Note: --selected-run is repeatable; handled manually.
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			// Extract --selected-run values before fs.Parse (repeatable flag, not registered on FlagSet).
			var selectedRuns []string
			var filteredArgs []string
			for i := 0; i < len(args); i++ {
				if args[i] == "--selected-run" && i+1 < len(args) {
					selectedRuns = append(selectedRuns, args[i+1])
					i++
				} else if strings.HasPrefix(args[i], "--selected-run=") {
					selectedRuns = append(selectedRuns, strings.TrimPrefix(args[i], "--selected-run="))
				} else {
					filteredArgs = append(filteredArgs, args[i])
				}
			}

			fs := flag.NewFlagSet("benchmarks compare", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			benchmark := fs.String("benchmark", "", "Benchmark")
			split := fs.String("split", "", "Split")
			controlWorkflow := fs.String("control-workflow", "", "Control workflow")
			if err := fs.Parse(filteredArgs); err != nil {
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			q := url.Values{}
			if *benchmark != "" {
				q.Set("benchmark", *benchmark)
			}
			if *split != "" {
				q.Set("split", *split)
			}
			if *controlWorkflow != "" {
				q.Set("control", *controlWorkflow)
			}

			var data map[string]interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/compare", q, &data); err != nil {
				return HandleError(err)
			}

			// Client-side: recompute deltas if selected runs provided.
			if len(selectedRuns) > 0 {
				enrichCompareData(data, selectedRuns, *controlWorkflow)
			}

			return rc.Output(data, compareTable)
		},
	}
}

func benchmarksCompareItemsCmd() *app.Command {
	return &app.Command{
		Name:      "compare-items",
		Desc:      "Compare items between two benchmark runs",
		UsageLine: "conctl benchmarks compare-items --base <run-id> --candidate <run-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks compare-items", flag.ContinueOnError)
			fs.String("base", "", "Base run ID (required)")
			fs.String("candidate", "", "Candidate run ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks compare-items", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("base", "", "Base run ID (required)")
			fs.String("candidate", "", "Candidate run ID (required)")

			rc, base, candidate, code := parsePairArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("base", base)
			q.Set("candidate", candidate)

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/compare-items", q, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, compareItemsTable)
		},
	}
}
