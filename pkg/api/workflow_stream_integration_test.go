package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func TestIntegration_JobStreamTerminalSnapshot(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	registry.Register(&MockExecuteProvider{
		name:     "mock",
		models:   []providers.Model{{ID: "mock-model", Provider: "mock"}},
		response: "Snapshot response",
	})

	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })

	api := NewWorkflowAPI(db, registry, manager)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	submitBody, _ := json.Marshal(map[string]interface{}{
		"workflow": map[string]interface{}{
			"name": "Terminal snapshot test",
			"nodes": []interface{}{
				strictPromptNodeMap("node1", "mock-model", "hello"),
			},
		},
		"force_new_run": true,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(submitBody)))
	if w.Code != http.StatusCreated {
		t.Fatalf("submit failed: %d - %s", w.Code, w.Body.String())
	}

	var submitResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&submitResp); err != nil {
		t.Fatalf("decode submit response failed: %v", err)
	}
	jobID, _ := submitResp["job_id"].(string)
	if jobID == "" {
		t.Fatal("expected job_id")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jw := httptest.NewRecorder()
		router.ServeHTTP(jw, httptest.NewRequest("GET", "/api/jobs/"+jobID, nil))
		var jr map[string]interface{}
		_ = json.NewDecoder(jw.Body).Decode(&jr)
		status, _ := jr["status"].(string)
		if status == events.JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	streamReq := httptest.NewRequest("GET", "/api/jobs/"+jobID+"/stream", nil)
	streamW := httptest.NewRecorder()
	router.ServeHTTP(streamW, streamReq)

	if streamW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", streamW.Code, streamW.Body.String())
	}

	var body map[string]interface{}
	if err := json.NewDecoder(streamW.Body).Decode(&body); err != nil {
		t.Fatalf("decode stream snapshot body failed: %v", err)
	}

	if got, _ := body["job_id"].(string); got != jobID {
		t.Fatalf("expected job_id %s, got %v", jobID, body["job_id"])
	}
	if got, _ := body["status"].(string); got != events.JobStatusCompleted {
		t.Fatalf("expected status %s, got %v", events.JobStatusCompleted, body["status"])
	}
	if complete, _ := body["complete"].(bool); !complete {
		t.Fatalf("expected complete=true, got %v", body["complete"])
	}
	if _, ok := body["snapshot_sequence"].(float64); !ok {
		t.Fatalf("expected numeric snapshot_sequence, got %T", body["snapshot_sequence"])
	}
	if nodes, ok := body["nodes"].([]interface{}); !ok || len(nodes) == 0 {
		t.Fatalf("expected nodes in terminal snapshot, got %v", body["nodes"])
	}
}

func TestIntegration_JobStreamResumeFromReplaysOnlyNewerEvents(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	jobID := "ws-resume-" + uuid.NewString()
	err = db.CreateExecution(&storage.Job{
		ID:                  jobID,
		Description:         "resume replay test",
		Model:               "workflow",
		Status:              events.JobStatusRunning,
		WorkflowID:          "wf-ws-resume",
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         "{}",
		DAGHash:             "resume-hash",
	})
	if err != nil {
		t.Fatalf("failed to create running job: %v", err)
	}

	ctx := context.Background()
	if _, err := db.AppendEventWithDetails(ctx, jobID, events.EventStatus, "", "queued", "", "", map[string]interface{}{
		"message": "queued",
	}); err != nil {
		t.Fatalf("append seq1 failed: %v", err)
	}
	if _, err := db.AppendEventWithDetails(ctx, jobID, events.EventNodeStart, "node1", "started", "", "", map[string]interface{}{
		"node_id": "node1",
		"message": "started",
	}); err != nil {
		t.Fatalf("append seq2 failed: %v", err)
	}
	if _, err := db.AppendEventWithDetails(ctx, jobID, events.EventNodeComplete, "node1", "completed", "", "", map[string]interface{}{
		"node_id": "node1",
		"message": "completed",
		"output":  "ok",
	}); err != nil {
		t.Fatalf("append seq3 failed: %v", err)
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = db.AppendEventWithDetails(context.Background(), jobID, events.EventComplete, "", "done", "", "", map[string]interface{}{
			"message": "done",
		})
		_ = db.UpdateExecutionStatus(jobID, events.JobStatusCompleted)
	}()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/jobs/" + jobID + "/stream?resume_from=1"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	defer conn.Close()

	var snapshot map[string]interface{}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("failed to read snapshot message: %v", err)
	}
	if typ, _ := snapshot["type"].(string); typ != "snapshot" {
		t.Fatalf("expected first ws message type=snapshot, got %v", snapshot["type"])
	}

	readReplaySeq := func() int64 {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var m map[string]interface{}
		if err := conn.ReadJSON(&m); err != nil {
			t.Fatalf("failed to read replay event: %v", err)
		}
		data, ok := m["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected replay event data map, got %T", m["data"])
		}
		seq, ok := data["sequence"].(float64)
		if !ok {
			t.Fatalf("expected numeric sequence in data, got %T", data["sequence"])
		}
		return int64(seq)
	}

	seqA := readReplaySeq()
	seqB := readReplaySeq()
	if seqA != 2 || seqB != 3 {
		t.Fatalf("expected replay sequences [2,3], got [%d,%d]", seqA, seqB)
	}

	sawSequenceOne := false
	closeCode := 0
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				closeCode = closeErr.Code
				break
			}
			t.Fatalf("unexpected ws read error: %v", err)
		}
		if data, ok := msg["data"].(map[string]interface{}); ok {
			if seq, ok := data["sequence"].(float64); ok && int64(seq) == 1 {
				sawSequenceOne = true
			}
		}
	}

	if closeCode != events.CloseNormal {
		t.Fatalf("expected close code %d, got %d", events.CloseNormal, closeCode)
	}
	if sawSequenceOne {
		t.Fatal("resume_from replay included old sequence=1 event")
	}
}

func TestIntegration_JobStreamCloseCodeMatchesTerminalStatus(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		expectedCode int
	}{
		{name: "completed", status: events.JobStatusCompleted, expectedCode: events.CloseNormal},
		{name: "failed", status: events.JobStatusFailed, expectedCode: events.CloseFailed},
		{name: "cancelled", status: events.JobStatusCancelled, expectedCode: events.CloseCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := storage.NewStorage(":memory:")
			if err != nil {
				t.Fatalf("Failed to create storage: %v", err)
			}

			registry := providers.NewRegistry()
			api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
			router := mux.NewRouter()
			api.RegisterRoutes(router)

			jobID := "ws-close-" + uuid.NewString()
			err = db.CreateExecution(&storage.Job{
				ID:                  jobID,
				Description:         "close code test",
				Model:               "workflow",
				Status:              events.JobStatusRunning,
				WorkflowID:          "wf-close",
				WorkflowExecutionID: jobID,
				RunID:               jobID,
				RunNumber:           1,
				DAGSnapshot:         "{}",
				DAGHash:             "close-hash",
			})
			if err != nil {
				t.Fatalf("failed to create running job: %v", err)
			}

			server := httptest.NewServer(router)
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/jobs/" + jobID + "/stream"
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("ws dial failed: %v", err)
			}
			defer conn.Close()

			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var snapshot map[string]interface{}
			if err := conn.ReadJSON(&snapshot); err != nil {
				t.Fatalf("failed to read snapshot message: %v", err)
			}
			if typ, _ := snapshot["type"].(string); typ != "snapshot" {
				t.Fatalf("expected snapshot message first, got %v", snapshot["type"])
			}

			go func() {
				time.Sleep(200 * time.Millisecond)
				_ = db.UpdateExecutionStatus(jobID, tt.status)
			}()

			closeCode := 0
			deadline := time.Now().Add(6 * time.Second)
			for time.Now().Before(deadline) {
				conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				var msg map[string]interface{}
				err := conn.ReadJSON(&msg)
				if err != nil {
					var closeErr *websocket.CloseError
					if errors.As(err, &closeErr) {
						closeCode = closeErr.Code
						break
					}
					t.Fatalf("unexpected ws read error: %v", err)
				}
			}

			if closeCode != tt.expectedCode {
				t.Fatalf("expected close code %d, got %d", tt.expectedCode, closeCode)
			}
		})
	}
}
