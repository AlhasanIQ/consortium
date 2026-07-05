package conctl

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/alhasaniq/consortium/pkg/seeds"
)

const (
	optimizeLeaseTTL           = 30 * time.Second
	optimizeHeartbeatInterval  = 10 * time.Second
	optimizeDefaultQuickChecks = 15
)

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

func (f *repeatedStringFlag) Values() []string {
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(*f))
	for _, value := range *f {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// OptimizeResource returns optimization orchestration commands.
func OptimizeResource() *app.Resource {
	return &app.Resource{
		Name: "optimize",
		Desc: "Declarative workflow optimization runs and lineage inspection",
		Commands: []*app.Command{
			optimizeListCmd(),
			optimizeStartCmd(),
			optimizeStatusCmd(),
			optimizeSpecCmd(),
			optimizeCompareCmd(),
			optimizeArtifactsCmd(),
			optimizeExportCmd(),
			optimizeResumeCmd(),
			optimizePauseCmd(),
			optimizeCancelCmd(),
			optimizeOrganismsCmd(),
			optimizeBestCmd(),
			optimizeDiffCmd(),
			optimizeLineageCmd(),
			optimizeLearningLogCmd(),
			optimizePromoteCmd(),
		},
	}
}

func optimizeMutateByIDCmd(name string, desc string, pathTemplate string) *app.Command {
	return &app.Command{
		Name:      name,
		Desc:      desc,
		UsageLine: "conctl optimize " + name + " --id <run-id> --yes [--dry-run]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("optimize "+name, flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("optimize "+name, flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			runID := fs.String("id", "", "Run ID (required)")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*runID) == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "optimize "+name); !ok {
				return code
			}
			path := strings.Replace(pathTemplate, "{id}", url.PathEscape(strings.TrimSpace(*runID)), 1)
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

func parseTimeout(raw string) (time.Duration, error) {
	timeout := strings.TrimSpace(raw)
	if timeout == "" {
		return 30 * time.Second, nil
	}
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 30 * time.Second, nil
	}
	return d, nil
}

func loadParsedSeed(workflowID string) (*optimize.ParsedSeed, error) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil, fmt.Errorf("workflow ID is required")
	}
	seedJSON, err := seeds.GetJSONByID(workflowID)
	if err != nil {
		return nil, fmt.Errorf("load embedded seed %s: %w", workflowID, err)
	}
	parsed, err := optimize.ParseSeedOptimizeSpec([]byte(seedJSON))
	if err != nil {
		return nil, fmt.Errorf("parse seed optimize spec: %w", err)
	}
	return parsed, nil
}

type modelSwapOptions struct {
	Top      int
	Track    string
	MinIntel float64
	MaxCost  float64
}

func dedupeStringsPreserveOrder(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func looksLikeClaudeAlias(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "opus", "sonnet", "haiku":
		return true
	default:
		return false
	}
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func isHTTPStatusError(err error, status int) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf("http %d:", status))
}

func summarizeOptimizeParamChanges(changes []interface{}) string {
	if len(changes) == 0 {
		return "(baseline)"
	}
	parts := make([]string, 0, len(changes))
	for _, raw := range changes {
		change, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		path := strings.TrimSpace(stringVal(change, "path"))
		if path == "" {
			path = strings.TrimSpace(stringVal(change, "param_path"))
		}
		oldValue := strings.TrimSpace(stringVal(change, "old_value"))
		newValue := strings.TrimSpace(stringVal(change, "new_value"))
		label := abbreviateOptimizePath(path)
		switch {
		case oldValue != "" && newValue != "":
			label = fmt.Sprintf("%s→%s", label, truncate(newValue, 12))
		case newValue != "":
			label = fmt.Sprintf("%s=%s", label, truncate(newValue, 12))
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "(changes)"
	}
	if len(parts) > 3 {
		return strings.Join(parts[:3], " ") + " ..."
	}
	return strings.Join(parts, " ")
}

func abbreviateOptimizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "change"
	}
	lastDot := strings.LastIndex(path, ".")
	if lastDot >= 0 && lastDot+1 < len(path) {
		return path[lastDot+1:]
	}
	return path
}

func deriveOptimizeControlRunID(payload map[string]interface{}) string {
	runs, ok := payload["runs"].([]interface{})
	if !ok {
		return ""
	}
	bestID := ""
	bestAcc := -1.0
	for _, raw := range runs {
		run, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		runID := strings.TrimSpace(stringVal(run, "id"))
		if runID == "" {
			continue
		}
		bestFitness, _ := run["best_fitness"].(map[string]interface{})
		acc := floatVal(bestFitness, "adjusted_accuracy")
		if acc > bestAcc {
			bestAcc = acc
			bestID = runID
		}
	}
	return bestID
}
