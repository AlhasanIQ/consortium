package conctl

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	seedpkg "github.com/alhasaniq/consortium/pkg/seeds"
)

const defaultSeedsDir = "pkg/seeds/data"
const defaultSeedsMatrixOutput = ".tmp/seeds-matrix.html"

// workflowsSeedsMatrixCmd returns the command definition.
func workflowsSeedsMatrixCmd() *app.Command {
	return &app.Command{
		Name:      "seeds-matrix",
		Desc:      "Generate HTML matrix visualization of seed workflow configurations.\n\nReads seed JSON files from disk. Does not require a running server.",
		UsageLine: "conctl workflows seeds-matrix [--seeds <dir>] [--output <file>]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("workflows seeds-matrix", flag.ContinueOnError)
			fs.String("seeds", defaultSeedsDir, "Directory containing seed JSON files")
			fs.String("output", defaultSeedsMatrixOutput, "Output HTML file path")
			return fs
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			return workflowsSeedsMatrixRun(gf, args)
		},
	}
}

// ---------- data types ----------

type seedWorkflowRaw struct {
	ID       string
	Name     string
	Metadata map[string]interface{}
	Nodes    []map[string]interface{}
}

type seedAgentInfo struct {
	Model           string
	ReasoningEffort string
	MaxTokens       string
	TimeoutSeconds  string
}

type seedResultInfo struct {
	AggregationMethod        string
	AggregationWorkflowID    string
	Model                    string
	ReasoningEffort          string
	MaxTokens                string
	TimeoutSeconds           string
	BenchmarkOutputPackaging bool
}

type seedChildInfo struct {
	ChildWorkflowID string
	MaxTokens       string
	TimeoutSeconds  string
}

type seedContractInfo struct {
	Model           string
	ReasoningEffort string
	MaxTokens       string
	TimeoutSeconds  string
}

type seedWorkflowRefInfo struct {
	NodeID     string
	WorkflowID string
	OutputKey  string
}

// ---------- main run ----------

func workflowsSeedsMatrixRun(_ app.GlobalFlags, args []string) int {
	fs := flag.NewFlagSet("workflows seeds-matrix", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	seedsDir := fs.String("seeds", defaultSeedsDir, "Directory containing seed JSON files")
	output := fs.String("output", defaultSeedsMatrixOutput, "Output HTML file path")
	if err := fs.Parse(args); err != nil {
		return app.ExitUsage
	}

	seeds, err := loadSeedWorkflowsFromDir(*seedsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}

	byID := make(map[string]seedWorkflowRaw, len(seeds))
	var aggregationIDs, compositeIDs, benchmarkWrapperIDs, baselineIDs, reasoningIDs []string
	for _, s := range seeds {
		byID[s.ID] = s
		switch seedpkg.Layer(s.ID) {
		case seedpkg.LayerL0:
			aggregationIDs = append(aggregationIDs, s.ID)
		case seedpkg.LayerL2:
			compositeIDs = append(compositeIDs, s.ID)
		case seedpkg.LayerL3:
			if extractSeedChild(s.Nodes).ChildWorkflowID == "" {
				baselineIDs = append(baselineIDs, s.ID)
			} else {
				benchmarkWrapperIDs = append(benchmarkWrapperIDs, s.ID)
			}
		case seedpkg.LayerL1:
			reasoningIDs = append(reasoningIDs, s.ID)
		}
	}
	sort.Strings(aggregationIDs)
	sort.Strings(compositeIDs)
	sort.Strings(benchmarkWrapperIDs)
	sort.Strings(baselineIDs)
	sort.Strings(reasoningIDs)

	htmlContent := renderSeedsMatrixHTML(byID, aggregationIDs, compositeIDs, benchmarkWrapperIDs, baselineIDs, reasoningIDs, *seedsDir)

	dir := filepath.Dir(*output)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: create directory %s: %v\n", dir, err)
			return app.ExitInternal
		}
	}
	if err := os.WriteFile(*output, []byte(htmlContent), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error: write output: %v\n", err)
		return app.ExitInternal
	}

	abs, _ := filepath.Abs(*output)
	fmt.Fprintf(os.Stderr, "Generated: %s\n", abs)
	return app.ExitSuccess
}

// ---------- seed loading ----------

func loadSeedWorkflowsFromDir(dir string) ([]seedWorkflowRaw, error) {
	pattern := filepath.Join(dir, "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no JSON files found in %s", dir)
	}

	var seeds []seedWorkflowRaw
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		id, _ := raw["id"].(string)
		if id == "" {
			continue
		}
		name, _ := raw["name"].(string)
		metadata, _ := raw["metadata"].(map[string]interface{})

		var nodes []map[string]interface{}
		if rawNodes, ok := raw["nodes"].([]interface{}); ok {
			for _, n := range rawNodes {
				if nm, ok := n.(map[string]interface{}); ok {
					nodes = append(nodes, nm)
				}
			}
		}
		seeds = append(seeds, seedWorkflowRaw{
			ID:       id,
			Name:     name,
			Metadata: metadata,
			Nodes:    nodes,
		})
	}
	return seeds, nil
}

// ---------- node extraction ----------

func extractSeedAgents(nodes []map[string]interface{}) []seedAgentInfo {
	var agents []seedAgentInfo
	for _, n := range nodes {
		nodeType, _ := n["type"].(string)
		if nodeType != "agent" {
			continue
		}
		data, _ := n["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		config, _ := data["config"].(map[string]interface{})
		if config == nil {
			continue
		}
		agents = append(agents, seedAgentInfo{
			Model:           seedStr(config, "model"),
			ReasoningEffort: seedReasoningEffort(config),
			MaxTokens:       seedNumStr(config, "maxTokens"),
			TimeoutSeconds:  seedNumStr(config, "timeoutSeconds"),
		})
	}
	return agents
}

func extractSeedResult(nodes []map[string]interface{}) seedResultInfo {
	if result, ok := extractSeedAggregationResult(nodes, "aggregation"); ok {
		return result
	}
	if result, ok := extractSeedAggregationResult(nodes, "result"); ok {
		return result
	}
	return seedResultInfo{}
}

func extractSeedAggregationResult(nodes []map[string]interface{}, wantedType string) (seedResultInfo, bool) {
	for _, n := range nodes {
		if !seedNodeHasType(n, wantedType) {
			continue
		}
		data, _ := n["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		config, _ := data["config"].(map[string]interface{})
		if config == nil {
			continue
		}
		method := seedStr(config, "aggregationMethod")
		if method == "" {
			continue
		}
		aggConfig, _ := config["aggregationConfig"].(map[string]interface{})

		model := ""
		effort := ""
		maxTokens := ""
		if aggConfig != nil {
			// Try all known model field names.
			for _, key := range []string{"model", "judge_model", "scoring_model"} {
				if v := seedStr(aggConfig, key); v != "" {
					model = v
					break
				}
			}
			effort = seedReasoningEffortFrom(aggConfig)
			maxTokens = seedNumStr(aggConfig, "max_tokens")
		}

		return seedResultInfo{
			AggregationMethod:        method,
			AggregationWorkflowID:    seedStr(config, "aggregationWorkflowId"),
			Model:                    model,
			ReasoningEffort:          effort,
			MaxTokens:                maxTokens,
			TimeoutSeconds:           seedNumStr(config, "timeoutSeconds"),
			BenchmarkOutputPackaging: seedBool(config, "benchmarkOutputPackaging"),
		}, true
	}
	return seedResultInfo{}, false
}

func seedNodeHasType(n map[string]interface{}, wantedType string) bool {
	if nodeType, _ := n["type"].(string); nodeType == wantedType {
		return true
	}
	data, _ := n["data"].(map[string]interface{})
	if data == nil {
		return false
	}
	dataType, _ := data["type"].(string)
	return dataType == wantedType
}

func extractSeedChild(nodes []map[string]interface{}) seedChildInfo {
	for _, n := range nodes {
		nodeType, _ := n["type"].(string)
		if nodeType != "child_workflow" {
			continue
		}
		data, _ := n["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		config, _ := data["config"].(map[string]interface{})
		if config == nil {
			continue
		}
		return seedChildInfo{
			ChildWorkflowID: seedStr(config, "childWorkflowId"),
			MaxTokens:       seedNumStr(config, "maxTokens"),
			TimeoutSeconds:  seedNumStr(config, "timeoutSeconds"),
		}
	}
	return seedChildInfo{}
}

func extractSeedContract(nodes []map[string]interface{}) seedContractInfo {
	for _, n := range nodes {
		nodeType, _ := n["type"].(string)
		if nodeType != "contract_extract" {
			continue
		}
		data, _ := n["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		config, _ := data["config"].(map[string]interface{})
		if config == nil {
			continue
		}
		return seedContractInfo{
			Model:           seedStr(config, "model"),
			ReasoningEffort: seedReasoningEffort(config),
			MaxTokens:       seedNumStr(config, "maxTokens"),
			TimeoutSeconds:  seedNumStr(config, "timeoutSeconds"),
		}
	}
	// Legacy fallback for older benchmark parents where the contract was an agent.
	for _, n := range nodes {
		nodeType, _ := n["type"].(string)
		if nodeType != "agent" {
			continue
		}
		data, _ := n["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		config, _ := data["config"].(map[string]interface{})
		if config == nil {
			continue
		}
		return seedContractInfo{
			Model:           seedStr(config, "model"),
			ReasoningEffort: seedReasoningEffort(config),
			MaxTokens:       seedNumStr(config, "maxTokens"),
			TimeoutSeconds:  seedNumStr(config, "timeoutSeconds"),
		}
	}
	return seedContractInfo{}
}

func extractSeedWorkflowRefs(nodes []map[string]interface{}) []seedWorkflowRefInfo {
	var refs []seedWorkflowRefInfo
	for _, n := range nodes {
		if !seedNodeHasType(n, "workflow_ref") {
			continue
		}
		data, _ := n["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		config, _ := data["config"].(map[string]interface{})
		if config == nil {
			continue
		}
		refs = append(refs, seedWorkflowRefInfo{
			NodeID:     seedStr(n, "id"),
			WorkflowID: firstNonEmpty(seedStr(config, "workflowId"), seedStr(config, "workflowRefId")),
			OutputKey:  seedStr(config, "outputKey"),
		})
	}
	return refs
}

func extractSeedOperationTypes(nodes []map[string]interface{}) []string {
	var ops []string
	for _, n := range nodes {
		if !seedNodeHasType(n, "operation") {
			continue
		}
		data, _ := n["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		config, _ := data["config"].(map[string]interface{})
		if config == nil {
			continue
		}
		if op := firstNonEmpty(seedStr(config, "operationType"), seedStr(config, "operation_type")); op != "" {
			ops = append(ops, op)
		}
	}
	sort.Strings(ops)
	return ops
}

// ---------- helpers ----------

func seedStr(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func seedNumStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch n := v.(type) {
	case float64:
		if n == float64(int(n)) {
			return fmt.Sprintf("%d", int(n))
		}
		return fmt.Sprintf("%.2f", n)
	case string:
		return n
	default:
		return fmt.Sprintf("%v", v)
	}
}

func seedBool(m map[string]interface{}, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func seedReasoningEffort(config map[string]interface{}) string {
	return seedReasoningEffortFrom(config)
}

func seedReasoningEffortFrom(config map[string]interface{}) string {
	reasoning, _ := config["openRouterReasoning"].(map[string]interface{})
	if reasoning == nil {
		return ""
	}
	effort, _ := reasoning["effort"].(string)
	return effort
}

func seedTags(metadata map[string]interface{}) []string {
	rawTags, _ := metadata["tags"].([]interface{})
	tags := make([]string, 0, len(rawTags))
	for _, t := range rawTags {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}
	sort.Strings(tags)
	return tags
}

func seedTier(id string) string {
	if strings.HasSuffix(id, "-cheap") {
		return "cheap"
	}
	return "std"
}

func seedBoolLabel(value bool) string {
	if value {
		return "yes"
	}
	return ""
}

func benchmarkLayerWarning(childWorkflowID string) string {
	if seedpkg.Layer(childWorkflowID) == seedpkg.LayerL0 {
		return "WARNING: benchmark wrapper references L0 directly"
	}
	return ""
}

// dedupAgentModels returns unique models preserving first-seen order.
func dedupAgentModels(agents []seedAgentInfo) []string {
	seen := make(map[string]bool)
	var models []string
	for _, a := range agents {
		if a.Model != "" && !seen[a.Model] {
			seen[a.Model] = true
			models = append(models, a.Model)
		}
	}
	return models
}

// dedupAgentEfforts returns unique efforts preserving first-seen order.
func dedupAgentEfforts(agents []seedAgentInfo) []string {
	seen := make(map[string]bool)
	var efforts []string
	for _, a := range agents {
		if a.ReasoningEffort != "" && !seen[a.ReasoningEffort] {
			seen[a.ReasoningEffort] = true
			efforts = append(efforts, a.ReasoningEffort)
		}
	}
	return efforts
}

// representativeAgent returns the maxTokens/timeoutSeconds from the first agent.
func representativeAgent(agents []seedAgentInfo) (maxTokens, timeout string) {
	if len(agents) == 0 {
		return "", ""
	}
	return agents[0].MaxTokens, agents[0].TimeoutSeconds
}

// ---------- HTML rendering ----------

func renderSeedsMatrixHTML(byID map[string]seedWorkflowRaw, aggregationIDs, compositeIDs, benchmarkWrapperIDs, baselineIDs, reasoningIDs []string, seedsDir string) string {
	var b strings.Builder
	now := time.Now().Format(time.RFC3339)

	b.WriteString(`<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Seed Workflow Matrix</title>
  <style>
    :root { --bg:#0b1220; --panel:#121a2b; --panel2:#10182a; --text:#e5e7eb; --muted:#9ca3af; --line:#25314a; --accent:#7dd3fc; --cheap:#f59e0b; --std:#22c55e; }
    * { box-sizing: border-box; }
    body { margin:0; font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial; background: radial-gradient(1000px 500px at 15% -20%, #1e293b 0%, var(--bg) 60%); color: var(--text); }
    .wrap { max-width: 1920px; margin: 24px auto 48px; padding: 0 16px; }
    h1 { margin: 0 0 8px; font-size: 28px; }
    p { margin: 0 0 16px; color: var(--muted); }
    .card { background: linear-gradient(180deg, var(--panel), var(--panel2)); border:1px solid var(--line); border-radius:12px; padding:12px; margin-bottom:16px; overflow:hidden; }
    .table-wrap { overflow:auto; border-radius:10px; border:1px solid var(--line); }
    table { border-collapse: collapse; width: 100%; min-width: 1800px; }
    thead th { position: sticky; top: 0; z-index: 2; background:#0f172a; color:var(--accent); font-size:12px; text-transform: uppercase; letter-spacing:0.04em; }
    th, td { border-bottom:1px solid var(--line); border-right:1px solid var(--line); padding:10px; font-size:12px; vertical-align:top; white-space:normal; word-break:break-word; }
    tr:nth-child(even) td { background: rgba(255,255,255,0.01); }
    th:last-child, td:last-child { border-right:none; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
    .badge { display:inline-block; padding:2px 8px; border-radius:999px; font-size:11px; font-weight:700; }
    .badge.std { background:rgba(34,197,94,0.15); color:#86efac; border:1px solid rgba(34,197,94,0.35); }
    .badge.cheap { background:rgba(245,158,11,0.15); color:#fcd34d; border:1px solid rgba(245,158,11,0.35); }
    .section-title { margin:4px 0 10px; font-size:18px; }
    .model-chip { display:inline-flex; align-items:center; padding:2px 8px; margin:1px 4px 1px 0; border-radius:999px; border:1px solid transparent; font-size:11px; line-height:1.35; white-space:nowrap; }
    .chip-gemini{background:rgba(59,130,246,.2);color:#bfdbfe;border-color:rgba(59,130,246,.4)}
    .chip-kimi{background:rgba(244,114,182,.2);color:#fbcfe8;border-color:rgba(244,114,182,.4)}
    .chip-mimo{background:rgba(20,184,166,.2);color:#99f6e4;border-color:rgba(20,184,166,.4)}
    .chip-grok{background:rgba(239,68,68,.2);color:#fecaca;border-color:rgba(239,68,68,.4)}
    .chip-minimax{background:rgba(168,85,247,.2);color:#e9d5ff;border-color:rgba(168,85,247,.4)}
    .chip-glm{background:rgba(245,158,11,.2);color:#fde68a;border-color:rgba(245,158,11,.4)}
    .chip-default{background:rgba(100,116,139,.25);color:#cbd5e1;border-color:rgba(100,116,139,.45)}
    .effort-cell { font-weight:700; }
    .effort-none{background:rgba(100,116,139,.25)!important;color:#cbd5e1}
    .effort-high{background:rgba(34,197,94,.22)!important;color:#bbf7d0}
    .effort-medium{background:rgba(245,158,11,.22)!important;color:#fde68a}
    .effort-low{background:rgba(56,189,248,.2)!important;color:#bae6fd}
    .effort-mixed{background:rgba(59,130,246,.2)!important;color:#bfdbfe}
  </style>
</head>
<body>
  <div class="wrap">
    <h1>Seed Workflow Matrix</h1>
    <p>Generated `)
	b.WriteString(html.EscapeString(now))
	b.WriteString(` from <span class="mono">`)
	b.WriteString(html.EscapeString(seedsDir))
	b.WriteString(`</span>. Numeric token values are shown with <span class="mono">t</span>; timeouts with <span class="mono">s</span>.</p>
`)

	// Table 0: L0 Aggregation Sources
	b.WriteString(`    <div class="card">
      <h2 class="section-title">Aggregation Sources (L0)</h2>
      <div class="table-wrap"><table><thead><tr>`)
	for _, h := range []string{"Workflow", "Name", "Operation types"} {
		b.WriteString("<th>")
		b.WriteString(html.EscapeString(h))
		b.WriteString("</th>")
	}
	b.WriteString("</tr></thead><tbody>\n")
	for _, id := range aggregationIDs {
		seed := byID[id]
		b.WriteString("<tr>")
		writeCell(&b, id, true)
		writeCell(&b, seed.Name, true)
		writeCell(&b, strings.Join(extractSeedOperationTypes(seed.Nodes), ", "), true)
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table></div></div>\n")

	// Table 0b: L2 Composite Workflows
	b.WriteString(`    <div class="card">
      <h2 class="section-title">Composite Workflows (L2)</h2>
      <div class="table-wrap"><table><thead><tr>`)
	for _, h := range []string{"Workflow", "Name", "Workflow refs", "Output keys"} {
		b.WriteString("<th>")
		b.WriteString(html.EscapeString(h))
		b.WriteString("</th>")
	}
	b.WriteString("</tr></thead><tbody>\n")
	for _, id := range compositeIDs {
		seed := byID[id]
		refs := extractSeedWorkflowRefs(seed.Nodes)
		refIDs := make([]string, 0, len(refs))
		outputKeys := make([]string, 0, len(refs))
		for _, ref := range refs {
			refIDs = append(refIDs, ref.WorkflowID)
			if ref.OutputKey != "" {
				outputKeys = append(outputKeys, ref.NodeID+"="+ref.OutputKey)
			}
		}
		b.WriteString("<tr>")
		writeCell(&b, id, true)
		writeCell(&b, seed.Name, true)
		writeCell(&b, strings.Join(refIDs, ", "), true)
		writeCell(&b, strings.Join(outputKeys, ", "), true)
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table></div></div>\n")

	// Table 1: Benchmark Wrappers
	b.WriteString(`    <div class="card">
      <h2 class="section-title">Benchmark Wrappers: Parent vs Child</h2>
      <div class="table-wrap"><table><thead><tr>`)
	benchHeaders := []string{
		"Parent workflow", "Tier", "Parent tags",
		"Parent contract model", "Parent contract reasoning effort", "Parent contract maxTokens", "Parent contract timeoutSeconds",
		"Parent childWorkflowId", "Parent child maxTokens", "Parent child timeoutSeconds",
		"Child agent models", "Child agent reasoning efforts", "Child agent maxTokens", "Child agent timeoutSeconds",
		"Child aggregator method", "Child L0 source workflow", "Child aggregator models", "Child aggregator reasoning efforts", "Child aggregator max_tokens", "Child result timeoutSeconds",
		"Parent output packaging", "Layer warning",
	}
	for _, h := range benchHeaders {
		b.WriteString("<th>")
		b.WriteString(html.EscapeString(h))
		b.WriteString("</th>")
	}
	b.WriteString("</tr></thead><tbody>\n")

	for _, id := range benchmarkWrapperIDs {
		seed := byID[id]
		tier := seedTier(id)
		tags := seedTags(seed.Metadata)
		contract := extractSeedContract(seed.Nodes)
		child := extractSeedChild(seed.Nodes)
		parentResult := extractSeedResult(seed.Nodes)

		// Resolve child workflow.
		var childAgentModels, childAgentEfforts, childAgentMaxTokens, childAgentTimeout string
		var childResult seedResultInfo
		if childSeed, ok := byID[child.ChildWorkflowID]; ok {
			childAgents := extractSeedAgents(childSeed.Nodes)
			childAgentModels = strings.Join(dedupAgentModels(childAgents), ", ")
			childAgentEfforts = strings.Join(dedupAgentEfforts(childAgents), ", ")
			mt, to := representativeAgent(childAgents)
			childAgentMaxTokens = mt
			childAgentTimeout = to
			childResult = extractSeedResult(childSeed.Nodes)
		}

		b.WriteString("<tr>")
		writeCell(&b, id, true)
		writeTierCell(&b, tier)
		writeCell(&b, strings.Join(tags, ", "), true)
		writeCell(&b, contract.Model, true)
		writeCell(&b, contract.ReasoningEffort, true)
		writeCell(&b, contract.MaxTokens, true)
		writeCell(&b, contract.TimeoutSeconds, true)
		writeCell(&b, child.ChildWorkflowID, true)
		writeCell(&b, child.MaxTokens, true)
		writeCell(&b, child.TimeoutSeconds, true)
		writeCell(&b, childAgentModels, true)
		writeCell(&b, childAgentEfforts, true)
		writeCell(&b, childAgentMaxTokens, true)
		writeCell(&b, childAgentTimeout, true)
		writeCell(&b, childResult.AggregationMethod, true)
		writeCell(&b, childResult.AggregationWorkflowID, true)
		writeCell(&b, childResult.Model, true)
		writeCell(&b, childResult.ReasoningEffort, true)
		writeCell(&b, childResult.MaxTokens, true)
		writeCell(&b, childResult.TimeoutSeconds, true)
		writeCell(&b, seedBoolLabel(parentResult.BenchmarkOutputPackaging), true)
		writeCell(&b, benchmarkLayerWarning(child.ChildWorkflowID), true)
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table></div></div>\n")

	// Table 1b: Direct-model Benchmark Baselines
	b.WriteString(`    <div class="card">
      <h2 class="section-title">Direct-Model Baselines</h2>
      <div class="table-wrap"><table><thead><tr>`)
	for _, h := range []string{"Parent workflow", "Tier", "Solver model", "Solver reasoning effort", "Solver maxTokens", "Contract model", "Output packaging", "Aggregator method"} {
		b.WriteString("<th>")
		b.WriteString(html.EscapeString(h))
		b.WriteString("</th>")
	}
	b.WriteString("</tr></thead><tbody>\n")
	for _, id := range baselineIDs {
		seed := byID[id]
		agents := extractSeedAgents(seed.Nodes)
		solver := seedAgentInfo{}
		if len(agents) > 0 {
			solver = agents[0]
		}
		contract := extractSeedContract(seed.Nodes)
		result := extractSeedResult(seed.Nodes)
		b.WriteString("<tr>")
		writeCell(&b, id, true)
		writeTierCell(&b, seedTier(id))
		writeCell(&b, solver.Model, true)
		writeCell(&b, solver.ReasoningEffort, true)
		writeCell(&b, solver.MaxTokens, true)
		writeCell(&b, contract.Model, true)
		writeCell(&b, seedBoolLabel(result.BenchmarkOutputPackaging), true)
		writeCell(&b, result.AggregationMethod, true)
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table></div></div>\n")

	// Table 2: Reasoning Seeds
	b.WriteString(`    <div class="card">
      <h2 class="section-title">Reasoning Seeds: Agents vs Aggregator</h2>
      <div class="table-wrap"><table><thead><tr>`)
	reasoningHeaders := []string{
		"Workflow", "Tier", "Tags",
		"Agent models", "Agent reasoning efforts", "Agent maxTokens", "Agent timeoutSeconds",
		"Aggregator method", "L0 source workflow", "Aggregator models", "Aggregator reasoning efforts", "Aggregator max_tokens", "Result timeoutSeconds",
	}
	for _, h := range reasoningHeaders {
		b.WriteString("<th>")
		b.WriteString(html.EscapeString(h))
		b.WriteString("</th>")
	}
	b.WriteString("</tr></thead><tbody>\n")

	for _, id := range reasoningIDs {
		seed := byID[id]
		tier := seedTier(id)
		tags := seedTags(seed.Metadata)
		agents := extractSeedAgents(seed.Nodes)
		result := extractSeedResult(seed.Nodes)
		mt, to := representativeAgent(agents)

		b.WriteString("<tr>")
		writeCell(&b, id, true)
		writeTierCell(&b, tier)
		writeCell(&b, strings.Join(tags, ", "), true)
		writeCell(&b, strings.Join(dedupAgentModels(agents), ", "), true)
		writeCell(&b, strings.Join(dedupAgentEfforts(agents), ", "), true)
		writeCell(&b, mt, true)
		writeCell(&b, to, true)
		writeCell(&b, result.AggregationMethod, true)
		writeCell(&b, result.AggregationWorkflowID, true)
		writeCell(&b, result.Model, true)
		writeCell(&b, result.ReasoningEffort, true)
		writeCell(&b, result.MaxTokens, true)
		writeCell(&b, result.TimeoutSeconds, true)
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table></div></div>\n")

	// Close and add JS.
	b.WriteString(`  </div>
  <script>
    (() => {
      const tables = document.querySelectorAll('.card table');
      const modelClass = (model) => {
        const m = model.toLowerCase();
        if (m.includes('gemini')) return 'chip-gemini';
        if (m.includes('kimi')) return 'chip-kimi';
        if (m.includes('mimo')) return 'chip-mimo';
        if (m.includes('grok')) return 'chip-grok';
        if (m.includes('minimax')) return 'chip-minimax';
        if (m.includes('glm')) return 'chip-glm';
        return 'chip-default';
      };
      const colorizeModelCell = (cell) => {
        const raw = cell.textContent.trim(); if (!raw) return;
        const items = raw.split(',').map(s => s.trim()).filter(Boolean); if (items.length === 0) return;
        cell.innerHTML = '';
        items.forEach((item) => {
          const span = document.createElement('span');
          span.className = 'model-chip ' + modelClass(item) + ' mono';
          span.textContent = item;
          cell.appendChild(span);
        });
      };
      const suffixNumericCell = (cell, suffix) => {
        const raw = cell.textContent.trim(); if (!raw) return;
        cell.textContent = raw.replace(/-?\d+(?:\.\d+)?(?![a-zA-Z])/g, (m) => m + suffix);
      };
      const styleEffortCell = (cell) => {
        const raw = (cell.textContent || '').trim().toLowerCase(); if (!raw) return;
        const vals = raw.split(',').map((s) => s.trim()).filter(Boolean); if (vals.length === 0) return;
        const uniq = Array.from(new Set(vals));
        cell.classList.add('effort-cell');
        if (uniq.length > 1) { cell.classList.add('effort-mixed'); return; }
        if (uniq[0] === 'high') { cell.classList.add('effort-high'); return; }
        if (uniq[0] === 'medium') { cell.classList.add('effort-medium'); return; }
        if (uniq[0] === 'low') { cell.classList.add('effort-low'); return; }
        if (uniq[0] === 'none') { cell.classList.add('effort-none'); return; }
        cell.classList.add('effort-mixed');
      };
      tables.forEach((table) => {
        const headers = table?.tHead?.rows?.[0]?.cells; if (!headers || !table.tBodies[0]) return;
        const modelCols = []; const effortCols = []; const tokenCols = []; const timeoutCols = [];
        Array.from(headers).forEach((th, idx) => {
          const label = (th.textContent || '').toLowerCase(); const col = idx + 1;
          if (label.includes('models') || label.includes('model')) modelCols.push(col);
          if (label.includes('reasoning effort')) effortCols.push(col);
          if (label.includes('maxtokens') || label.includes('max_tokens')) tokenCols.push(col);
          if (label.includes('timeoutseconds')) timeoutCols.push(col);
        });
        Array.from(table.tBodies[0].rows).forEach((row) => {
          modelCols.forEach((c) => row.cells[c-1] && colorizeModelCell(row.cells[c-1]));
          effortCols.forEach((c) => row.cells[c-1] && styleEffortCell(row.cells[c-1]));
          tokenCols.forEach((c) => row.cells[c-1] && suffixNumericCell(row.cells[c-1], 't'));
          timeoutCols.forEach((c) => row.cells[c-1] && suffixNumericCell(row.cells[c-1], 's'));
        });
      });
    })();
  </script>
</body></html>
`)
	return b.String()
}

func writeCell(b *strings.Builder, value string, mono bool) {
	if mono {
		b.WriteString(`<td class="mono">`)
	} else {
		b.WriteString("<td>")
	}
	b.WriteString(html.EscapeString(value))
	b.WriteString("</td>")
}

func writeTierCell(b *strings.Builder, tier string) {
	b.WriteString("<td>")
	if tier == "cheap" {
		b.WriteString(`<span class="badge cheap">cheap</span>`)
	} else {
		b.WriteString(`<span class="badge std">std</span>`)
	}
	b.WriteString("</td>")
}
