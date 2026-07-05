package conctl

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// LocalResource returns the local project helpers resource.
func LocalResource() *app.Resource {
	return &app.Resource{
		Name: "local",
		Desc: "Local project helpers (Make-backed)",
		Commands: []*app.Command{
			localSetupCmd(),
			localDevCmd(),
			localBackendFgCmd(),
			localBackendStartCmd(),
			localBackendStopCmd(),
			localBackendRestartCmd(),
			localBackendStatusCmd(),
			localBackendLogsCmd(),
			localFrontendFgCmd(),
			localFrontendStartCmd(),
			localFrontendStopCmd(),
			localFrontendRestartCmd(),
			localFrontendStatusCmd(),
			localFrontendLogsCmd(),
			localDBQueryCmd(),
			localDBResetCmd(),
			localRestartCmd(),
		},
	}
}

func localSetupCmd() *app.Command {
	return &app.Command{
		Name:      "setup",
		Desc:      "Create or refresh .env.local for this worktree",
		UsageLine: "conctl local setup --yes [--profile name] [--force]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local setup", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			fs.Bool("force", false, "Overwrite existing .env.local")
			fs.String("profile", "", "Worktree profile name")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local setup", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			force := fs.Bool("force", false, "Overwrite existing .env.local")
			profile := fs.String("profile", "", "Worktree profile name")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local setup"); !ok {
				return code
			}
			vars := map[string]string{}
			if *force {
				vars["FORCE"] = "1"
			}
			if strings.TrimSpace(*profile) != "" {
				vars["PROFILE"] = strings.TrimSpace(*profile)
			}
			return runMakeWithVars(gf, "worktree-setup", vars)
		},
	}
}

func localDevCmd() *app.Command {
	return &app.Command{
		Name:      "dev",
		Desc:      "Start both backend and frontend in background",
		UsageLine: "conctl local dev --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local dev", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local dev", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local dev"); !ok {
				return code
			}
			return runMake(gf, "dev")
		},
	}
}

func localBackendFgCmd() *app.Command {
	return &app.Command{
		Name:      "backend-fg",
		Desc:      "Run backend server in foreground (blocks terminal)",
		UsageLine: "conctl local backend-fg --yes [--force]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local backend-fg", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			fs.Bool("force", false, "Skip safety checks (active jobs/benchmarks)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local backend-fg", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			force := fs.Bool("force", false, "Skip safety checks")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local backend-fg"); !ok {
				return code
			}
			if *force {
				return runMakeWithVar(gf, "backend", "FORCE", "1")
			}
			return runMake(gf, "backend")
		},
	}
}

func localBackendStartCmd() *app.Command {
	return &app.Command{
		Name:      "backend-start",
		Desc:      "Start backend server in background",
		UsageLine: "conctl local backend-start --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local backend-start", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local backend-start", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local backend-start"); !ok {
				return code
			}
			return runMake(gf, "backend-bg")
		},
	}
}

func localBackendStopCmd() *app.Command {
	return &app.Command{
		Name:      "backend-stop",
		Desc:      "Stop the backend server (blocked by active work; use --force to override)",
		UsageLine: "conctl local backend-stop --yes [--force]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local backend-stop", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			fs.Bool("force", false, "Skip safety checks (active jobs/benchmarks)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local backend-stop", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			force := fs.Bool("force", false, "Skip safety checks")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local backend-stop"); !ok {
				return code
			}
			if *force {
				return runMakeWithVar(gf, "backend-stop", "FORCE", "1")
			}
			return runMake(gf, "backend-stop")
		},
	}
}

func localBackendRestartCmd() *app.Command {
	return &app.Command{
		Name:      "backend-restart",
		Desc:      "Restart the backend server",
		UsageLine: "conctl local backend-restart --yes [--force]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local backend-restart", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			fs.Bool("force", false, "Skip safety checks (active jobs/benchmarks)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local backend-restart", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			force := fs.Bool("force", false, "Skip safety checks")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local backend-restart"); !ok {
				return code
			}
			if *force {
				return runMakeWithVar(gf, "backend-restart", "FORCE", "1")
			}
			return runMake(gf, "backend-restart")
		},
	}
}

func localBackendStatusCmd() *app.Command {
	return &app.Command{
		Name:      "backend-status",
		Desc:      "Check backend server status",
		UsageLine: "conctl local backend-status",
		Run: func(gf app.GlobalFlags, args []string) int {
			return runMake(gf, "backend-status")
		},
	}
}

func localFrontendFgCmd() *app.Command {
	return &app.Command{
		Name:      "frontend-fg",
		Desc:      "Run frontend dev server in foreground (blocks terminal)",
		UsageLine: "conctl local frontend-fg --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local frontend-fg", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local frontend-fg", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local frontend-fg"); !ok {
				return code
			}
			return runMake(gf, "frontend")
		},
	}
}

func localFrontendStartCmd() *app.Command {
	return &app.Command{
		Name:      "frontend-start",
		Desc:      "Start frontend dev server in background",
		UsageLine: "conctl local frontend-start --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local frontend-start", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local frontend-start", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local frontend-start"); !ok {
				return code
			}
			return runMake(gf, "frontend-bg")
		},
	}
}

func localFrontendStopCmd() *app.Command {
	return &app.Command{
		Name:      "frontend-stop",
		Desc:      "Stop the frontend dev server",
		UsageLine: "conctl local frontend-stop --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local frontend-stop", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local frontend-stop", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local frontend-stop"); !ok {
				return code
			}
			return runMake(gf, "frontend-stop")
		},
	}
}

func localFrontendRestartCmd() *app.Command {
	return &app.Command{
		Name:      "frontend-restart",
		Desc:      "Restart the frontend dev server",
		UsageLine: "conctl local frontend-restart --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local frontend-restart", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local frontend-restart", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local frontend-restart"); !ok {
				return code
			}
			return runMake(gf, "frontend-restart")
		},
	}
}

func localFrontendStatusCmd() *app.Command {
	return &app.Command{
		Name:      "frontend-status",
		Desc:      "Check frontend server status",
		UsageLine: "conctl local frontend-status",
		Run: func(gf app.GlobalFlags, args []string) int {
			return runMake(gf, "frontend-status")
		},
	}
}

func localBackendLogsCmd() *app.Command {
	return &app.Command{
		Name:      "backend-logs",
		Desc:      "View backend server logs",
		UsageLine: "conctl local backend-logs [--tail N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local backend-logs", flag.ContinueOnError)
			fs.Int("tail", 0, "Show last N lines (0 = all)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local backend-logs", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			tail := fs.Int("tail", 0, "Tail lines")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			return runMakeWithTail(gf, "backend-logs", *tail)
		},
	}
}

func localFrontendLogsCmd() *app.Command {
	return &app.Command{
		Name:      "frontend-logs",
		Desc:      "View frontend server logs",
		UsageLine: "conctl local frontend-logs [--tail N]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local frontend-logs", flag.ContinueOnError)
			fs.Int("tail", 0, "Show last N lines (0 = all)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local frontend-logs", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			tail := fs.Int("tail", 0, "Tail lines")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			return runMakeWithTail(gf, "frontend-logs", *tail)
		},
	}
}

func localDBQueryCmd() *app.Command {
	return &app.Command{
		Name:      "db-query",
		Desc:      "Run a SQL query against the local database",
		UsageLine: "conctl local db-query --sql <query>",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local db-query", flag.ContinueOnError)
			fs.String("sql", "", "SQL query (required)")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local db-query", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			sql := fs.String("sql", "", "SQL query (required)")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if *sql == "" {
				fmt.Fprintln(os.Stderr, "Error: --sql is required")
				return app.ExitUsage
			}
			return runMakeWithVar(gf, "db-query", "SQL", *sql)
		},
	}
}

func localDBResetCmd() *app.Command {
	return &app.Command{
		Name:      "db-reset",
		Desc:      "Reset the local database (destructive)",
		UsageLine: "conctl local db-reset --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local db-reset", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm destructive operation")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local db-reset", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local db-reset"); !ok {
				return code
			}
			return runMake(gf, "db-reset")
		},
	}
}

func localRestartCmd() *app.Command {
	return &app.Command{
		Name:      "restart",
		Desc:      "Restart backend and frontend servers",
		UsageLine: "conctl local restart --yes",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("local restart", flag.ContinueOnError)
			fs.Bool("yes", false, "Confirm")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			fs := flag.NewFlagSet("local restart", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			yes := fs.Bool("yes", false, "Confirm")
			if err := fs.Parse(args); err != nil {
				return app.ExitUsage
			}
			if code, ok := RequireYes(*yes, "local restart"); !ok {
				return code
			}
			return runMake(gf, "restart")
		},
	}
}

// runMake executes a make target from the project root.
func runMake(gf app.GlobalFlags, target string) int {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitInternal
	}

	cmd := exec.Command("make", target)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if gf.Verbose {
		fmt.Fprintf(os.Stderr, "[conctl] make %s (in %s)\n", target, root)
	}

	if err := cmd.Run(); err != nil {
		return app.ExitInternal
	}
	return app.ExitSuccess
}

// runMakeWithTail runs a make target and optionally tails the output.
func runMakeWithTail(gf app.GlobalFlags, target string, tail int) int {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitInternal
	}

	if tail <= 0 {
		cmd := exec.Command("make", target)
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return app.ExitInternal
		}
		return app.ExitSuccess
	}

	// Pipe through tail.
	makeCmd := exec.Command("make", target)
	makeCmd.Dir = root
	makeCmd.Stderr = os.Stderr

	out, err := makeCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitInternal
	}

	lines := strings.Split(string(out), "\n")
	start := len(lines) - tail
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		fmt.Fprintln(os.Stdout, line)
	}
	return app.ExitSuccess
}

// runMakeWithVar runs a make target with a variable assignment.
func runMakeWithVar(gf app.GlobalFlags, target, varName, varValue string) int {
	return runMakeWithVars(gf, target, map[string]string{varName: varValue})
}

func runMakeWithVars(gf app.GlobalFlags, target string, vars map[string]string) int {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitInternal
	}

	args := []string{"make", target}
	for name, value := range vars {
		args = append(args, name+"="+value)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if gf.Verbose {
		fmt.Fprintf(os.Stderr, "[conctl] %s (in %s)\n", strings.Join(args, " "), root)
	}

	if err := cmd.Run(); err != nil {
		return app.ExitInternal
	}
	return app.ExitSuccess
}

// findProjectRoot walks up from CWD looking for a Makefile with consortium markers.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(dir + "/Makefile"); err == nil {
			if _, err := os.Stat(dir + "/cmd/server"); err == nil {
				return dir, nil
			}
		}
		parent := dir[:strings.LastIndex(dir, "/")]
		if parent == dir || parent == "" {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("cannot find consortium project root (looked for Makefile + cmd/server)")
}
