package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

func (a *App) printHelp(w io.Writer) {
	fmt.Fprintln(w, "conctl — Consortium admin CLI for AI agents")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: conctl [global-flags] <resource> <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --url <url>        Base server URL (default: http://localhost:8080)")
	fmt.Fprintln(w, "  --timeout <dur>    Per-request timeout (default: 30s)")
	fmt.Fprintln(w, "  --format <fmt>     Output format: json, table, md, jsonl (default: json)")
	fmt.Fprintln(w, "  --output <path>    Output target: - for stdout, file path (default: -)")
	fmt.Fprintln(w, "  --verbose          Emit request/response timing to stderr")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Resources:")
	maxLen := 0
	for _, r := range a.resources {
		if len(r.Name) > maxLen {
			maxLen = len(r.Name)
		}
	}
	for _, r := range a.resources {
		fmt.Fprintf(w, "  %-*s  %s\n", maxLen, r.Name, r.Desc)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Built-in commands:")
	fmt.Fprintln(w, "  help         Show help (use --format json for machine-readable)")
	fmt.Fprintln(w, "  version      Show version info")
	fmt.Fprintln(w, "  completion   Generate shell completions (e.g. conctl completion bash)")
	fmt.Fprintln(w)
}

func (a *App) runHelp(gf GlobalFlags, args []string) int {
	if gf.Format == "json" {
		return a.runHelpJSON()
	}

	if len(args) == 0 {
		a.printHelp(os.Stdout)
		return ExitSuccess
	}

	resourceName := args[0]
	resource := a.findResource(resourceName)
	if resource == nil {
		fmt.Fprintf(os.Stderr, "Error: unknown resource %q\n\n", resourceName)
		a.printHelp(os.Stderr)
		return ExitUsage
	}

	if len(args) > 1 {
		cmdName := args[1]
		cmd := resource.FindCommand(cmdName)
		if cmd == nil {
			fmt.Fprintf(os.Stderr, "Error: unknown command %q for resource %q\n\n", cmdName, resourceName)
			resource.PrintHelp(os.Stderr)
			return ExitUsage
		}
		cmd.PrintHelp(os.Stdout)
		return ExitSuccess
	}

	resource.PrintHelp(os.Stdout)
	return ExitSuccess
}

func (c *Command) PrintHelp(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s\n\n", c.UsageLine)
	fmt.Fprintf(w, "%s\n\n", c.Desc)
	if c.Flags == nil {
		return
	}
	fmt.Fprintln(w, "Flags:")
	fs := c.Flags()
	fs.SetOutput(w)
	fs.PrintDefaults()
}

// helpSchema is the machine-readable help output.
type helpSchema struct {
	Binary    string               `json:"binary"`
	Version   string               `json:"version"`
	Resources []helpResourceSchema `json:"resources"`
}

type helpResourceSchema struct {
	Name     string              `json:"name"`
	Desc     string              `json:"desc"`
	Commands []helpCommandSchema `json:"commands"`
}

type helpCommandSchema struct {
	Name      string           `json:"name"`
	Desc      string           `json:"desc"`
	UsageLine string           `json:"usage_line"`
	Flags     []helpFlagSchema `json:"flags,omitempty"`
}

type helpFlagSchema struct {
	Name    string `json:"name"`
	Default string `json:"default"`
	Usage   string `json:"usage"`
}

func (a *App) runHelpJSON() int {
	schema := helpSchema{
		Binary:  "conctl",
		Version: a.version,
	}

	for _, r := range a.resources {
		rs := helpResourceSchema{
			Name: r.Name,
			Desc: r.Desc,
		}
		for _, c := range r.Commands {
			cs := helpCommandSchema{
				Name:      c.Name,
				Desc:      c.Desc,
				UsageLine: c.UsageLine,
			}
			if c.Flags != nil {
				fs := c.Flags()
				fs.VisitAll(func(f *flag.Flag) {
					cs.Flags = append(cs.Flags, helpFlagSchema{
						Name:    f.Name,
						Default: f.DefValue,
						Usage:   f.Usage,
					})
				})
			}
			rs.Commands = append(rs.Commands, cs)
		}
		schema.Resources = append(schema.Resources, rs)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(schema)
	return ExitSuccess
}
