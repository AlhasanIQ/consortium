package conctl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

func TestLoadSeedWorkflowsFromDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := loadSeedWorkflowsFromDir(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestWorkflowsSeedsMatrixRun_GeneratesHTML(t *testing.T) {
	dir := t.TempDir()

	reasoningSeed := map[string]interface{}{
		"id":   "reasoning-demo",
		"name": "Reasoning Demo",
		"metadata": map[string]interface{}{
			"tags": []string{"reasoning"},
		},
		"nodes": []map[string]interface{}{
			{
				"id":   "agent-a",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent A",
					"config": map[string]interface{}{
						"name":           "Agent A",
						"model":          "google/gemini-3-flash-preview",
						"maxTokens":      -1,
						"timeoutSeconds": 120,
						"openRouterReasoning": map[string]interface{}{
							"effort": "high",
						},
					},
				},
			},
			{
				"id":   "aggregation-demo",
				"type": "aggregation",
				"data": map[string]interface{}{
					"type":  "aggregation",
					"label": "Majority Aggregation",
					"config": map[string]interface{}{
						"aggregationMethod":     "majority_vote",
						"aggregationWorkflowId": "aggregation-majority-vote",
						"timeoutSeconds":        120,
					},
				},
			},
			{
				"id":   "result-demo",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Result",
					"config": map[string]interface{}{
						"name":         "final_answer",
						"outputFormat": "text",
					},
				},
			},
		},
	}

	aggregationSeed := map[string]interface{}{
		"id":   "aggregation-majority-vote",
		"name": "Aggregation Majority Vote",
		"nodes": []map[string]interface{}{
			{
				"id":   "count-votes",
				"type": "operation",
				"data": map[string]interface{}{
					"type": "operation",
					"config": map[string]interface{}{
						"operationType": "count_votes",
					},
				},
			},
		},
	}

	compositeSeed := map[string]interface{}{
		"id":   "composite-demo",
		"name": "Composite Demo",
		"nodes": []map[string]interface{}{
			{
				"id":   "phase-a",
				"type": "workflow_ref",
				"data": map[string]interface{}{
					"type": "workflow_ref",
					"config": map[string]interface{}{
						"workflowId": "reasoning-demo",
					},
				},
			},
			{
				"id":   "phase-b",
				"type": "workflow_ref",
				"data": map[string]interface{}{
					"type": "workflow_ref",
					"config": map[string]interface{}{
						"workflowId": "reasoning-demo-2",
					},
				},
			},
		},
	}

	benchmarkSeed := map[string]interface{}{
		"id":   "benchmark-demo",
		"name": "Benchmark Demo",
		"metadata": map[string]interface{}{
			"tags": []string{"benchmark", "wrapper"},
		},
		"nodes": []map[string]interface{}{
			{
				"id":   "child-demo",
				"type": "child_workflow",
				"data": map[string]interface{}{
					"type":  "child_workflow",
					"label": "Child",
					"config": map[string]interface{}{
						"childWorkflowId": "reasoning-demo",
						"maxTokens":       -1,
						"timeoutSeconds":  900,
					},
				},
			},
			{
				"id":   "contract",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Contract",
					"config": map[string]interface{}{
						"name":           "Contract",
						"model":          "x-ai/grok-4.1-fast",
						"maxTokens":      1024,
						"timeoutSeconds": 20,
						"openRouterReasoning": map[string]interface{}{
							"effort": "none",
						},
					},
				},
			},
		},
	}

	benchmarkDirectL0Seed := map[string]interface{}{
		"id":   "benchmark-direct-l0-demo",
		"name": "Benchmark Direct L0 Demo",
		"nodes": []map[string]interface{}{
			{
				"id":   "child-l0",
				"type": "child_workflow",
				"data": map[string]interface{}{
					"type":  "child_workflow",
					"label": "Child L0",
					"config": map[string]interface{}{
						"childWorkflowId": "aggregation-majority-vote",
						"maxTokens":       -1,
						"timeoutSeconds":  900,
					},
				},
			},
		},
	}

	baselineSeed := map[string]interface{}{
		"id":   "benchmark-baseline-demo",
		"name": "Benchmark Baseline Demo",
		"nodes": []map[string]interface{}{
			{
				"id":   "agent-solver",
				"type": "agent",
				"data": map[string]interface{}{
					"type": "agent",
					"config": map[string]interface{}{
						"name":      "Solver",
						"model":     "solver-model",
						"maxTokens": -1,
					},
				},
			},
			{
				"id":   "agent-contract",
				"type": "contract_extract",
				"data": map[string]interface{}{
					"type": "contract_extract",
					"config": map[string]interface{}{
						"name":      "Contract",
						"model":     "contract-model",
						"maxTokens": 1024,
					},
				},
			},
		},
	}

	mustWriteJSON(t, filepath.Join(dir, "aggregation-majority-vote.json"), aggregationSeed)
	mustWriteJSON(t, filepath.Join(dir, "composite-demo.json"), compositeSeed)
	mustWriteJSON(t, filepath.Join(dir, "reasoning-demo.json"), reasoningSeed)
	mustWriteJSON(t, filepath.Join(dir, "reasoning-demo-2.json"), reasoningSeed)
	mustWriteJSON(t, filepath.Join(dir, "benchmark-demo.json"), benchmarkSeed)
	mustWriteJSON(t, filepath.Join(dir, "benchmark-direct-l0-demo.json"), benchmarkDirectL0Seed)
	mustWriteJSON(t, filepath.Join(dir, "benchmark-baseline-demo.json"), baselineSeed)

	output := filepath.Join(dir, "seeds-matrix.html")
	exitCode := workflowsSeedsMatrixRun(app.GlobalFlags{}, []string{"--seeds", dir, "--output", output})
	if exitCode != app.ExitSuccess {
		t.Fatalf("workflowsSeedsMatrixRun exit code = %d, want %d", exitCode, app.ExitSuccess)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	html := string(data)
	if !strings.Contains(html, "Seed Workflow Matrix") {
		t.Fatal("expected title in generated HTML")
	}
	if !strings.Contains(html, "benchmark-demo") {
		t.Fatal("expected benchmark seed in generated HTML")
	}
	if !strings.Contains(html, "reasoning-demo") {
		t.Fatal("expected reasoning seed in generated HTML")
	}
	if !strings.Contains(html, "majority_vote") {
		t.Fatal("expected macro aggregation method in generated HTML")
	}
	if !strings.Contains(html, "Aggregation Sources") || !strings.Contains(html, "aggregation-majority-vote") {
		t.Fatal("expected L0 aggregation source section in generated HTML")
	}
	if !strings.Contains(html, "Composite Workflows") || !strings.Contains(html, "composite-demo") || !strings.Contains(html, "reasoning-demo-2") {
		t.Fatal("expected L2 composite workflow refs in generated HTML")
	}
	if !strings.Contains(html, "aggregation-majority-vote") {
		t.Fatal("expected L1 aggregationWorkflowId source in generated HTML")
	}
	if !strings.Contains(html, "Direct-Model Baselines") || !strings.Contains(html, "benchmark-baseline-demo") || !strings.Contains(html, "solver-model") {
		t.Fatal("expected direct-model baseline section and solver model in generated HTML")
	}
	if !strings.Contains(html, "contract-model") {
		t.Fatal("expected contract_extract model in generated HTML")
	}
	if !strings.Contains(html, "benchmark-direct-l0-demo") || !strings.Contains(html, "WARNING: benchmark wrapper references L0 directly") {
		t.Fatal("expected direct L3 to L0 warning in generated HTML")
	}
}

func mustWriteJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal JSON %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write JSON %s: %v", path, err)
	}
}
