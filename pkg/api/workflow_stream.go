package api

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// handleJobStream handles WebSocket connections for streaming job execution events
// This endpoint does NOT trigger execution; it subscribes to durable events
// emitted by background workers and supports replay/resume.
// Query parameters:
//   - resume_from: sequence number to resume from (for reconnection)
func (api *WorkflowAPI) handleJobStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if jobID == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing job ID", nil)
		return
	}

	// Parse resume_from query parameter for reconnection support
	var resumeFrom int64 = 0
	if resumeStr := r.URL.Query().Get("resume_from"); resumeStr != "" {
		if parsed, err := strconv.ParseInt(resumeStr, 10, 64); err == nil {
			resumeFrom = parsed
		}
	}

	// Get the job to verify it exists and get current state
	job, err := api.storage.GetExecution(jobID)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "Job not found", err)
		return
	}

	ctx := r.Context()

	// If job is already completed or failed, return a snapshot (no WebSocket stream needed)
	if events.IsTerminalStatus(job.Status) {
		snapshot, snapErr := api.storage.GetJobSnapshot(ctx, jobID)
		if snapErr != nil {
			api.respondWithError(w, http.StatusInternalServerError, "Failed to get job snapshot", snapErr)
			return
		}

		// Build node data for response
		nodeData := mapWorkflowNodesToResponse(snapshot.Nodes, nodeResponseOpts{})

		api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
			"job_id":            jobID,
			"status":            job.Status,
			"complete":          true,
			"message":           "Job already finished",
			"snapshot_sequence": snapshot.SnapshotSequence,
			"result_text":       snapshot.FinalOutput,
			"nodes":             nodeData,
		})
		return
	}

	// Only pending and running jobs can be streamed.
	if job.Status != events.JobStatusPending && job.Status != events.JobStatusRunning {
		api.respondWithError(w, http.StatusConflict, "Job is in invalid state for streaming", nil)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	var connWriteMu sync.Mutex

	sendMessage := func(msg ExecutionMessage) error {
		connWriteMu.Lock()
		defer connWriteMu.Unlock()
		return api.sendMessage(conn, msg)
	}

	// Determine close code based on final job status
	var finalStatus string
	defer func() {
		closeCode := events.StatusToCloseCode(finalStatus)
		if finalStatus == "" {
			closeCode = events.CloseNormal
		}
		closeMsg := websocket.FormatCloseMessage(closeCode, events.CloseCodeToReason(closeCode))
		connWriteMu.Lock()
		err := conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		connWriteMu.Unlock()
		if err != nil {
			log.Printf("Error sending close message: %v", err)
		}
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Error closing WebSocket connection: %v", closeErr)
		}
	}()

	// Get the workflow from the job's request data
	// Start ping/pong keepalive
	pingTicker := time.NewTicker(30 * time.Second)
	pingDone := make(chan struct{})
	connClosed := make(chan struct{})
	var connClosedOnce sync.Once
	markConnClosed := func() {
		connClosedOnce.Do(func() {
			close(connClosed)
		})
	}
	defer func() {
		pingTicker.Stop()
		close(pingDone)
		markConnClosed()
	}()

	// Set up pong handler with read deadline
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	// Start ping goroutine
	go func() {
		for {
			select {
			case <-pingTicker.C:
				connWriteMu.Lock()
				if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
					connWriteMu.Unlock()
					markConnClosed()
					return
				}
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					connWriteMu.Unlock()
					markConnClosed()
					return
				}
				connWriteMu.Unlock()
			case <-pingDone:
				return
			}
		}
	}()

	sendSnapshot := func(snapshot *storage.JobSnapshot) error {
		snapshotMsg := map[string]interface{}{
			"type":              "snapshot",
			"snapshot_sequence": snapshot.SnapshotSequence,
			"job": map[string]interface{}{
				"id":           snapshot.Job.ID,
				"status":       snapshot.Job.Status,
				"workflow_id":  snapshot.Job.WorkflowID,
				"tokens_total": snapshot.Job.TokensTotal,
				"cost":         snapshot.Job.Cost,
				"error":        snapshot.Job.ErrorMessage,
			},
		}
		connWriteMu.Lock()
		defer connWriteMu.Unlock()
		if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		return conn.WriteJSON(snapshotMsg)
	}

	sendEventsAfter := func(after int64) (int64, error) {
		lastSequence := after
		replayEvents, replayErr := api.storage.GetEventsAfter(ctx, jobID, after)
		if replayErr != nil {
			return lastSequence, replayErr
		}
		for _, event := range replayEvents {
			msg := ExecutionMessage{
				Type:      event.Type,
				JobID:     event.JobID,
				NodeID:    event.NodeID,
				Message:   event.Message,
				Error:     event.Error,
				Code:      event.Code,
				Timestamp: event.Timestamp,
			}
			if output, ok := event.Payload["output"].(string); ok {
				msg.Output = output
			}
			// Build msg.Data by flattening the nested "data" sub-object and
			// carrying over non-structural keys — matching the format the
			// old direct-callback path produced.
			data := make(map[string]interface{})
			data["sequence"] = event.Sequence
			if event.ExecutionID != "" {
				data["execution_id"] = event.ExecutionID
			}
			if event.RunID != "" {
				data["run_id"] = event.RunID
			}
			if event.AgentRunID != "" {
				data["agent_run_id"] = event.AgentRunID
			}
			if event.Iteration > 0 {
				data["iteration"] = event.Iteration
			}
			if rawData, ok := event.Payload["data"].(map[string]interface{}); ok {
				for k, v := range rawData {
					data[k] = v
				}
			}
			for k, v := range event.Payload {
				switch k {
				case "job_id", "node_id", "message", "output", "error", "code", "data":
					// Already extracted into typed fields or flattened above.
				default:
					data[k] = v
				}
			}
			msg.Data = data
			if err := sendMessage(msg); err != nil {
				markConnClosed()
				return lastSequence, err
			}
			lastSequence = event.Sequence
		}
		return lastSequence, nil
	}

	// Subscribe-only: send snapshot, replay missed events, then tail until terminal.
	// Background workers handle execution — this handler never triggers it.
	snapshot, snapErr := api.storage.GetJobSnapshot(ctx, jobID)
	if snapErr != nil {
		log.Printf("Failed to get snapshot for job stream: %v", snapErr)
		if sendErr := sendMessage(ExecutionMessage{
			Type:    EventError,
			JobID:   jobID,
			Message: "Failed to get job snapshot",
			Error:   snapErr.Error(),
			Code:    "SNAPSHOT_FAILED",
		}); sendErr != nil {
			log.Printf("Error sending error message: %v", sendErr)
		}
		finalStatus = events.JobStatusFailed
		return
	}

	if err := sendSnapshot(snapshot); err != nil {
		log.Printf("Error sending snapshot: %v", err)
		finalStatus = events.JobStatusFailed
		return
	}

	// Replay events from resume_from or snapshot sequence.
	lastSequence := snapshot.SnapshotSequence
	if resumeFrom > 0 {
		replayedSeq, replayErr := sendEventsAfter(resumeFrom)
		if replayErr != nil {
			log.Printf("Error replaying events: %v", replayErr)
			finalStatus = events.JobStatusFailed
			return
		}
		lastSequence = replayedSeq
	}

	// Tail new events until job reaches terminal state.
	pollTicker := time.NewTicker(500 * time.Millisecond)
	defer pollTicker.Stop()

	for {
		select {
		case <-connClosed:
			return
		case <-pollTicker.C:
		}

		replayedSeq, replayErr := sendEventsAfter(lastSequence)
		if replayErr != nil {
			log.Printf("Error streaming events for job %s: %v", jobID, replayErr)
			finalStatus = events.JobStatusFailed
			return
		}
		lastSequence = replayedSeq

		currentJob, getErr := api.storage.GetExecution(jobID)
		if getErr != nil {
			log.Printf("Failed to fetch job status for %s: %v", jobID, getErr)
			finalStatus = events.JobStatusFailed
			return
		}
		if events.IsTerminalStatus(currentJob.Status) {
			finalStatus = currentJob.Status
			// Flush any final events persisted before terminal status update.
			if _, err := sendEventsAfter(lastSequence); err != nil {
				log.Printf("Error flushing terminal events for job %s: %v", jobID, err)
			}
			return
		}
	}
}
