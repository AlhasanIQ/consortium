package conctl

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	"github.com/alhasaniq/consortium/internal/conctl/output"
)

// OverviewResource returns the overview resource.
func OverviewResource() *app.Resource {
	return &app.Resource{
		Name: "overview",
		Desc: "System overview metrics and recent activity",
		Commands: []*app.Command{
			overviewGetCmd(),
		},
	}
}

func overviewGetCmd() *app.Command {
	return &app.Command{
		Name:      "get",
		Desc:      "Fetch system overview metrics",
		UsageLine: "conctl overview get [--watch --interval <dur>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("overview get", flag.ContinueOnError)
			fs.Bool("watch", false, "Poll continuously")
			fs.String("interval", "3s", "Poll interval (requires --watch)")
			return fs
		},
		Run: simpleWatchableGet("/api/admin/overview", nil),
	}
}

// runWatch implements --watch polling for any fetch function.
// It replaces the RunContext's timeout-bound context with a cancel-only context
// so that watch polling runs indefinitely until interrupted (Ctrl-C or signal).
func runWatch(rc *RunContext, intervalStr string, fetch func() (interface{}, error), tableFn output.TableFn) int {
	d, err := time.ParseDuration(intervalStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --interval %q: %v\n", intervalStr, err)
		return app.ExitUsage
	}

	// Replace timeout context with cancel-only so watch runs indefinitely.
	rc.Cancel()
	ctx, cancel := context.WithCancel(context.Background())
	rc.Ctx = ctx
	rc.Cancel = cancel

	// Re-register signal handler for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	ticker := time.NewTicker(d)
	defer ticker.Stop()

	first := true
	for {
		data, err := fetch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		} else {
			w, cleanup, werr := output.Writer(rc.GF.Output)
			if werr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", werr)
				return app.ExitInternal
			}
			if !first {
				fmt.Fprintf(w, "--- poll @ %s ---\n", time.Now().UTC().Format(time.RFC3339))
			}
			first = false
			_ = output.Render(w, rc.GF.Format, data, tableFn)
			cleanup()
		}

		select {
		case <-rc.Ctx.Done():
			return app.ExitSuccess
		case <-ticker.C:
		}
	}
}

// runWaitUntil polls until check(data) returns true, then prints the final result and exits.
// Agent-friendly: only the final result goes to stdout; progress lines go to stderr.
// Uses a 30-minute timeout by default (overridable via global --timeout).
func runWaitUntil(rc *RunContext, intervalStr string, fetch func() (interface{}, error), check func(interface{}) bool, tableFn output.TableFn) int {
	d, err := time.ParseDuration(intervalStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --interval %q: %v\n", intervalStr, err)
		return app.ExitUsage
	}

	// Replace the default 30s timeout with 30m for wait-until operations.
	// Respect explicit user timeout overrides.
	const waitUntilDefault = 30 * time.Minute
	if !rc.GF.TimeoutExplicit && rc.GF.Timeout == "30s" {
		if deadline, ok := rc.Ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 30*time.Second {
				rc.Cancel()
				ctx, cancel := context.WithTimeout(context.Background(), waitUntilDefault)
				rc.Ctx = ctx
				rc.Cancel = cancel
			}
		}
	}

	// Re-register signal handler for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		rc.Cancel()
	}()

	start := time.Now()
	ticker := time.NewTicker(d)
	defer ticker.Stop()

	for {
		data, err := fetch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		} else if check(data) {
			return rc.Output(data, tableFn)
		} else {
			elapsed := time.Since(start).Truncate(time.Second)
			fmt.Fprintf(os.Stderr, "waiting... (%s elapsed)\n", elapsed)
		}

		select {
		case <-rc.Ctx.Done():
			fmt.Fprintf(os.Stderr, "Error: timed out after %s\n", time.Since(start).Truncate(time.Second))
			return app.ExitServer
		case <-ticker.C:
		}
	}
}
