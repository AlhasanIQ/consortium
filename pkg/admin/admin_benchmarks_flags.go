package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
)

// --- Dataset flag types ---

type datasetFlagCreateRequest struct {
	Flags []datasetFlagInput `json:"flags"`
}

type datasetFlagInput struct {
	Benchmark      string `json:"benchmark"`
	Split          string `json:"split"`
	ItemID         string `json:"item_id"`
	DatasetVersion string `json:"dataset_version"`
	Reason         string `json:"reason"`
	Source         string `json:"source"`
}

type datasetFlagResolveRequest struct {
	ResolvedBy     string `json:"resolved_by"`
	ResolvedReason string `json:"resolved_reason"`
}

// --- Dataset flag handlers ---

func (s *Server) handleListDatasetFlags(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	benchmark := strings.TrimSpace(q.Get("benchmark"))
	split := strings.TrimSpace(q.Get("split"))
	activeOnly := strings.EqualFold(strings.TrimSpace(q.Get("active_only")), "true") ||
		strings.TrimSpace(q.Get("active_only")) == "1"

	// Default to active-only when no filter specified
	if q.Get("active_only") == "" {
		activeOnly = true
	}

	flags, err := s.storage.ListFlags(benchmark, split, activeOnly)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to list flags: %v", err), http.StatusInternalServerError)
		return
	}
	if flags == nil {
		flags = []storage.DatasetFlag{}
	}
	writeJSONResponse(w, map[string]interface{}{
		"flags": flags,
		"total": len(flags),
	})
}

func (s *Server) handleCreateDatasetFlags(w http.ResponseWriter, r *http.Request) {
	var req datasetFlagCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Flags) == 0 {
		writeJSONError(w, "At least one flag is required", http.StatusBadRequest)
		return
	}

	stoFlags := make([]storage.DatasetFlag, len(req.Flags))
	for i, f := range req.Flags {
		stoFlags[i] = storage.DatasetFlag{
			Benchmark:      f.Benchmark,
			Split:          f.Split,
			ItemID:         f.ItemID,
			DatasetVersion: f.DatasetVersion,
			Reason:         f.Reason,
			Source:         f.Source,
		}
	}

	result, err := s.storage.UpsertActiveFlags(stoFlags)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to create flags: %v", err), http.StatusBadRequest)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"flags":   result,
		"created": len(result),
	})
}

func (s *Server) handleResolveDatasetFlag(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id := int64(parseIntDefault(idStr, 0))
	if id <= 0 {
		writeJSONError(w, "Invalid flag ID", http.StatusBadRequest)
		return
	}

	var req datasetFlagResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	err := s.storage.ResolveFlag(id, req.ResolvedBy, req.ResolvedReason)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONError(w, fmt.Sprintf("Failed to resolve flag: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"resolved": true})
}

func (s *Server) handleDeleteDatasetFlag(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id := int64(parseIntDefault(idStr, 0))
	if id <= 0 {
		writeJSONError(w, "Invalid flag ID", http.StatusBadRequest)
		return
	}

	err := s.storage.DeleteFlag(id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONError(w, fmt.Sprintf("Failed to delete flag: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"deleted": true})
}
