package app

import (
	"flag"
	"testing"
)

func TestRun_NoArgs(t *testing.T) {
	a := New("test", "abc123")
	code := a.Run(nil)
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
}

func TestRun_UnknownResource(t *testing.T) {
	a := New("test", "abc123")
	code := a.Run([]string{"nosuchresource"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
}

func TestRun_Version(t *testing.T) {
	a := New("1.0.0", "deadbeef")
	code := a.Run([]string{"version"})
	if code != ExitSuccess {
		t.Errorf("expected exit %d, got %d", ExitSuccess, code)
	}
}

func TestRun_Help(t *testing.T) {
	a := New("test", "abc123")
	code := a.Run([]string{"help"})
	if code != ExitSuccess {
		t.Errorf("expected exit %d, got %d", ExitSuccess, code)
	}
}

func TestRun_HelpJSON(t *testing.T) {
	a := New("test", "abc123")
	code := a.Run([]string{"--format", "json", "help"})
	if code != ExitSuccess {
		t.Errorf("expected exit %d, got %d", ExitSuccess, code)
	}
}

func TestParseGlobalFlags_Defaults(t *testing.T) {
	t.Setenv("CONCTL_URL", "")
	t.Setenv("CONCTL_TIMEOUT", "")
	a := New("test", "abc")
	gf, remaining, err := a.parseGlobalFlags([]string{"jobs", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gf.URL != "http://localhost:8080" {
		t.Errorf("expected default URL, got %q", gf.URL)
	}
	if gf.Format != "md" {
		t.Errorf("expected md format, got %q", gf.Format)
	}
	if gf.Timeout != "30s" {
		t.Errorf("expected 30s timeout, got %q", gf.Timeout)
	}
	if gf.TimeoutExplicit {
		t.Error("expected timeout explicit=false for defaults")
	}
	if gf.Output != "-" {
		t.Errorf("expected - output, got %q", gf.Output)
	}
	if gf.Verbose {
		t.Error("expected verbose=false")
	}
	if len(remaining) != 2 || remaining[0] != "jobs" || remaining[1] != "list" {
		t.Errorf("unexpected remaining: %v", remaining)
	}
}

func TestParseGlobalFlags_Override(t *testing.T) {
	a := New("test", "abc")
	gf, remaining, err := a.parseGlobalFlags([]string{
		"--url", "http://example.com:9090",
		"--format", "table",
		"--timeout", "10s",
		"--verbose",
		"jobs", "list", "--status", "running",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gf.URL != "http://example.com:9090" {
		t.Errorf("expected overridden URL, got %q", gf.URL)
	}
	if gf.Format != "table" {
		t.Errorf("expected table format, got %q", gf.Format)
	}
	if gf.Timeout != "10s" {
		t.Errorf("expected 10s timeout, got %q", gf.Timeout)
	}
	if !gf.TimeoutExplicit {
		t.Error("expected timeout explicit=true when --timeout is provided")
	}
	if !gf.Verbose {
		t.Error("expected verbose=true")
	}
	// Resource args should be preserved.
	if len(remaining) != 4 {
		t.Errorf("expected 4 remaining args, got %d: %v", len(remaining), remaining)
	}
	if remaining[0] != "jobs" {
		t.Errorf("expected first remaining to be 'jobs', got %q", remaining[0])
	}
}

func TestParseGlobalFlags_InvalidFormat(t *testing.T) {
	a := New("test", "abc")
	_, _, err := a.parseGlobalFlags([]string{"--format", "yaml", "jobs"})
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestParseGlobalFlags_InvalidTimeout(t *testing.T) {
	a := New("test", "abc")
	_, _, err := a.parseGlobalFlags([]string{"--timeout", "notaduration", "jobs"})
	if err == nil {
		t.Error("expected error for invalid timeout")
	}
}

func TestRun_ResourceNoCommand(t *testing.T) {
	a := New("test", "abc")
	a.Register(&Resource{
		Name: "testres",
		Desc: "Test resource",
		Commands: []*Command{
			{Name: "list", Desc: "List items"},
		},
	})
	code := a.Run([]string{"testres"})
	if code != ExitUsage {
		t.Errorf("expected exit %d for no command, got %d", ExitUsage, code)
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	a := New("test", "abc")
	a.Register(&Resource{
		Name: "testres",
		Desc: "Test resource",
		Commands: []*Command{
			{Name: "list", Desc: "List items"},
		},
	})
	code := a.Run([]string{"testres", "nosuchcmd"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
}

func TestRun_CommandHelpFlagDoesNotExecuteCommand(t *testing.T) {
	a := New("test", "abc")
	executed := false
	a.Register(&Resource{
		Name: "testres",
		Desc: "Test resource",
		Commands: []*Command{
			{
				Name:      "run",
				Desc:      "Run something",
				UsageLine: "conctl testres run",
				Run: func(gf GlobalFlags, args []string) int {
					executed = true
					return ExitUsage
				},
			},
		},
	})
	code := a.Run([]string{"testres", "run", "--help"})
	if code != ExitSuccess {
		t.Errorf("expected exit %d, got %d", ExitSuccess, code)
	}
	if executed {
		t.Error("command help should not execute the command")
	}
}

func TestRun_CommandHelpFlagSetDoesNotExecuteCommand(t *testing.T) {
	a := New("test", "abc")
	executed := false
	a.Register(&Resource{
		Name: "testres",
		Desc: "Test resource",
		Commands: []*Command{
			{
				Name:      "run",
				Desc:      "Run something",
				UsageLine: "conctl testres run --id <id>",
				Flags: func() *flag.FlagSet {
					fs := flag.NewFlagSet("testres run", flag.ContinueOnError)
					fs.String("id", "", "ID")
					return fs
				},
				Run: func(gf GlobalFlags, args []string) int {
					executed = true
					return ExitUsage
				},
			},
		},
	})

	for _, args := range [][]string{
		{"testres", "run", "--help"},
		{"testres", "run", "--id", "job-1", "--help"},
	} {
		executed = false
		code := a.Run(args)
		if code != ExitSuccess {
			t.Errorf("a.Run(%v): expected exit %d, got %d", args, ExitSuccess, code)
		}
		if executed {
			t.Errorf("a.Run(%v): command help should not execute the command", args)
		}
	}
}

func TestRun_CommandHelpValueStillExecutesCommand(t *testing.T) {
	a := New("test", "abc")
	executed := false
	a.Register(&Resource{
		Name: "testres",
		Desc: "Test resource",
		Commands: []*Command{
			{
				Name:      "run",
				Desc:      "Run something",
				UsageLine: "conctl testres run",
				Flags: func() *flag.FlagSet {
					fs := flag.NewFlagSet("testres run", flag.ContinueOnError)
					fs.String("query", "", "Query text")
					return fs
				},
				Run: func(gf GlobalFlags, args []string) int {
					executed = true
					return ExitSuccess
				},
			},
		},
	})
	code := a.Run([]string{"testres", "run", "--query", "--help"})
	if code != ExitSuccess {
		t.Errorf("expected exit %d, got %d", ExitSuccess, code)
	}
	if !executed {
		t.Error("command flag value should not be treated as command help")
	}
}

func TestRun_ResourceDoubleDashHPrintsHelp(t *testing.T) {
	a := New("test", "abc")
	a.Register(&Resource{
		Name: "testres",
		Desc: "Test resource",
		Commands: []*Command{
			{
				Name:      "run",
				Desc:      "Run something",
				UsageLine: "conctl testres run",
				Run: func(gf GlobalFlags, args []string) int {
					return ExitUsage
				},
			},
		},
	})
	code := a.Run([]string{"testres", "--h"})
	if code != ExitSuccess {
		t.Errorf("expected exit %d, got %d", ExitSuccess, code)
	}
}

func TestRun_CommandExecution(t *testing.T) {
	a := New("test", "abc")
	executed := false
	a.Register(&Resource{
		Name: "testres",
		Desc: "Test resource",
		Commands: []*Command{
			{
				Name: "run",
				Desc: "Run something",
				Run: func(gf GlobalFlags, args []string) int {
					executed = true
					return ExitSuccess
				},
			},
		},
	})
	code := a.Run([]string{"testres", "run"})
	if code != ExitSuccess {
		t.Errorf("expected exit %d, got %d", ExitSuccess, code)
	}
	if !executed {
		t.Error("command was not executed")
	}
}
