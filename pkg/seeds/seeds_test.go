package seeds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type seedShapeNode struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Position struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"position"`
	Data struct {
		Type   string `json:"type"`
		Config struct {
			AggregationMethod        string                 `json:"aggregationMethod"`
			AggregationConfig        map[string]interface{} `json:"aggregationConfig"`
			AggregationWorkflowID    string                 `json:"aggregationWorkflowId"`
			WorkflowID               string                 `json:"workflowId"`
			ChildWorkflowID          string                 `json:"childWorkflowId"`
			BenchmarkOutputPackaging bool                   `json:"benchmarkOutputPackaging"`
		} `json:"config"`
	} `json:"data"`
}

type seedShapeEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type seedShapeWorkflow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Metadata struct {
		Tags          []string          `json:"tags"`
		InputContract map[string]string `json:"input_contract"`
	} `json:"metadata"`
	Nodes []seedShapeNode `json:"nodes"`
	Edges []seedShapeEdge `json:"edges"`
}

var task8L0SeedIDs = []string{
	"aggregation-collect",
	"aggregation-majority-vote",
	"aggregation-synthesis",
	"aggregation-judge",
	"aggregation-debate-decide",
	"aggregation-scoring",
	"aggregation-peer-matrix",
}

var task8ReasoningAggregationRefs = map[string]string{
	"reasoning-adversarial-defense-judge-pick-cheap": "aggregation-judge",
	"reasoning-adversarial-defense-judge-pick":       "aggregation-judge",
	"reasoning-camp-split-judge-pick-cheap":          "aggregation-debate-decide",
	"reasoning-camp-split-judge-pick":                "aggregation-debate-decide",
	"reasoning-informed-captain-synthesis-cheap":     "aggregation-synthesis",
	"reasoning-informed-captain-synthesis":           "aggregation-synthesis",
	"reasoning-judge-pick-cheap":                     "aggregation-judge",
	"reasoning-judge-pick":                           "aggregation-judge",
	"reasoning-judge-score-pick-cheap":               "aggregation-scoring",
	"reasoning-judge-score-pick":                     "aggregation-scoring",
	"reasoning-majority-pick-cheap":                  "aggregation-majority-vote",
	"reasoning-majority-pick":                        "aggregation-majority-vote",
	"reasoning-multi-round-majority-pick-cheap":      "aggregation-majority-vote",
	"reasoning-multi-round-majority-pick":            "aggregation-majority-vote",
	"reasoning-peer-score-pick-cheap":                "aggregation-peer-matrix",
	"reasoning-peer-score-pick":                      "aggregation-peer-matrix",
	"reasoning-self-consistency-majority-pick-cheap": "aggregation-majority-vote",
	"reasoning-self-consistency-majority-pick":       "aggregation-majority-vote",
}

var task8BenchmarkWrappers = map[string]string{
	"benchmark-adversarial-defense-judge-pick-cheap":      "reasoning-adversarial-defense-judge-pick-cheap",
	"benchmark-adversarial-defense-judge-pick":            "reasoning-adversarial-defense-judge-pick",
	"benchmark-camp-split-judge-pick-cheap":               "reasoning-camp-split-judge-pick-cheap",
	"benchmark-camp-split-judge-pick":                     "reasoning-camp-split-judge-pick",
	"benchmark-informed-captain-synthesis-cheap":          "reasoning-informed-captain-synthesis-cheap",
	"benchmark-informed-captain-synthesis":                "reasoning-informed-captain-synthesis",
	"benchmark-judge-pick-cheap":                          "reasoning-judge-pick-cheap",
	"benchmark-judge-pick":                                "reasoning-judge-pick",
	"benchmark-judge-score-pick-cheap":                    "reasoning-judge-score-pick-cheap",
	"benchmark-judge-score-pick":                          "reasoning-judge-score-pick",
	"benchmark-majority-pick-cheap":                       "reasoning-majority-pick-cheap",
	"benchmark-majority-pick":                             "reasoning-majority-pick",
	"benchmark-multi-round-majority-pick-cheap":           "reasoning-multi-round-majority-pick-cheap",
	"benchmark-multi-round-majority-pick":                 "reasoning-multi-round-majority-pick",
	"benchmark-peer-score-pick-cheap":                     "reasoning-peer-score-pick-cheap",
	"benchmark-peer-score-pick":                           "reasoning-peer-score-pick",
	"benchmark-self-consistency-majority-pick-cheap":      "reasoning-self-consistency-majority-pick-cheap",
	"benchmark-self-consistency-majority-pick":            "reasoning-self-consistency-majority-pick",
	"benchmark-math-adversarial-defense-judge-pick-cheap": "reasoning-adversarial-defense-judge-pick-cheap",
	"benchmark-math-camp-split-judge-pick-cheap":          "reasoning-camp-split-judge-pick-cheap",
	"benchmark-math-informed-captain-synthesis-cheap":     "reasoning-informed-captain-synthesis-cheap",
	"benchmark-math-judge-pick-cheap":                     "reasoning-judge-pick-cheap",
	"benchmark-math-judge-score-pick-cheap":               "reasoning-judge-score-pick-cheap",
	"benchmark-math-majority-pick-cheap":                  "reasoning-majority-pick-cheap",
	"benchmark-math-multi-round-majority-pick-cheap":      "reasoning-multi-round-majority-pick-cheap",
	"benchmark-math-peer-score-pick-cheap":                "reasoning-peer-score-pick-cheap",
	"benchmark-math-self-consistency-majority-pick-cheap": "reasoning-self-consistency-majority-pick-cheap",
}

var task8BaselineBenchmarks = []string{
	"benchmark-baseline-deepseek-v4-flash-cheap",
	"benchmark-baseline-mimo-cheap",
	"benchmark-baseline-minimax-cheap",
	"benchmark-math-baseline-deepseek-v4-flash-cheap",
	"benchmark-math-baseline-mimo-cheap",
	"benchmark-math-baseline-minimax-cheap",
}

func TestGetAllJSON_ReturnsNonEmpty(t *testing.T) {
	all := GetAllJSON()
	if len(all) == 0 {
		t.Fatal("expected non-empty seed list")
	}
}

func TestGetAllJSON_ValidJSON(t *testing.T) {
	all := GetAllJSON()
	for i, seedJSON := range all {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(seedJSON), &parsed); err != nil {
			t.Errorf("seed[%d]: invalid JSON: %v", i, err)
		}
	}
}

func TestGetAllJSON_AllHaveID(t *testing.T) {
	all := GetAllJSON()
	for i, seedJSON := range all {
		var data struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(seedJSON), &data); err != nil {
			t.Errorf("seed[%d]: parse error: %v", i, err)
			continue
		}
		if data.ID == "" {
			t.Errorf("seed[%d]: missing ID", i)
		}
	}
}

func TestGetAllJSON_UniqueIDs(t *testing.T) {
	all := GetAllJSON()
	seen := make(map[string]bool)
	for _, seedJSON := range all {
		var data struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(seedJSON), &data); err != nil {
			continue
		}
		if seen[data.ID] {
			t.Errorf("duplicate seed ID: %s", data.ID)
		}
		seen[data.ID] = true
	}
}

func TestGetJSONByID_Found(t *testing.T) {
	// Get any valid ID from the seed list
	all := GetAllJSON()
	if len(all) == 0 {
		t.Skip("no seeds available")
	}
	var firstID string
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(all[0]), &data); err != nil {
		t.Fatalf("failed to parse first seed: %v", err)
	}
	firstID = data.ID

	result, err := GetJSONByID(firstID)
	if err != nil {
		t.Fatalf("GetJSONByID(%q) unexpected error: %v", firstID, err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	// Verify the returned JSON contains the right ID
	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("returned JSON is invalid: %v", err)
	}
	if parsed.ID != firstID {
		t.Errorf("expected ID %q, got %q", firstID, parsed.ID)
	}
}

func TestGetJSONByID_NotFound(t *testing.T) {
	_, err := GetJSONByID("nonexistent-workflow-id")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetAllInfo_ReturnsNonEmpty(t *testing.T) {
	infos := GetAllInfo()
	if len(infos) == 0 {
		t.Fatal("expected non-empty info list")
	}
}

func TestGetAllInfo_MatchesSeedCount(t *testing.T) {
	all := GetAllJSON()
	infos := GetAllInfo()
	if len(infos) != len(all) {
		t.Errorf("info count %d != seed count %d", len(infos), len(all))
	}
}

func TestGetAllInfo_PopulatedFields(t *testing.T) {
	infos := GetAllInfo()
	for _, info := range infos {
		if info.ID == "" {
			t.Error("Info has empty ID")
		}
		if info.Name == "" {
			t.Errorf("Info %q has empty Name", info.ID)
		}
	}
}

func TestGetAllInfo_SeedNamingConventions(t *testing.T) {
	infos := GetAllInfo()
	hasReasoning := false
	hasBenchmark := false
	for _, info := range infos {
		if strings.HasPrefix(info.ID, "reasoning-") {
			hasReasoning = true
		}
		if strings.HasPrefix(info.ID, "benchmark-") {
			hasBenchmark = true
		}
	}
	if !hasReasoning {
		t.Error("expected at least one reasoning-* seed")
	}
	if !hasBenchmark {
		t.Error("expected at least one benchmark-* seed")
	}
}

func TestLayerIncludesAggregationL0(t *testing.T) {
	if got := Layer("aggregation-synthesis"); got != LayerL0 {
		t.Fatalf("Layer(aggregation-synthesis) = %q, want %q", got, LayerL0)
	}
	if got := Layer("reasoning-informed-captain-synthesis"); got != LayerL1 {
		t.Fatalf("reasoning layer = %q, want %q", got, LayerL1)
	}
	if got := Layer("composite-router"); got != LayerL2 {
		t.Fatalf("composite layer = %q, want %q", got, LayerL2)
	}
	if got := Layer("benchmark-informed-captain-synthesis"); got != LayerL3 {
		t.Fatalf("benchmark layer = %q, want %q", got, LayerL3)
	}
}

func TestGetAllInfoIncludesAggregationL0Seeds(t *testing.T) {
	info := GetAllInfo()
	want := map[string]bool{
		"aggregation-collect":       false,
		"aggregation-majority-vote": false,
		"aggregation-synthesis":     false,
		"aggregation-judge":         false,
		"aggregation-debate-decide": false,
		"aggregation-scoring":       false,
		"aggregation-peer-matrix":   false,
	}
	for _, seed := range info {
		if _, ok := want[seed.ID]; ok {
			want[seed.ID] = seed.Layer == LayerL0
		}
	}
	for id, ok := range want {
		if !ok {
			t.Fatalf("missing L0 seed %s", id)
		}
	}
}

func TestAggregationL0SeedsUseInternalNodeShape(t *testing.T) {
	all := GetAllJSON()
	seen := map[string]bool{
		"aggregation-collect":       false,
		"aggregation-majority-vote": false,
		"aggregation-synthesis":     false,
		"aggregation-judge":         false,
		"aggregation-debate-decide": false,
		"aggregation-scoring":       false,
		"aggregation-peer-matrix":   false,
	}
	for i, seedJSON := range all {
		var wf seedShapeWorkflow
		if err := json.Unmarshal([]byte(seedJSON), &wf); err != nil {
			t.Errorf("seed[%d]: parse error: %v", i, err)
			continue
		}
		if Layer(wf.ID) != LayerL0 {
			continue
		}
		if _, ok := seen[wf.ID]; ok {
			seen[wf.ID] = true
		}
		hasOperation := false
		hasAggregationMacro := false
		hasCollectResult := false
		for _, node := range wf.Nodes {
			switch node.Type {
			case "operation":
				hasOperation = true
			case "aggregation":
				hasAggregationMacro = true
			case "result":
				if node.Data.Config.AggregationMethod == "collect" {
					hasCollectResult = true
				}
			}
			switch node.Data.Type {
			case "operation":
				hasOperation = true
			case "aggregation":
				hasAggregationMacro = true
			case "result":
				if node.Data.Config.AggregationMethod == "collect" {
					hasCollectResult = true
				}
			}
		}
		if hasAggregationMacro {
			t.Errorf("%s: L0 seed must not use visible aggregation macro nodes", wf.ID)
		}
		if !hasOperation {
			t.Errorf("%s: L0 seed must include deterministic operation nodes", wf.ID)
		}
		if !hasCollectResult {
			t.Errorf("%s: L0 seed must terminate in a collect result node", wf.ID)
		}
		if strings.Contains(strings.ToLower(seedJSON), "benchmark") {
			t.Errorf("%s: L0 seed must not contain benchmark-specific instructions", wf.ID)
		}
	}
	for id, ok := range seen {
		if !ok {
			t.Fatalf("missing L0 internal-shape seed %s", id)
		}
	}
}

func TestTask8SeedManifestL0SeedsExistAndStayInternal(t *testing.T) {
	byID := seedWorkflowsByID(t)
	for _, id := range task8L0SeedIDs {
		wf, ok := byID[id]
		if !ok {
			t.Fatalf("missing L0 seed %s", id)
		}
		path := filepath.Join("data", id+".json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: expected file %s to exist: %v", id, path, err)
		}
		if got := Layer(wf.ID); got != LayerL0 {
			t.Fatalf("%s: Layer = %q, want %q", id, got, LayerL0)
		}
		if strings.Contains(strings.ToLower(seedJSONByID(t, id)), "benchmark") {
			t.Fatalf("%s: L0 aggregation workflow must not contain benchmark-specific instructions", id)
		}
	}
}

func TestAggregationL0DescriptorMetadataMatchesCompilerMethods(t *testing.T) {
	expectedMethodTags := map[string]string{
		"aggregation-collect":       "collect",
		"aggregation-majority-vote": "majority-vote",
		"aggregation-synthesis":     "synthesis",
		"aggregation-judge":         "judge",
		"aggregation-debate-decide": "debate-decide",
		"aggregation-scoring":       "scoring",
		"aggregation-peer-matrix":   "peer-matrix",
	}

	byID := seedWorkflowsByID(t)
	for id, wantMethodTag := range expectedMethodTags {
		wf, ok := byID[id]
		if !ok {
			t.Fatalf("missing L0 seed %s", id)
		}
		if !stringSliceContains(wf.Metadata.Tags, "aggregation") {
			t.Errorf("%s: descriptor tags must include aggregation", id)
		}
		if !stringSliceContains(wf.Metadata.Tags, wantMethodTag) {
			t.Errorf("%s: descriptor tags %v must include compiler method tag %q", id, wf.Metadata.Tags, wantMethodTag)
		}
	}

	dynamicContracts := map[string][]string{
		"aggregation-scoring":     {"scoring_model", "rubric_mode"},
		"aggregation-peer-matrix": {"rubric_model", "rubric_mode"},
	}
	for id, requiredTerms := range dynamicContracts {
		wf := byID[id]
		contract := strings.ToLower(wf.Metadata.InputContract["aggregation_config"])
		for _, term := range requiredTerms {
			if !strings.Contains(contract, term) {
				t.Errorf("%s: aggregation_config input contract %q must mention %s", id, contract, term)
			}
		}
	}
}

func TestTask8ReasoningSeedsReferenceExpectedL0AggregationWorkflow(t *testing.T) {
	byID := seedWorkflowsByID(t)
	for id, wantWorkflowID := range task8ReasoningAggregationRefs {
		wf, ok := byID[id]
		if !ok {
			t.Fatalf("missing reasoning seed %s", id)
		}
		if got := Layer(wf.ID); got != LayerL1 {
			t.Fatalf("%s: Layer = %q, want %q", id, got, LayerL1)
		}
		aggregationNodes := seedNodesOfType(wf, "aggregation")
		if len(aggregationNodes) == 0 {
			t.Fatalf("%s: expected an aggregation node referencing %s", id, wantWorkflowID)
		}
		found := false
		for _, node := range aggregationNodes {
			if node.Data.Config.AggregationWorkflowID == "" {
				t.Errorf("%s: aggregation node %s missing aggregationWorkflowId", id, node.ID)
				continue
			}
			if node.Data.Config.AggregationWorkflowID == wantWorkflowID {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: no aggregation node references %s", id, wantWorkflowID)
		}
	}
}

func TestL1ReasoningSeedsAvoidBenchmarkSpecificAnswerExtraction(t *testing.T) {
	bannedPhrases := []string{
		"question and options",
		"multiple choice",
		"multiple-choice",
		"option letter",
		"answer option letter",
		"a-d",
	}

	for _, wf := range seedWorkflowsByID(t) {
		if Layer(wf.ID) != LayerL1 || !strings.HasPrefix(wf.ID, "reasoning-") {
			continue
		}
		seedJSON := seedJSONByID(t, wf.ID)
		lower := strings.ToLower(seedJSON)
		for _, phrase := range bannedPhrases {
			if strings.Contains(lower, phrase) {
				t.Errorf("%s: L1 reasoning seed contains benchmark-specific phrase %q", wf.ID, phrase)
			}
		}
		for _, node := range seedNodesOfType(wf, "aggregation") {
			pattern, _ := node.Data.Config.AggregationConfig["extraction_pattern"].(string)
			if pattern == "" {
				continue
			}
			hasSingleLetterBranch := strings.Contains(pattern, "([A-Z])") || strings.Contains(pattern, "([A-D])")
			hasBroadAnswerBranch := strings.Contains(pattern, "[^\\n")
			if hasSingleLetterBranch && !hasBroadAnswerBranch {
				t.Errorf("%s: aggregation node %s uses single-letter-only extraction pattern %q", wf.ID, node.ID, pattern)
			}
		}
	}
}

func TestL1ReasoningExtractionPatternsHandleLettersAndShortAnswers(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{name: "mcqa letter", text: "Reasoning...\nFinal answer: A\n", want: "A"},
		{name: "mcqa letter with because explanation", text: "Answer: A is correct because B is wrong.\n", want: "A"},
		{name: "mcqa letter with best-choice explanation", text: "Final answer: A is the best choice.\n", want: "A"},
		{name: "mcqa parenthesized letter with explanation", text: "Answer: (C) is correct.\n", want: "C"},
		{name: "numeric answer", text: "Answer is 42\n", want: "42"},
		{name: "symbolic answer", text: "Final answer: x=3\n", want: "X=3"},
		{name: "decimal answer", text: "Final answer: 3.14.\n", want: "3.14"},
		{name: "symbolic decimal answer", text: "Final answer: x=3.5 because it solves the equation.\n", want: "X=3.5"},
		{name: "explanation after answer", text: "Answer is 42 because that is the result.\n", want: "42"},
		{name: "short answer with internal period", text: "Final answer: St. Louis\n", want: "ST. LOUIS"},
	}

	for _, wf := range seedWorkflowsByID(t) {
		if Layer(wf.ID) != LayerL1 || !strings.HasPrefix(wf.ID, "reasoning-") {
			continue
		}
		for _, node := range seedNodesOfType(wf, "aggregation") {
			pattern, _ := node.Data.Config.AggregationConfig["extraction_pattern"].(string)
			if pattern == "" {
				continue
			}
			for _, tc := range cases {
				got := extractAnswerForSeedPattern(tc.text, pattern)
				if got != tc.want {
					t.Errorf("%s/%s %s: ExtractAnswer = %q, want %q with pattern %q", wf.ID, node.ID, tc.name, got, tc.want, pattern)
				}
			}
		}
	}
}

func TestTask8CompositeSeedComposesL1WithWorkflowRefs(t *testing.T) {
	byID := seedWorkflowsByID(t)
	wf, ok := byID["composite-judge-synthesis-cheap"]
	if !ok {
		t.Fatal("missing composite-judge-synthesis-cheap seed")
	}
	if got := Layer(wf.ID); got != LayerL2 {
		t.Fatalf("Layer(%s) = %q, want %q", wf.ID, got, LayerL2)
	}
	if len(seedNodesOfType(wf, "child_workflow")) > 0 {
		t.Fatalf("%s: composite seed must not use child_workflow", wf.ID)
	}
	if len(seedNodesOfType(wf, "aggregation")) > 0 {
		t.Fatalf("%s: composite seed must compose L1 workflows, not direct L0 aggregation nodes", wf.ID)
	}
	refs := seedNodesOfType(wf, "workflow_ref")
	if len(refs) < 2 {
		t.Fatalf("%s: expected at least two workflow_ref nodes, got %d", wf.ID, len(refs))
	}
	for _, ref := range refs {
		target := ref.Data.Config.WorkflowID
		if !strings.HasPrefix(target, "reasoning-") {
			t.Fatalf("%s: workflow_ref %s targets %q, want L1 reasoning workflow", wf.ID, ref.ID, target)
		}
	}
}

func TestTask8BenchmarkWrappersKeepChildWorkflowBoundary(t *testing.T) {
	byID := seedWorkflowsByID(t)
	for id, wantChildID := range task8BenchmarkWrappers {
		wf, ok := byID[id]
		if !ok {
			t.Fatalf("missing benchmark wrapper seed %s", id)
		}
		childNodes := seedNodesOfType(wf, "child_workflow")
		if len(childNodes) == 0 {
			t.Fatalf("%s: benchmark wrapper must keep child_workflow boundary to %s", id, wantChildID)
		}
		found := false
		for _, child := range childNodes {
			childID := child.Data.Config.ChildWorkflowID
			if strings.HasPrefix(childID, "aggregation-") {
				t.Fatalf("%s: benchmark wrapper must not evaluate L0 aggregation workflow %q", id, childID)
			}
			if childID == wantChildID {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: child_workflow target not found; want %s", id, wantChildID)
		}
		packaging := seedNodeByID(wf, "aggregation-benchmark-answer")
		if packaging == nil {
			t.Fatalf("%s: benchmark wrapper missing aggregation-benchmark-answer packaging node", id)
		}
		if packaging.Data.Config.AggregationMethod != "collect" {
			t.Fatalf("%s: benchmark answer packaging aggregationMethod = %q, want collect", id, packaging.Data.Config.AggregationMethod)
		}
		if !packaging.Data.Config.BenchmarkOutputPackaging {
			t.Fatalf("%s: benchmark answer aggregation must be marked benchmarkOutputPackaging", id)
		}
	}
}

func TestTask8BaselineBenchmarksRemainDirectModelBaselines(t *testing.T) {
	byID := seedWorkflowsByID(t)
	for _, id := range task8BaselineBenchmarks {
		wf, ok := byID[id]
		if !ok {
			t.Fatalf("missing baseline seed %s", id)
		}
		if got := Layer(wf.ID); got != LayerL3 {
			t.Fatalf("%s: Layer = %q, want %q", id, got, LayerL3)
		}
		if len(seedNodesOfType(wf, "child_workflow")) > 0 {
			t.Fatalf("%s: direct-model baseline must not be forced through child_workflow", id)
		}
		if seedNodeByID(wf, "agent-solver") == nil {
			t.Fatalf("%s: direct-model baseline missing agent-solver", id)
		}
		if seedNodeByID(wf, "result-benchmark-answer") == nil {
			t.Fatalf("%s: direct-model baseline missing benchmark answer result", id)
		}
		packaging := seedNodeByID(wf, "aggregation-benchmark-answer")
		if packaging == nil {
			t.Fatalf("%s: direct-model baseline missing aggregation-benchmark-answer packaging node", id)
		}
		if packaging.Data.Config.AggregationMethod != "collect" {
			t.Fatalf("%s: benchmark answer packaging aggregationMethod = %q, want collect", id, packaging.Data.Config.AggregationMethod)
		}
		if !packaging.Data.Config.BenchmarkOutputPackaging {
			t.Fatalf("%s: benchmark answer aggregation must be marked benchmarkOutputPackaging", id)
		}
	}
}

func TestTask8NoSeedStoresHiddenLLMAggregationOnResult(t *testing.T) {
	for _, wf := range seedWorkflowsByID(t) {
		for _, node := range wf.Nodes {
			if node.Type != "result" && node.Data.Type != "result" {
				continue
			}
			method := node.Data.Config.AggregationMethod
			if method != "" && method != "collect" {
				t.Fatalf("%s: result node %s stores hidden LLM aggregation method %q", wf.ID, node.ID, method)
			}
			if len(node.Data.Config.AggregationConfig) > 0 {
				t.Fatalf("%s: result node %s stores hidden aggregationConfig", wf.ID, node.ID)
			}
		}
	}
}

func TestTask8NonAggregationExampleSeedsStayOutOfMigration(t *testing.T) {
	byID := seedWorkflowsByID(t)
	for _, id := range []string{"agent-run-novomo-basic", "superagent-novo-run-basic"} {
		wf, ok := byID[id]
		if !ok {
			t.Fatalf("missing non-aggregation seed %s", id)
		}
		if len(seedNodesOfType(wf, "workflow_ref")) > 0 {
			t.Fatalf("%s: non-aggregation example seed should not gain workflow_ref nodes", id)
		}
		for _, node := range seedNodesOfType(wf, "aggregation") {
			if node.Data.Config.AggregationWorkflowID != "" {
				t.Fatalf("%s: aggregation node %s should not gain L0 aggregation workflow references", id, node.ID)
			}
		}
	}
}

func TestGetAllJSON_AggregationsUseVisibleMacroShape(t *testing.T) {
	all := GetAllJSON()
	for i, seedJSON := range all {
		var wf seedShapeWorkflow
		if err := json.Unmarshal([]byte(seedJSON), &wf); err != nil {
			t.Errorf("seed[%d]: parse error: %v", i, err)
			continue
		}
		if Layer(wf.ID) == LayerL0 {
			continue
		}
		if Layer(wf.ID) == LayerL2 {
			continue
		}

		nodesByID := make(map[string]seedShapeNode, len(wf.Nodes))
		aggregationCount := 0
		for _, node := range wf.Nodes {
			nodesByID[node.ID] = node
			if node.Type == "aggregation" || node.Data.Type == "aggregation" {
				aggregationCount++
				if node.Data.Config.AggregationMethod == "" {
					t.Errorf("%s: aggregation node %s missing aggregationMethod", wf.ID, node.ID)
				}
			}
			if node.Type == "result" || node.Data.Type == "result" {
				if node.Data.Config.AggregationMethod != "" {
					t.Errorf("%s: result node %s still owns aggregationMethod %q", wf.ID, node.ID, node.Data.Config.AggregationMethod)
				}
				if len(node.Data.Config.AggregationConfig) > 0 {
					t.Errorf("%s: result node %s still owns aggregationConfig", wf.ID, node.ID)
				}
			}
		}
		if aggregationCount == 0 {
			t.Errorf("%s: expected at least one visible aggregation node", wf.ID)
		}

		outgoingResultTargets := make(map[string]int)
		for _, edge := range wf.Edges {
			source, sourceOK := nodesByID[edge.Source]
			target, targetOK := nodesByID[edge.Target]
			if !sourceOK || !targetOK {
				continue
			}
			if source.Type == "aggregation" || source.Data.Type == "aggregation" {
				if target.Type != "result" && target.Data.Type != "result" {
					t.Errorf("%s: aggregation node %s must flow to a result node, got %s", wf.ID, source.ID, target.ID)
				}
				outgoingResultTargets[source.ID]++
			}
		}
		for nodeID, count := range outgoingResultTargets {
			if count != 1 {
				t.Errorf("%s: aggregation node %s has %d outgoing result edges, want 1", wf.ID, nodeID, count)
			}
		}
	}
}

func TestGetAllJSON_SeedPositionsFlowTopToBottom(t *testing.T) {
	all := GetAllJSON()
	for i, seedJSON := range all {
		var wf seedShapeWorkflow
		if err := json.Unmarshal([]byte(seedJSON), &wf); err != nil {
			t.Errorf("seed[%d]: parse error: %v", i, err)
			continue
		}

		nodesByID := make(map[string]seedShapeNode, len(wf.Nodes))
		for _, node := range wf.Nodes {
			nodesByID[node.ID] = node
		}
		for _, edge := range wf.Edges {
			source, sourceOK := nodesByID[edge.Source]
			target, targetOK := nodesByID[edge.Target]
			if !sourceOK || !targetOK {
				continue
			}
			if target.Position.Y <= source.Position.Y {
				t.Errorf(
					"%s: edge %s -> %s should flow top-to-bottom, got source y %.0f and target y %.0f",
					wf.ID,
					edge.Source,
					edge.Target,
					source.Position.Y,
					target.Position.Y,
				)
			}
		}
	}
}

func seedWorkflowsByID(t *testing.T) map[string]seedShapeWorkflow {
	t.Helper()
	out := make(map[string]seedShapeWorkflow)
	for i, seedJSON := range GetAllJSON() {
		var wf seedShapeWorkflow
		if err := json.Unmarshal([]byte(seedJSON), &wf); err != nil {
			t.Fatalf("seed[%d]: parse error: %v", i, err)
		}
		out[wf.ID] = wf
	}
	return out
}

func seedJSONByID(t *testing.T, id string) string {
	t.Helper()
	jsonText, err := GetJSONByID(id)
	if err != nil {
		t.Fatalf("GetJSONByID(%q): %v", id, err)
	}
	return jsonText
}

func seedNodesOfType(wf seedShapeWorkflow, nodeType string) []seedShapeNode {
	var nodes []seedShapeNode
	for _, node := range wf.Nodes {
		if node.Type == nodeType || node.Data.Type == nodeType {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func seedNodeByID(wf seedShapeWorkflow, nodeID string) *seedShapeNode {
	for i := range wf.Nodes {
		if wf.Nodes[i].ID == nodeID {
			return &wf.Nodes[i]
		}
	}
	return nil
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func extractAnswerForSeedPattern(text, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	for _, match := range matches[1:] {
		if strings.TrimSpace(match) != "" {
			return strings.ToUpper(strings.TrimSpace(match))
		}
	}
	return ""
}

func TestGetAllInfo_ReadsAggregationMacroNodes(t *testing.T) {
	infos := GetAllInfo()
	infoByID := make(map[string]Info, len(infos))
	for _, info := range infos {
		infoByID[info.ID] = info
	}

	all := GetAllJSON()
	for i, seedJSON := range all {
		var wf seedShapeWorkflow
		if err := json.Unmarshal([]byte(seedJSON), &wf); err != nil {
			t.Errorf("seed[%d]: parse error: %v", i, err)
			continue
		}
		if Layer(wf.ID) == LayerL0 {
			continue
		}
		if Layer(wf.ID) == LayerL2 {
			continue
		}
		expected := ""
		for _, node := range wf.Nodes {
			if node.Type == "aggregation" || node.Data.Type == "aggregation" {
				expected = node.Data.Config.AggregationMethod
			}
		}
		if expected == "" {
			t.Errorf("%s: no aggregation macro method found", wf.ID)
			continue
		}
		if got := infoByID[wf.ID].AggregationMethod; got != expected {
			t.Errorf("%s: GetAllInfo AggregationMethod = %q, want %q", wf.ID, got, expected)
		}
	}
}
