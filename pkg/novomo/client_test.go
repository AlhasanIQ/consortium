package novomo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientSubmitRunSendsAuthAndIdempotency(t *testing.T) {
	var capturedAuth string
	var capturedKey string
	var capturedReq SubmitRunRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedKey = r.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(r.Body).Decode(&capturedReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"novomo-1","status":"pending"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	result, err := client.SubmitRun(context.Background(), SubmitRunRequest{
		Prompt:         "do work",
		Harness:        "claude-code",
		Sandbox:        "docker",
		TimeoutSeconds: 30,
		IdempotencyKey: "wf:agent:1",
	})
	if err != nil {
		t.Fatalf("SubmitRun failed: %v", err)
	}

	if result.RunID != "novomo-1" {
		t.Fatalf("RunID = %q, want novomo-1", result.RunID)
	}
	if result.Status != StatusRunning {
		t.Fatalf("Status = %q, want %q", result.Status, StatusRunning)
	}
	if capturedAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", capturedAuth)
	}
	if capturedKey != "wf:agent:1" {
		t.Fatalf("Idempotency-Key = %q", capturedKey)
	}
	if capturedReq.Prompt != "do work" || capturedReq.Harness != "claude-code" || capturedReq.Sandbox != "docker" || capturedReq.TimeoutSeconds != 30 {
		t.Fatalf("unexpected request: %+v", capturedReq)
	}
}

func TestClientSubmitRunOmitsEmptySandbox(t *testing.T) {
	var rawBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"novomo-1","status":"pending"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if _, err := client.SubmitRun(context.Background(), SubmitRunRequest{
		Prompt:         "do work",
		Harness:        "claude-code",
		TimeoutSeconds: 30,
	}); err != nil {
		t.Fatalf("SubmitRun failed: %v", err)
	}
	if _, ok := rawBody["sandbox"]; ok {
		t.Fatalf("empty sandbox should be omitted, got body %+v", rawBody)
	}
}

func TestClientSubmitRunSendsInheritFrom(t *testing.T) {
	var rawBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"novomo-1","status":"pending"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if _, err := client.SubmitRun(context.Background(), SubmitRunRequest{
		Prompt:         "continue work",
		Harness:        "claude-code",
		TimeoutSeconds: 30,
		InheritFrom:    &HandoffRef{Kind: "job_run", ID: "run-123", Policy: "latest"},
	}); err != nil {
		t.Fatalf("SubmitRun failed: %v", err)
	}

	rawRef, ok := rawBody["inherit_from"].(map[string]interface{})
	if !ok {
		t.Fatalf("inherit_from not forwarded: %+v", rawBody)
	}
	if rawRef["kind"] != "job_run" || rawRef["id"] != "run-123" || rawRef["policy"] != "latest" {
		t.Fatalf("unexpected inherit_from: %+v", rawRef)
	}
}

func TestClientSubmitRunSendsTaskID(t *testing.T) {
	var rawBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"novomo-1","status":"pending"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if _, err := client.SubmitRun(context.Background(), SubmitRunRequest{
		Prompt:         "continue work",
		Harness:        "claude-code",
		TaskID:         "consortium-run-agent",
		TimeoutSeconds: 30,
	}); err != nil {
		t.Fatalf("SubmitRun failed: %v", err)
	}

	if rawBody["task_id"] != "consortium-run-agent" {
		t.Fatalf("task_id not forwarded: %+v", rawBody)
	}
}

func TestNewClientFromEnvDefaultsToLocalNovomoRuntime(t *testing.T) {
	t.Setenv("NOVOMO_URL", "")
	t.Setenv("NOVOMO_API_KEY", "")

	client, err := NewClientFromEnv()
	if err != nil {
		t.Fatalf("NewClientFromEnv failed: %v", err)
	}
	if client.BaseURL() != "http://localhost:8090" {
		t.Fatalf("BaseURL = %q, want http://localhost:8090", client.BaseURL())
	}
}

func TestClientGetRunParsesCurrentJobDetailCompleted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/novomo-1" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"job": {
				"id": "novomo-1",
				"status": "completed",
				"summary": "final output",
				"harness": "claude-code",
				"tokens_input": 12,
				"tokens_output": 7,
				"cost_usd": 0.42,
				"started_at": "2026-05-12T18:11:04Z",
				"finished_at": "2026-05-12T18:18:33Z"
			},
			"runs": [
				{"id":"run-a","job_id":"novomo-1","status":"completed"}
			]
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	run, err := client.GetRun(context.Background(), "novomo-1")
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if run.RunID != "novomo-1" || run.Status != StatusCompleted || run.Output != "final output" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.TokensInput != 12 || run.TokensOutput != 7 || run.CostUSD != 0.42 {
		t.Fatalf("unexpected usage: %+v", run)
	}
	if run.ErrorCode != "" || run.ErrorMessage != "" {
		t.Fatalf("completed run should not expose error fields: %+v", run)
	}
	if run.StartedAt == nil || run.FinishedAt == nil {
		t.Fatalf("expected parsed timestamps: %+v", run)
	}
}

func TestClientGetRunParsesCurrentJobDetailFailureFromJobRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"job": {
				"id": "novomo-1",
				"status": "failed",
				"error_message": "container exited",
				"tokens_input": 3,
				"tokens_output": 2,
				"cost_usd": 0.05
			},
			"runs": [
				{"id":"run-a","job_id":"novomo-1","status":"crashed","error_code":"crashed","error_message":"container exited"}
			]
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	run, err := client.GetRun(context.Background(), "novomo-1")
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if run.Status != StatusFailed {
		t.Fatalf("Status = %q", run.Status)
	}
	if run.ErrorCode != "crashed" || run.ErrorMessage != "container exited" {
		t.Fatalf("unexpected error fields: %+v", run)
	}
}

func TestClientGetRunParsesFlatCompatibilityResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"run_id": "flat-1",
			"job_run_id": "jobrun-flat-1",
			"status": "completed",
			"output": "flat output",
			"tokens_input": 4,
			"tokens_output": 5,
			"cost_usd": 0.11
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	run, err := client.GetRun(context.Background(), "flat-1")
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if run.RunID != "flat-1" || run.Status != StatusCompleted || run.Output != "flat output" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.RawJobRunID != "jobrun-flat-1" {
		t.Fatalf("RawJobRunID = %q, want jobrun-flat-1", run.RawJobRunID)
	}
}

func TestClientSubmitNovoRunCreatesTaskThenWakes(t *testing.T) {
	var createdTaskID string
	var createdGoal string
	var createdSummary string
	var wakeBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
			http.Error(w, "missing", http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks":
			var body struct {
				ID      string `json:"id"`
				Goal    string `json:"goal"`
				Summary string `json:"summary"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create task: %v", err)
			}
			createdTaskID = body.ID
			createdGoal = body.Goal
			createdSummary = body.Summary
			if !strings.HasPrefix(createdTaskID, "consortium-") || len(createdTaskID) > 64 {
				t.Fatalf("task id %q is not deterministic/sanitized", createdTaskID)
			}
			_, _ = w.Write([]byte(`{"task_id":"` + createdTaskID + `","goal":"wake goal","status":"queued","source":"http","created_at":"2026-05-28T10:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/"+createdTaskID+"/wake":
			if err := json.NewDecoder(r.Body).Decode(&wakeBody); err != nil {
				t.Fatalf("decode wake body: %v", err)
			}
			_, _ = w.Write([]byte(`{"task_id":"` + createdTaskID + `","novo_run_id":"nr-super-1","status":"started"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	resp, err := client.SubmitNovoRun(context.Background(), SubmitNovoRunRequest{
		Goal:           "wake goal",
		TaskSummary:    "wake summary",
		Identity:       "sde-novo",
		Sandbox:        "host",
		RuntimeURL:     "http://127.0.0.1:18082",
		TimeoutSeconds: 120,
		GraceSeconds:   5,
		RepoSpecs: []map[string]interface{}{{
			"name": "app",
			"source": map[string]interface{}{
				"type":      "host_path",
				"host_path": map[string]interface{}{"path": "/tmp/app"},
			},
		}},
		WorkSource: map[string]interface{}{
			"type":         "gitea_branch",
			"gitea_branch": map[string]interface{}{"branch_ref": "main"},
		},
		InheritFrom:    &HandoffRef{Kind: "novo_run", ID: "nr-prior", Policy: "latest"},
		IdempotencyKey: "exec-1:superagent:1",
	})
	if err != nil {
		t.Fatalf("SubmitNovoRun failed: %v", err)
	}
	if resp.NovoRunID != "nr-super-1" || resp.TaskID != createdTaskID || resp.Status != StatusRunning {
		t.Fatalf("unexpected submit response: %+v", resp)
	}
	if createdGoal != "wake goal" || createdSummary != "wake summary" {
		t.Fatalf("unexpected task create body goal=%q summary=%q", createdGoal, createdSummary)
	}
	if wakeBody["goal"] != "wake goal" || wakeBody["identity"] != "sde-novo" || wakeBody["sandbox"] != "host" {
		t.Fatalf("missing wake fields: %+v", wakeBody)
	}
	if wakeBody["timeout"] != "2m0s" || wakeBody["grace"] != "5s" {
		t.Fatalf("unexpected wake durations: %+v", wakeBody)
	}
	if _, ok := wakeBody["repo_specs"].([]interface{}); !ok {
		t.Fatalf("repo_specs not forwarded: %+v", wakeBody)
	}
	if _, ok := wakeBody["work_source"].(map[string]interface{}); !ok {
		t.Fatalf("work_source not forwarded: %+v", wakeBody)
	}
	rawRef, ok := wakeBody["inherit_from"].(map[string]interface{})
	if !ok {
		t.Fatalf("inherit_from not forwarded: %+v", wakeBody)
	}
	if rawRef["kind"] != "novo_run" || rawRef["id"] != "nr-prior" || rawRef["policy"] != "latest" {
		t.Fatalf("unexpected inherit_from: %+v", rawRef)
	}
}

func TestClientSubmitNovoRunAdoptsExecutingTaskLease(t *testing.T) {
	wakeCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/task-existing":
			_, _ = w.Write([]byte(`{"task_id":"task-existing","goal":"already running","status":"executing","source":"http","current_novo_run_id":"nr-existing","created_at":"2026-05-28T10:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/task-existing/wake":
			wakeCalled = true
			_, _ = w.Write([]byte(`{"task_id":"task-existing","novo_run_id":"nr-new","status":"started"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	resp, err := client.SubmitNovoRun(context.Background(), SubmitNovoRunRequest{
		TaskID:         "task-existing",
		TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatalf("SubmitNovoRun failed: %v", err)
	}
	if resp.NovoRunID != "nr-existing" || resp.TaskID != "task-existing" {
		t.Fatalf("expected adoption of existing lease, got %+v", resp)
	}
	if wakeCalled {
		t.Fatal("wake should not be called when task already has an active NovoRun lease")
	}
}

func TestClientSubmitNovoRunRequiresTaskOrIdempotency(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_, err = client.SubmitNovoRun(context.Background(), SubmitNovoRunRequest{Goal: "wake"})
	if err == nil {
		t.Fatal("expected error")
	}
	novomoErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected novomo error, got %T: %v", err, err)
	}
	if novomoErr.Code != "INVALID_REQUEST" || novomoErr.Retryable {
		t.Fatalf("unexpected error: %+v", novomoErr)
	}
}

func TestClientGetNovoRunParsesDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/novo-runs/nr-super-1" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"novo_run": {
				"id": "nr-super-1",
				"identity": "sde-novo",
				"goal": "wake goal",
				"status": "completed",
				"source": "cli",
				"runlog_path": "/tmp/nr-super-1",
				"harness": "claude-code",
				"sandbox": "host",
				"summary": "wake summary output",
				"tokens_input": 21,
				"tokens_output": 13,
				"cost_usd": 0.31,
				"active_task_id": "task-existing",
				"started_at": "2026-05-28T10:00:00Z",
				"completed_at": "2026-05-28T10:01:00Z"
			},
			"jobs": []
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	run, err := client.GetNovoRun(context.Background(), "nr-super-1")
	if err != nil {
		t.Fatalf("GetNovoRun failed: %v", err)
	}
	if run.RunID != "nr-super-1" || run.TaskID != "task-existing" || run.Status != StatusCompleted || run.Output != "wake summary output" {
		t.Fatalf("unexpected novo run: %+v", run)
	}
	if run.TokensInput != 21 || run.TokensOutput != 13 || run.CostUSD != 0.31 {
		t.Fatalf("unexpected usage: %+v", run)
	}
	if run.StartedAt == nil || run.FinishedAt == nil {
		t.Fatalf("expected parsed timestamps: %+v", run)
	}
}

func TestClientGetNovoRunPreservesCancelledStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"novo_run": {
				"id": "nr-cancelled",
				"status": "cancelled",
				"summary": "",
				"active_task_id": "task-cancelled"
			},
			"jobs": []
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	run, err := client.GetNovoRun(context.Background(), "nr-cancelled")
	if err != nil {
		t.Fatalf("GetNovoRun failed: %v", err)
	}
	if run.Status != StatusCancelled || run.ErrorCode != "cancelled" {
		t.Fatalf("expected cancelled status/error code, got %+v", run)
	}
}

func TestClientClassifiesHTTPFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		retryable bool
		code      string
	}{
		{name: "bad request", status: http.StatusBadRequest, retryable: false, code: "BAD_REQUEST"},
		{name: "old runtime rejects sandbox field", status: http.StatusBadRequest, body: `{"error":{"message":"json: unknown field \"sandbox\""}}`, retryable: false, code: "NOVOMO_UNSUPPORTED_SANDBOX_FIELD"},
		{name: "auth", status: http.StatusUnauthorized, retryable: false, code: "AUTH"},
		{name: "capacity", status: http.StatusServiceUnavailable, retryable: true, code: "NOVOMO_UNAVAILABLE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := tt.body
				if body == "" {
					body = "nope"
				}
				http.Error(w, body, tt.status)
			}))
			defer server.Close()

			client, err := NewClient(Config{BaseURL: server.URL})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}
			_, err = client.GetRun(context.Background(), "x")
			if err == nil {
				t.Fatal("expected error")
			}
			novomoErr, ok := AsError(err)
			if !ok {
				t.Fatalf("expected novomo Error, got %T", err)
			}
			if novomoErr.Retryable != tt.retryable || novomoErr.Code != tt.code {
				t.Fatalf("unexpected classification: %+v", novomoErr)
			}
		})
	}
}

func TestClientClassifiesContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	_, err = client.GetRun(ctx, "slow")
	if err == nil {
		t.Fatal("expected deadline error")
	}
	novomoErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected novomo Error, got %T", err)
	}
	if !novomoErr.Retryable || novomoErr.Code != "NOVOMO_UNAVAILABLE" {
		t.Fatalf("unexpected classification: %+v", novomoErr)
	}
	if !strings.Contains(novomoErr.Error(), "deadline") {
		t.Fatalf("error should mention deadline, got %q", novomoErr.Error())
	}
}

func TestClientStopRunPostsStopEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-1","status":"failed"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.StopRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("StopRun failed: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/runs/run-1/stop" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
}

func TestClientStopNovoRunPostsStopEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"novo_run_id":"novo-1","status":"cancelled"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.StopNovoRun(context.Background(), "novo-1"); err != nil {
		t.Fatalf("StopNovoRun failed: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/novo-runs/novo-1/stop" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
}

func TestClientStopRunClassifiesNotStoppable(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "string error",
			body: `{"error":"not_stoppable","message":"run is already terminal"}`,
		},
		{
			name: "object error",
			body: `{"error":{"code":"not_stoppable","message":"run is already terminal"}}`,
		},
		{
			name: "top level code",
			body: `{"code":"not_stoppable","message":"run is already terminal"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client, err := NewClient(Config{BaseURL: server.URL})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}
			err = client.StopRun(context.Background(), "run-1")
			if err == nil {
				t.Fatal("expected not_stoppable error")
			}
			novomoErr, ok := AsError(err)
			if !ok {
				t.Fatalf("expected novomo error, got %T: %v", err, err)
			}
			if novomoErr.StatusCode != http.StatusConflict || novomoErr.Code != "NOT_STOPPABLE" || novomoErr.Retryable {
				t.Fatalf("unexpected classification: %+v", novomoErr)
			}
		})
	}
}
