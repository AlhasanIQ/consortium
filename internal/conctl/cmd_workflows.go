package conctl

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// WorkflowsResource returns the workflows resource.
func WorkflowsResource() *app.Resource {
	return &app.Resource{
		Name: "workflows",
		Desc: "Workflow definitions (list, detail, export, update)",
		Commands: []*app.Command{
			workflowsListCmd(),
			workflowsGetCmd(),
			workflowsExportCmd(),
			workflowsUpdateCmd(),
			workflowsSeedsMatrixCmd(),
		},
	}
}

func workflowsListCmd() *app.Command {
	return &app.Command{
		Name:      "list",
		Desc:      "List all workflow definitions with stats",
		UsageLine: "conctl workflows list [--limit N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("workflows list", flag.ContinueOnError)
			fs.Int("limit", 50, "Max workflows to return")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("workflows list", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			limit := fs.Int("limit", 50, "Max workflows")

			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("limit", strconv.Itoa(*limit))

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/workflows", q, &data); err != nil {
				return HandleError(err)
			}

			exitCode := rc.Output(data, workflowsTable)
			hintTruncated(fs, *limit, countItems(data, "Workflows"), "workflows")
			return exitCode
		},
	}
}

func workflowsGetCmd() *app.Command {
	return &app.Command{
		Name:      "get",
		Desc:      "Get workflow detail including definition",
		UsageLine: "conctl workflows get --id <workflow-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("workflows get", flag.ContinueOnError)
			fs.String("id", "", "Workflow ID (required)")
			return fs
		},
		Run: simpleGetByIDWithTable("/api/admin/workflows/{id}", workflowGetTable),
	}
}

func workflowsExportCmd() *app.Command {
	return &app.Command{
		Name:      "export",
		Desc:      "Export workflow definition as clean JSON for editing.\n\nUses the core API (not admin) to return the raw workflow file format\n(id, name, description, nodes, edges). Pipe to a file for mutation.",
		UsageLine: "conctl workflows export --id <workflow-id> [--output <file>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("workflows export", flag.ContinueOnError)
			fs.String("id", "", "Workflow ID (required)")
			fs.String("output", "", "Write to file instead of stdout")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("workflows export", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.String("id", "", "Workflow ID (required)")
			output := fs.String("output", "", "Write to file instead of stdout")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *id == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			raw, err := rc.Client.GetRaw(rc.Ctx, "/api/admin/workflows/"+*id+"/export", nil)
			if err != nil {
				return HandleError(err)
			}

			// Pretty-print with seed-canonical key order.
			pretty, err := marshalWorkflowOrdered(raw)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return app.ExitInternal
			}

			if *output != "" {
				if err := os.WriteFile(*output, pretty, 0644); err != nil {
					fmt.Fprintf(os.Stderr, "Error: write %s: %v\n", *output, err)
					return app.ExitInternal
				}
				fmt.Fprintf(os.Stderr, "Exported %s to %s\n", *id, *output)
				return app.ExitSuccess
			}

			os.Stdout.Write(pretty) //nolint:errcheck
			return app.ExitSuccess
		},
	}
}

func workflowsUpdateCmd() *app.Command {
	return &app.Command{
		Name:      "update",
		Desc:      "Update a workflow definition from a JSON file (mutating).\n\nReads workflow JSON from a file (or stdin with --file -) and applies it\nvia PUT /api/workflows/{id}. The workflow ID in the URL takes precedence.",
		UsageLine: "conctl workflows update --id <workflow-id> --file <path|-> --yes [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("workflows update", flag.ContinueOnError)
			fs.String("id", "", "Workflow ID (required)")
			fs.String("file", "", "Workflow JSON file path, or - for stdin (required)")
			fs.Bool("yes", false, "Confirm mutation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("workflows update", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.String("id", "", "Workflow ID (required)")
			file := fs.String("file", "", "Workflow JSON file (required)")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *id == "" || *file == "" {
				fmt.Fprintln(os.Stderr, "Error: --id and --file are required")
				return app.ExitUsage
			}

			workflow, code := readJSONFileOrStdin(*file)
			if code != app.ExitSuccess {
				return code
			}

			if *dryRun {
				return DryRunOutput("PUT", "/api/admin/workflows/"+*id, workflow)
			}

			if code, ok := RequireYes(*yes, "workflows update"); !ok {
				return code
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.PutJSON(rc.Ctx, "/api/admin/workflows/"+*id, workflow, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

// workflowGetTable renders a concise summary of a single workflow.
func workflowGetTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	// Header from Workflow stats.
	wf, _ := m["Workflow"].(map[string]interface{})
	if wf != nil {
		fmt.Fprintf(w, "Workflow: %s\n", stringVal(wf, "id"))
		fmt.Fprintf(w, "Name:     %s\n", stringVal(wf, "name"))
		if desc := stringVal(wf, "description"); desc != "" {
			fmt.Fprintf(w, "Desc:     %s\n", desc)
		}
		fmt.Fprintf(w, "Nodes:    %.0f  Executions: %.0f  Success: %.1f%%  Cost: %s\n",
			floatVal(wf, "NodeCount"),
			floatVal(wf, "ExecutionCount"),
			floatVal(wf, "SuccessRate"),
			formatCostVal(floatVal(wf, "TotalCost")))
		if models, ok := wf["ModelsUsed"].([]interface{}); ok && len(models) > 0 {
			var names []string
			for _, mod := range models {
				if s, ok := mod.(string); ok {
					names = append(names, truncateModel(s))
				}
			}
			fmt.Fprintf(w, "Models:   %s\n", strings.Join(names, ", "))
		}
		fmt.Fprintln(w)
	}

	// Node table from Definition.
	def, _ := m["Definition"].(map[string]interface{})
	if def != nil {
		if nodes, ok := def["nodes"].([]interface{}); ok && len(nodes) > 0 {
			headers := []string{"NODE_ID", "TYPE", "MODEL"}
			var rows [][]string
			for _, n := range nodes {
				nm, ok := n.(map[string]interface{})
				if !ok {
					continue
				}
				// Model may be top-level or nested in data.config.
				model := stringVal(nm, "model")
				if model == "" {
					if data, ok := nm["data"].(map[string]interface{}); ok {
						if cfg, ok := data["config"].(map[string]interface{}); ok {
							model = stringVal(cfg, "model")
						}
					}
				}
				rows = append(rows, []string{
					stringVal(nm, "id"),
					stringVal(nm, "type"),
					truncateModel(model),
				})
			}
			printTableAuto(w, headers, rows, format)
			fmt.Fprintln(w)
		}

		// Edges.
		if edges, ok := def["edges"].([]interface{}); ok && len(edges) > 0 {
			fmt.Fprintln(w, "Edges:")
			for _, e := range edges {
				em, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				fmt.Fprintf(w, "  %s -> %s\n", stringVal(em, "source"), stringVal(em, "target"))
			}
			fmt.Fprintln(w)
		}
	}

	// Recent executions (last 3).
	if execs, ok := m["RecentExecutions"].([]interface{}); ok && len(execs) > 0 {
		limit := 3
		if len(execs) < limit {
			limit = len(execs)
		}
		fmt.Fprintf(w, "Recent Executions (%d of %d):\n", limit, len(execs))
		headers := []string{"ID", "STATUS", "COST", "TOKENS", "CREATED"}
		var rows [][]string
		for i := 0; i < limit; i++ {
			em, ok := execs[i].(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, []string{
				stringVal(em, "id"),
				stringVal(em, "status"),
				formatCostVal(floatVal(em, "cost")),
				fmt.Sprintf("%.0f", floatVal(em, "tokens_total")),
				truncate(stringVal(em, "created_at"), 19),
			})
		}
		printTableAuto(w, headers, rows, format)
	}
}

func workflowsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	wfs, ok := m["Workflows"].([]interface{})
	if !ok {
		return
	}
	headers := []string{"ID", "NAME", "EXECUTIONS", "SUCCESS%", "COST", "NODES"}
	var rows [][]string
	for _, wf := range wfs {
		wm, ok := wf.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, []string{
			stringVal(wm, "id"),
			stringVal(wm, "name"),
			fmt.Sprintf("%.0f", floatVal(wm, "ExecutionCount")),
			fmt.Sprintf("%.1f%%", floatVal(wm, "SuccessRate")),
			fmt.Sprintf("$%.4f", floatVal(wm, "TotalCost")),
			fmt.Sprintf("%.0f", floatVal(wm, "NodeCount")),
		})
	}
	printTableAuto(w, headers, rows, format)
}
