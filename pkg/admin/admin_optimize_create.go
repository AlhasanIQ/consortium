// admin_optimize_create.go — handleOptimizeRunCreate handler and its
// private helpers (request parsing, map accessors, first-non-empty
// coalescing).  Extracted from admin_optimize.go for readability.
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/google/uuid"
)

func (s *Server) handleOptimizeRunCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm() // Allow JSON body fallback when form parsing is not applicable.

	body := map[string]interface{}{}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	workflowID := firstNonEmpty(
		strings.TrimSpace(r.FormValue("workflow_id")),
		mapStringFromKeys(body, "workflow_id", "workflow"),
	)
	benchmark := firstNonEmpty(strings.TrimSpace(r.FormValue("benchmark")), mapStringFromKeys(body, "benchmark"))
	split := firstNonEmpty(strings.TrimSpace(r.FormValue("split")), mapStringFromKeys(body, "split"), "dev")
	itemLimit := optimize.IntFromMap(body, "item_limit", "itemLimit")
	if raw := strings.TrimSpace(r.FormValue("item_limit")); raw != "" {
		itemLimit = parseIntDefault(raw, itemLimit)
	}
	concurrency := optimize.IntFromMap(body, "concurrency")
	if raw := strings.TrimSpace(r.FormValue("concurrency")); raw != "" {
		concurrency = parseIntDefault(raw, concurrency)
	}
	populationSize := optimize.IntFromMap(body, "population_size", "populationSize")
	if raw := strings.TrimSpace(r.FormValue("population_size")); raw != "" {
		populationSize = parseIntDefault(raw, populationSize)
	}
	childrenPerParent := optimize.IntFromMap(body, "children_per_parent", "childrenPerParent")
	if raw := strings.TrimSpace(r.FormValue("children_per_parent")); raw != "" {
		childrenPerParent = parseIntDefault(raw, childrenPerParent)
	}
	maxChildrenPerGeneration := optimize.IntFromMap(body, "max_children_per_generation", "maxChildrenPerGeneration")
	if raw := strings.TrimSpace(r.FormValue("max_children_per_generation")); raw != "" {
		maxChildrenPerGeneration = parseIntDefault(raw, maxChildrenPerGeneration)
	}
	claudeModel := firstNonEmpty(strings.TrimSpace(r.FormValue("claude_model")), mapStringFromKeys(body, "claude_model", "claudeModel"), "opus")
	strategy := optimize.NormalizeOptimizeStrategy(firstNonEmpty(strings.TrimSpace(r.FormValue("strategy")), mapStringFromKeys(body, "strategy"), optimize.OptimizeStrategyEvolutionary))
	mutatorMode := optimize.NormalizeMutatorMode(firstNonEmpty(strings.TrimSpace(r.FormValue("mutator_mode")), mapStringFromKeys(body, "mutator_mode", "mutatorMode"), optimize.MutatorModeAuto))
	adaptiveFanout := firstTrue(
		parseAdminBool(r.FormValue("adaptive_fanout")),
		mapBoolFromKeys(body, "adaptive_fanout", "adaptiveFanout"),
	)
	compactArtifacts := firstTrue(
		parseAdminBool(r.FormValue("compact_artifacts")),
		mapBoolFromKeys(body, "compact_artifacts", "compactArtifacts"),
	)
	budgetUSD := firstPositiveFloat(
		parseFloatDefault(r.FormValue("budget_usd"), 0),
		optimize.FloatFromMap(body, "budget_usd", "budgetUSD"),
	)
	var rngSeed *int64
	if seedVal := firstNonEmpty(strings.TrimSpace(r.FormValue("rng_seed")), mapStringFromKeys(body, "rng_seed", "rngSeed")); seedVal != "" {
		parsed, parseErr := strconv.ParseInt(seedVal, 10, 64)
		if parseErr != nil {
			writeJSONError(w, "rng_seed must be a valid int64", http.StatusBadRequest)
			return
		}
		rngSeed = &parsed
	}

	if workflowID == "" || benchmark == "" {
		writeJSONError(w, "workflow_id and benchmark are required", http.StatusBadRequest)
		return
	}
	if itemLimit < 0 {
		writeJSONError(w, "item_limit must be >= 0", http.StatusBadRequest)
		return
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if populationSize <= 0 {
		populationSize = 5
	}
	if childrenPerParent <= 0 {
		childrenPerParent = 1
	}
	if maxChildrenPerGeneration < 0 {
		writeJSONError(w, "max_children_per_generation must be >= 0", http.StatusBadRequest)
		return
	}
	if !optimize.IsSupportedOptimizeStrategy(strategy) {
		writeJSONError(w, "strategy must be one of evolutionary, darwinian, dspy", http.StatusBadRequest)
		return
	}
	if !optimize.IsSupportedMutatorMode(mutatorMode) {
		writeJSONError(w, "mutator_mode must be one of combinatorial, llm, miprov2, gepa, auto", http.StatusBadRequest)
		return
	}

	workflowDef, err := s.storage.GetWorkflow(workflowID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Workflow not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load workflow", http.StatusInternalServerError)
		return
	}

	var spec *optimize.OptimizeSpec
	if rawSpec, ok := body["spec"]; ok {
		specBytes, err := json.Marshal(rawSpec)
		if err != nil {
			writeJSONError(w, "Invalid spec payload", http.StatusBadRequest)
			return
		}
		var parsed optimize.OptimizeSpec
		if err := json.Unmarshal(specBytes, &parsed); err != nil {
			writeJSONError(w, fmt.Sprintf("Invalid spec JSON: %v", err), http.StatusBadRequest)
			return
		}
		spec = &parsed
	} else {
		seedJSON, err := s.storage.GetSeedWorkflowJSONByID(workflowID)
		if err != nil {
			writeJSONError(w, "Optimize spec not found in seed; provide spec explicitly", http.StatusBadRequest)
			return
		}
		parsedSeed, err := optimize.ParseSeedOptimizeSpec([]byte(seedJSON))
		if err != nil {
			writeJSONError(w, fmt.Sprintf("Invalid seed optimize spec: %v", err), http.StatusBadRequest)
			return
		}
		spec = parsedSeed.OptimizeSpec
	}
	if err := spec.Validate(); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid optimize spec: %v", err), http.StatusBadRequest)
		return
	}
	if err := optimize.ValidateRunConfiguration(strategy, mutatorMode, spec); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid strategy configuration: %v", err), http.StatusBadRequest)
		return
	}
	for _, declaration := range spec.Params {
		if err := optimize.ValidateParamScope(declaration); err != nil {
			writeJSONError(w, fmt.Sprintf("Invalid optimize param scope: %v", err), http.StatusBadRequest)
			return
		}
	}

	if budgetUSD <= 0 {
		budgetUSD = spec.StopPolicy.BudgetUSD
	}
	if budgetUSD <= 0 {
		budgetUSD = 10
	}

	run := &optimize.OptimizationRun{
		ID:                       "opt-" + uuid.NewString(),
		WorkflowID:               workflowID,
		WorkflowVersion:          workflowDef.Version,
		Benchmark:                benchmark,
		Split:                    split,
		ItemLimit:                itemLimit,
		Concurrency:              max(concurrency, 1),
		Spec:                     spec,
		Strategy:                 strategy,
		PopulationSize:           max(populationSize, 1),
		ChildrenPerParent:        max(childrenPerParent, 1),
		MaxChildrenPerGeneration: max(maxChildrenPerGeneration, 0),
		AdaptiveFanout:           adaptiveFanout,
		ClaudeModel:              claudeModel,
		MutatorMode:              mutatorMode,
		RNGSeed:                  rngSeed,
		CompactArtifacts:         compactArtifacts,
		TotalBudgetUSD:           budgetUSD,
		Status:                   "pending",
		CreatedAt:                time.Now().UTC(),
		UpdatedAt:                time.Now().UTC(),
	}

	if err := s.storage.CreateOptimizationRun(run); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to create optimization run: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSONResponse(w, run)
}

// --- Helpers used only by handleOptimizeRunCreate ---

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseFloatDefault(raw string, fallback float64) float64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstTrue(values ...bool) bool {
	for _, value := range values {
		if value {
			return true
		}
	}
	return false
}

func mapStringFromKeys(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			if value, ok := raw.(string); ok {
				if strings.TrimSpace(value) != "" {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	return ""
}

func mapBoolFromKeys(m map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			switch value := raw.(type) {
			case bool:
				return value
			case string:
				return strings.EqualFold(strings.TrimSpace(value), "true")
			}
		}
	}
	return false
}
