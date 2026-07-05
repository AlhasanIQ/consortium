// admin_optimize.go — Optimization run handlers: list, detail, lifecycle
// (pause/resume/cancel), heartbeat, progress, status patch, and compare.
// The create handler lives in admin_optimize_create.go.
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
)

func (s *Server) handleOptimizeRuns(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflow"))
	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	runs, err := s.storage.ListOptimizationRuns(status, workflowID, limit)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to list optimization runs: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"runs": runs})
}

type optimizeGenerationAggregate struct {
	Generation     int     `json:"generation"`
	BestAccuracy   float64 `json:"best_accuracy"`
	MeanAccuracy   float64 `json:"mean_accuracy"`
	WorstAccuracy  float64 `json:"worst_accuracy"`
	CumulativeCost float64 `json:"cumulative_cost"`
}

type optimizeCompareResponse struct {
	Runs           []optimize.OptimizationRun               `json:"runs"`
	BestOrganisms  []*optimize.Organism                     `json:"best_organisms"`
	GenerationData map[string][]optimizeGenerationAggregate `json:"generation_data"`
}

func (s *Server) handleOptimizeCompare(w http.ResponseWriter, r *http.Request) {
	idsRaw := strings.TrimSpace(r.URL.Query().Get("ids"))
	if idsRaw == "" {
		writeJSONError(w, "ids is required", http.StatusBadRequest)
		return
	}
	if !safeCSVValue.MatchString(idsRaw) {
		writeJSONError(w, "ids contains invalid characters", http.StatusBadRequest)
		return
	}
	ids := make([]string, 0)
	for _, raw := range strings.Split(idsRaw, ",") {
		value := strings.TrimSpace(raw)
		if value != "" {
			ids = append(ids, value)
		}
	}
	ids = dedupeOrderedStrings(ids)
	if len(ids) < 2 {
		writeJSONError(w, "at least 2 run IDs are required", http.StatusBadRequest)
		return
	}

	resp := optimizeCompareResponse{
		Runs:           make([]optimize.OptimizationRun, 0, len(ids)),
		BestOrganisms:  make([]*optimize.Organism, 0, len(ids)),
		GenerationData: make(map[string][]optimizeGenerationAggregate, len(ids)),
	}

	for _, runID := range ids {
		run, err := s.storage.GetOptimizationRun(runID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeJSONError(w, fmt.Sprintf("Optimization run not found: %s", runID), http.StatusNotFound)
				return
			}
			writeJSONError(w, fmt.Sprintf("Failed to load optimization run %s: %v", runID, err), http.StatusInternalServerError)
			return
		}
		resp.Runs = append(resp.Runs, *run)

		best, err := s.storage.GetBestOptimizationOrganisms(runID, 1)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to load best organism for %s: %v", runID, err), http.StatusInternalServerError)
			return
		}
		if len(best) > 0 {
			resp.BestOrganisms = append(resp.BestOrganisms, best[0])
		}

		organisms, err := s.storage.ListOptimizationOrganisms(runID, nil, 10000)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to load organisms for %s: %v", runID, err), http.StatusInternalServerError)
			return
		}
		resp.GenerationData[runID] = buildOptimizeGenerationAggregates(run, organisms)
	}

	writeJSONResponse(w, resp)
}

func buildOptimizeGenerationAggregates(run *optimize.OptimizationRun, organisms []*optimize.Organism) []optimizeGenerationAggregate {
	type generationStats struct {
		best       float64
		worst      float64
		sum        float64
		count      int
		generation int
		cost       float64
	}

	byGen := make(map[int]*generationStats)
	for _, organism := range organisms {
		if organism == nil || organism.Fitness == nil {
			continue
		}
		acc := organism.Fitness.AdjustedAccuracy
		stats := byGen[organism.Generation]
		if stats == nil {
			stats = &generationStats{
				best:       acc,
				worst:      acc,
				sum:        acc,
				count:      1,
				generation: organism.Generation,
			}
			byGen[organism.Generation] = stats
		} else {
			if acc > stats.best {
				stats.best = acc
			}
			if acc < stats.worst {
				stats.worst = acc
			}
			stats.sum += acc
			stats.count++
		}
		items := organism.Fitness.TotalItems
		if items <= 0 {
			items = run.ItemLimit
		}
		if items > 0 {
			stats.cost += organism.Fitness.CostPerItem * float64(items)
		}
	}

	generations := make([]int, 0, len(byGen))
	for generation := range byGen {
		generations = append(generations, generation)
	}
	sort.Ints(generations)

	out := make([]optimizeGenerationAggregate, 0, len(generations))
	cumulativeCost := 0.0
	for _, generation := range generations {
		stats := byGen[generation]
		if stats == nil || stats.count == 0 {
			continue
		}
		cumulativeCost += stats.cost
		out = append(out, optimizeGenerationAggregate{
			Generation:     generation,
			BestAccuracy:   stats.best,
			MeanAccuracy:   stats.sum / float64(stats.count),
			WorstAccuracy:  stats.worst,
			CumulativeCost: cumulativeCost,
		})
	}
	return out
}

func (s *Server) handleOptimizeRunDetail(w http.ResponseWriter, r *http.Request) {
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
	writeJSONResponse(w, run)
}

func (s *Server) handleOptimizeRunPause(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	if err := s.storage.UpdateOptimizationRunStatus(runID, "paused"); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to pause run: %v", err), http.StatusInternalServerError)
		return
	}
	_ = s.storage.ClearOptimizationRunLease(runID)
	writeJSONResponse(w, map[string]interface{}{"status": "paused"})
}

func (s *Server) handleOptimizeRunResume(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	if err := s.storage.UpdateOptimizationRunStatus(runID, "running"); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to resume run: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"status": "running"})
}

func (s *Server) handleOptimizeRunCancel(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	if err := s.storage.UpdateOptimizationRunStatus(runID, "cancelled"); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to cancel run: %v", err), http.StatusInternalServerError)
		return
	}
	_ = s.storage.ClearOptimizationRunLease(runID)
	writeJSONResponse(w, map[string]interface{}{"status": "cancelled"})
}

func (s *Server) handleOptimizeRunHeartbeat(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		OwnerPID      int    `json:"owner_pid"`
		OwnerHostname string `json:"owner_hostname"`
		Clear         bool   `json:"clear"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Clear {
		if err := s.storage.ClearOptimizationRunLease(runID); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to clear heartbeat: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, map[string]interface{}{"cleared": true})
		return
	}
	if req.OwnerPID == 0 || strings.TrimSpace(req.OwnerHostname) == "" {
		writeJSONError(w, "owner_pid and owner_hostname are required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	if run.IsOwnedAndActive(now, 30*time.Second) {
		if run.OwnerPID != req.OwnerPID || !strings.EqualFold(run.OwnerHostname, req.OwnerHostname) {
			heartbeatAge := now.Sub(*run.LastHeartbeatAt).Truncate(time.Second)
			if heartbeatAge < 0 {
				heartbeatAge = 0
			}
			writeJSONError(
				w,
				fmt.Sprintf(
					"run owned by PID %d on %s (last heartbeat %s ago)",
					run.OwnerPID,
					run.OwnerHostname,
					heartbeatAge,
				),
				http.StatusConflict,
			)
			return
		}
	}

	if err := s.storage.UpdateOptimizationRunLease(runID, req.OwnerPID, req.OwnerHostname); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to update heartbeat: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"ok": true})
}

func (s *Server) handleOptimizeRunProgress(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	var req struct {
		Generation          int               `json:"generation"`
		BestOrganismID      string            `json:"best_organism_id"`
		BestFitness         *optimize.Fitness `json:"best_fitness"`
		SpentUSD            float64           `json:"spent_usd"`
		TotalOrganisms      int               `json:"total_organisms"`
		DSPYMetricCallsUsed int               `json:"dspy_metric_calls_used"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	if err := s.storage.UpdateOptimizationRunProgress(runID, req.Generation, req.BestOrganismID, req.BestFitness, req.SpentUSD, req.TotalOrganisms, req.DSPYMetricCallsUsed); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to update run progress: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"updated": true})
}

func (s *Server) handleOptimizeRunStatusPatch(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		writeJSONError(w, "status is required", http.StatusBadRequest)
		return
	}
	if err := s.storage.UpdateOptimizationRunStatus(runID, req.Status); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to update run status: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"status": req.Status})
}
