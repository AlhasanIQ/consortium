package conctl

import (
	"flag"
	"net/url"
	"os"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// AdmissionResource returns the admission control resource.
func AdmissionResource() *app.Resource {
	return &app.Resource{
		Name: "admission",
		Desc: "Admission gate status and control",
		Commands: []*app.Command{
			admissionStatusCmd(),
			admissionPauseCmd(),
			admissionResumeCmd(),
		},
	}
}

func admissionStatusCmd() *app.Command {
	return &app.Command{
		Name:      "status",
		Desc:      "Get admission gate status",
		UsageLine: "conctl admission status",
		Run: func(gf app.GlobalFlags, args []string) int {
			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/admission", nil, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func admissionPauseCmd() *app.Command {
	return &app.Command{
		Name:      "pause",
		Desc:      "Pause admission for new root submissions",
		UsageLine: "conctl admission pause --yes [--message <text>] [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("admission pause", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			fs.String("message", "", "Optional reason message")
			fs.Bool("dry-run", false, "Dry run")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("admission pause", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			message := fs.String("message", "", "Optional reason message")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "admission pause"); !ok {
				return code
			}

			form := url.Values{}
			if trimmed := strings.TrimSpace(*message); trimmed != "" {
				form.Set("message", trimmed)
			}
			if *dryRun {
				return DryRunOutput("POST", "/api/admin/admission/pause", form)
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.PostForm(rc.Ctx, "/api/admin/admission/pause", form, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func admissionResumeCmd() *app.Command {
	return simpleMutateFor("admission", "resume", "Resume admission for new root submissions", "/api/admin/admission/resume")
}
