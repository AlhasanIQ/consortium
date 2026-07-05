package app

import (
	"flag"
	"fmt"
	"io"
)

// ExitCode constants for deterministic agent-parseable exit codes.
const (
	ExitSuccess  = 0
	ExitUsage    = 2
	ExitAuth     = 3
	ExitNotFound = 4
	ExitConflict = 5
	ExitServer   = 6
	ExitInternal = 1
)

// GlobalFlags holds flags shared by all commands.
type GlobalFlags struct {
	URL             string
	Timeout         string
	TimeoutExplicit bool
	Format          string
	Output          string
	Verbose         bool
}

// Command represents a leaf command in the CLI tree.
type Command struct {
	// Name is the leaf verb (e.g. "list", "get", "cancel").
	Name string
	// Desc is a one-line description shown in help.
	Desc string
	// UsageLine shows usage pattern (e.g. "conctl jobs list [flags]").
	UsageLine string
	// Flags returns a FlagSet for this command.
	Flags func() *flag.FlagSet
	// Run executes the command. It receives parsed global flags and
	// the remaining args after global flag parsing.
	Run func(gf GlobalFlags, args []string) int
}

// Resource groups related commands under a resource name.
type Resource struct {
	Name     string
	Desc     string
	Commands []*Command
}

// FindCommand returns the command matching name, or nil.
func (r *Resource) FindCommand(name string) *Command {
	for _, c := range r.Commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// PrintHelp writes resource-level help to w.
func (r *Resource) PrintHelp(w io.Writer) {
	fmt.Fprintf(w, "Usage: conctl %s <command> [flags]\n\n", r.Name)
	fmt.Fprintf(w, "%s\n\n", r.Desc)
	fmt.Fprintf(w, "Commands:\n")
	maxLen := 0
	for _, c := range r.Commands {
		if len(c.Name) > maxLen {
			maxLen = len(c.Name)
		}
	}
	for _, c := range r.Commands {
		fmt.Fprintf(w, "  %-*s  %s\n", maxLen, c.Name, c.Desc)
	}
	fmt.Fprintln(w)
}
