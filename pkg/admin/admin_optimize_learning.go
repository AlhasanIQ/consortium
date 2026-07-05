// admin_optimize_learning.go — Learning log handlers for optimization runs.
package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/gorilla/mux"
)

func (s *Server) handleOptimizeRunLearningLog(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	if r.Method == http.MethodPost {
		var entry optimize.LearningEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			writeJSONError(w, "Invalid learning entry payload", http.StatusBadRequest)
			return
		}
		if err := s.storage.AppendOptimizationLearningEntry(runID, &entry); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to append learning entry: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, map[string]interface{}{"appended": true})
		return
	}

	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)
	entries, err := s.storage.GetOptimizationLearningLog(runID, limit)
	if err != nil {
		writeJSONError(w, "Failed to load learning log", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"entries": entries})
}
