package novomo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Status string

const (
	// TODO(v0.1-security): Keep localhost as the v0.1 dev default, but require
	// explicit NOVOMO_URL and transport/auth review for production agent runs.
	DefaultBaseURL         = "http://localhost:8090"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// HandoffRef is an opaque Novomo inheritance handle. Consortium validates only
// the envelope shape; Novomo owns the semantics for the referenced run/work.
type HandoffRef struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Policy string `json:"policy,omitempty"`
}

// BaseURL returns the normalized Novomo runtime URL configured for this
// client. Superagent wakes use it as the default runtime_url when a workflow
// node does not provide an override.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

type SubmitRunRequest struct {
	Prompt         string      `json:"prompt"`
	Harness        string      `json:"harness"`
	Sandbox        string      `json:"sandbox,omitempty"`
	TaskID         string      `json:"task_id,omitempty"`
	TimeoutSeconds int         `json:"timeout_seconds"`
	InheritFrom    *HandoffRef `json:"inherit_from,omitempty"`
	IdempotencyKey string      `json:"-"`
}

type SubmitRunResponse struct {
	RunID    string `json:"run_id"`
	JobRunID string `json:"job_run_id,omitempty"`
	Status   Status `json:"status"`
}

type Run struct {
	RunID         string
	TaskID        string
	Status        Status
	Output        string
	TokensInput   int
	TokensOutput  int
	CostUSD       float64
	ErrorCode     string
	ErrorMessage  string
	Harness       string
	StartedAt     *time.Time
	FinishedAt    *time.Time
	RawStatus     string
	RawJobRunID   string
	RawJobRunCode string
}

type SubmitNovoRunRequest struct {
	Goal           string                   `json:"-"`
	TaskID         string                   `json:"-"`
	TaskSummary    string                   `json:"-"`
	Identity       string                   `json:"-"`
	Image          string                   `json:"-"`
	Sandbox        string                   `json:"-"`
	RuntimeURL     string                   `json:"-"`
	TimeoutSeconds int                      `json:"-"`
	GraceSeconds   int                      `json:"-"`
	RepoSpecs      []map[string]interface{} `json:"-"`
	WorkSource     map[string]interface{}   `json:"-"`
	InheritFrom    *HandoffRef              `json:"-"`
	IdempotencyKey string                   `json:"-"`
}

type SubmitNovoRunResponse struct {
	NovoRunID string `json:"novo_run_id"`
	TaskID    string `json:"task_id"`
	Status    Status `json:"status"`
}

type NovoRun = Run

type Error struct {
	Code       string
	Message    string
	StatusCode int
	Retryable  bool
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func NewClientFromEnv() (*Client, error) {
	baseURL := strings.TrimSpace(os.Getenv("NOVOMO_URL"))
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return NewClient(Config{
		BaseURL: baseURL,
		APIKey:  os.Getenv("NOVOMO_API_KEY"),
	})
}

func NewClient(cfg Config) (*Client, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, &Error{Code: "NOVOMO_URL_MISSING", Message: "novomo base URL is required when constructing a client directly", Retryable: false}
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, &Error{Code: "NOVOMO_URL_INVALID", Message: fmt.Sprintf("invalid NOVOMO_URL %q", base), Retryable: false, Err: err}
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(base, "/"),
		apiKey:     strings.TrimSpace(cfg.APIKey),
		httpClient: httpClient,
	}, nil
}

func (c *Client) SubmitRun(ctx context.Context, req SubmitRunRequest) (*SubmitRunResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal novomo submit request: %w", err)
	}

	httpReq, err := c.newRequest(ctx, http.MethodPost, "/v1/runs", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		httpReq.Header.Set("Idempotency-Key", strings.TrimSpace(req.IdempotencyKey))
	}

	var resp struct {
		RunID    string `json:"run_id"`
		JobRunID string `json:"job_run_id"`
		Status   string `json:"status"`
	}
	if err := c.doJSON(httpReq, &resp); err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(resp.RunID)
	if runID == "" {
		return nil, &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "novomo submit response missing run_id", Retryable: false}
	}
	return &SubmitRunResponse{
		RunID:    runID,
		JobRunID: strings.TrimSpace(resp.JobRunID),
		Status:   normalizeStatus(resp.Status),
	}, nil
}

func (c *Client) GetRun(ctx context.Context, runID string) (*Run, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, &Error{Code: "INVALID_REQUEST", Message: "run id is required", Retryable: false}
	}

	httpReq, err := c.newRequest(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, err
	}

	var raw json.RawMessage
	if err := c.doJSON(httpReq, &raw); err != nil {
		return nil, err
	}
	run, err := decodeRun(raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(run.RunID) == "" {
		run.RunID = runID
	}
	return run, nil
}

func (c *Client) StopRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return &Error{Code: "INVALID_REQUEST", Message: "run id is required", Retryable: false}
	}
	return c.stop(ctx, "/v1/runs/"+url.PathEscape(runID)+"/stop")
}

func (c *Client) SubmitNovoRun(ctx context.Context, req SubmitNovoRunRequest) (*SubmitNovoRunResponse, error) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		if strings.TrimSpace(req.IdempotencyKey) == "" {
			return nil, &Error{Code: "INVALID_REQUEST", Message: "task_id or idempotency key is required", Retryable: false}
		}
		taskID = deterministicTaskID(req.IdempotencyKey)
		task, err := c.getTask(ctx, taskID)
		if err != nil {
			if nerr, ok := AsError(err); !ok || nerr.Code != "NOT_FOUND" {
				return nil, err
			}
			task, err = c.createTask(ctx, createTaskRequest{
				ID:      taskID,
				Goal:    strings.TrimSpace(req.Goal),
				Summary: strings.TrimSpace(req.TaskSummary),
			})
			if err != nil {
				return nil, err
			}
		}
		if task.TaskID != "" {
			taskID = task.TaskID
		}
		if strings.TrimSpace(task.CurrentNovoRunID) != "" {
			return &SubmitNovoRunResponse{
				NovoRunID: strings.TrimSpace(task.CurrentNovoRunID),
				TaskID:    taskID,
				Status:    StatusRunning,
			}, nil
		}
	} else {
		task, err := c.getTask(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(task.CurrentNovoRunID) != "" {
			return &SubmitNovoRunResponse{
				NovoRunID: strings.TrimSpace(task.CurrentNovoRunID),
				TaskID:    taskID,
				Status:    StatusRunning,
			}, nil
		}
	}

	wakeReq := wakeTaskRequest{
		Goal:        strings.TrimSpace(req.Goal),
		Identity:    strings.TrimSpace(req.Identity),
		Image:       strings.TrimSpace(req.Image),
		Sandbox:     strings.TrimSpace(req.Sandbox),
		RuntimeURL:  strings.TrimSpace(req.RuntimeURL),
		RepoSpecs:   req.RepoSpecs,
		WorkSource:  req.WorkSource,
		InheritFrom: req.InheritFrom,
	}
	if req.TimeoutSeconds > 0 {
		wakeReq.Timeout = (time.Duration(req.TimeoutSeconds) * time.Second).String()
	}
	if req.GraceSeconds > 0 {
		wakeReq.Grace = (time.Duration(req.GraceSeconds) * time.Second).String()
	}

	payload, err := json.Marshal(wakeReq)
	if err != nil {
		return nil, fmt.Errorf("marshal novomo wake request: %w", err)
	}
	httpReq, err := c.newRequest(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(taskID)+"/wake", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		httpReq.Header.Set("Idempotency-Key", strings.TrimSpace(req.IdempotencyKey))
	}

	var resp struct {
		TaskID    string `json:"task_id"`
		NovoRunID string `json:"novo_run_id"`
		Status    string `json:"status"`
	}
	if err := c.doJSON(httpReq, &resp); err != nil {
		return nil, err
	}
	novoRunID := strings.TrimSpace(resp.NovoRunID)
	if novoRunID == "" {
		return nil, &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "novomo wake response missing novo_run_id", Retryable: false}
	}
	return &SubmitNovoRunResponse{
		NovoRunID: novoRunID,
		TaskID:    coalesce(strings.TrimSpace(resp.TaskID), taskID),
		Status:    normalizeStatus(resp.Status),
	}, nil
}

func (c *Client) GetNovoRun(ctx context.Context, runID string) (*NovoRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, &Error{Code: "INVALID_REQUEST", Message: "novo run id is required", Retryable: false}
	}
	httpReq, err := c.newRequest(ctx, http.MethodGet, "/v1/novo-runs/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.doJSON(httpReq, &raw); err != nil {
		return nil, err
	}
	run, err := decodeNovoRun(raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(run.RunID) == "" {
		run.RunID = runID
	}
	return run, nil
}

func (c *Client) StopNovoRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return &Error{Code: "INVALID_REQUEST", Message: "novo run id is required", Retryable: false}
	}
	return c.stop(ctx, "/v1/novo-runs/"+url.PathEscape(runID)+"/stop")
}

func (c *Client) stop(ctx context.Context, path string) error {
	httpReq, err := c.newRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	return c.doJSON(httpReq, nil)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

func (c *Client) doJSON(req *http.Request, dest interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return classifyTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return classifyTransportError(readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyHTTPError(resp.StatusCode, body)
	}
	if dest == nil {
		return nil
	}
	if raw, ok := dest.(*json.RawMessage); ok {
		*raw = append((*raw)[:0], body...)
		return nil
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "decode novomo response: " + err.Error(), Retryable: false, Err: err}
	}
	return nil
}

type createTaskRequest struct {
	ID      string `json:"id,omitempty"`
	Goal    string `json:"goal"`
	Summary string `json:"summary,omitempty"`
}

type taskResponse struct {
	TaskID           string `json:"task_id"`
	Status           string `json:"status"`
	CurrentNovoRunID string `json:"current_novo_run_id"`
}

type wakeTaskRequest struct {
	Goal        string                   `json:"goal,omitempty"`
	Identity    string                   `json:"identity,omitempty"`
	Image       string                   `json:"image,omitempty"`
	Sandbox     string                   `json:"sandbox,omitempty"`
	RuntimeURL  string                   `json:"runtime_url,omitempty"`
	Timeout     string                   `json:"timeout,omitempty"`
	Grace       string                   `json:"grace,omitempty"`
	RepoSpecs   []map[string]interface{} `json:"repo_specs,omitempty"`
	WorkSource  map[string]interface{}   `json:"work_source,omitempty"`
	InheritFrom *HandoffRef              `json:"inherit_from,omitempty"`
}

func (c *Client) getTask(ctx context.Context, taskID string) (*taskResponse, error) {
	httpReq, err := c.newRequest(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(strings.TrimSpace(taskID)), nil)
	if err != nil {
		return nil, err
	}
	var resp taskResponse
	if err := c.doJSON(httpReq, &resp); err != nil {
		return nil, err
	}
	if strings.TrimSpace(resp.TaskID) == "" {
		resp.TaskID = strings.TrimSpace(taskID)
	}
	return &resp, nil
}

func (c *Client) createTask(ctx context.Context, body createTaskRequest) (*taskResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal novomo task request: %w", err)
	}
	httpReq, err := c.newRequest(ctx, http.MethodPost, "/v1/tasks", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var resp taskResponse
	if err := c.doJSON(httpReq, &resp); err != nil {
		return nil, err
	}
	if strings.TrimSpace(resp.TaskID) == "" {
		return nil, &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "novomo task response missing task_id", Retryable: false}
	}
	return &resp, nil
}

func classifyHTTPError(status int, body []byte) error {
	msg, bodyCode := parseHTTPErrorBody(body)
	switch status {
	case http.StatusBadRequest:
		if isUnknownSandboxFieldError(msg) {
			return &Error{
				Code:       "NOVOMO_UNSUPPORTED_SANDBOX_FIELD",
				Message:    defaultMessage(msg, "novomo runtime does not accept sandbox field; deploy Novomo per-job sandbox support before Consortium agent_run sandbox support"),
				StatusCode: status,
				Retryable:  false,
			}
		}
		return &Error{Code: "BAD_REQUEST", Message: defaultMessage(msg, "novomo rejected request"), StatusCode: status, Retryable: false}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &Error{Code: "AUTH", Message: defaultMessage(msg, "novomo authentication failed"), StatusCode: status, Retryable: false}
	case http.StatusNotFound:
		return &Error{Code: "NOT_FOUND", Message: defaultMessage(msg, "novomo run not found"), StatusCode: status, Retryable: false}
	case http.StatusConflict:
		code := "CONFLICT"
		if strings.EqualFold(bodyCode, "not_stoppable") || strings.Contains(strings.ToLower(msg), "not_stoppable") {
			code = "NOT_STOPPABLE"
		}
		return &Error{Code: code, Message: defaultMessage(msg, "novomo run is not stoppable"), StatusCode: status, Retryable: false}
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		return &Error{Code: "NOVOMO_UNAVAILABLE", Message: defaultMessage(msg, fmt.Sprintf("novomo unavailable: HTTP %d", status)), StatusCode: status, Retryable: true}
	}
	return &Error{Code: "NOVOMO_HTTP_ERROR", Message: defaultMessage(msg, fmt.Sprintf("novomo HTTP error: %d", status)), StatusCode: status, Retryable: false}
}

func parseHTTPErrorBody(body []byte) (message string, code string) {
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return "", ""
	}

	var payload struct {
		Error   interface{} `json:"error"`
		Code    string      `json:"code"`
		Message string      `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw, ""
	}

	code = strings.TrimSpace(payload.Code)
	message = strings.TrimSpace(payload.Message)
	switch errPayload := payload.Error.(type) {
	case string:
		if code == "" {
			code = strings.TrimSpace(errPayload)
		}
		if message == "" {
			message = strings.TrimSpace(errPayload)
		}
	case map[string]interface{}:
		if code == "" {
			if value, ok := errPayload["code"].(string); ok {
				code = strings.TrimSpace(value)
			}
		}
		if message == "" {
			if value, ok := errPayload["message"].(string); ok {
				message = strings.TrimSpace(value)
			}
		}
	}
	if message == "" {
		message = code
	}
	if message == "" {
		message = raw
	}
	return message, code
}

func isUnknownSandboxFieldError(msg string) bool {
	normalized := strings.ToLower(msg)
	return strings.Contains(normalized, "unknown field") && strings.Contains(normalized, "sandbox")
}

func classifyTransportError(err error) error {
	retryable := false
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		retryable = !errors.Is(err, context.Canceled)
	} else {
		var netErr net.Error
		retryable = errors.As(err, &netErr)
	}
	code := "NOVOMO_UNAVAILABLE"
	if errors.Is(err, context.Canceled) {
		code = "CANCELLED"
	}
	return &Error{Code: code, Message: err.Error(), Retryable: retryable, Err: err}
}

func decodeRun(raw json.RawMessage) (*Run, error) {
	var detail jobDetailResponse
	if err := json.Unmarshal(raw, &detail); err == nil && detail.Job != nil {
		return runFromJobDetail(&detail), nil
	}

	var flat flatRunResponse
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "decode novomo run response: " + err.Error(), Retryable: false, Err: err}
	}
	if strings.TrimSpace(flat.RunID) == "" && strings.TrimSpace(flat.Status) == "" {
		return nil, &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "novomo run response missing job or run_id", Retryable: false}
	}
	return runFromFlat(flat), nil
}

func decodeNovoRun(raw json.RawMessage) (*NovoRun, error) {
	var detail novoRunDetailResponse
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "decode novomo run response: " + err.Error(), Retryable: false, Err: err}
	}
	if detail.NovoRun == nil {
		return nil, &Error{Code: "NOVOMO_MALFORMED_RESPONSE", Message: "novomo run response missing novo_run", Retryable: false}
	}
	return runFromNovoRun(detail.NovoRun), nil
}

type jobDetailResponse struct {
	Job  *jobResponse  `json:"job"`
	Runs []runResponse `json:"runs"`
}

type jobResponse struct {
	ID             string   `json:"id"`
	TaskID         string   `json:"task_id"`
	Status         string   `json:"status"`
	Summary        string   `json:"summary"`
	Harness        string   `json:"harness"`
	TokensInput    int      `json:"tokens_input"`
	TokensOutput   int      `json:"tokens_output"`
	CostUSD        float64  `json:"cost_usd"`
	ErrorCode      string   `json:"error_code"`
	ErrorMessage   string   `json:"error_message"`
	StartedAt      timeText `json:"started_at"`
	FinishedAt     timeText `json:"finished_at"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type runResponse struct {
	ID           string `json:"id"`
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type flatRunResponse struct {
	RunID        string   `json:"run_id"`
	TaskID       string   `json:"task_id"`
	JobRunID     string   `json:"job_run_id"`
	Status       string   `json:"status"`
	Output       string   `json:"output"`
	TokensInput  int      `json:"tokens_input"`
	TokensOutput int      `json:"tokens_output"`
	CostUSD      float64  `json:"cost_usd"`
	Error        flatErr  `json:"error"`
	StartedAt    timeText `json:"started_at"`
	FinishedAt   timeText `json:"finished_at"`
}

type novoRunDetailResponse struct {
	NovoRun *novoRunResponse `json:"novo_run"`
}

type novoRunResponse struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Summary      string   `json:"summary"`
	Harness      string   `json:"harness"`
	TokensInput  *int     `json:"tokens_input"`
	TokensOutput *int     `json:"tokens_output"`
	CostUSD      *float64 `json:"cost_usd"`
	ErrorCode    string   `json:"error_code"`
	ErrorMessage string   `json:"error_message"`
	StartedAt    timeText `json:"started_at"`
	CompletedAt  timeText `json:"completed_at"`
	ActiveTaskID string   `json:"active_task_id"`
}

type flatErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type timeText struct {
	Time  *time.Time
	Valid bool
}

func (t *timeText) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil
	}
	t.Time = &parsed
	t.Valid = true
	return nil
}

func runFromJobDetail(detail *jobDetailResponse) *Run {
	job := detail.Job
	out := &Run{
		RunID:        strings.TrimSpace(job.ID),
		TaskID:       strings.TrimSpace(job.TaskID),
		Status:       normalizeStatus(job.Status),
		Output:       job.Summary,
		TokensInput:  job.TokensInput,
		TokensOutput: job.TokensOutput,
		CostUSD:      job.CostUSD,
		ErrorCode:    strings.TrimSpace(job.ErrorCode),
		ErrorMessage: strings.TrimSpace(job.ErrorMessage),
		Harness:      strings.TrimSpace(job.Harness),
		StartedAt:    job.StartedAt.Time,
		FinishedAt:   job.FinishedAt.Time,
		RawStatus:    strings.TrimSpace(job.Status),
	}
	latest := latestRun(detail.Runs)
	if latest != nil {
		out.RawJobRunID = latest.ID
		out.RawJobRunCode = strings.TrimSpace(coalesce(latest.ErrorCode, latest.Status))
		if out.Status == StatusFailed && out.ErrorCode == "" {
			out.ErrorCode = strings.TrimSpace(coalesce(latest.ErrorCode, latest.Status))
		}
		if out.ErrorMessage == "" {
			out.ErrorMessage = strings.TrimSpace(latest.ErrorMessage)
		}
	}
	if out.Status == StatusFailed && out.ErrorCode == "" {
		out.ErrorCode = "unknown"
	}
	if out.Status == StatusCancelled && out.ErrorCode == "" {
		out.ErrorCode = "cancelled"
	}
	return out
}

func runFromFlat(flat flatRunResponse) *Run {
	out := &Run{
		RunID:        strings.TrimSpace(flat.RunID),
		TaskID:       strings.TrimSpace(flat.TaskID),
		Status:       normalizeStatus(flat.Status),
		Output:       flat.Output,
		TokensInput:  flat.TokensInput,
		TokensOutput: flat.TokensOutput,
		CostUSD:      flat.CostUSD,
		ErrorCode:    strings.TrimSpace(flat.Error.Code),
		ErrorMessage: strings.TrimSpace(flat.Error.Message),
		StartedAt:    flat.StartedAt.Time,
		FinishedAt:   flat.FinishedAt.Time,
		RawStatus:    strings.TrimSpace(flat.Status),
		RawJobRunID:  strings.TrimSpace(flat.JobRunID),
	}
	if out.Status == StatusCancelled && out.ErrorCode == "" {
		out.ErrorCode = "cancelled"
	}
	return out
}

func runFromNovoRun(nr *novoRunResponse) *NovoRun {
	out := &Run{
		RunID:        strings.TrimSpace(nr.ID),
		TaskID:       strings.TrimSpace(nr.ActiveTaskID),
		Status:       normalizeStatus(nr.Status),
		Output:       nr.Summary,
		ErrorCode:    strings.TrimSpace(nr.ErrorCode),
		ErrorMessage: strings.TrimSpace(nr.ErrorMessage),
		Harness:      strings.TrimSpace(nr.Harness),
		StartedAt:    nr.StartedAt.Time,
		FinishedAt:   nr.CompletedAt.Time,
		RawStatus:    strings.TrimSpace(nr.Status),
	}
	if nr.TokensInput != nil {
		out.TokensInput = *nr.TokensInput
	}
	if nr.TokensOutput != nil {
		out.TokensOutput = *nr.TokensOutput
	}
	if nr.CostUSD != nil {
		out.CostUSD = *nr.CostUSD
	}
	if out.Status == StatusFailed && out.ErrorCode == "" {
		out.ErrorCode = strings.TrimSpace(coalesce(nr.ErrorCode, nr.Status, "unknown"))
	}
	if out.Status == StatusCancelled && out.ErrorCode == "" {
		out.ErrorCode = "cancelled"
	}
	return out
}

func latestRun(runs []runResponse) *runResponse {
	if len(runs) == 0 {
		return nil
	}
	return &runs[len(runs)-1]
}

func normalizeStatus(status string) Status {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return StatusCompleted
	case "failed", "timeout", "crashed", "startup_failed":
		return StatusFailed
	case "cancelled", "canceled":
		return StatusCancelled
	case "paused":
		return StatusFailed
	default:
		return StatusRunning
	}
}

func deterministicTaskID(idempotencyKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(idempotencyKey)))
	return "consortium-" + hex.EncodeToString(sum[:])[:24]
}

func coalesce(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultMessage(msg, fallback string) string {
	if strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	return fallback
}
