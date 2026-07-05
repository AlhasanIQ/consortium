// admin_optimize_organisms.go — Organism CRUD, fitness updates, lineage,
// param-change audit, and mutation artifact handlers for optimization runs.
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
	"github.com/gorilla/mux"
)

func (s *Server) handleOptimizeRunOrganisms(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	generationStr := strings.TrimSpace(r.URL.Query().Get("generation"))
	bestOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("best")), "1")
	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)

	if r.Method == http.MethodPost {
		var organism optimize.Organism
		if err := json.NewDecoder(r.Body).Decode(&organism); err != nil {
			writeJSONError(w, "Invalid organism payload", http.StatusBadRequest)
			return
		}
		organism.OptRunID = runID
		if organism.CreatedAt.IsZero() {
			organism.CreatedAt = time.Now().UTC()
		}
		if err := s.storage.CreateOptimizationOrganism(&organism); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to create organism: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, map[string]interface{}{"created": true, "organism_id": organism.ID})
		return
	}

	var (
		organisms []*optimize.Organism
		err       error
	)
	if bestOnly {
		organisms, err = s.storage.GetBestOptimizationOrganisms(runID, limit)
	} else if generationStr != "" {
		gen, parseErr := strconv.Atoi(generationStr)
		if parseErr != nil {
			writeJSONError(w, "generation must be integer", http.StatusBadRequest)
			return
		}
		organisms, err = s.storage.ListOptimizationOrganisms(runID, &gen, limit)
	} else {
		organisms, err = s.storage.ListOptimizationOrganisms(runID, nil, limit)
	}
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to list organisms: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"organisms": organisms, "total": len(organisms)})
}

func (s *Server) handleOptimizeRunOrganismDetail(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	orgID := mux.Vars(r)["orgID"]
	organism, err := s.storage.GetOptimizationOrganism(orgID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Organism not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load organism", http.StatusInternalServerError)
		return
	}
	if organism.OptRunID != runID {
		writeJSONError(w, "Organism not found in run", http.StatusNotFound)
		return
	}
	changes, err := s.storage.GetOptimizationParamChanges(orgID)
	if err != nil {
		writeJSONError(w, "Failed to load param changes", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"organism": organism, "param_changes": changes})
}

func (s *Server) handleOptimizeRunLineage(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	organisms, err := s.storage.ListOptimizationOrganisms(runID, nil, 10000)
	if err != nil {
		writeJSONError(w, "Failed to load lineage", http.StatusInternalServerError)
		return
	}
	if len(organisms) == 0 {
		writeJSONResponse(w, map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}})
		return
	}

	nodes := make([]map[string]interface{}, 0, len(organisms))
	edges := make([]map[string]interface{}, 0)
	for _, organism := range organisms {
		node := map[string]interface{}{
			"id":         organism.ID,
			"generation": organism.Generation,
			"parent_ids": organism.ParentIDs,
		}
		if organism.Fitness != nil {
			node["composite_score"] = organism.Fitness.CompositeScore
			node["feasible"] = organism.Fitness.Feasible
		}
		nodes = append(nodes, node)
		for _, parentID := range organism.ParentIDs {
			edges = append(edges, map[string]interface{}{"from": parentID, "to": organism.ID})
		}
	}
	writeJSONResponse(w, map[string]interface{}{"nodes": nodes, "edges": edges})
}

func (s *Server) handleOptimizeOrganismDetail(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["orgID"]
	organism, err := s.storage.GetOptimizationOrganism(orgID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Organism not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load organism", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"organism": organism})
}

func (s *Server) handleOptimizeOrganismFitnessPatch(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["orgID"]
	var req struct {
		BenchRunID string            `json:"bench_run_id"`
		Fitness    *optimize.Fitness `json:"fitness"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	if req.Fitness == nil {
		writeJSONError(w, "fitness is required", http.StatusBadRequest)
		return
	}
	if err := s.storage.UpdateOptimizationOrganismFitness(orgID, req.BenchRunID, req.Fitness); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to update organism fitness: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"updated": true})
}

func (s *Server) handleOptimizeOrganismLineage(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["orgID"]
	lineage, err := s.storage.GetOptimizationLineage(orgID)
	if err != nil {
		writeJSONError(w, "Failed to load organism lineage", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"organisms": lineage})
}

func (s *Server) handleOptimizeOrganismParamChanges(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["orgID"]
	if r.Method == http.MethodPost {
		var req struct {
			Changes []optimize.ParamChange `json:"changes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if err := s.storage.CreateOptimizationParamChanges(orgID, req.Changes); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to create param changes: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, map[string]interface{}{"created": true})
		return
	}
	changes, err := s.storage.GetOptimizationParamChanges(orgID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to list param changes: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"changes": changes})
}

func (s *Server) handleOptimizeOrganismMutationArtifacts(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["orgID"]
	if r.Method == http.MethodPost {
		var req struct {
			Artifacts []optimize.MutationArtifact `json:"artifacts"`
			Compact   bool                        `json:"compact"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if err := s.storage.CreateOptimizationMutationArtifacts(orgID, req.Artifacts, req.Compact); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to store mutation artifacts: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, map[string]interface{}{"created": true})
		return
	}
	artifacts, err := s.storage.GetOptimizationMutationArtifacts(orgID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to list mutation artifacts: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"artifacts": artifacts})
}
