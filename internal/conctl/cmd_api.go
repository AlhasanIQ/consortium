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
	"github.com/alhasaniq/consortium/internal/conctl/output"
)

// APIResource returns OpenAI-compatible API management commands.
func APIResource() *app.Resource {
	return &app.Resource{
		Name: "api",
		Desc: "OpenAI-compatible API keys, usage, and model routes",
		Commands: []*app.Command{
			apiKeysCmd(),
			apiKeyCreateCmd(),
			apiKeyRevokeCmd(),
			apiUsageCmd(),
			apiUsageExportCmd(),
			apiMetricsCmd(),
			apiRoutesCmd(),
			apiRouteUpsertCmd(),
			apiRouteDeleteCmd(),
		},
	}
}

func apiKeysCmd() *app.Command {
	return &app.Command{
		Name:      "keys",
		Desc:      "List API keys",
		UsageLine: "conctl api keys [--user-id <id>] [--include-revoked]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api keys", flag.ContinueOnError)
			fs.String("user-id", "", "Filter by user ID")
			fs.Bool("include-revoked", false, "Include revoked keys")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api keys", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			userID := fs.String("user-id", "", "Filter by user ID")
			includeRevoked := fs.Bool("include-revoked", false, "Include revoked keys")
			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			if strings.TrimSpace(*userID) != "" {
				q.Set("user_id", strings.TrimSpace(*userID))
			}
			if *includeRevoked {
				q.Set("include_revoked", "true")
			}
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/api-keys", q, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, apiKeysTable)
		},
	}
}

func apiKeyCreateCmd() *app.Command {
	return &app.Command{
		Name:      "key-create",
		Desc:      "Create an API key and print the one-time secret",
		UsageLine: "conctl api key-create --name <name> --yes [--user-id <id>] [--workflow-id <id>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api key-create", flag.ContinueOnError)
			fs.String("name", "", "Key display name (required)")
			fs.String("user-id", "", "User ID")
			fs.String("workflow-id", "", "Workflow override for this key")
			fs.Int("requests-per-minute", 60, "Requests per minute")
			fs.Int("tokens-per-minute", 120000, "Tokens per minute")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api key-create", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			name := fs.String("name", "", "Key display name")
			userID := fs.String("user-id", "", "User ID")
			workflowID := fs.String("workflow-id", "", "Workflow override")
			requestsPerMinute := fs.Int("requests-per-minute", 60, "Requests per minute")
			tokensPerMinute := fs.Int("tokens-per-minute", 120000, "Tokens per minute")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*name) == "" {
				fmt.Fprintln(os.Stderr, "Error: --name is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "api key-create"); !ok {
				return code
			}
			body := map[string]interface{}{
				"name":                strings.TrimSpace(*name),
				"user_id":             strings.TrimSpace(*userID),
				"workflow_id":         strings.TrimSpace(*workflowID),
				"requests_per_minute": *requestsPerMinute,
				"tokens_per_minute":   *tokensPerMinute,
			}
			if *dryRun {
				return DryRunOutput("POST", "/api/admin/api-keys", body)
			}
			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()
			var data interface{}
			if err := rc.Client.PostJSON(rc.Ctx, "/api/admin/api-keys", body, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func apiKeyRevokeCmd() *app.Command {
	return &app.Command{
		Name:      "key-revoke",
		Desc:      "Soft revoke an API key",
		UsageLine: "conctl api key-revoke --id <key-id> --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api key-revoke", flag.ContinueOnError)
			fs.String("id", "", "API key ID (required)")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api key-revoke", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.String("id", "", "API key ID")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*id) == "" {
				fmt.Fprintln(os.Stderr, "Error: --id is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "api key-revoke"); !ok {
				return code
			}
			path := "/api/admin/api-keys/" + url.PathEscape(strings.TrimSpace(*id))
			if *dryRun {
				return DryRunOutput("DELETE", path, nil)
			}
			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()
			var data interface{}
			if err := rc.Client.DeleteJSON(rc.Ctx, path, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func apiUsageCmd() *app.Command {
	return &app.Command{
		Name:      "usage",
		Desc:      "List API usage with summary",
		UsageLine: "conctl api usage [--limit N] [--key-id <id>] [--model <model>] [--status <status>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api usage", flag.ContinueOnError)
			addAPIUsageFilterFlags(fs)
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api usage", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			addAPIUsageFilterFlags(fs)
			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/api-usage", apiUsageQuery(fs), &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, apiUsageTable)
		},
	}
}

func apiUsageExportCmd() *app.Command {
	return &app.Command{
		Name:      "usage-export",
		Desc:      "Export API usage CSV",
		UsageLine: "conctl api usage-export [--limit N] [global --output <file>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api usage-export", flag.ContinueOnError)
			addAPIUsageFilterFlags(fs)
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api usage-export", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			addAPIUsageFilterFlags(fs)
			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()
			raw, err := rc.Client.GetRaw(rc.Ctx, "/api/admin/api-usage/export", apiUsageQuery(fs))
			if err != nil {
				return HandleError(err)
			}
			w, cleanup, err := output.Writer(gf.Output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return app.ExitInternal
			}
			defer cleanup()
			if _, err := w.Write(raw); err != nil {
				fmt.Fprintf(os.Stderr, "Error: write output: %v\n", err)
				return app.ExitInternal
			}
			return app.ExitSuccess
		},
	}
}

func apiMetricsCmd() *app.Command {
	return &app.Command{
		Name:      "metrics",
		Desc:      "Show OpenAI-compatible API metrics",
		UsageLine: "conctl api metrics [--stale-minutes N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api metrics", flag.ContinueOnError)
			fs.Int("stale-minutes", 15, "Age threshold for stale running/background rows")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api metrics", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			staleMinutes := fs.Int("stale-minutes", 15, "Age threshold for stale running/background rows")
			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()
			q := url.Values{}
			if *staleMinutes > 0 {
				q.Set("stale_minutes", strconv.Itoa(*staleMinutes))
			}
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/api-metrics", q, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, apiMetricsTable)
		},
	}
}

func apiRoutesCmd() *app.Command {
	return &app.Command{
		Name:      "routes",
		Desc:      "List OpenAI model routes",
		UsageLine: "conctl api routes [--include-disabled]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api routes", flag.ContinueOnError)
			fs.Bool("include-disabled", true, "Include disabled routes")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api routes", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			includeDisabled := fs.Bool("include-disabled", true, "Include disabled routes")
			rc, code := parseArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()
			q := url.Values{}
			q.Set("include_disabled", strconv.FormatBool(*includeDisabled))
			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/model-routes", q, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, apiRoutesTable)
		},
	}
}

func apiRouteUpsertCmd() *app.Command {
	return &app.Command{
		Name:      "route-upsert",
		Desc:      "Create or update an OpenAI model route",
		UsageLine: "conctl api route-upsert --api-model <name> --mode workflow|direct_model --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api route-upsert", flag.ContinueOnError)
			fs.String("api-model", "", "OpenAI-facing model name (required)")
			fs.String("mode", "", "Route mode: workflow or direct_model (required)")
			fs.String("workflow-id", "", "Workflow ID for workflow mode")
			fs.String("provider-model", "", "Provider model for direct_model mode")
			fs.String("description", "", "Route description")
			fs.Bool("default", false, "Set as default route")
			fs.Bool("enabled", true, "Enable route")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api route-upsert", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			apiModel := fs.String("api-model", "", "OpenAI-facing model name")
			mode := fs.String("mode", "", "Route mode")
			workflowID := fs.String("workflow-id", "", "Workflow ID")
			providerModel := fs.String("provider-model", "", "Provider model")
			description := fs.String("description", "", "Description")
			isDefault := fs.Bool("default", false, "Set as default route")
			enabled := fs.Bool("enabled", true, "Enable route")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*apiModel) == "" || strings.TrimSpace(*mode) == "" {
				fmt.Fprintln(os.Stderr, "Error: --api-model and --mode are required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "api route-upsert"); !ok {
				return code
			}
			body := map[string]interface{}{
				"api_model":      strings.TrimSpace(*apiModel),
				"mode":           strings.TrimSpace(*mode),
				"workflow_id":    strings.TrimSpace(*workflowID),
				"provider_model": strings.TrimSpace(*providerModel),
				"description":    strings.TrimSpace(*description),
				"is_default":     *isDefault,
				"enabled":        *enabled,
			}
			if *dryRun {
				return DryRunOutput("POST", "/api/admin/model-routes", body)
			}
			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()
			var data interface{}
			if err := rc.Client.PostJSON(rc.Ctx, "/api/admin/model-routes", body, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func apiRouteDeleteCmd() *app.Command {
	return &app.Command{
		Name:      "route-delete",
		Desc:      "Delete an OpenAI model route",
		UsageLine: "conctl api route-delete --model <api-model> --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("api route-delete", flag.ContinueOnError)
			fs.String("model", "", "OpenAI-facing model name (required)")
			fs.Bool("yes", false, "Confirm mutating operation")
			fs.Bool("dry-run", false, "Print request plan without executing")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("api route-delete", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			model := fs.String("model", "", "OpenAI-facing model name")
			yes := fs.Bool("yes", false, "Confirm")
			dryRun := fs.Bool("dry-run", false, "Dry run")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if strings.TrimSpace(*model) == "" {
				fmt.Fprintln(os.Stderr, "Error: --model is required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "api route-delete"); !ok {
				return code
			}
			path := "/api/admin/model-routes/" + url.PathEscape(strings.TrimSpace(*model))
			if *dryRun {
				return DryRunOutput("DELETE", path, nil)
			}
			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()
			var data interface{}
			if err := rc.Client.DeleteJSON(rc.Ctx, path, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, nil)
		},
	}
}

func addAPIUsageFilterFlags(fs *flag.FlagSet) {
	fs.String("from", "", "Start timestamp")
	fs.String("to", "", "End timestamp")
	fs.String("key-id", "", "Filter by API key ID")
	fs.String("model", "", "Filter by requested model")
	fs.String("endpoint", "", "Filter by endpoint")
	fs.String("status", "", "Filter by status")
	fs.Int("limit", 100, "Max rows")
}

func apiUsageQuery(fs *flag.FlagSet) url.Values {
	q := url.Values{}
	for flagName, queryName := range map[string]string{
		"from":     "from",
		"to":       "to",
		"key-id":   "key_id",
		"model":    "model",
		"endpoint": "endpoint",
		"status":   "status",
	} {
		if value := strings.TrimSpace(fs.Lookup(flagName).Value.String()); value != "" {
			q.Set(queryName, value)
		}
	}
	if limit := strings.TrimSpace(fs.Lookup("limit").Value.String()); limit != "" {
		q.Set("limit", limit)
	}
	return q
}

func apiKeysTable(w io.Writer, data interface{}, format string) {
	rows := [][]string{}
	for _, item := range listField(data, "api_keys") {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		status := "active"
		if stringVal(row, "revoked_at") != "" {
			status = "revoked"
		}
		rows = append(rows, []string{
			stringVal(row, "id"),
			stringVal(row, "name"),
			stringVal(row, "prefix"),
			stringVal(row, "user_id"),
			strconv.Itoa(int(floatVal(row, "requests_per_minute"))),
			strconv.Itoa(int(floatVal(row, "tokens_per_minute"))),
			status,
		})
	}
	printTableAuto(w, []string{"ID", "Name", "Prefix", "User", "RPM", "TPM", "Status"}, rows, format)
}

func apiUsageTable(w io.Writer, data interface{}, format string) {
	if m, ok := data.(map[string]interface{}); ok {
		if summary, ok := m["summary"].(map[string]interface{}); ok {
			fmt.Fprintf(w, "Requests: %d  Tokens: %d  Cost: %s\n\n",
				int(floatVal(summary, "requests")),
				int(floatVal(summary, "tokens_total")),
				formatCostVal(floatVal(summary, "cost")),
			)
		}
	}
	rows := [][]string{}
	for _, item := range listField(data, "api_usage", "usage") {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, []string{
			stringVal(row, "created_at"),
			stringVal(row, "key_id"),
			stringVal(row, "endpoint"),
			stringVal(row, "requested_model"),
			stringVal(row, "status"),
			strconv.Itoa(int(floatVal(row, "http_status"))),
			strconv.Itoa(int(floatVal(row, "tokens_total"))),
			formatCostVal(floatVal(row, "cost")),
			truncate(stringVal(row, "error_message"), 40),
		})
	}
	printTableAuto(w, []string{"Created", "Key", "Endpoint", "Model", "Status", "HTTP", "Tokens", "Cost", "Error"}, rows, format)
}

func apiMetricsTable(w io.Writer, data interface{}, format string) {
	root, _ := data.(map[string]interface{})
	metrics := mapField(root, "openai_api_metrics")
	usage := mapField(metrics, "usage")
	statuses := mapField(metrics, "requests_by_status")
	classes := mapField(metrics, "http_status_classes")

	rows := [][]string{
		{"Requests", strconv.Itoa(int(floatVal(usage, "requests")))},
		{"Tokens", strconv.Itoa(int(floatVal(usage, "tokens_total")))},
		{"Cost", formatCostVal(floatVal(usage, "cost"))},
		{"Avg Latency ms", fmt.Sprintf("%.1f", floatVal(metrics, "avg_latency_ms"))},
		{"Succeeded", strconv.Itoa(int(floatVal(statuses, "succeeded")))},
		{"Failed", strconv.Itoa(int(floatVal(statuses, "failed")))},
		{"Running", strconv.Itoa(int(floatVal(statuses, "running")))},
		{"Cancelled", strconv.Itoa(int(floatVal(statuses, "cancelled")))},
		{"2xx", strconv.Itoa(int(floatVal(classes, "2xx")))},
		{"4xx", strconv.Itoa(int(floatVal(classes, "4xx")))},
		{"5xx", strconv.Itoa(int(floatVal(classes, "5xx")))},
		{"Stale Running Usage", strconv.Itoa(int(floatVal(metrics, "stale_running_usage")))},
		{"Stale Background Responses", strconv.Itoa(int(floatVal(metrics, "stale_background_responses")))},
		{"Pending Idempotency", strconv.Itoa(int(floatVal(metrics, "pending_idempotency")))},
	}
	printTableAuto(w, []string{"Metric", "Value"}, rows, format)
}

func apiRoutesTable(w io.Writer, data interface{}, format string) {
	rows := [][]string{}
	for _, item := range listField(data, "model_routes") {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		target := stringVal(row, "provider_model")
		if target == "" {
			target = stringVal(row, "workflow_id")
		}
		rows = append(rows, []string{
			stringVal(row, "api_model"),
			stringVal(row, "mode"),
			target,
			strconv.FormatBool(boolVal(row, "is_default")),
			strconv.FormatBool(boolVal(row, "enabled")),
			truncate(stringVal(row, "description"), 40),
		})
	}
	printTableAuto(w, []string{"Model", "Mode", "Target", "Default", "Enabled", "Description"}, rows, format)
}

func listField(data interface{}, keys ...string) []interface{} {
	if items, ok := data.([]interface{}); ok {
		return items
	}
	if m, ok := data.(map[string]interface{}); ok {
		for _, key := range keys {
			if items, ok := m[key].([]interface{}); ok {
				return items
			}
		}
	}
	return nil
}
