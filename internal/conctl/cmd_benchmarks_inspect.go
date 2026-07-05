package conctl

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

func benchmarksGetCmd() *app.Command {
	return &app.Command{
		Name:      "get",
		Desc:      "Get benchmark run detail with items.\n\n--incorrect adds FAILURE and JOB columns to the items table.\n--category cross-references the analysis endpoint to filter items by failure category\n(e.g. all_steps_wrong, some_right_child_wrong, all_right_child_wrong, child_right_parent_wrong).",
		UsageLine: "conctl benchmarks get --id <run-id> [--page N] [--page-size N] [--incorrect] [--category <cat>] [--subject <s>] [--failure-reason <r>] [--watch]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks get", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Int("page", 1, "Page number")
			fs.Int("page-size", 100, "Items per page")
			fs.Bool("incorrect", false, "Show only incorrect items")
			fs.String("category", "", "Filter by analysis category (e.g. some_right_child_wrong)")
			fs.String("subject", "", "Filter by subject")
			fs.String("failure-reason", "", "Filter by failure reason")
			fs.Bool("watch", false, "Poll continuously")
			fs.String("interval", "3s", "Poll interval")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks get", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			page := fs.Int("page", 1, "Page")
			pageSize := fs.Int("page-size", 100, "Page size")
			incorrect := fs.Bool("incorrect", false, "Only incorrect")
			category := fs.String("category", "", "Analysis category filter")
			subject := fs.String("subject", "", "Subject filter")
			failureReason := fs.String("failure-reason", "", "Failure reason filter")
			watch := fs.Bool("watch", false, "Poll")
			interval := fs.String("interval", "3s", "Poll interval")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("page", strconv.Itoa(*page))
			q.Set("page_size", strconv.Itoa(*pageSize))
			if *incorrect || *category != "" {
				q.Set("incorrect", "1")
			}
			if *subject != "" {
				q.Set("subject", *subject)
			}
			if *failureReason != "" {
				q.Set("failure_reason", *failureReason)
			}

			fetch := func() (interface{}, error) {
				var data interface{}
				if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/"+id, q, &data); err != nil {
					return nil, err
				}
				// When --category is specified, cross-reference with analysis to filter items.
				if *category != "" {
					if dataMap, ok := data.(map[string]interface{}); ok {
						var analysisData map[string]interface{}
						if aErr := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/"+id+"/analysis", nil, &analysisData); aErr == nil {
							categorySet := buildCategoryItemSet(analysisData, *category)
							filterItemsByCategory(dataMap, categorySet)
						}
					}
				}
				return data, nil
			}

			if *watch {
				return runWatch(rc, *interval, fetch, benchmarkGetTable)
			}

			data, fetchErr := fetch()
			if fetchErr != nil {
				return HandleError(fetchErr)
			}
			outCode := rc.Output(data, benchmarkGetTable)
			printBenchmarkRetryHint(rc, id, data)
			return outCode
		},
	}
}

func benchmarksAnalysisCmd() *app.Command {
	return &app.Command{
		Name:      "analysis",
		Desc:      "Get wrong-answer analysis for a benchmark run.\n\nAGENTS column format: model:answer:mark — mark is Y (correct), N (wrong), X (parse failure).\nExample: mimo-v2-flash:A:Y grok-4.1-fast:F:N means mimo answered A (correct), grok answered F (wrong).\n\nAlso includes model/node performance sections for agent models and aggregation nodes\n(cost, retries, latency, accuracy) over all items with child workflow data.",
		UsageLine: "conctl benchmarks analysis --id <run-id> [--category <cat>] [--top N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks analysis", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("category", "", "Filter by category")
			fs.Int("top", 10, "Top N rows for diagnostics sections (slowest/most retries)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks analysis", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			category := fs.String("category", "", "Category filter")
			top := fs.Int("top", 10, "Top N diagnostics rows")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			if *top < 1 {
				fmt.Fprintln(os.Stderr, "Error: --top must be >= 1")
				return app.ExitUsage
			}

			q := url.Values{}
			q.Set("top", strconv.Itoa(*top))

			var data map[string]interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/"+id+"/analysis", q, &data); err != nil {
				return HandleError(err)
			}

			// Client-side category filter.
			if *category != "" {
				if items, ok := data["items"].([]interface{}); ok {
					var filtered []interface{}
					for _, item := range items {
						im, ok := item.(map[string]interface{})
						if !ok {
							continue
						}
						if stringVal(im, "category") == *category {
							filtered = append(filtered, item)
						}
					}
					data["items"] = filtered
				}
			}

			outCode := rc.Output(data, benchmarkAnalysisTable)
			printBenchmarkAnalysisRetryHint(id, data)
			return outCode
		},
	}
}

func benchmarksAnalyzeCmd() *app.Command {
	cmd := benchmarksAnalysisCmd()
	return &app.Command{
		Name:      "analyze",
		Desc:      cmd.Desc + "\n\nAlias of `analysis`.",
		UsageLine: "conctl benchmarks analyze --id <run-id> [--category <cat>] [--top N]",
		Flags:     cmd.Flags,
		Run:       cmd.Run,
	}
}

func benchmarksRetriesCmd() *app.Command {
	return &app.Command{
		Name:      "retries",
		Desc:      "Deep retry/error diagnostics for benchmark runs.\n\nShows retry layer/phase/code summary and a detailed attempt table.\nBy default inspects top retry-heavy items from analysis diagnostics.",
		UsageLine: "conctl benchmarks retries --id <run-id> [--top N] [--item <item-id>] [--max-rows N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks retries", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Int("top", 10, "Top retry-heavy items to inspect")
			fs.String("item", "", "Inspect a single item ID (e.g. row-17)")
			fs.Int("max-rows", 200, "Max detailed attempt rows to print")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks retries", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			top := fs.Int("top", 10, "Top retry-heavy items")
			item := fs.String("item", "", "Single item ID")
			maxRows := fs.Int("max-rows", 200, "Max detail rows")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			if *top < 1 {
				*top = 1
			}
			if *maxRows < 1 {
				*maxRows = 1
			}

			itemIDs, err := benchmarkRetryTargetItems(rc, id, *item, *top)
			if err != nil {
				return HandleError(err)
			}
			if len(itemIDs) == 0 {
				fmt.Fprintln(os.Stderr, "No retry-heavy items found for this run")
				return app.ExitSuccess
			}

			var details []benchmarkRetryDetailRow
			seenJobs := make(map[string]struct{})
			for _, itemID := range itemIDs {
				itemDetail, err := fetchBenchmarkItemDetail(rc, id, itemID)
				if err != nil {
					return HandleError(err)
				}
				parentJobIDs, childJobIDs := extractBenchmarkItemJobIDs(itemDetail)
				for _, jobID := range parentJobIDs {
					if strings.TrimSpace(jobID) == "" {
						continue
					}
					seenJobs[jobID] = struct{}{}
					jobRows, err := loadRetryDetailRowsForJob(rc, itemID, "parent", jobID)
					if err != nil {
						return HandleError(err)
					}
					details = append(details, jobRows...)
				}
				for _, jobID := range childJobIDs {
					if strings.TrimSpace(jobID) == "" {
						continue
					}
					seenJobs[jobID] = struct{}{}
					jobRows, err := loadRetryDetailRowsForJob(rc, itemID, "child", jobID)
					if err != nil {
						return HandleError(err)
					}
					details = append(details, jobRows...)
				}
			}

			// Layer/phase/code summary.
			type key struct {
				layer string
				phase string
				code  string
			}
			counts := make(map[key]int)
			for _, row := range details {
				if row.Layer == "-" && row.Phase == "-" && row.Code == "-" {
					continue
				}
				k := key{
					layer: fallbackOr(row.Layer, "-"),
					phase: fallbackOr(row.Phase, "-"),
					code:  fallbackOr(row.Code, "-"),
				}
				counts[k]++
			}
			summary := make([]benchmarkRetrySummaryRow, 0, len(counts))
			for k, count := range counts {
				summary = append(summary, benchmarkRetrySummaryRow{
					Layer: k.layer,
					Phase: k.phase,
					Code:  k.code,
					Count: count,
				})
			}
			sort.SliceStable(summary, func(i, j int) bool {
				if summary[i].Count != summary[j].Count {
					return summary[i].Count > summary[j].Count
				}
				if summary[i].Layer != summary[j].Layer {
					return summary[i].Layer < summary[j].Layer
				}
				if summary[i].Phase != summary[j].Phase {
					return summary[i].Phase < summary[j].Phase
				}
				return summary[i].Code < summary[j].Code
			})

			sort.SliceStable(details, func(i, j int) bool {
				if details[i].ItemID != details[j].ItemID {
					return details[i].ItemID < details[j].ItemID
				}
				if details[i].Scope != details[j].Scope {
					return details[i].Scope < details[j].Scope
				}
				if details[i].JobID != details[j].JobID {
					return details[i].JobID < details[j].JobID
				}
				if details[i].NodeID != details[j].NodeID {
					return details[i].NodeID < details[j].NodeID
				}
				return details[i].Attempt < details[j].Attempt
			})

			totalRows := len(details)
			if len(details) > *maxRows {
				details = details[:*maxRows]
			}

			data := map[string]interface{}{
				"run_id":          id,
				"inspected_items": itemIDs,
				"inspected_jobs":  len(seenJobs),
				"summary":         summary,
				"details":         details,
				"total_rows":      totalRows,
				"shown_rows":      len(details),
			}

			return rc.Output(data, benchmarkRetriesTable)
		},
	}
}

func benchmarksItemCmd() *app.Command {
	return &app.Command{
		Name:      "item",
		Desc:      "Get detail for a specific benchmark item",
		UsageLine: "conctl benchmarks item --id <run-id> --item <item-id> [--show-output] [--show-prompts]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks item", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")
			fs.Bool("show-output", false, "Show raw LLM output per node")
			fs.Bool("show-prompts", false, "Show prompts sent to each node")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks item", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")
			showOutput := fs.Bool("show-output", false, "Show raw LLM output per node")
			showPrompts := fs.Bool("show-prompts", false, "Show prompts sent to each node")

			rc, _, data, code := parseBenchmarkItemArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			if *showOutput {
				data["_showOutput"] = true
			}
			if *showPrompts {
				data["_showPrompts"] = true
			}
			injectExpectedAnswer(data)
			return rc.Output(data, benchmarkItemTable)
		},
	}
}

func benchmarksDrillCmd() *app.Command {
	return &app.Command{
		Name:      "drill",
		Desc:      "Deep investigation of a benchmark item with failure category.\n\nPrompts and LLM outputs are shown by default (use --brief to suppress both).\nUnlike \"item\", drill automatically fetches the analysis category.",
		UsageLine: "conctl benchmarks drill --id <run-id> --item <item-id> [--brief]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks drill", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")
			fs.Bool("brief", false, "Suppress prompts and LLM outputs (compact view)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks drill", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")
			brief := fs.Bool("brief", false, "Suppress prompts and LLM outputs")

			rc, runID, itemData, code := parseBenchmarkItemArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			// Fetch analysis to get failure category for this item.
			itemID := normalizeBenchmarkItemID(fs.Lookup("item").Value.String())
			var category string
			if aim := fetchBenchmarkAnalysisItem(rc, runID, itemID); aim != nil {
				category = stringVal(aim, "category")
			}

			// Drill always shows prompts and output by default; --brief suppresses both.
			if !*brief {
				itemData["_showOutput"] = true
				itemData["_showPrompts"] = true
			}
			if category != "" {
				itemData["_category"] = category
			}
			injectExpectedAnswer(itemData)

			return rc.Output(itemData, benchmarkDrillTable)
		},
	}
}

func benchmarksExplainCmd() *app.Command {
	return &app.Command{
		Name:      "explain",
		Desc:      "Narrative explanation of why a benchmark item was incorrect.\n\nWhen a job has multiple execution attempts, the narrative uses the latest attempt.",
		UsageLine: "conctl benchmarks explain --id <run-id> --item <item-id>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks explain", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks explain", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")

			rc, runID, itemData, code := parseBenchmarkItemArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			// Fetch analysis for category and agent answers.
			itemID := normalizeBenchmarkItemID(fs.Lookup("item").Value.String())
			analysisItem := fetchBenchmarkAnalysisItem(rc, runID, itemID)

			// Render narrative to stdout.
			w, cleanup, errW := openOutput(rc.GF.Output)
			if errW != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", errW)
				return app.ExitInternal
			}
			defer cleanup()

			renderExplainNarrative(w, itemData, analysisItem)
			return app.ExitSuccess
		},
	}
}

func benchmarksDagCmd() *app.Command {
	return &app.Command{
		Name:      "dag",
		Desc:      "Show frozen DAG snapshot for a benchmark item's execution.\n\nUse --child to show the child workflow's frozen DAG instead of the parent's.",
		UsageLine: "conctl benchmarks dag --id <run-id> --item <item-id> [--child]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks dag", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")
			fs.Bool("child", false, "Show child workflow's frozen DAG")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks dag", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			fs.String("item", "", "Item ID (required)")
			child := fs.Bool("child", false, "Show child workflow's frozen DAG")

			rc, _, data, code := parseBenchmarkItemArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			// Extract dag_snapshot from the first job summary.
			jobSummaries, ok := data["JobSummaries"].([]interface{})
			if !ok || len(jobSummaries) == 0 {
				fmt.Fprintln(os.Stderr, "No job summaries found for this item")
				return app.ExitInternal
			}
			jsm, ok := jobSummaries[0].(map[string]interface{})
			if !ok {
				fmt.Fprintln(os.Stderr, "Invalid job summary format")
				return app.ExitInternal
			}
			job, _ := jsm["Job"].(map[string]interface{})
			if job == nil {
				fmt.Fprintln(os.Stderr, "No job data in summary")
				return app.ExitInternal
			}
			dagSnapshot := stringVal(job, "dag_snapshot")
			if dagSnapshot == "" {
				fmt.Fprintln(os.Stderr, "No DAG snapshot found (legacy job without durable runtime?)")
				return app.ExitInternal
			}

			// When --child is set, fetch the child job's frozen DAG instead.
			if *child {
				childJobID := ""
				if childJobs, ok := jsm["ChildJobs"].([]interface{}); ok {
					for _, cj := range childJobs {
						cjm, ok := cj.(map[string]interface{})
						if !ok {
							continue
						}
						childJobID = stringVal(cjm, "JobID")
						if childJobID != "" {
							break
						}
					}
				}
				if childJobID == "" {
					fmt.Fprintln(os.Stderr, "No child job found for this item")
					return app.ExitInternal
				}
				var childJobData interface{}
				if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/jobs/"+childJobID, nil, &childJobData); err != nil {
					return HandleError(err)
				}
				childMap, _ := childJobData.(map[string]interface{})
				childJob, _ := childMap["Job"].(map[string]interface{})
				if childJob == nil {
					fmt.Fprintln(os.Stderr, "Could not load child job detail")
					return app.ExitInternal
				}
				dagSnapshot = stringVal(childJob, "dag_snapshot")
				if dagSnapshot == "" {
					fmt.Fprintln(os.Stderr, "No DAG snapshot on child job")
					return app.ExitInternal
				}
			}

			// Pretty-print the JSON.
			var parsed interface{}
			if jsonErr := json.Unmarshal([]byte(dagSnapshot), &parsed); jsonErr != nil {
				fmt.Fprintln(os.Stdout, dagSnapshot)
				return app.ExitSuccess
			}
			prettyJSON, jsonErr := json.MarshalIndent(parsed, "", "  ")
			if jsonErr != nil {
				fmt.Fprintln(os.Stdout, dagSnapshot)
				return app.ExitSuccess
			}
			fmt.Fprintln(os.Stdout, string(prettyJSON))
			return app.ExitSuccess
		},
	}
}

// openOutput opens the output writer (stdout or file).
func openOutput(target string) (io.Writer, func(), error) {
	if target == "" || target == "-" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(target)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

func benchmarksExportCmd() *app.Command {
	return &app.Command{
		Name:      "export",
		Desc:      "Export benchmark items with full detail as JSONL.\n\n--concurrency is capped to [1, 20]. Items are fetched in parallel and written in order.",
		UsageLine: "conctl benchmarks export --id <run-id> [--incorrect] [--output <file>] [--concurrency N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks export", flag.ContinueOnError)
			fs.String("id", "", "Run ID (required)")
			fs.Bool("incorrect", false, "Only export incorrect items")
			fs.String("output", "-", "Output file (- for stdout)")
			fs.Int("concurrency", 5, "Parallel item fetches")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks export", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			fs.String("id", "", "Run ID (required)")
			incorrect := fs.Bool("incorrect", false, "Only incorrect")
			outputPath := fs.String("output", "-", "Output file")
			concurrency := fs.Int("concurrency", 5, "Parallel item fetches")

			rc, id, code := parseIDArgs(gf, fs, args)
			if code != -1 {
				return code
			}
			defer rc.Cancel()

			if *concurrency < 1 {
				*concurrency = 1
			}
			if *concurrency > 20 {
				*concurrency = 20
			}

			w, cleanup, errW := openOutput(*outputPath)
			if errW != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", errW)
				return app.ExitInternal
			}
			defer cleanup()

			// Collect all item IDs by paginating through the run.
			var itemIDs []string
			page := 1
			for {
				q := url.Values{}
				q.Set("page", strconv.Itoa(page))
				q.Set("page_size", "100")
				if *incorrect {
					q.Set("incorrect", "1")
				}

				var pageData map[string]interface{}
				if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/"+id, q, &pageData); err != nil {
					return HandleError(err)
				}

				items, ok := pageData["Items"].([]interface{})
				if !ok || len(items) == 0 {
					break
				}
				for _, item := range items {
					im, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					itemIDs = append(itemIDs, stringVal(im, "item_id"))
				}
				if !boolVal(pageData, "HasNext") {
					break
				}
				page++
			}

			if len(itemIDs) == 0 {
				fmt.Fprintln(os.Stderr, "(no items to export)")
				return app.ExitSuccess
			}
			fmt.Fprintf(os.Stderr, "Exporting %d items...\n", len(itemIDs))

			// Fetch item details concurrently.
			type result struct {
				idx  int
				data map[string]interface{}
				err  error
			}
			results := make([]result, len(itemIDs))
			sem := make(chan struct{}, *concurrency)
			var wg sync.WaitGroup
			for i, itemID := range itemIDs {
				wg.Add(1)
				go func(idx int, iid string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					var detail map[string]interface{}
					path := fmt.Sprintf("/api/admin/benchmarks/%s/items", id)
					query := url.Values{}
					query.Set("item_id", iid)
					if fetchErr := rc.Client.GetJSON(rc.Ctx, path, query, &detail); fetchErr != nil {
						results[idx] = result{idx: idx, err: fetchErr}
						return
					}
					results[idx] = result{idx: idx, data: detail}
				}(i, itemID)
			}
			wg.Wait()

			// Write JSONL in order.
			enc := json.NewEncoder(w)
			exported := 0
			for _, r := range results {
				if r.err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to fetch item: %v\n", r.err)
					continue
				}
				if err := enc.Encode(r.data); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing: %v\n", err)
					return app.ExitInternal
				}
				exported++
			}
			fmt.Fprintf(os.Stderr, "Exported %d items\n", exported)
			return app.ExitSuccess
		},
	}
}

// --- Retry diagnostic types and helpers ---

type benchmarkRetryDetailRow struct {
	ItemID    string `json:"item_id"`
	Scope     string `json:"scope"`
	JobID     string `json:"job_id"`
	NodeID    string `json:"node_id"`
	Attempt   int    `json:"attempt"`
	Status    string `json:"status"`
	Layer     string `json:"retry_layer"`
	Phase     string `json:"error_phase"`
	Code      string `json:"error_code"`
	RequestID string `json:"request_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type benchmarkRetrySummaryRow struct {
	Layer string `json:"retry_layer"`
	Phase string `json:"error_phase"`
	Code  string `json:"error_code"`
	Count int    `json:"count"`
}

func benchmarkRetryTargetItems(rc *RunContext, runID, singleItem string, top int) ([]string, error) {
	if strings.TrimSpace(singleItem) != "" {
		return []string{normalizeBenchmarkItemID(singleItem)}, nil
	}

	q := url.Values{}
	q.Set("top", strconv.Itoa(top))

	var analysisData map[string]interface{}
	if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/"+runID+"/analysis", q, &analysisData); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	itemIDs := make([]string, 0, top)
	if diagnostics, ok := analysisData["diagnostics"].(map[string]interface{}); ok {
		if list, ok := diagnostics["most_retries_items"].([]interface{}); ok {
			for _, entry := range list {
				em, ok := entry.(map[string]interface{})
				if !ok {
					continue
				}
				itemID := normalizeBenchmarkItemID(stringVal(em, "item_id"))
				if itemID == "" {
					continue
				}
				if _, exists := seen[itemID]; exists {
					continue
				}
				seen[itemID] = struct{}{}
				itemIDs = append(itemIDs, itemID)
			}
		}
	}
	if len(itemIDs) > 0 {
		return itemIDs, nil
	}

	// Fallback: take top incorrect analysis items if diagnostics list is empty.
	if list, ok := analysisData["items"].([]interface{}); ok {
		for _, entry := range list {
			em, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			itemID := normalizeBenchmarkItemID(stringVal(em, "item_id"))
			if itemID == "" {
				continue
			}
			if _, exists := seen[itemID]; exists {
				continue
			}
			seen[itemID] = struct{}{}
			itemIDs = append(itemIDs, itemID)
			if len(itemIDs) >= top {
				break
			}
		}
	}
	return itemIDs, nil
}

func extractBenchmarkItemJobIDs(itemDetail map[string]interface{}) (parentJobIDs []string, childJobIDs []string) {
	seenParent := make(map[string]struct{})
	seenChild := make(map[string]struct{})

	jobSummaries, ok := itemDetail["JobSummaries"].([]interface{})
	if !ok {
		return nil, nil
	}
	for _, summary := range jobSummaries {
		jsm, ok := summary.(map[string]interface{})
		if !ok {
			continue
		}
		parentID := stringVal(jsm, "JobID")
		if parentID == "" {
			if job, ok := jsm["Job"].(map[string]interface{}); ok {
				parentID = stringVal(job, "id")
			}
		}
		if parentID != "" {
			if _, exists := seenParent[parentID]; !exists {
				seenParent[parentID] = struct{}{}
				parentJobIDs = append(parentJobIDs, parentID)
			}
		}

		childJobs, ok := jsm["ChildJobs"].([]interface{})
		if !ok {
			continue
		}
		for _, child := range childJobs {
			cjm, ok := child.(map[string]interface{})
			if !ok {
				continue
			}
			childID := stringVal(cjm, "JobID")
			if childID == "" {
				if job, ok := cjm["Job"].(map[string]interface{}); ok {
					childID = stringVal(job, "id")
				}
			}
			if childID == "" {
				continue
			}
			if _, exists := seenChild[childID]; exists {
				continue
			}
			seenChild[childID] = struct{}{}
			childJobIDs = append(childJobIDs, childID)
		}
	}
	return parentJobIDs, childJobIDs
}

func loadRetryDetailRowsForJob(rc *RunContext, itemID, scope, jobID string) ([]benchmarkRetryDetailRow, error) {
	var data map[string]interface{}
	path := "/api/admin/jobs/" + jobID + "/node-execution-attempts"
	if err := rc.Client.GetJSON(rc.Ctx, path, nil, &data); err != nil {
		return nil, err
	}

	attempts, ok := data["node_execution_attempts"].([]interface{})
	if !ok || len(attempts) == 0 {
		return nil, nil
	}

	rows := make([]benchmarkRetryDetailRow, 0, len(attempts))
	for _, attempt := range attempts {
		am, ok := attempt.(map[string]interface{})
		if !ok {
			continue
		}
		attemptNo := int(floatVal(am, "attempt"))
		status := stringVal(am, "status")
		errMsg := strings.TrimSpace(stringVal(am, "error_message"))
		code := strings.TrimSpace(stringVal(am, "error_code"))
		meta, _ := am["metadata"].(map[string]interface{})

		layer := stringVal(meta, "retry_layer")
		phase := stringVal(meta, "error_phase")
		if layer == "" || phase == "" {
			inferredLayer, inferredPhase := inferLayerPhaseFromAttempt(code, errMsg)
			if layer == "" {
				layer = inferredLayer
			}
			if phase == "" {
				phase = inferredPhase
			}
		}
		if code == "" {
			code = stringVal(meta, "provider_error_code")
		}
		reqID := stringVal(meta, "provider_request_id")
		if reqID == "" {
			reqID = stringVal(meta, "provider_response_id")
		}

		include := attemptNo > 1 || errMsg != "" || strings.EqualFold(status, "failed") || strings.EqualFold(status, "retrying")
		if !include {
			continue
		}

		rows = append(rows, benchmarkRetryDetailRow{
			ItemID:    itemID,
			Scope:     scope,
			JobID:     jobID,
			NodeID:    stringVal(am, "node_id"),
			Attempt:   attemptNo,
			Status:    status,
			Layer:     fallbackOr(layer, "-"),
			Phase:     fallbackOr(phase, "-"),
			Code:      fallbackOr(code, "-"),
			RequestID: reqID,
			Error:     errMsg,
		})
	}
	return rows, nil
}

func inferLayerPhaseFromAttempt(errorCode, errMsg string) (layer string, phase string) {
	code := strings.ToUpper(strings.TrimSpace(errorCode))
	lower := strings.ToLower(strings.TrimSpace(errMsg))

	switch {
	case strings.Contains(lower, "failed to read response"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "broken pipe"),
		strings.Contains(lower, "unexpected eof"):
		return "network", "transport_read"
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "timed out"),
		code == "TIMEOUT":
		return "node_runtime", "node_timeout"
	case strings.Contains(lower, "admission pool exhausted"),
		strings.Contains(lower, "server at capacity"),
		code == "POOL_EXHAUSTED":
		return "admission", "pool_exhausted"
	case code == "RATE_LIMIT" || strings.Contains(lower, "rate limit"):
		return "provider", "http_status"
	case code != "":
		return "runtime", "execution"
	default:
		return "-", "-"
	}
}

func fallbackOr(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func benchmarkRetriesTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	fmt.Fprintf(w, "Run: %s\n", stringVal(m, "run_id"))
	fmt.Fprintf(w, "Inspected jobs: %v  Detail rows: %v/%v\n\n",
		m["inspected_jobs"], m["shown_rows"], m["total_rows"])

	var summaryRows [][]string
	switch summary := m["summary"].(type) {
	case []benchmarkRetrySummaryRow:
		for _, entry := range summary {
			summaryRows = append(summaryRows, []string{
				entry.Layer,
				entry.Phase,
				entry.Code,
				fmt.Sprintf("%d", entry.Count),
			})
		}
	case []interface{}:
		for _, entry := range summary {
			em, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			summaryRows = append(summaryRows, []string{
				stringVal(em, "retry_layer"),
				stringVal(em, "error_phase"),
				stringVal(em, "error_code"),
				fmt.Sprintf("%.0f", floatVal(em, "count")),
			})
		}
	}
	if len(summaryRows) > 0 {
		headers := []string{"LAYER", "PHASE", "CODE", "COUNT"}
		fmt.Fprintln(w, "Retry/Error Summary:")
		printTableAuto(w, headers, summaryRows, format)
		fmt.Fprintln(w)
	}

	var detailRows [][]string
	switch details := m["details"].(type) {
	case []benchmarkRetryDetailRow:
		for _, entry := range details {
			detailRows = append(detailRows, []string{
				entry.ItemID,
				entry.Scope,
				truncate(entry.JobID, 18),
				truncate(entry.NodeID, 24),
				fmt.Sprintf("%d", entry.Attempt),
				entry.Status,
				truncate(entry.Layer, 12),
				truncate(entry.Phase, 16),
				truncate(entry.Code, 18),
				truncate(entry.RequestID, 14),
				truncate(entry.Error, 56),
			})
		}
	case []interface{}:
		for _, entry := range details {
			em, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			detailRows = append(detailRows, []string{
				stringVal(em, "item_id"),
				stringVal(em, "scope"),
				truncate(stringVal(em, "job_id"), 18),
				truncate(stringVal(em, "node_id"), 24),
				fmt.Sprintf("%.0f", floatVal(em, "attempt")),
				stringVal(em, "status"),
				truncate(stringVal(em, "retry_layer"), 12),
				truncate(stringVal(em, "error_phase"), 16),
				truncate(stringVal(em, "error_code"), 18),
				truncate(stringVal(em, "request_id"), 14),
				truncate(stringVal(em, "error"), 56),
			})
		}
	}
	if len(detailRows) > 0 {
		headers := []string{"ITEM", "SCOPE", "JOB", "NODE", "ATTEMPT", "STATUS", "LAYER", "PHASE", "CODE", "REQ_ID", "ERROR"}
		fmt.Fprintln(w, "Detailed Attempts:")
		printTableAuto(w, headers, detailRows, format)
	}
}

func printBenchmarkRetryHint(rc *RunContext, runID string, data interface{}) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	run, _ := m["Run"].(map[string]interface{})
	if run == nil {
		return
	}
	retriedItems := int(floatVal(run, "retried_items"))
	totalAttempts := int(floatVal(run, "total_attempts"))
	totalItems := int(floatVal(run, "total_items"))
	if retriedItems <= 0 && totalAttempts <= totalItems {
		// Fallback: benchmark get metrics miss child-level retries. Probe analysis diagnostics.
		if rc == nil {
			return
		}
		q := url.Values{}
		q.Set("top", "1")
		var analysisData map[string]interface{}
		if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/"+runID+"/analysis", q, &analysisData); err != nil {
			return
		}
		diagnostics, _ := analysisData["diagnostics"].(map[string]interface{})
		mostRetries, _ := diagnostics["most_retries_items"].([]interface{})
		if len(mostRetries) == 0 {
			return
		}
		fmt.Fprintf(os.Stderr, "Hint: child-level retries/errors detected. Dig deeper: conctl benchmarks retries --id %s --top 10\n", runID)
		return
	}
	fmt.Fprintf(os.Stderr, "Hint: retries/errors detected (retried_items=%d, total_attempts=%d). Dig deeper: conctl benchmarks retries --id %s --top 10\n",
		retriedItems, totalAttempts, runID)
}

func printBenchmarkAnalysisRetryHint(runID string, data map[string]interface{}) {
	if data == nil {
		return
	}
	diagnostics, _ := data["diagnostics"].(map[string]interface{})
	if diagnostics == nil {
		return
	}
	mostRetries, _ := diagnostics["most_retries_items"].([]interface{})
	if len(mostRetries) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "Hint: top retry-heavy items detected. Inspect retry layers/phases: conctl benchmarks retries --id %s --top %d\n",
		runID, len(mostRetries))
}
