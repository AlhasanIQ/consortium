package benchloop

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Config holds all benchloop configuration derived from CLI flags.
type Config struct {
	// Target workflows that may be mutated.
	Workflows []string

	// Benchmark config.
	Benchmark   string
	RunSet      string
	Split       string // required for new runs (resume uses existing matrix lock)
	ItemLimit   int    // required for new runs (0 = full split)
	Concurrency int    // required for new runs
	BestRunID   string // optional baseline run ID

	// ExplicitFlags tracks which CLI flags were explicitly provided by the user.
	// Used to distinguish "user wants X" from "default value".
	ExplicitFlags map[string]bool

	// Loop config.
	MaxIterations    int
	StopAfterPlateau int
	IterationTimeout time.Duration

	// Budget.
	TotalBudgetUSD float64
	AgentBudgetUSD float64

	// Agent config.
	ClaudeBin         string
	Model             string
	AgentOutputFormat string // json|stream-json
	AllowModelSwaps   bool

	// Control.
	Resume  bool
	DryRun  bool
	Verbose bool

	// Derived at startup.
	Workdir string
}

// Validate checks that required fields are present and values are sane.
func (c *Config) Validate() error {
	if !c.Resume && len(c.Workflows) == 0 {
		return fmt.Errorf("--workflows is required for fresh runs")
	}
	if c.Resume && c.IsExplicit("workflows") && len(c.Workflows) == 0 {
		return fmt.Errorf("--workflows cannot be empty when provided")
	}
	for _, wf := range c.Workflows {
		if strings.TrimSpace(wf) == "" {
			return fmt.Errorf("workflow ID cannot be empty")
		}
	}

	if c.Benchmark == "" {
		return fmt.Errorf("--benchmark cannot be empty")
	}
	if c.RunSet != "lite" && c.RunSet != "custom" {
		return fmt.Errorf("--run-set must be 'lite' or 'custom', got %q", c.RunSet)
	}
	if c.RunSet == "custom" && strings.TrimSpace(c.Split) == "" {
		return fmt.Errorf("--run-set custom requires --split")
	}
	if c.RunSet == "lite" && c.IsExplicit("split") {
		normalized := strings.ToLower(strings.TrimSpace(c.Split))
		if normalized != "dev" && normalized != "validation" {
			return fmt.Errorf("--run-set lite maps to dev/validation only; for split=%q use --run-set custom", c.Split)
		}
	}
	if !c.Resume {
		if !c.IsExplicit("split") || !c.IsExplicit("item-limit") || !c.IsExplicit("concurrency") {
			return fmt.Errorf("new runs require --split, --item-limit, and --concurrency (matrix is operator-defined)")
		}
	}
	// On resume, split/item_limit/concurrency come from matrix.lock.json, so only validate
	// these value ranges when explicitly supplied.
	// item-limit 0 means "all items" (no limit), so allow >= 0.
	if c.IsExplicit("item-limit") && c.ItemLimit < 0 {
		return fmt.Errorf("--item-limit must be >= 0 (0 = all items)")
	}
	if c.IsExplicit("concurrency") && c.Concurrency < 1 {
		return fmt.Errorf("--concurrency must be >= 1")
	}
	if c.IsExplicit("split") && c.Split == "" {
		return fmt.Errorf("--split value cannot be empty when provided")
	}

	if c.MaxIterations < 1 {
		return fmt.Errorf("--max-iterations must be >= 1")
	}
	if c.StopAfterPlateau < 1 {
		return fmt.Errorf("--stop-after-plateau must be >= 1")
	}
	if c.IterationTimeout < time.Minute {
		return fmt.Errorf("--iteration-timeout must be >= 1m")
	}

	if c.TotalBudgetUSD < 0 {
		return fmt.Errorf("--total-budget-usd must be >= 0 (0 = unlimited)")
	}
	if c.AgentBudgetUSD < 0 {
		return fmt.Errorf("--agent-budget-usd must be >= 0 (0 = unlimited)")
	}
	if c.AgentBudgetUSD > 0 && c.TotalBudgetUSD > 0 && c.AgentBudgetUSD > c.TotalBudgetUSD {
		return fmt.Errorf("--agent-budget-usd (%.2f) cannot exceed --total-budget-usd (%.2f)", c.AgentBudgetUSD, c.TotalBudgetUSD)
	}

	c.AgentOutputFormat = strings.ToLower(strings.TrimSpace(c.AgentOutputFormat))
	if c.AgentOutputFormat == "" {
		c.AgentOutputFormat = "json"
	}
	if c.AgentOutputFormat != "json" && c.AgentOutputFormat != "stream-json" {
		return fmt.Errorf("--agent-output-format must be json|stream-json, got %q", c.AgentOutputFormat)
	}

	// Verify claude binary exists.
	if _, err := exec.LookPath(c.ClaudeBin); err != nil {
		return fmt.Errorf("claude binary %q not found in PATH: %w", c.ClaudeBin, err)
	}

	// Verify workdir.
	if c.Workdir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}
		c.Workdir = wd
	}
	if info, err := os.Stat(c.Workdir); err != nil || !info.IsDir() {
		return fmt.Errorf("workdir %q is not a valid directory", c.Workdir)
	}

	return nil
}

// IsExplicit returns true if the named flag was explicitly provided by the user.
func (c *Config) IsExplicit(name string) bool {
	if c.ExplicitFlags == nil {
		return false
	}
	return c.ExplicitFlags[name]
}

// InferSplit returns the split name based on run-set if not explicitly provided.
func (c *Config) InferSplit() string {
	if c.Split != "" {
		return c.Split
	}
	return "dev"
}

// EffectiveAgentOutputFormat returns the output format passed to claude -p.
func (c *Config) EffectiveAgentOutputFormat() string {
	if c == nil {
		return "json"
	}
	format := strings.ToLower(strings.TrimSpace(c.AgentOutputFormat))
	if format == "" {
		return "json"
	}
	if format == "stream-json" {
		return "stream-json"
	}
	return "json"
}
