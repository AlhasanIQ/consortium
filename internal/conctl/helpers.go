package conctl

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	"github.com/alhasaniq/consortium/internal/conctl/output"
)

// simpleWatchableGet returns a Run function for commands that support --watch + --interval
// and delegate to watchableGet. Eliminates repeated flag-parse + RunContext boilerplate.
func simpleWatchableGet(path string, tableFn output.TableFn) func(app.GlobalFlags, []string) int {
	return func(gf app.GlobalFlags, args []string) int {
		fs := flag.NewFlagSet("", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		watch := fs.Bool("watch", false, "Poll continuously")
		interval := fs.String("interval", "3s", "Poll interval")
		if err := fs.Parse(args); err != nil {
			return app.ExitUsage
		}

		rc, err := NewRunContext(gf)
		if err != nil {
			return HandleError(err)
		}
		defer rc.Cancel()

		return watchableGet(rc, *watch, *interval, path, nil, tableFn)
	}
}

// readJSONFileOrStdin reads and decodes a JSON value from a file path or stdin (when path is "-").
func readJSONFileOrStdin(path string) (interface{}, int) {
	var reader io.Reader
	if path == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: open %s: %v\n", path, err)
			return nil, app.ExitUsage
		}
		defer f.Close() //nolint:errcheck
		reader = f
	}

	var data interface{}
	if err := json.NewDecoder(reader).Decode(&data); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid JSON: %v\n", err)
		return nil, app.ExitUsage
	}
	return data, app.ExitSuccess
}

// simpleGetByIDWithTable returns a Run function for GET /path/{id} commands with a table formatter.
func simpleGetByIDWithTable(pathTemplate string, tableFn output.TableFn) func(app.GlobalFlags, []string) int {
	return func(gf app.GlobalFlags, args []string) int {
		fs := flag.NewFlagSet("", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		id := fs.String("id", "", "ID (required)")
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

		path := strings.Replace(pathTemplate, "{id}", *id, 1)
		var data interface{}
		if err := rc.Client.GetJSON(rc.Ctx, path, nil, &data); err != nil {
			return HandleError(err)
		}
		return rc.Output(data, tableFn)
	}
}

// simpleGetByID returns a Run function for GET /path/{id} commands.
func simpleGetByID(pathTemplate string) func(app.GlobalFlags, []string) int {
	return simpleGetByIDWithTable(pathTemplate, nil)
}

// watchableGet encapsulates the watch-or-single-fetch pattern used by many list/status commands.
// query may be nil for endpoints with no query params.
// tableFn is the table renderer (may be nil for JSON-only output).
func watchableGet(
	rc *RunContext,
	watch bool,
	interval string,
	path string,
	query url.Values,
	tableFn output.TableFn,
) int {
	fetch := func() (interface{}, error) {
		var data interface{}
		if err := rc.Client.GetJSON(rc.Ctx, path, query, &data); err != nil {
			return nil, err
		}
		return data, nil
	}

	if watch {
		return runWatch(rc, interval, fetch, tableFn)
	}

	data, err := fetch()
	if err != nil {
		return HandleError(err)
	}
	return rc.Output(data, tableFn)
}

// simpleMutateByIDFor returns a Command for POST /path/{id} (mutating, requires --yes).
// resource is the parent resource name used in usage text (e.g. "jobs", "admission").
func simpleMutateByIDFor(resource, name, desc, pathTemplate string) *app.Command {
	qual := resource + " " + name
	return &app.Command{
		Name:      name,
		Desc:      desc,
		UsageLine: fmt.Sprintf("conctl %s --id <id> --yes [--dry-run]", qual),
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet(qual, flag.ContinueOnError)
			fs.String("id", "", "ID (required)")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet(qual, flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.String("id", "", "ID (required)")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *id == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, qual); !ok {
				return code
			}

			path := strings.Replace(pathTemplate, "{id}", *id, 1)

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

// simpleMutateByID returns a Command for POST /path/{id} scoped to the "jobs" resource.
func simpleMutateByID(name, desc, pathTemplate string) *app.Command {
	return simpleMutateByIDFor("jobs", name, desc, pathTemplate)
}

// simpleMutateFor returns a Command for POST /path (mutating, requires --yes, no ID).
// resource is the parent resource name used in usage text (e.g. "jobs", "admission").
func simpleMutateFor(resource, name, desc, path string) *app.Command {
	qual := resource + " " + name
	return &app.Command{
		Name:      name,
		Desc:      desc,
		UsageLine: fmt.Sprintf("conctl %s --yes [--dry-run]", qual),
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet(qual, flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet(qual, flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, qual); !ok {
				return code
			}

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

// simpleMutate returns a Command for POST /path scoped to the "jobs" resource.
func simpleMutate(name, desc, path string) *app.Command {
	return simpleMutateFor("jobs", name, desc, path)
}

// parseIDArgs handles the common preamble for commands that require --id:
// parse a FlagSet (which must have --id defined), validate it is non-empty,
// and create a RunContext. Returns (rc, id, exitCode). On success exitCode is -1;
// otherwise the caller should return exitCode immediately.
// The caller is responsible for deferring rc.Cancel().
func parseIDArgs(gf app.GlobalFlags, fs *flag.FlagSet, args []string) (*RunContext, string, int) {
	if err := fs.Parse(args); err != nil {
		return nil, "", app.ExitUsage
	}
	id := strings.TrimSpace(fs.Lookup("id").Value.String())
	if id == "" {
		fmt.Fprintln(os.Stderr, "Error: --id is required")
		return nil, "", app.ExitUsage
	}

	rc, err := NewRunContext(gf)
	if err != nil {
		return nil, "", HandleError(err)
	}
	return rc, id, -1
}

// parseBenchmarkItemArgs handles the common preamble for benchmark item commands:
// parse a FlagSet (which must have --id and --item defined), validate both required,
// normalize the item ID, create a RunContext, and fetch the item detail.
// The caller's FlagSet should already have --id and --item defined along with any extra flags.
// Returns (rc, runID, itemData, exitCode). On success exitCode is -1; otherwise the caller
// should return exitCode immediately. The caller is responsible for deferring rc.Cancel().
func parseBenchmarkItemArgs(gf app.GlobalFlags, fs *flag.FlagSet, args []string) (*RunContext, string, map[string]interface{}, int) {
	if err := fs.Parse(args); err != nil {
		return nil, "", nil, app.ExitUsage
	}
	id := fs.Lookup("id").Value.String()
	item := fs.Lookup("item").Value.String()
	if id == "" || item == "" {
		fmt.Fprintln(os.Stderr, "Error: --id and --item are required")
		return nil, "", nil, app.ExitUsage
	}
	itemID := normalizeBenchmarkItemID(item)

	rc, err := NewRunContext(gf)
	if err != nil {
		return nil, "", nil, HandleError(err)
	}

	data, err := fetchBenchmarkItemDetail(rc, id, itemID)
	if err != nil {
		rc.Cancel()
		return nil, "", nil, HandleError(err)
	}
	return rc, id, data, -1
}

// injectExpectedAnswer sets _expected_answer on item data from the DatasetItem's answer_label.
func injectExpectedAnswer(data map[string]interface{}) {
	if dsItem, ok := data["DatasetItem"].(map[string]interface{}); ok {
		data["_expected_answer"] = stringVal(dsItem, "answer_label")
	}
}

// parseArgs handles the common preamble for commands without a required --id:
// parse a FlagSet and create a RunContext. Returns (rc, exitCode). On success
// exitCode is -1; otherwise the caller should return exitCode immediately.
// The caller is responsible for deferring rc.Cancel().
func parseArgs(gf app.GlobalFlags, fs *flag.FlagSet, args []string) (*RunContext, int) {
	if err := fs.Parse(args); err != nil {
		return nil, app.ExitUsage
	}

	rc, err := NewRunContext(gf)
	if err != nil {
		return nil, HandleError(err)
	}
	return rc, -1
}

// parsePairArgs handles the common preamble for commands requiring --base and --candidate:
// parse a FlagSet (which must have --base and --candidate defined), validate both non-empty,
// and create a RunContext. Returns (rc, base, candidate, exitCode). On success exitCode is -1;
// otherwise the caller should return exitCode immediately.
// The caller is responsible for deferring rc.Cancel().
func parsePairArgs(gf app.GlobalFlags, fs *flag.FlagSet, args []string) (*RunContext, string, string, int) {
	if err := fs.Parse(args); err != nil {
		return nil, "", "", app.ExitUsage
	}
	base := strings.TrimSpace(fs.Lookup("base").Value.String())
	candidate := strings.TrimSpace(fs.Lookup("candidate").Value.String())
	if base == "" || candidate == "" {
		fmt.Fprintln(os.Stderr, "Error: --base and --candidate are required")
		return nil, "", "", app.ExitUsage
	}

	rc, err := NewRunContext(gf)
	if err != nil {
		return nil, "", "", HandleError(err)
	}
	return rc, base, candidate, -1
}

// dryRunOrPostForm handles the common pattern for mutating commands that support --dry-run:
// if dryRun is true, print the request plan; otherwise create a RunContext, POST the form,
// and output the result. Returns an exit code.
func dryRunOrPostForm(gf app.GlobalFlags, dryRun bool, path string, form url.Values) int {
	if dryRun {
		return DryRunOutput("POST", path, form)
	}

	rc, err := NewRunContext(gf)
	if err != nil {
		return HandleError(err)
	}
	defer rc.Cancel()

	var data interface{}
	if err := rc.Client.PostForm(rc.Ctx, path, form, &data); err != nil {
		return HandleError(err)
	}
	return rc.Output(data, nil)
}

// isFlagSet returns true if the named flag was explicitly set on the command line.
func isFlagSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// hintTruncated prints a stderr notice when a default limit caused output truncation.
// It checks whether --limit was explicitly set by the user; if not and the displayed
// count equals (or exceeds) the limit, it warns that results were truncated.
func hintTruncated(fs *flag.FlagSet, limit int, displayed int, noun string) {
	if displayed < limit {
		return
	}
	// Only hint when the user didn't explicitly pass --limit.
	if isFlagSet(fs, "limit") {
		return
	}
	fmt.Fprintf(os.Stderr, "\nShowing first %d %s (truncated). Use --limit 0 to see all.\n", limit, noun)
}
