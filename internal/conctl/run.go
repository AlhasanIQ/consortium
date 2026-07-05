package conctl

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	chttp "github.com/alhasaniq/consortium/internal/conctl/http"
	"github.com/alhasaniq/consortium/internal/conctl/output"
)

// RunContext bundles common dependencies for command execution.
type RunContext struct {
	Client *chttp.Client
	GF     app.GlobalFlags
	Ctx    context.Context
	Cancel context.CancelFunc
}

// NewRunContext creates a RunContext from global flags with a timeout-bound context.
func NewRunContext(gf app.GlobalFlags) (*RunContext, error) {
	client, err := chttp.NewClient(gf.URL, gf.Timeout, gf.Verbose)
	if err != nil {
		return nil, err
	}
	d, _ := time.ParseDuration(gf.Timeout)
	ctx, cancel := context.WithTimeout(context.Background(), d)
	return &RunContext{
		Client: client,
		GF:     gf,
		Ctx:    ctx,
		Cancel: cancel,
	}, nil
}

// Output renders data to the configured output target in the requested format.
func (rc *RunContext) Output(data interface{}, tableFn output.TableFn) int {
	w, cleanup, err := output.Writer(rc.GF.Output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitInternal
	}
	defer cleanup()

	if err := output.Render(w, rc.GF.Format, data, tableFn); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitInternal
	}
	return app.ExitSuccess
}

// HandleError prints an API error to stderr and returns the appropriate exit code.
func HandleError(err error) int {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return chttp.ExitCode(err)
}

// RequireYes checks that --yes was provided for mutating commands.
func RequireYes(yes bool, action string) (int, bool) {
	if !yes {
		fmt.Fprintf(os.Stderr, "Error: %s requires --yes flag to confirm\n", action)
		return app.ExitUsage, false
	}
	return 0, true
}

// DryRunOutput prints the request plan for --dry-run.
func DryRunOutput(method, path string, params interface{}) int {
	fmt.Fprintf(os.Stderr, "[dry-run] %s %s\n", method, path)
	if params != nil {
		fmt.Fprintf(os.Stderr, "[dry-run] params: %+v\n", params)
	}
	return app.ExitSuccess
}
