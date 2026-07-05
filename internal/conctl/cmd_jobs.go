package conctl

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// JobsResource returns the jobs resource.
func JobsResource() *app.Resource {
	return &app.Resource{
		Name: "jobs",
		Desc: "Job management (list, detail, cancel, archive, bulk operations)",
		Commands: []*app.Command{
			jobsListCmd(),
			jobsGetCmd(),
			jobsNodesCmd(),
			jobsAttemptsCmd(),
			jobsAgentRunsCmd(),
			jobsStopAgentRunCmd(),
			jobsTraceCmd(),
			jobsConfigCmd(),
			jobsDiffCmd(),
			jobsChildrenCmd(),
			jobsExportCmd(),
			jobsCancelCmd(),
			jobsResumeCmd(),
			jobsRetryCmd(),
			jobsArchiveCmd(),
			jobsUnarchiveCmd(),
			jobsPauseAllCmd(),
			jobsResumeAllCmd(),
			jobsCancelAllCmd(),
		},
	}
}

func jobsListCmd() *app.Command {
	return &app.Command{
		Name:      "list",
		Desc:      "List jobs with optional filters.\n\n--show controls scope: parents (default) = root jobs only, children = child jobs only, all = both.",
		UsageLine: "conctl jobs list [--show parents|children|all] [--status <status>] [--page N] [--limit N] [--watch]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs list", flag.ContinueOnError)
			fs.String("show", "parents", "Filter: parents, children, all")
			fs.String("status", "", "Filter by status")
			fs.Int("page", 1, "Page number")
			fs.Int("limit", 25, "Items per page")
			fs.Bool("watch", false, "Poll continuously")
			fs.String("interval", "3s", "Poll interval")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs list", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			show := fs.String("show", "parents", "Filter: parents, children, all")
			status := fs.String("status", "", "Filter by status")
			page := fs.Int("page", 1, "Page number")
			limit := fs.Int("limit", 25, "Items per page")
			watch := fs.Bool("watch", false, "Poll continuously")
			interval := fs.String("interval", "3s", "Poll interval")

			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("show", *show)
			if *status != "" {
				q.Set("status", *status)
			}
			q.Set("page", strconv.Itoa(*page))
			q.Set("limit", strconv.Itoa(*limit))

			if *watch {
				return watchableGet(rc, true, *interval, "/api/admin/jobs", q, jobsTable)
			}

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/jobs", q, &data); err != nil {
				return HandleError(err)
			}
			exitCode := rc.Output(data, jobsTable)
			hintTruncated(fs, *limit, countItems(data, "Jobs"), "jobs")
			return exitCode
		},
	}
}

func jobsGetCmd() *app.Command {
	return &app.Command{
		Name:      "get",
		Desc:      "Get job detail with computed metrics.\n\nUnless --raw is set, adds a computed section: avg_node_latency_ms, p95_node_latency_ms,\ntotal_attempts, retry_attempts, end_to_end_ms, active_share_pct, queue_delay_ms.",
		UsageLine: "conctl jobs get --id <job-id> [--raw]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs get", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			fs.Bool("raw", false, "Skip computed metrics enrichment")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs get", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Job ID (required)")
			raw := fs.Bool("raw", false, "Skip computed metrics")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			var data map[string]interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/jobs/"+id, nil, &data); err != nil {
				return HandleError(err)
			}

			if !*raw {
				enrichJobDetail(data)
			}

			return rc.Output(data, jobGetTable)
		},
	}
}

func jobsNodesCmd() *app.Command {
	return &app.Command{
		Name:      "nodes",
		Desc:      "Get job nodes with metrics.\n\nAggregation child nodes (sub-nodes) are hidden from the table. Use --format json to see all nodes.",
		UsageLine: "conctl jobs nodes --id <job-id> [--search <text>] [--status <status>] [--type <type>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs nodes", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			fs.String("search", "", "Filter by node ID or model (substring)")
			fs.String("status", "", "Filter by status")
			fs.String("type", "", "Filter by node type")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs nodes", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Job ID (required)")
			search := fs.String("search", "", "Search filter")
			status := fs.String("status", "", "Status filter")
			nodeType := fs.String("type", "", "Type filter")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			if *search != "" {
				q.Set("search", *search)
			}
			if *status != "" {
				q.Set("status", *status)
			}
			if *nodeType != "" {
				q.Set("type", *nodeType)
			}

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/jobs/"+id+"/nodes", q, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, jobNodesTable)
		},
	}
}

func jobsAttemptsCmd() *app.Command {
	return &app.Command{
		Name:      "attempts",
		Desc:      "Get node execution attempts for a job",
		UsageLine: "conctl jobs attempts --id <job-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs attempts", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			return fs
		},
		Run: simpleGetByIDWithTable("/api/admin/jobs/{id}/node-execution-attempts", jobAttemptsTable),
	}
}

func jobsAgentRunsCmd() *app.Command {
	return &app.Command{
		Name:      "agent-runs",
		Desc:      "Get agent run summaries for a job",
		UsageLine: "conctl jobs agent-runs --id <job-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs agent-runs", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			return fs
		},
		Run: simpleGetByIDWithTable("/api/admin/jobs/{id}/agent-runs", jobAgentRunsTable),
	}
}

func jobsStopAgentRunCmd() *app.Command {
	return &app.Command{
		Name:      "stop-agent-run",
		Desc:      "Stop an active Novomo-backed agent run for a job",
		UsageLine: "conctl jobs stop-agent-run --id <job-id> --agent-run-id <agent-run-id> --yes [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs stop-agent-run", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			fs.String("agent-run-id", "", "Agent run row ID from jobs agent-runs (required)")
			fs.Bool("yes", false, "Confirm")
			fs.Bool("dry-run", false, "Dry run")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs stop-agent-run", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			jobID := fs.String("id", "", "Job ID (required)")
			agentRunID := fs.String("agent-run-id", "", "Agent run row ID (required)")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *jobID == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if *agentRunID == "" {
				fmt.Fprintln(os.Stderr, "Error: --agent-run-id is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "jobs stop-agent-run"); !ok {
				return code
			}

			path := "/api/admin/jobs/" + url.PathEscape(*jobID) + "/agent-runs/" + url.PathEscape(*agentRunID) + "/stop"
			if *dryRun {
				return DryRunOutput("POST", path, nil)
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.PostJSON(rc.Ctx, path, nil, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func jobsTraceCmd() *app.Command {
	return &app.Command{
		Name:      "trace",
		Desc:      "Get execution trace spans for a job",
		UsageLine: "conctl jobs trace --id <job-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs trace", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			return fs
		},
		Run: simpleGetByIDWithTable("/api/jobs/{id}/trace", jobTraceTable),
	}
}

func jobsConfigCmd() *app.Command {
	return &app.Command{
		Name:      "config",
		Desc:      "Get normalized config snapshot for a job",
		UsageLine: "conctl jobs config --id <job-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs config", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			return fs
		},
		Run: simpleGetByID("/api/jobs/{id}/config"),
	}
}

func jobsDiffCmd() *app.Command {
	return &app.Command{
		Name:      "diff",
		Desc:      "Compare normalized configs between two jobs",
		UsageLine: "conctl jobs diff --base <job-id> --candidate <job-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs diff", flag.ContinueOnError)
			fs.String("base", "", "Base job ID (required)")
			fs.String("candidate", "", "Candidate job ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs diff", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("base", "", "Base job ID (required)")
			fs.String("candidate", "", "Candidate job ID (required)")

			rc, base, candidate, code := parsePairArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			path := "/api/jobs/" + base + "/diff/" + candidate
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, nil, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func jobsChildrenCmd() *app.Command {
	return &app.Command{
		Name:      "children",
		Desc:      "List child jobs for a parent job",
		UsageLine: "conctl jobs children --id <job-id> [--status <status>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs children", flag.ContinueOnError)
			fs.String("id", "", "Parent job ID (required)")
			fs.String("status", "", "Filter by status")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs children", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Parent job ID (required)")
			status := fs.String("status", "", "Filter by status")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("parent", id)
			q.Set("show", "all")
			if *status != "" {
				q.Set("status", *status)
			}

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/jobs", q, &data); err != nil {
				return HandleError(err)
			}
			// Strip extraneous fields (AllCount, PoolCapacity, etc.) for cleaner output.
			if dataMap, ok := data.(map[string]interface{}); ok {
				slim := map[string]interface{}{
					"Jobs": dataMap["Jobs"],
				}
				if p, ok := dataMap["Pagination"]; ok {
					slim["Pagination"] = p
				}
				data = slim
			}
			return rc.Output(data, jobChildrenTable)
		},
	}
}

func jobsExportCmd() *app.Command {
	return &app.Command{
		Name:      "export",
		Desc:      "Export job detail with computed metrics",
		UsageLine: "conctl jobs export --id <job-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs export", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs export", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Job ID (required)")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			var data map[string]interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/jobs/"+id, nil, &data); err != nil {
				return HandleError(err)
			}

			enrichJobDetail(data)
			return rc.Output(data, nil)
		},
	}
}

func jobsCancelCmd() *app.Command {
	return simpleMutateByID("cancel", "Cancel a job", "/api/admin/jobs/{id}/cancel")
}

func jobsResumeCmd() *app.Command {
	return simpleMutateByID("resume", "Resume a paused job", "/api/admin/jobs/{id}/resume")
}

func jobsRetryCmd() *app.Command {
	return &app.Command{
		Name:      "retry",
		Desc:      "Retry a failed/cancelled job by submitting a fresh run",
		UsageLine: "conctl jobs retry --id <job-id> --yes [--admission-bypass] [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("jobs retry", flag.ContinueOnError)
			fs.String("id", "", "Job ID (required)")
			fs.Bool("yes", false, "Confirm")
			fs.Bool("admission-bypass", false, "Allow one-off probe while admission is paused")
			fs.Bool("dry-run", false, "Dry run")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("jobs retry", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.String("id", "", "Job ID (required)")
			yes := fs.Bool("yes", false, "Confirm")
			admissionBypass := fs.Bool("admission-bypass", false, "Allow one-off probe while admission is paused")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *id == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "jobs retry"); !ok {
				return code
			}

			path := "/api/admin/jobs/" + *id + "/retry"
			form := url.Values{}
			if *admissionBypass {
				form.Set("admission_bypass", "true")
			}
			return dryRunOrPostForm(gf, *dryRun, path, form)
		},
	}
}

func jobsArchiveCmd() *app.Command {
	return simpleMutateByID("archive", "Archive a job", "/api/admin/jobs/{id}/archive")
}

func jobsUnarchiveCmd() *app.Command {
	return simpleMutateByID("unarchive", "Unarchive a job", "/api/admin/jobs/{id}/unarchive")
}

func jobsPauseAllCmd() *app.Command {
	return simpleMutate("pause-all", "Pause all pending root jobs", "/api/admin/jobs/pause-all")
}

func jobsResumeAllCmd() *app.Command {
	return simpleMutate("resume-all", "Resume all paused jobs", "/api/admin/jobs/resume-all")
}

func jobsCancelAllCmd() *app.Command {
	return simpleMutate("cancel-all", "Cancel all pending/running/paused jobs", "/api/admin/jobs/cancel-all")
}
