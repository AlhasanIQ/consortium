package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// App is the top-level CLI application.
type App struct {
	version   string
	commit    string
	resources []*Resource
}

// New creates a new App.
func New(version, commit string) *App {
	return &App{version: version, commit: commit}
}

// Run dispatches to the appropriate resource and command.
// Returns an exit code.
func (a *App) Run(args []string) int {
	// Parse global flags.
	gf, remaining, err := a.parseGlobalFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitUsage
	}

	if len(remaining) == 0 {
		a.printHelp(os.Stderr)
		return ExitUsage
	}

	resourceName := remaining[0]
	remaining = remaining[1:]

	// Built-in commands.
	switch resourceName {
	case "help":
		return a.runHelp(gf, remaining)
	case "version":
		return a.runVersion(gf)
	case "completion":
		return a.runCompletion(remaining)
	}

	// Find resource.
	resource := a.findResource(resourceName)
	if resource == nil {
		fmt.Fprintf(os.Stderr, "Error: unknown resource %q\n\n", resourceName)
		a.printHelp(os.Stderr)
		return ExitUsage
	}

	if len(remaining) == 0 {
		resource.PrintHelp(os.Stderr)
		return ExitUsage
	}

	cmdName := remaining[0]
	remaining = remaining[1:]
	if isHelpArg(cmdName) {
		resource.PrintHelp(os.Stdout)
		return ExitSuccess
	}

	cmd := resource.FindCommand(cmdName)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "Error: unknown command %q for resource %q\n\n", cmdName, resourceName)
		resource.PrintHelp(os.Stderr)
		return ExitUsage
	}

	if commandWantsHelp(cmd, remaining) {
		cmd.PrintHelp(os.Stdout)
		return ExitSuccess
	}

	return cmd.Run(gf, remaining)
}

func (a *App) parseGlobalFlags(args []string) (GlobalFlags, []string, error) {
	var gf GlobalFlags
	fs := flag.NewFlagSet("conctl", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.StringVar(&gf.URL, "url", envOrDefault("CONCTL_URL", "http://localhost:8080"), "Base server URL")
	fs.StringVar(&gf.Timeout, "timeout", envOrDefault("CONCTL_TIMEOUT", "30s"), "Per-request timeout")
	fs.StringVar(&gf.Format, "format", envOrDefault("CONCTL_FORMAT", "md"), "Output format: md, json, table, jsonl")
	fs.StringVar(&gf.Output, "output", "-", "Output target: - for stdout, otherwise file path")
	fs.BoolVar(&gf.Verbose, "verbose", false, "Emit request/response timing to stderr")

	// Separate global flags from resource/command args.
	// Extract global flag tokens, leave everything else as remaining.
	var globalArgs, remaining []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			remaining = append(remaining, args[i+1:]...)
			break
		}
		if isGlobalFlag(arg) {
			globalArgs = append(globalArgs, arg)
			// If --flag value form (not --flag=value), consume the next arg.
			if needsValue(arg) && i+1 < len(args) {
				i++
				globalArgs = append(globalArgs, args[i])
			}
			continue
		}
		// First non-global-flag: rest is resource/command/command-flags.
		remaining = append(remaining, args[i:]...)
		break
	}

	if len(globalArgs) > 0 {
		if err := fs.Parse(globalArgs); err != nil {
			return gf, nil, err
		}
	}
	for _, arg := range globalArgs {
		if arg == "--timeout" || strings.HasPrefix(arg, "--timeout=") {
			gf.TimeoutExplicit = true
			break
		}
	}

	// Validate timeout parses.
	if _, err := time.ParseDuration(gf.Timeout); err != nil {
		return gf, nil, fmt.Errorf("invalid --timeout %q: %v", gf.Timeout, err)
	}

	// Validate format.
	switch gf.Format {
	case "json", "table", "md", "jsonl":
	default:
		return gf, nil, fmt.Errorf("invalid --format %q: must be json, table, md, or jsonl", gf.Format)
	}

	return gf, remaining, nil
}

func isGlobalFlag(s string) bool {
	switch s {
	case "--url", "--timeout", "--format", "--output", "--verbose":
		return true
	}
	// Handle --flag=value form.
	for _, prefix := range []string{"--url=", "--timeout=", "--format=", "--output="} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func needsValue(s string) bool {
	switch s {
	case "--url", "--timeout", "--format", "--output":
		return true
	}
	return false
}

func isHelpArg(arg string) bool {
	switch arg {
	case "-h", "--h", "-help", "--help":
		return true
	default:
		return false
	}
}

func commandWantsHelp(cmd *Command, args []string) bool {
	if len(args) == 0 {
		return false
	}
	if cmd.Flags == nil {
		return len(args) == 1 && isHelpArg(args[0])
	}

	fs := cmd.Flags()
	fs.SetOutput(io.Discard)
	// This assumes Command.Flags mirrors the FlagSet parsed by Command.Run.
	// Help rendering and JSON help already rely on that invariant.
	return errors.Is(fs.Parse(args), flag.ErrHelp)
}

func (a *App) findResource(name string) *Resource {
	for _, r := range a.resources {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func (a *App) runVersion(gf GlobalFlags) int {
	if gf.Format == "json" {
		data := map[string]string{
			"version": a.version,
			"commit":  a.commit,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(data)
	} else {
		fmt.Fprintf(os.Stdout, "conctl %s (%s)\n", a.version, a.commit)
	}
	return ExitSuccess
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
