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

func benchmarksFlagCmd() *app.Command {
	return &app.Command{
		Name:      "flag",
		Desc:      "Flag benchmark items with suspected wrong gold labels.\n\nMultiple items can be flagged by repeating --item.",
		UsageLine: "conctl benchmarks flag --benchmark <name> --split <split> --item <id> [--item <id2>] --reason <text> [--source <src>] --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks flag", flag.ContinueOnError)
			fs.String("benchmark", "", "Benchmark name (required)")
			fs.String("split", "", "Split name (required)")
			fs.String("item", "", "Item ID to flag (required, repeatable via comma-separated)")
			fs.String("reason", "", "Reason for flagging (required)")
			fs.String("source", "", "Source attribution (e.g. human:manual, benchloop:session-123)")
			fs.Bool("yes", false, "Confirm mutation")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks flag", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			benchmark := fs.String("benchmark", "", "Benchmark")
			split := fs.String("split", "", "Split")
			item := fs.String("item", "", "Item ID(s), comma-separated")
			reason := fs.String("reason", "", "Reason")
			source := fs.String("source", "", "Source")
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *benchmark == "" || *split == "" || *item == "" || *reason == "" {
				fmt.Fprintln(os.Stderr, "Error: --benchmark, --split, --item, and --reason are required")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "benchmarks flag"); !ok {
				return code
			}

			itemIDs := strings.Split(*item, ",")
			flags := make([]map[string]string, 0, len(itemIDs))
			for _, id := range itemIDs {
				id = strings.TrimSpace(id)
				if id == "" {
					continue
				}
				f := map[string]string{
					"benchmark": *benchmark,
					"split":     *split,
					"item_id":   id,
					"reason":    *reason,
				}
				if *source != "" {
					f["source"] = *source
				}
				flags = append(flags, f)
			}
			if len(flags) == 0 {
				fmt.Fprintln(os.Stderr, "Error: no valid item IDs provided")
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			body := map[string]interface{}{"flags": flags}
			var data interface{}
			if err := rc.Client.PostJSON(rc.Ctx, "/api/admin/benchmarks/dataset-flags", body, &data); err != nil {
				return HandleError(err)
			}
			return rc.Output(data, func(w io.Writer, d interface{}, format string) {
				dataMap, _ := d.(map[string]interface{})
				created, _ := dataMap["created"].(float64)
				fmt.Fprintf(w, "Flagged %d item(s) in %s/%s\n", int(created), *benchmark, *split)
				if flagsList, ok := dataMap["flags"].([]interface{}); ok {
					for _, fl := range flagsList {
						flm, _ := fl.(map[string]interface{})
						fmt.Fprintf(w, "  ID=%v  item=%v  reason=%v\n",
							flm["id"], flm["item_id"], flm["reason"])
					}
				}
			})
		},
	}
}

func benchmarksUnflagCmd() *app.Command {
	return &app.Command{
		Name:      "unflag",
		Desc:      "Resolve (unflag) a dataset flag by ID or by natural key.\n\nProvide either --id or all of --benchmark/--split/--item.",
		UsageLine: "conctl benchmarks unflag {--id <flag-id> | --benchmark <name> --split <split> --item <id>} --reason <text> [--resolved-by <who>] --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks unflag", flag.ContinueOnError)
			fs.Int("id", 0, "Flag ID to resolve")
			fs.String("benchmark", "", "Benchmark name (for natural key)")
			fs.String("split", "", "Split name (for natural key)")
			fs.String("item", "", "Item ID (for natural key)")
			fs.String("reason", "", "Resolve reason (required)")
			fs.String("resolved-by", "", "Who resolved it")
			fs.Bool("yes", false, "Confirm mutation")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks unflag", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			id := fs.Int("id", 0, "Flag ID")
			benchmark := fs.String("benchmark", "", "Benchmark")
			split := fs.String("split", "", "Split")
			item := fs.String("item", "", "Item ID")
			reason := fs.String("reason", "", "Resolve reason")
			resolvedBy := fs.String("resolved-by", "", "Resolved by")
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *reason == "" {
				fmt.Fprintln(os.Stderr, "Error: --reason is required")
				return app.ExitUsage
			}
			byID := *id > 0
			byKey := *benchmark != "" && *split != "" && *item != ""
			if !byID && !byKey {
				fmt.Fprintln(os.Stderr, "Error: provide --id or all of --benchmark/--split/--item")
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "benchmarks unflag"); !ok {
				return code
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			body := map[string]string{
				"resolved_reason": *reason,
				"resolved_by":     *resolvedBy,
			}

			if byID {
				var data interface{}
				path := fmt.Sprintf("/api/admin/benchmarks/dataset-flags/%d/resolve", *id)
				if err := rc.Client.PatchJSON(rc.Ctx, path, body, &data); err != nil {
					return HandleError(err)
				}
				fmt.Fprintf(os.Stdout, "Resolved flag %d\n", *id)
				return app.ExitSuccess
			}

			// Resolve by natural key: list active flags for this item, then resolve by ID
			q := url.Values{}
			q.Set("benchmark", *benchmark)
			q.Set("split", *split)
			q.Set("active_only", "true")
			var listData map[string]interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/dataset-flags", q, &listData); err != nil {
				return HandleError(err)
			}
			flags, _ := listData["flags"].([]interface{})
			var targetID float64
			for _, fl := range flags {
				flm, _ := fl.(map[string]interface{})
				if flm["item_id"] == *item {
					targetID, _ = flm["id"].(float64)
					break
				}
			}
			if targetID == 0 {
				fmt.Fprintf(os.Stderr, "Error: no active flag found for %s/%s/%s\n", *benchmark, *split, *item)
				return app.ExitInternal
			}

			var data interface{}
			path := fmt.Sprintf("/api/admin/benchmarks/dataset-flags/%d/resolve", int64(targetID))
			if err := rc.Client.PatchJSON(rc.Ctx, path, body, &data); err != nil {
				return HandleError(err)
			}
			fmt.Fprintf(os.Stdout, "Resolved flag %d (%s/%s/%s)\n", int64(targetID), *benchmark, *split, *item)
			return app.ExitSuccess
		},
	}
}

func benchmarksFlagsCmd() *app.Command {
	return &app.Command{
		Name:      "flags",
		Desc:      "List dataset flags for a benchmark/split.\n\nBy default shows only active (unresolved) flags. Use --all to include resolved.",
		UsageLine: "conctl benchmarks flags --benchmark <name> [--split <split>] [--all]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("benchmarks flags", flag.ContinueOnError)
			fs.String("benchmark", "", "Benchmark name (required)")
			fs.String("split", "", "Split name")
			fs.Bool("all", false, "Include resolved flags")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("benchmarks flags", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			benchmark := fs.String("benchmark", "", "Benchmark")
			split := fs.String("split", "", "Split")
			all := fs.Bool("all", false, "Include resolved")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *benchmark == "" {
				fmt.Fprintln(os.Stderr, "Error: --benchmark is required")
				return app.ExitUsage
			}

			rc, err := NewRunContext(gf)
			if err != nil {
				return HandleError(err)
			}
			defer rc.Cancel()

			q := url.Values{}
			q.Set("benchmark", *benchmark)
			if *split != "" {
				q.Set("split", *split)
			}
			if *all {
				q.Set("active_only", "false")
			} else {
				q.Set("active_only", "true")
			}

			var data interface{}
			if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/dataset-flags", q, &data); err != nil {
				return HandleError(err)
			}

			return rc.Output(data, func(w io.Writer, d interface{}, format string) {
				dataMap, _ := d.(map[string]interface{})
				flags, _ := dataMap["flags"].([]interface{})
				total, _ := dataMap["total"].(float64)

				if len(flags) == 0 {
					fmt.Fprintf(w, "No flags found for %s", *benchmark)
					if *split != "" {
						fmt.Fprintf(w, "/%s", *split)
					}
					fmt.Fprintln(w)
					return
				}

				fmt.Fprintf(w, "Dataset flags (%d):\n\n", int(total))
				fmt.Fprintf(w, "  %-6s %-20s %-8s %-12s %-50s %s\n",
					"ID", "BENCHMARK", "SPLIT", "ITEM", "REASON", "STATUS")
				fmt.Fprintf(w, "  %-6s %-20s %-8s %-12s %-50s %s\n",
					"------", "--------------------", "--------", "------------",
					"--------------------------------------------------", "--------")

				for _, fl := range flags {
					flm, _ := fl.(map[string]interface{})
					id, _ := flm["id"].(float64)
					bm, _ := flm["benchmark"].(string)
					sp, _ := flm["split"].(string)
					itemID, _ := flm["item_id"].(string)
					reason, _ := flm["reason"].(string)
					status := "ACTIVE"
					if flm["resolved_at"] != nil {
						status = "RESOLVED"
					}
					if len(reason) > 50 {
						reason = reason[:47] + "..."
					}
					fmt.Fprintf(w, "  %-6d %-20s %-8s %-12s %-50s %s\n",
						int(id), truncStr(bm, 20), truncStr(sp, 8), truncStr(itemID, 12), reason, status)
				}
			})
		},
	}
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
