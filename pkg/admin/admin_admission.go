package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
)

func (s *Server) handleAdmissionStatus(w http.ResponseWriter, r *http.Request) {
	paused, reason := s.jobManager.AdmissionState()
	state := "accepting"
	if paused {
		state = "paused"
	}
	writeJSONResponse(w, map[string]interface{}{
		"paused": paused,
		"state":  state,
		"reason": reason,
	})
}

func (s *Server) handleAdmissionPause(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, "Invalid form payload", http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(r.FormValue("message"))
	changed := s.jobManager.PauseAdmission(message)
	paused, reason := s.jobManager.AdmissionState()
	state := "accepting"
	if paused {
		state = "paused"
	}
	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"changed": changed,
		"paused":  paused,
		"state":   state,
		"reason":  reason,
	})
}

func (s *Server) handleAdmissionResume(w http.ResponseWriter, r *http.Request) {
	changed := s.jobManager.ResumeAdmission()
	paused, reason := s.jobManager.AdmissionState()
	state := "accepting"
	if paused {
		state = "paused"
	}
	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"changed": changed,
		"paused":  paused,
		"state":   state,
		"reason":  reason,
	})
}

func parseAdminBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := strings.TrimSpace(vars["id"])
	if jobID == "" {
		writeJSONError(w, "Missing job ID", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, "Invalid form payload", http.StatusBadRequest)
		return
	}
	admissionBypass := parseAdminBool(r.FormValue("admission_bypass"))

	resp, err := s.jobManager.RetryJob(r.Context(), jobID, jobs.RetryJobOptions{
		AdmissionBypass: admissionBypass,
	})
	if err != nil {
		statusCode := http.StatusInternalServerError
		body := map[string]interface{}{
			"error": err.Error(),
		}
		switch {
		case jobs.IsAdmissionPausedError(err):
			statusCode = http.StatusServiceUnavailable
			body["code"] = "ADMISSION_PAUSED"
			if reason, ok := jobs.AdmissionPauseFromError(err); ok && reason != nil {
				body["details"] = reason
			}
		case errors.Is(err, storage.ErrNotFound):
			statusCode = http.StatusNotFound
		case errors.Is(err, jobs.ErrJobStateConflict):
			statusCode = http.StatusConflict
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"success":            true,
		"source_job_id":      jobID,
		"job_id":             resp.JobID,
		"workflow_id":        resp.WorkflowID,
		"status":             resp.Status,
		"duplicate":          resp.Duplicate,
		"admission_bypass":   admissionBypass,
		"dedup_bypassed":     resp.DedupBypassed,
		"dedup_reason":       resp.DedupReason,
		"dedup_source_job":   resp.DedupSourceJobID,
		"dedup_source_state": resp.DedupSourceStatus,
	})
}
