package conctl

import (
	"flag"
	"fmt"
	"os"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// TestResource returns the test resource.
func TestResource() *app.Resource {
	return &app.Resource{
		Name: "test",
		Desc: "Test workflow execution (cost-inducing)",
		Commands: []*app.Command{
			testWorkflowCmd(),
		},
	}
}

func testWorkflowCmd() *app.Command {
	return &app.Command{
		Name:      "workflow",
		Desc:      "Execute a workflow for testing (cost-inducing)",
		UsageLine: "conctl test workflow --file <path|-> --yes [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("test workflow", flag.ContinueOnError)
			fs.String("file", "", "Workflow JSON file path, or - for stdin (required)")
			fs.Bool("yes", false, "Confirm cost-inducing operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("test workflow", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			file := fs.String("file", "", "Workflow JSON file (required)")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *file == "" {
				fmt.Fprintln(os.Stderr, "Error: --file is required")
				return app.ExitUsage
			}

			if code, ok := RequireYes(*yes, "test workflow"); !ok {
				return code
			}

			workflow, code := readJSONFileOrStdin(*file)
			if code != app.ExitSuccess {
				return code
			}

			body := map[string]interface{}{
				"workflow": workflow,
			}

			if *dryRun {
				return DryRunOutput("POST", "/api/admin/test/workflow", body)
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.PostJSON(rc.Ctx, "/api/admin/test/workflow", body, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}
