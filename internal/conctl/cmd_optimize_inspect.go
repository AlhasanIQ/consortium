package conctl

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

func optimizeListCmd() *app.Command {
	return &app.Command{
		Name:      "list",
		Desc:      "List optimization runs",
		UsageLine: "conctl optimize list [--status <status>] [--workflow <id>] [--limit N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize list", flag.ContinueOnError)
			fs.String("status", "", "Filter by run status")
			fs.String("workflow", "", "Filter by workflow ID")
			fs.Int("limit", 20, "Max runs to return")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize list", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			status := fs.String("status", "", "Filter by run status")
			workflowID := fs.String("workflow", "", "Filter by workflow ID")
			limit := fs.Int("limit", 20, "Max runs")

			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			if strings.TrimSpace(*status) != "" {
				q.Set("status", strings.TrimSpace(*status))
			}
			if strings.TrimSpace(*workflowID) != "" {
				q.Set("workflow", strings.TrimSpace(*workflowID))
			}
			q.Set("limit", strconv.Itoa(*limit))

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/optimize/runs", q, &data); err != nil {
				return HandleError(err)
			}
			exitCode := rc.Output(data, optimizeRunsTable)
			hintTruncated(fs, *limit, countItems(data, "runs"), "optimization runs")
			return exitCode
		},
	}
}

func optimizeStatusCmd() *app.Command {
	return &app.Command{
		Name:      "status",
		Desc:      "Show optimization run status",
		UsageLine: "conctl optimize status --id <run-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize status", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			return optimizeStatusRun(gf, args)
		},
	}
}

func optimizeCompareCmd() *app.Command {
	return &app.Command{
		Name:      "compare",
		Desc:      "Compare optimization runs",
		UsageLine: "conctl optimize compare --ids <run-a,run-b,...> [--control <run-id>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize compare", flag.ContinueOnError)
			fs.String("ids", "", "Comma-separated optimization run IDs (required)")
			fs.String("control", "", "Optional control run ID for delta computation")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize compare", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			idsCSV := fs.String("ids", "", "Run IDs CSV")
			control := fs.String("control", "", "Control run ID")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}

			rawIDs := strings.Split(strings.TrimSpace(*idsCSV), ",")
			ids := make([]string, 0, len(rawIDs))
			for _, raw := range rawIDs {
				value := strings.TrimSpace(raw)
				if value != "" {
					ids = append(ids, value)
				}
			}
			ids = dedupeStringsPreserveOrder(ids)
			if len(ids) < 2 {
				fmt.Fprintln(os.Stderr, "Error: --ids requires at least two run IDs")
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("ids", strings.Join(ids, ","))
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/optimize/compare", q, &data); err != nil {
				return HandleError(err)
			}

			controlID := strings.TrimSpace(*control)
			if payload, ok := data.(map[string]interface{}); ok {
				if controlID == "" {
					controlID = deriveOptimizeControlRunID(payload)
				}
				payload["__control_id"] = controlID
			}

			return rc.Output(data, optimizeCompareTable)
		},
	}
}

func optimizeArtifactsCmd() *app.Command {
	return &app.Command{
		Name:      "artifacts",
		Desc:      "Show LLM mutation artifacts for an organism",
		UsageLine: "conctl optimize artifacts --organism <organism-id> [--prompt-only|--output-only]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize artifacts", flag.ContinueOnError)
			fs.String("organism", "", "Organism ID (required)")
			fs.Bool("prompt-only", false, "Show only input prompts")
			fs.Bool("output-only", false, "Show only raw outputs")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize artifacts", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			organismID := fs.String("organism", "", "Organism ID")
			promptOnly := fs.Bool("prompt-only", false, "Prompt only")
			outputOnly := fs.Bool("output-only", false, "Output only")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*organismID) == "" {
				fmt.Fprintln(os.Stderr, "Error: --organism is required")
				return app.ExitUsage
			}
			if *promptOnly && *outputOnly {
				fmt.Fprintln(os.Stderr, "Error: --prompt-only and --output-only are mutually exclusive")
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			path := "/api/admin/optimize/organisms/" + url.PathEscape(strings.TrimSpace(*organismID)) + "/mutation-artifacts"
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, nil, &data); err != nil {
				return HandleError(err)
			}
			if payload, ok := data.(map[string]interface{}); ok {
				payload["__organism_id"] = strings.TrimSpace(*organismID)
				if artifacts, ok := payload["artifacts"].([]interface{}); ok {
					for _, raw := range artifacts {
						artifact, ok := raw.(map[string]interface{})
						if !ok {
							continue
						}
						if *promptOnly {
							delete(artifact, "raw_output")
						}
						if *outputOnly {
							delete(artifact, "input_prompt")
						}
					}
				}
			}
			return rc.Output(data, optimizeArtifactsTable)
		},
	}
}

func optimizeSpecCmd() *app.Command {
	return &app.Command{
		Name:      "spec",
		Desc:      "Show frozen optimize spec for a run",
		UsageLine: "conctl optimize spec --id <run-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize spec", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize spec", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")

			rc, runID, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			path := "/api/admin/optimize/runs/" + url.PathEscape(runID)
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, nil, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, optimizeSpecTable)
		},
	}
}

func optimizeExportCmd() *app.Command {
	return &app.Command{
		Name:      "export",
		Desc:      "Export an organism's materialized workflow JSON",
		UsageLine: "conctl optimize export --id <run-id> [--organism <id> | --best] [--pretty]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize export", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("organism", "", "Specific organism ID")
			fs.Bool("best", true, "Export best organism when --organism is not provided")
			fs.Bool("pretty", true, "Pretty-print JSON")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize export", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			organismID := fs.String("organism", "", "Organism ID")
			best := fs.Bool("best", true, "Use best organism")
			pretty := fs.Bool("pretty", true, "Pretty-print JSON")

			rc, runID, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			resolvedOrganismID := strings.TrimSpace(*organismID)
			if resolvedOrganismID == "" {
				if !*best {
					fmt.Fprintln(os.Stderr, "Error: provide --organism or set --best=true")
					return app.ExitUsage
				}
				path := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/organisms"
				q := url.Values{}
				q.Set("best", "1")
				q.Set("limit", "1")
				var data interface{}
				if err := rc.Client.GetJSON(rc.Ctx, path, q, &data); err != nil {
					return HandleError(err)
				}
				m, ok := data.(map[string]interface{})
				if !ok {
					return HandleError(fmt.Errorf("invalid organisms response"))
				}
				list, ok := m["organisms"].([]interface{})
				if !ok || len(list) == 0 {
					return HandleError(fmt.Errorf("no best organism found for run %s", runID))
				}
				first, ok := list[0].(map[string]interface{})
				if !ok {
					return HandleError(fmt.Errorf("invalid best organism payload"))
				}
				resolvedOrganismID = strings.TrimSpace(stringVal(first, "id"))
				if resolvedOrganismID == "" {
					return HandleError(fmt.Errorf("best organism missing id"))
				}
			}

			detailPath := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/organisms/" + url.PathEscape(resolvedOrganismID)
			var detail interface{}
			if err := rc.Client.GetJSON(rc.Ctx, detailPath, nil, &detail); err != nil {
				return HandleError(err)
			}
			payload, ok := detail.(map[string]interface{})
			if !ok {
				return HandleError(fmt.Errorf("invalid organism detail response"))
			}
			organismPayload, ok := payload["organism"].(map[string]interface{})
			if !ok {
				return HandleError(fmt.Errorf("organism detail missing organism payload"))
			}
			workflowJSON, exists := organismPayload["workflow_json"]
			if !exists {
				return HandleError(fmt.Errorf("organism detail missing workflow_json"))
			}

			var out []byte
			var marshalErr error
			if *pretty {
				out, marshalErr = json.MarshalIndent(workflowJSON, "", "  ")
			} else {
				out, marshalErr = json.Marshal(workflowJSON)
			}
			if marshalErr != nil {
				return HandleError(fmt.Errorf("marshal workflow JSON: %w", marshalErr))
			}
			if _, err := os.Stdout.Write(append(out, '\n')); err != nil {
				return HandleError(fmt.Errorf("write workflow JSON: %w", err))
			}
			return app.ExitSuccess
		},
	}
}

func optimizeOrganismsCmd() *app.Command {
	return &app.Command{
		Name:      "organisms",
		Desc:      "List organisms for a run",
		UsageLine: "conctl optimize organisms --id <run-id> [--generation N] [--limit N] [--show-changes]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize organisms", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Int("generation", -1, "Filter by generation")
			fs.Int("limit", 20, "Max organisms")
			fs.Bool("show-changes", false, "Fetch and display abbreviated parameter changes per organism")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize organisms", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			generation := fs.Int("generation", -1, "Generation filter")
			limit := fs.Int("limit", 20, "Limit")
			showChanges := fs.Bool("show-changes", false, "Show abbreviated parameter changes")

			rc, runID, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			if *generation >= 0 {
				q.Set("generation", strconv.Itoa(*generation))
			}
			if *limit > 0 {
				q.Set("limit", strconv.Itoa(*limit))
			}
			path := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/organisms"
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, q, &data); err != nil {
				return HandleError(err)
			}

			if *showChanges {
				if payload, ok := data.(map[string]interface{}); ok {
					if organisms, ok := payload["organisms"].([]interface{}); ok && len(organisms) > 0 {
						summaries := make(map[string]string, len(organisms))
						for _, raw := range organisms {
							org, ok := raw.(map[string]interface{})
							if !ok {
								continue
							}
							orgID := strings.TrimSpace(stringVal(org, "id"))
							if orgID == "" {
								continue
							}
							detailPath := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/organisms/" + url.PathEscape(orgID)
							var detail interface{}
							if err := rc.Client.GetJSON(rc.Ctx, detailPath, nil, &detail); err != nil {
								summaries[orgID] = "(error fetching changes)"
								continue
							}
							detailMap, ok := detail.(map[string]interface{})
							if !ok {
								continue
							}
							changes, _ := detailMap["param_changes"].([]interface{})
							summaries[orgID] = summarizeOptimizeParamChanges(changes)
						}
						payload["__change_summaries"] = summaries
					}
				}
			}
			return rc.Output(data, optimizeOrganismsTable)
		},
	}
}

func optimizeBestCmd() *app.Command {
	return &app.Command{
		Name:      "best",
		Desc:      "Show best organism for a run",
		UsageLine: "conctl optimize best --id <run-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize best", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize best", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")

			rc, runID, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("best", "1")
			q.Set("limit", "1")
			path := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/organisms"
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, q, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, optimizeOrganismsTable)
		},
	}
}

func optimizeDiffCmd() *app.Command {
	return &app.Command{
		Name:      "diff",
		Desc:      "Show organism diff from parent using recorded param changes",
		UsageLine: "conctl optimize diff --id <run-id> --organism <organism-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize diff", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("organism", "", "Organism ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize diff", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			organismID := fs.String("organism", "", "Organism ID (required)")

			rc, runID, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			if strings.TrimSpace(*organismID) == "" {
				fmt.Fprintln(os.Stderr, "Error: --organism is required")
				return app.ExitUsage
			}

			path := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/organisms/" + url.PathEscape(strings.TrimSpace(*organismID))
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, nil, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, optimizeDiffTable)
		},
	}
}

func optimizeLineageCmd() *app.Command {
	return &app.Command{
		Name:      "lineage",
		Desc:      "Show lineage tree for a run or ancestry for a specific organism",
		UsageLine: "conctl optimize lineage [--id <run-id>] [--organism <organism-id>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize lineage", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("organism", "", "Optional organism ID to show ancestry path")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize lineage", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			runID := fs.String("id", "", "Run ID")
			organismID := fs.String("organism", "", "Organism ID")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*runID) == "" && strings.TrimSpace(*organismID) == "" {
				fmt.Fprintln(os.Stderr, "Error: provide --id or --organism")
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			path := ""
			if strings.TrimSpace(*organismID) != "" {
				path = "/api/admin/optimize/organisms/" + url.PathEscape(strings.TrimSpace(*organismID)) + "/lineage"
			} else {
				path = "/api/admin/optimize/runs/" + url.PathEscape(strings.TrimSpace(*runID)) + "/lineage"
			}
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, nil, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, optimizeLineageTable)
		},
	}
}

func optimizeLearningLogCmd() *app.Command {
	return &app.Command{
		Name:      "learning-log",
		Desc:      "Show mutation learning log",
		UsageLine: "conctl optimize learning-log --id <run-id> [--limit N] [--outcome <type>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize learning-log", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Int("limit", 50, "Max entries")
			fs.String("outcome", "", "Filter by outcome: improvement|regression|no_change|constraint_violation")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize learning-log", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			limit := fs.Int("limit", 50, "Max entries")
			outcome := fs.String("outcome", "", "Outcome filter")

			rc, runID, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			outcomeFilter := strings.ToLower(strings.TrimSpace(*outcome))
			if outcomeFilter != "" &&
				outcomeFilter != "improvement" &&
				outcomeFilter != "regression" &&
				outcomeFilter != "no_change" &&
				outcomeFilter != "constraint_violation" {
				fmt.Fprintln(os.Stderr, "Error: --outcome must be improvement, regression, no_change, or constraint_violation")
				return app.ExitUsage
			}

			q := url.Values{}
			if *limit > 0 {
				q.Set("limit", strconv.Itoa(*limit))
			}
			path := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/learning-log"
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, path, q, &data); err != nil {
				return HandleError(err)
			}
			if outcomeFilter != "" {
				if payload, ok := data.(map[string]interface{}); ok {
					if entries, ok := payload["entries"].([]interface{}); ok {
						filtered := make([]interface{}, 0, len(entries))
						for _, raw := range entries {
							entry, ok := raw.(map[string]interface{})
							if !ok {
								continue
							}
							if strings.ToLower(strings.TrimSpace(stringVal(entry, "outcome"))) == outcomeFilter {
								filtered = append(filtered, entry)
							}
						}
						payload["entries"] = filtered
					}
				}
			}
			return rc.Output(data, optimizeLearningLogTable)
		},
	}
}

type optimizeStatusClient interface {
	GetJSON(ctx context.Context, path string, query url.Values, out interface{}) error
}

func optimizeStatusRun(gf app.GlobalFlags, args []string) int {
	fs := flag.NewFlagSet("optimize status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.String("id", "", "Run ID (required)")

	rc, runID, code := parseIDArgs(gf, fs, args)
	if code != -1 {
		return code
	}
	defer rc.Cancel()

	data, err := fetchOptimizeRunStatusData(rc.Ctx, rc.Client, runID)
	if err != nil {
		return HandleError(err)
	}
	return rc.Output(data, optimizeRunStatusTable)
}

func fetchOptimizeRunStatusData(ctx context.Context, client optimizeStatusClient, runID string) (interface{}, error) {
	runID = strings.TrimSpace(runID)
	path := "/api/admin/optimize/runs/" + url.PathEscape(runID)
	var runData interface{}
	if err := client.GetJSON(ctx, path, nil, &runData); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("limit", "10000")
	orgPath := "/api/admin/optimize/runs/" + url.PathEscape(runID) + "/organisms"
	var organismsData interface{}
	if err := client.GetJSON(ctx, orgPath, q, &organismsData); err != nil {
		return runData, nil
	}
	runMap, ok := runData.(map[string]interface{})
	if !ok {
		return runData, nil
	}
	if m, ok := organismsData.(map[string]interface{}); ok {
		if organisms, ok := m["organisms"].([]interface{}); ok {
			runMap["__organisms"] = organisms
		}
	}
	runMap["__run_id"] = runID
	return runMap, nil
}
