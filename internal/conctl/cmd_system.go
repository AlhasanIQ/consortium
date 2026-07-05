package conctl

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// SystemResource returns the system resource (core API health/readiness/models).
func SystemResource() *app.Resource {
	return &app.Resource{
		Name: "system",
		Desc: "System health, readiness, and model info",
		Commands: []*app.Command{
			systemHealthCmd(),
			systemReadinessCmd(),
			systemDBDiagnosticsCmd(),
			systemModelsCmd(),
		},
	}
}

func systemHealthCmd() *app.Command {
	return &app.Command{
		Name:      "health",
		Desc:      "Check server health (liveness)",
		UsageLine: "conctl system health",
		Run: func(gf app.GlobalFlags, args []string) int {
			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			body, err := rc.Client.GetRaw(rc.Ctx, "/health", nil)
			if err != nil {
				return HandleError(err)
			}
			status := strings.TrimSpace(string(body))
			data := map[string]string{"status": status}
			return rc.Output(data, nil)
		},
	}
}

func systemReadinessCmd() *app.Command {
	return &app.Command{
		Name:      "readiness",
		Desc:      "Check system capacity and pool stats",
		UsageLine: "conctl system readiness [--watch --interval <dur>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("system readiness", flag.ContinueOnError)
			fs.Bool("watch", false, "Poll continuously")
			fs.String("interval", "3s", "Poll interval")
			return fs
		},
		Run: simpleWatchableGet("/system/readiness", nil),
	}
}

func systemDBDiagnosticsCmd() *app.Command {
	return &app.Command{
		Name:      "db-diagnostics",
		Desc:      "Inspect SQLite pool, queue, worker, and query-trace diagnostics",
		UsageLine: "conctl system db-diagnostics [--tables] [--watch --interval <dur>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("system db-diagnostics", flag.ContinueOnError)
			fs.Bool("tables", false, "Include hot table row counts")
			fs.Bool("watch", false, "Poll continuously")
			fs.String("interval", "3s", "Poll interval")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("system db-diagnostics", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			includeTables := fs.Bool("tables", false, "Include hot table row counts")
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

			query := url.Values{}
			if *includeTables {
				query.Set("tables", "true")
			}
			return watchableGet(rc, *watch, *interval, "/api/admin/db-diagnostics", query, dbDiagnosticsTable)
		},
	}
}

func systemModelsCmd() *app.Command {
	return &app.Command{
		Name:      "models",
		Desc:      "List available AI models",
		UsageLine: "conctl system models [--limit N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("system models", flag.ContinueOnError)
			fs.Int("limit", 50, "Max models to display (0 = all)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("system models", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			limit := fs.Int("limit", 50, "Max models to display (0 = all)")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/models", nil, &data); err != nil {
				return HandleError(err)
			}

			// Count before truncation so we can hint accurately.
			total := countItems(data, "models")

			// Client-side limit.
			if *limit > 0 {
				data = limitModels(data, *limit)
			}

			code := rc.Output(data, modelsTable)
			if *limit > 0 && total >= *limit {
				hintTruncated(fs, *limit, total, "models")
			}
			return code
		},
	}
}

// limitModels truncates the models list to n items.
func limitModels(data interface{}, n int) interface{} {
	if m, ok := data.(map[string]interface{}); ok {
		if models, ok := m["models"].([]interface{}); ok && len(models) > n {
			m["models"] = models[:n]
		}
		return m
	}
	if models, ok := data.([]interface{}); ok && len(models) > n {
		return models[:n]
	}
	return data
}

func modelsTable(w io.Writer, data interface{}, format string) {
	models, ok := data.([]interface{})
	if !ok {
		// Try nested.
		if m, ok := data.(map[string]interface{}); ok {
			if ml, ok := m["models"].([]interface{}); ok {
				models = ml
			}
		}
	}
	if models == nil {
		return
	}
	headers := []string{"ID", "PROVIDER", "INPUT_COST", "OUTPUT_COST"}
	var rows [][]string
	for _, model := range models {
		mm, ok := model.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, []string{
			stringVal(mm, "id"),
			stringVal(mm, "provider"),
			formatCostVal(floatVal(mm, "input_cost_per_token")),
			formatCostVal(floatVal(mm, "output_cost_per_token")),
		})
	}
	printTableAuto(w, headers, rows, format)
}

func dbDiagnosticsTable(w io.Writer, data interface{}, format string) {
	root, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	database := mapField(root, "Database")
	workers := mapField(root, "Workers")

	if pool := mapField(database, "Pool"); pool != nil {
		fmt.Fprintln(w, "Database Pool")
		printTableAuto(w,
			[]string{"MAX_OPEN", "OPEN", "IN_USE", "IDLE", "WAIT_COUNT", "WAIT_MS"},
			[][]string{{
				numberField(pool, "MaxOpenConnections"),
				numberField(pool, "OpenConnections"),
				numberField(pool, "InUse"),
				numberField(pool, "Idle"),
				numberField(pool, "WaitCount"),
				numberField(pool, "WaitDurationMs"),
			}},
			format,
		)
		fmt.Fprintln(w)
	}

	if queue := mapField(database, "Queue"); queue != nil {
		fmt.Fprintln(w, "Durable Queue")
		printTableAuto(w,
			[]string{"PENDING", "RUNNING", "PENDING_ROOT", "PENDING_CHILD", "RUNNING_ROOT", "RUNNING_CHILD"},
			[][]string{{
				numberField(queue, "PendingDurableJobs"),
				numberField(queue, "RunningDurableJobs"),
				numberField(queue, "PendingRootJobs"),
				numberField(queue, "PendingChildJobs"),
				numberField(queue, "RunningRootJobs"),
				numberField(queue, "RunningChildJobs"),
			}},
			format,
		)
		fmt.Fprintln(w)
	}

	if workers != nil {
		fmt.Fprintln(w, "Workers")
		printTableAuto(w,
			[]string{"ACTIVE", "BUSY", "IDLE", "INITIAL", "MAX", "ADMISSION"},
			[][]string{{
				numberField(workers, "ActiveWorkers"),
				numberField(workers, "BusyWorkers"),
				numberField(workers, "IdleWorkers"),
				numberField(workers, "WorkerInitial"),
				numberField(workers, "WorkerMax"),
				numberField(workers, "AdmissionActive") + "/" + numberField(workers, "AdmissionCapacity"),
			}},
			format,
		)
		fmt.Fprintln(w)
	}

	if trace := mapField(database, "QueryTrace"); trace != nil {
		fmt.Fprintln(w, "Query Trace")
		printTableAuto(w,
			[]string{"ENABLED", "LOG_ALL", "SLOW_MS", "QUERIES", "EXECS", "SLOW", "ERRORS", "MAX_MS"},
			[][]string{{
				boolString(boolVal(trace, "Enabled")),
				boolString(boolVal(trace, "LogAll")),
				numberField(trace, "SlowThresholdMs"),
				numberField(trace, "QueryCount"),
				numberField(trace, "ExecCount"),
				numberField(trace, "SlowQueryCount"),
				numberField(trace, "ErrorCount"),
				numberField(trace, "MaxDurationMs"),
			}},
			format,
		)
		fmt.Fprintln(w)
	}

	if tables, ok := database["Tables"].([]interface{}); ok && len(tables) > 0 {
		fmt.Fprintln(w, "Hot Tables")
		rows := make([][]string, 0, len(tables))
		for _, item := range tables {
			table, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			errText := stringVal(table, "Error")
			if errText == "" {
				errText = "-"
			}
			rows = append(rows, []string{
				stringVal(table, "Name"),
				numberField(table, "Rows"),
				errText,
			})
		}
		printTableAuto(w, []string{"TABLE", "ROWS", "ERROR"}, rows, format)
	}
}

func mapField(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]interface{})
	return v
}

func numberField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return "0"
	}
	switch n := v.(type) {
	case float64:
		if n == float64(int64(n)) {
			return fmt.Sprintf("%.0f", n)
		}
		return fmt.Sprintf("%.1f", n)
	case int:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	case uint64:
		return fmt.Sprintf("%d", n)
	default:
		return fmt.Sprintf("%v", n)
	}
}

func boolString(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
