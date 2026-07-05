// admin_optimize_promote.go — Promotion handler and helpers for optimization runs.
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
)

func (s *Server) handleOptimizeRunPromote(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	run, err := s.storage.GetOptimizationRun(runID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Optimization run not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load optimization run", http.StatusInternalServerError)
		return
	}
	best, err := s.selectBestPromotableOrganism(run)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to select best organism for promotion: %v", err), http.StatusInternalServerError)
		return
	}
	if best == nil || best.Fitness == nil {
		writeJSONError(w, "Run has no feasible organism eligible for promotion", http.StatusConflict)
		return
	}

	baseline, err := s.findBaselineOrganism(run.ID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load baseline organism: %v", err), http.StatusInternalServerError)
		return
	}
	if baseline == nil || baseline.Fitness == nil {
		writeJSONError(w, "Baseline organism/fitness not found", http.StatusBadRequest)
		return
	}

	promotionPolicy := run.Spec.PromotionPolicy
	accuracyGain := best.Fitness.AdjustedAccuracy - baseline.Fitness.AdjustedAccuracy
	if accuracyGain < promotionPolicy.MinAccuracyGain {
		writeJSONError(w, fmt.Sprintf("best organism accuracy gain %.4f is below promotion threshold %.4f", accuracyGain, promotionPolicy.MinAccuracyGain), http.StatusConflict)
		return
	}
	for _, metric := range promotionPolicy.NoRegressionOn {
		bestMetric := best.Fitness.MetricValue(metric)
		baseMetric := baseline.Fitness.MetricValue(metric)
		if isRegressionMetric(metric, bestMetric, baseMetric) {
			writeJSONError(w, fmt.Sprintf("metric %s regressed: best=%.6f baseline=%.6f", metric, bestMetric, baseMetric), http.StatusConflict)
			return
		}
	}

	workflowDef, err := s.storage.GetWorkflow(run.WorkflowID)
	if err != nil {
		writeJSONError(w, "Failed to load workflow for promotion", http.StatusInternalServerError)
		return
	}
	if run.WorkflowVersion > 0 && workflowDef.Version != run.WorkflowVersion {
		writeJSONError(w, fmt.Sprintf("workflow version conflict: run started at version %d, current version is %d", run.WorkflowVersion, workflowDef.Version), http.StatusConflict)
		return
	}

	seedFile, fileErr := s.findSeedFilePath(run.WorkflowID)
	if fileErr == nil {
		if err := writePromotedSeedFile(seedFile, best.WorkflowJSON); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to update seed file: %v", err), http.StatusInternalServerError)
			return
		}
	}

	workflowDef.Definition = string(best.WorkflowJSON)
	if err := s.storage.UpdateWorkflow(workflowDef); err != nil {
		status := http.StatusInternalServerError
		if err == storage.ErrConflict {
			status = http.StatusConflict
		}
		writeJSONError(w, fmt.Sprintf("Failed to promote workflow: %v", err), status)
		return
	}

	changes, _ := s.storage.GetOptimizationParamChanges(best.ID)
	response := map[string]interface{}{
		"promoted":         true,
		"workflow_id":      run.WorkflowID,
		"organism_id":      best.ID,
		"changes":          changes,
		"accuracy_gain":    accuracyGain,
		"baseline":         baseline.Fitness,
		"promoted_fitness": best.Fitness,
		"generalization_check": map[string]interface{}{
			"required": promotionPolicy.RequireGeneralizationCheck,
			"passed":   !promotionPolicy.RequireGeneralizationCheck,
			"note":     "v1 does not implement non-MCQA generalization gate",
		},
	}
	if fileErr == nil {
		response["seed_file"] = seedFile
	}
	writeJSONResponse(w, response)
}

func (s *Server) selectBestPromotableOrganism(run *optimize.OptimizationRun) (*optimize.Organism, error) {
	if run == nil {
		return nil, fmt.Errorf("optimization run is nil")
	}
	preferredID := strings.TrimSpace(run.BestOrganismID)
	if preferredID != "" {
		best, err := s.storage.GetOptimizationOrganism(preferredID)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		if err == nil && best != nil && best.Fitness != nil && best.Fitness.Feasible {
			return best, nil
		}
	}

	organisms, err := s.storage.ListOptimizationOrganisms(run.ID, nil, 10000)
	if err != nil {
		return nil, err
	}
	var best *optimize.Organism
	for _, organism := range organisms {
		if organism == nil || organism.Fitness == nil || !organism.Fitness.Feasible {
			continue
		}
		if best == nil || isBetterPromotionCandidate(organism, best) {
			best = organism
		}
	}
	return best, nil
}

func isBetterPromotionCandidate(candidate, incumbent *optimize.Organism) bool {
	if candidate == nil || candidate.Fitness == nil {
		return false
	}
	if incumbent == nil || incumbent.Fitness == nil {
		return true
	}
	if candidate.Fitness.CompositeScore > incumbent.Fitness.CompositeScore+1e-9 {
		return true
	}
	if candidate.Fitness.CompositeScore < incumbent.Fitness.CompositeScore-1e-9 {
		return false
	}
	if candidate.Fitness.AdjustedAccuracy > incumbent.Fitness.AdjustedAccuracy+1e-9 {
		return true
	}
	if candidate.Fitness.AdjustedAccuracy < incumbent.Fitness.AdjustedAccuracy-1e-9 {
		return false
	}
	if candidate.Fitness.CostPerItem+1e-12 < incumbent.Fitness.CostPerItem {
		return true
	}
	return candidate.CreatedAt.Before(incumbent.CreatedAt)
}

func (s *Server) findBaselineOrganism(runID string) (*optimize.Organism, error) {
	gen := 0
	organisms, err := s.storage.ListOptimizationOrganisms(runID, &gen, 100)
	if err != nil {
		return nil, err
	}
	if len(organisms) == 0 {
		return nil, nil
	}
	for _, organism := range organisms {
		if organism.MutationType == "seed" {
			return organism, nil
		}
	}
	sort.Slice(organisms, func(i, j int) bool {
		return organisms[i].CreatedAt.Before(organisms[j].CreatedAt)
	})
	return organisms[0], nil
}

func isRegressionMetric(metric string, best float64, baseline float64) bool {
	metric = strings.TrimSpace(strings.ToLower(metric))
	switch metric {
	case "cost_per_item", "avg_latency_ms", "p95_latency_ms":
		return best > baseline
	default:
		return best < baseline
	}
}

func (s *Server) findSeedFilePath(workflowID string) (string, error) {
	root := s.workdir
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	pattern := filepath.Join(root, "pkg", "storage", "seeds", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var seed map[string]interface{}
		if err := json.Unmarshal(content, &seed); err != nil {
			continue
		}
		if id, _ := seed["id"].(string); id == workflowID {
			return file, nil
		}
	}
	return "", fmt.Errorf("seed file for workflow %s not found", workflowID)
}

func writePromotedSeedFile(seedPath string, promotedWorkflowJSON []byte) error {
	seedContent, err := os.ReadFile(seedPath)
	if err != nil {
		return err
	}
	var existingSeed map[string]interface{}
	if err := json.Unmarshal(seedContent, &existingSeed); err != nil {
		return fmt.Errorf("parse existing seed JSON: %w", err)
	}
	optimizeSection := existingSeed["optimize"]

	var promoted map[string]interface{}
	if err := json.Unmarshal(promotedWorkflowJSON, &promoted); err != nil {
		return fmt.Errorf("parse promoted workflow JSON: %w", err)
	}
	if optimizeSection != nil {
		promoted["optimize"] = optimizeSection
	}

	encoded, err := json.MarshalIndent(promoted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal promoted seed JSON: %w", err)
	}
	encoded = append(encoded, '\n')
	tmpPath := seedPath + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, seedPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
