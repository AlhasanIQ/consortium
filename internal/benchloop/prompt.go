package benchloop

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

//go:embed prompt_templates/*.tmpl
var promptTemplatesFS embed.FS

var (
	baseSystemPromptTemplate      = template.Must(template.ParseFS(promptTemplatesFS, "prompt_templates/base_system.tmpl"))
	iterationSystemPromptTemplate = template.Must(template.ParseFS(promptTemplatesFS, "prompt_templates/iteration_system_addendum.tmpl"))
)

type systemPromptTemplateData struct {
	AllowModelSwaps bool
	Workdir         string
	SeedIdentities  []SeedIdentity
}

// SeedIdentity captures the frozen identity properties of a reasoning seed workflow.
type SeedIdentity struct {
	ID                string
	AggregationMethod string
	Pattern           string // "single-model (Nx)" or "multi-model (N)"
	AgentCount        int
}

// scanSeedIdentities reads reasoning-*-cheap.json seeds from the seeds directory
// and extracts each workflow's identity-defining properties (aggregation method,
// structural pattern). Returns nil on any error (graceful degradation).
func scanSeedIdentities(workdir string) []SeedIdentity {
	seedDir := filepath.Join(workdir, "pkg", "storage", "seeds")
	matches, err := filepath.Glob(filepath.Join(seedDir, "reasoning-*-cheap.json"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)

	var identities []SeedIdentity
	for _, path := range matches {
		id, err := parseSeedIdentity(path)
		if err != nil {
			continue
		}
		identities = append(identities, id)
	}
	return identities
}

func parseSeedIdentity(path string) (SeedIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SeedIdentity{}, err
	}

	var seed struct {
		ID    string `json:"id"`
		Nodes []struct {
			Data struct {
				Type   string `json:"type"`
				Config struct {
					Model             string `json:"model"`
					AggregationMethod string `json:"aggregationMethod"`
				} `json:"config"`
			} `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &seed); err != nil {
		return SeedIdentity{}, err
	}

	var aggMethod string
	var models []string
	agentCount := 0
	for _, n := range seed.Nodes {
		if n.Data.Type == "agent" {
			agentCount++
			models = append(models, n.Data.Config.Model)
		}
		if n.Data.Config.AggregationMethod != "" {
			aggMethod = n.Data.Config.AggregationMethod
		}
	}

	// Determine pattern: single-model vs multi-model
	uniqueModels := make(map[string]bool)
	for _, m := range models {
		uniqueModels[m] = true
	}
	pattern := fmt.Sprintf("multi-model (%d)", agentCount)
	if len(uniqueModels) == 1 {
		pattern = fmt.Sprintf("single-model (%dx)", agentCount)
	}

	return SeedIdentity{
		ID:                seed.ID,
		AggregationMethod: aggMethod,
		Pattern:           pattern,
		AgentCount:        agentCount,
	}, nil
}

func renderPromptTemplate(tmpl *template.Template, data interface{}) string {
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		panic(fmt.Sprintf("render prompt template: %v", err))
	}
	return b.String()
}

// BuildBaseSystemPrompt returns the shared system prompt used by iteration agent sessions.
// Contains environment, conctl reference, workflow mutation rules, and constraints.
func BuildBaseSystemPrompt(cfg *Config) string {
	data := systemPromptTemplateData{
		AllowModelSwaps: cfg.AllowModelSwaps,
		Workdir:         cfg.Workdir,
		SeedIdentities:  scanSeedIdentities(cfg.Workdir),
	}
	return strings.TrimSpace(renderPromptTemplate(baseSystemPromptTemplate, data))
}

// BuildIterationSystemPrompt returns the system prompt for iteration sessions (Phase 1).
// It includes everything from the base prompt plus iteration-specific guidance:
// investigation methodology, progressive protocol, decision contract, debugging guidance.
func BuildIterationSystemPrompt(cfg *Config) string {
	var b strings.Builder
	b.WriteString(BuildBaseSystemPrompt(cfg))
	b.WriteString("\n\n")
	b.WriteString(strings.TrimSpace(renderPromptTemplate(iterationSystemPromptTemplate, systemPromptTemplateData{
		AllowModelSwaps: cfg.AllowModelSwaps,
		Workdir:         cfg.Workdir,
	})))
	return b.String()
}

// BuildIterationPrompt returns the dynamic per-iteration prompt.
func BuildIterationPrompt(cfg *Config, state *State, lock *MatrixLock, memory string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("## Iteration %d of %d\n\n", state.Iteration+1, state.MaxIterations))

	b.WriteString("### Objective\n")
	b.WriteString(fmt.Sprintf("Improve accuracy of %s on %s (%s split)\n\n",
		strings.Join(lock.WorkflowOrder, ", "), lock.Benchmark, lock.Split))

	b.WriteString("### Target Workflows (you may modify all of these)\n")
	for _, wf := range lock.TargetWorkflows {
		role := "reasoning child"
		for _, benchWf := range lock.WorkflowOrder {
			if benchWf == wf {
				role = "benchmark wrapper"
				break
			}
		}
		b.WriteString(fmt.Sprintf("- %s (%s)\n", wf, role))
	}

	b.WriteString("\n### Current Baseline\n")
	if state.CurrentRunID != "" {
		b.WriteString(fmt.Sprintf("- Accuracy: %.1f%%\n", state.CurrentAccuracy*100))
		b.WriteString(fmt.Sprintf("- Parse rate: %.0f%%\n", state.CurrentParseRate*100))
		b.WriteString(fmt.Sprintf("- Cost/item: $%.4f\n", state.CurrentCostPerItem))
		if state.CurrentAvgLatencyMS > 0 {
			b.WriteString(fmt.Sprintf("- Avg latency: %.0fms (p95: %.0fms)\n", state.CurrentAvgLatencyMS, state.CurrentP95LatencyMS))
		}
		b.WriteString(fmt.Sprintf("- Failed items: %d\n", state.CurrentFailedItems))
		b.WriteString(fmt.Sprintf("- Run ID: %s\n", state.CurrentRunID))
	} else {
		b.WriteString("- No baseline yet (this is the bootstrap iteration)\n")
		b.WriteString("- Run a benchmark and report results. No comparison needed.\n")
	}

	b.WriteString("\n### Budget\n")
	if cfg.AgentBudgetUSD > 0 {
		b.WriteString(fmt.Sprintf("- Agent session budget: $%.2f\n", cfg.AgentBudgetUSD))
	} else {
		b.WriteString("- Agent session budget: unlimited\n")
	}
	if cfg.TotalBudgetUSD > 0 {
		totalSpent := state.TotalAgentCostUSD + state.TotalBenchCostUSD
		remainingBudget := cfg.TotalBudgetUSD - totalSpent
		b.WriteString(fmt.Sprintf("- Total budget remaining: ~$%.2f\n", remainingBudget))
		b.WriteString(fmt.Sprintf("- Spend so far: $%.2f (agent $%.2f + benchmark $%.2f)\n", totalSpent, state.TotalAgentCostUSD, state.TotalBenchCostUSD))
	} else {
		b.WriteString("- Total budget: unlimited\n")
	}
	b.WriteString(fmt.Sprintf("- Iteration: %d/%d\n", state.Iteration+1, state.MaxIterations))
	if state.PlateauCount > 0 {
		b.WriteString(fmt.Sprintf("- Consecutive no-progress: %d/%d\n", state.PlateauCount, cfg.StopAfterPlateau))
	}

	b.WriteString("\n### Frozen Matrix\n")
	matrixJSON, _ := json.MarshalIndent(lock, "", "  ")
	b.WriteString("```json\n")
	b.WriteString(string(matrixJSON))
	b.WriteString("\n```\n")

	b.WriteString("\n### Benchmark Command Template\n")
	if cfg.AllowModelSwaps {
		b.WriteString(fmt.Sprintf("Model hints: `%s`\n", conctlInvocation(cfg.Workdir, "benchmarks models suggest --top 10")))
	} else {
		b.WriteString("Model hints: disabled for this run (`--allow-model-swaps=false`)\n")
	}
	if state.CurrentRunID != "" {
		b.WriteString(fmt.Sprintf("Targeted replay (1 item): `%s`\n", conctlInvocation(cfg.Workdir,
			fmt.Sprintf("benchmarks replay-items --yes --id %s --items <item-id> --changed-workflows <changed-workflow-ids> --mode required --concurrency 1", state.CurrentRunID))))
	}
	splitArg := ""
	if strings.EqualFold(lock.RunSet, "custom") {
		splitArg = fmt.Sprintf(" --split %s", lock.Split)
	}
	b.WriteString(fmt.Sprintf("Sanity: `%s`\n", conctlInvocation(cfg.Workdir,
		fmt.Sprintf("benchmarks run --yes --source benchloop --benchmarks %s --workflows %s --run-set %s%s --limit 5 --concurrency 2",
			lock.Benchmark, strings.Join(lock.WorkflowOrder, ","), lock.RunSet, splitArg))))
	runLabel := "Small set"
	if lock.ItemLimit == 0 {
		runLabel = "Full split"
	}
	b.WriteString(fmt.Sprintf("%s: `%s`\n", runLabel, conctlInvocation(cfg.Workdir,
		fmt.Sprintf("benchmarks run --yes --source benchloop --benchmarks %s --workflows %s --run-set %s%s --limit %d --concurrency %d",
			lock.Benchmark, strings.Join(lock.WorkflowOrder, ","), lock.RunSet, splitArg, lock.ItemLimit, lock.Concurrency))))
	b.WriteString(fmt.Sprintf("Wait for completion: `%s`\n", conctlInvocation(cfg.Workdir, "benchmarks runner-status --wait-until idle --interval 5s")))

	if state.CurrentRunID != "" {
		b.WriteString(fmt.Sprintf("\nCompare with baseline: `%s`\n",
			conctlInvocation(cfg.Workdir, fmt.Sprintf("benchmarks compare-items --base %s --candidate <your-new-run-id>", state.CurrentRunID))))
	}

	if memory != "" {
		b.WriteString("\n### Accumulated Memory\n")
		b.WriteString(memory)
		b.WriteString("\n")
	}

	return b.String()
}

func conctlInvocation(_ string, conctlArgs string) string {
	return fmt.Sprintf(`"${CONCTL_BIN}" %s`, conctlArgs)
}
