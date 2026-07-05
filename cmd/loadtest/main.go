// Package main provides a load testing CLI for the Consortium platform.
// It tests stream resilience, idempotency, and reconnection scenarios.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alhasaniq/consortium/internal/appenv"
	"github.com/gorilla/websocket"
)

// Scenario types
const (
	ScenarioSubmitStream = "submit_stream"
	ScenarioSubmitCancel = "submit_cancel"
	ScenarioReconnect    = "reconnect"
	ScenarioDurability   = "durability"
	ScenarioMixed        = "mixed"
	ScenarioOpenAI       = "openai"
)

// Config holds load test configuration
type Config struct {
	BaseURL      string
	Concurrency  int
	Duration     time.Duration
	Scenario     string
	Verbose      bool
	OpenAIAPIKey string
	OpenAIModel  string
}

// Metrics tracks test results
type Metrics struct {
	mu sync.Mutex

	SubmitLatencies    []time.Duration
	StreamLatencies    []time.Duration
	ReconnectSuccesses int64
	ReconnectFailures  int64
	EventLossCount     int64
	ErrorCount         int64
	SuccessCount       int64
	CancelCount        int64
	DuplicateCount     int64

	// Durability metrics
	DurabilityChecks   int64
	DurabilityFailures int64
	SnapshotSuccesses  int64
	SnapshotFailures   int64

	startTime time.Time
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		SubmitLatencies: make([]time.Duration, 0, 1000),
		StreamLatencies: make([]time.Duration, 0, 1000),
		startTime:       time.Now(),
	}
}

// AddSubmitLatency records a submit latency
func (m *Metrics) AddSubmitLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SubmitLatencies = append(m.SubmitLatencies, d)
}

// AddStreamLatency records a stream latency
func (m *Metrics) AddStreamLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StreamLatencies = append(m.StreamLatencies, d)
}

// Percentiles calculates p50, p95, p99 for a slice of durations
func Percentiles(durations []time.Duration) (p50, p95, p99 time.Duration) {
	if len(durations) == 0 {
		return 0, 0, 0
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	p50 = sorted[len(sorted)*50/100]
	p95 = sorted[len(sorted)*95/100]
	p99 = sorted[len(sorted)*99/100]
	return
}

// PrintSummary outputs the test results
func (m *Metrics) PrintSummary() {
	m.mu.Lock()
	defer m.mu.Unlock()

	elapsed := time.Since(m.startTime)

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("LOAD TEST SUMMARY")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("\nDuration: %v\n", elapsed.Round(time.Second))
	fmt.Printf("Total Requests: %d\n", len(m.SubmitLatencies))
	fmt.Printf("Requests/sec: %.2f\n", float64(len(m.SubmitLatencies))/elapsed.Seconds())

	fmt.Println("\n--- Results ---")
	fmt.Printf("Successes:    %d\n", m.SuccessCount)
	fmt.Printf("Errors:       %d\n", m.ErrorCount)
	fmt.Printf("Cancellations: %d\n", m.CancelCount)
	fmt.Printf("Duplicates:   %d\n", m.DuplicateCount)

	fmt.Println("\n--- Reconnection ---")
	fmt.Printf("Reconnect Success: %d\n", m.ReconnectSuccesses)
	fmt.Printf("Reconnect Failure: %d\n", m.ReconnectFailures)
	fmt.Printf("Event Loss Count:  %d\n", m.EventLossCount)

	if m.EventLossCount > 0 {
		fmt.Println("\n⚠️  WARNING: Event loss detected!")
	} else {
		fmt.Println("\n✓ No event loss detected")
	}

	if m.DurabilityChecks > 0 {
		fmt.Println("\n--- Durability ---")
		fmt.Printf("Durability Checks:   %d\n", m.DurabilityChecks)
		fmt.Printf("Durability Failures: %d\n", m.DurabilityFailures)
		fmt.Printf("Snapshot Successes:  %d\n", m.SnapshotSuccesses)
		fmt.Printf("Snapshot Failures:   %d\n", m.SnapshotFailures)

		if m.DurabilityFailures > 0 {
			fmt.Println("\n⚠️  WARNING: Durability failures detected!")
		} else {
			fmt.Println("\n✓ All durability checks passed")
		}
	}

	fmt.Println("\n--- Submit Latency ---")
	if len(m.SubmitLatencies) > 0 {
		p50, p95, p99 := Percentiles(m.SubmitLatencies)
		fmt.Printf("p50: %v\n", p50.Round(time.Millisecond))
		fmt.Printf("p95: %v\n", p95.Round(time.Millisecond))
		fmt.Printf("p99: %v\n", p99.Round(time.Millisecond))
	} else {
		fmt.Println("No data")
	}

	fmt.Println("\n--- Stream Latency (first event) ---")
	if len(m.StreamLatencies) > 0 {
		p50, p95, p99 := Percentiles(m.StreamLatencies)
		fmt.Printf("p50: %v\n", p50.Round(time.Millisecond))
		fmt.Printf("p95: %v\n", p95.Round(time.Millisecond))
		fmt.Printf("p99: %v\n", p99.Round(time.Millisecond))
	} else {
		fmt.Println("No data")
	}

	fmt.Println(strings.Repeat("=", 60))
}

// Worker runs load test scenarios
type Worker struct {
	id      int
	config  *Config
	metrics *Metrics
	client  *http.Client
}

// NewWorker creates a new test worker
func NewWorker(id int, config *Config, metrics *Metrics) *Worker {
	return &Worker{
		id:      id,
		config:  config,
		metrics: metrics,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Run executes scenarios until context is cancelled
func (w *Worker) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	if w.config.Scenario == ScenarioOpenAI {
		w.runOpenAI(ctx)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			w.runScenario(ctx)
		}
	}
}

func (w *Worker) runScenario(ctx context.Context) {
	scenario := w.config.Scenario
	if scenario == ScenarioMixed {
		// Random scenario selection
		scenarios := []string{ScenarioSubmitStream, ScenarioSubmitCancel, ScenarioReconnect, ScenarioDurability}
		scenario = scenarios[rand.Intn(len(scenarios))]
	}

	switch scenario {
	case ScenarioSubmitStream:
		w.runSubmitStream(ctx)
	case ScenarioSubmitCancel:
		w.runSubmitCancel(ctx)
	case ScenarioReconnect:
		w.runReconnect(ctx)
	case ScenarioDurability:
		w.runDurability(ctx)
	case ScenarioOpenAI:
		w.runOpenAI(ctx)
	}
}

func (w *Worker) runOpenAI(ctx context.Context) {
	started := time.Now()
	if err := w.runOpenAIGate(ctx); err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] OpenAI scenario error: %v", w.id, err)
		}
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}
	w.metrics.AddSubmitLatency(time.Since(started))
	atomic.AddInt64(&w.metrics.SuccessCount, 1)
}

func (w *Worker) runOpenAIGate(ctx context.Context) error {
	if err := w.checkOpenAIModels(ctx); err != nil {
		return err
	}
	if _, err := w.createOpenAIChat(ctx, false, "", "OpenAI loadtest chat"); err != nil {
		return err
	}
	if _, err := w.createOpenAIResponse(ctx, false, false, "", "OpenAI loadtest response"); err != nil {
		return err
	}
	if err := w.streamOpenAIChat(ctx); err != nil {
		return err
	}
	if err := w.streamOpenAIResponse(ctx); err != nil {
		return err
	}
	if err := w.checkOpenAIBackground(ctx); err != nil {
		return err
	}
	if err := w.checkOpenAIIdempotency(ctx); err != nil {
		return err
	}
	return nil
}

func (w *Worker) checkOpenAIModels(ctx context.Context) error {
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := w.doOpenAIJSON(ctx, http.MethodGet, "/v1/models", nil, "", &result); err != nil {
		return fmt.Errorf("openai models: %w", err)
	}
	if len(result.Data) == 0 {
		return fmt.Errorf("openai models returned no data")
	}
	return nil
}

func (w *Worker) createOpenAIChat(ctx context.Context, stream bool, idempotencyKey, prompt string) (string, error) {
	body := map[string]interface{}{
		"model": w.openAIModel(),
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if stream {
		body["stream"] = true
		if err := w.doOpenAIStream(ctx, "/v1/chat/completions", body, idempotencyKey); err != nil {
			return "", fmt.Errorf("openai chat stream: %w", err)
		}
		return "", nil
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := w.doOpenAIJSON(ctx, http.MethodPost, "/v1/chat/completions", body, idempotencyKey, &result); err != nil {
		return "", fmt.Errorf("openai chat: %w", err)
	}
	if strings.TrimSpace(result.ID) == "" {
		return "", fmt.Errorf("openai chat response missing id")
	}
	return result.ID, nil
}

func (w *Worker) createOpenAIResponse(ctx context.Context, stream, background bool, idempotencyKey, prompt string) (string, error) {
	body := map[string]interface{}{
		"model": w.openAIModel(),
		"input": prompt,
	}
	if stream {
		body["stream"] = true
		if err := w.doOpenAIStream(ctx, "/v1/responses", body, idempotencyKey); err != nil {
			return "", fmt.Errorf("openai responses stream: %w", err)
		}
		return "", nil
	}
	if background {
		body["background"] = true
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := w.doOpenAIJSON(ctx, http.MethodPost, "/v1/responses", body, idempotencyKey, &result); err != nil {
		return "", fmt.Errorf("openai responses: %w", err)
	}
	if strings.TrimSpace(result.ID) == "" {
		return "", fmt.Errorf("openai response missing id")
	}
	if background && result.Status != "in_progress" && result.Status != "queued" && result.Status != "completed" {
		return "", fmt.Errorf("openai background response status %q", result.Status)
	}
	return result.ID, nil
}

func (w *Worker) streamOpenAIChat(ctx context.Context) error {
	started := time.Now()
	_, err := w.createOpenAIChat(ctx, true, "", "OpenAI loadtest chat stream")
	if err != nil {
		return err
	}
	w.metrics.AddStreamLatency(time.Since(started))
	return nil
}

func (w *Worker) streamOpenAIResponse(ctx context.Context) error {
	started := time.Now()
	_, err := w.createOpenAIResponse(ctx, true, false, "", "OpenAI loadtest response stream")
	if err != nil {
		return err
	}
	w.metrics.AddStreamLatency(time.Since(started))
	return nil
}

func (w *Worker) checkOpenAIBackground(ctx context.Context) error {
	responseID, err := w.createOpenAIResponse(ctx, false, true, "", "OpenAI loadtest background")
	if err != nil {
		return err
	}
	if err := w.pollOpenAIBackgroundResponse(ctx, responseID); err != nil {
		return err
	}
	if err := w.fetchOpenAIResponseInputItems(ctx, responseID); err != nil {
		return err
	}

	cancelID, err := w.createOpenAIResponse(ctx, false, true, "", "OpenAI loadtest cancel")
	if err != nil {
		return err
	}
	if err := w.cancelOpenAIResponse(ctx, cancelID); err != nil {
		return err
	}
	atomic.AddInt64(&w.metrics.CancelCount, 1)
	return nil
}

func (w *Worker) pollOpenAIBackgroundResponse(ctx context.Context, responseID string) error {
	path := "/v1/responses/" + responseID
	for attempt := 0; attempt < 60; attempt++ {
		var result struct {
			Status string `json:"status"`
		}
		if err := w.doOpenAIJSON(ctx, http.MethodGet, path, nil, "", &result); err != nil {
			return fmt.Errorf("openai background retrieve: %w", err)
		}
		switch result.Status {
		case "completed":
			return nil
		case "failed", "cancelled":
			return fmt.Errorf("openai background response reached status %q", result.Status)
		}

		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("openai background response %s did not complete before poll limit", responseID)
}

func (w *Worker) fetchOpenAIResponseInputItems(ctx context.Context, responseID string) error {
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := w.doOpenAIJSON(ctx, http.MethodGet, "/v1/responses/"+responseID+"/input_items", nil, "", &result); err != nil {
		return fmt.Errorf("openai response input_items: %w", err)
	}
	return nil
}

func (w *Worker) cancelOpenAIResponse(ctx context.Context, responseID string) error {
	req, err := w.newOpenAIRequest(ctx, http.MethodPost, "/v1/responses/"+responseID+"/cancel", nil, "")
	if err != nil {
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("openai response cancel returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (w *Worker) checkOpenAIIdempotency(ctx context.Context) error {
	idempotencyKey := fmt.Sprintf("loadtest-openai-%d-%d", w.id, time.Now().UnixNano())
	firstID, err := w.createOpenAIChat(ctx, false, idempotencyKey, "OpenAI loadtest idempotency")
	if err != nil {
		return err
	}
	secondID, err := w.createOpenAIChat(ctx, false, idempotencyKey, "OpenAI loadtest idempotency")
	if err != nil {
		return err
	}
	if firstID != secondID {
		return fmt.Errorf("openai idempotency replay returned id %q after %q", secondID, firstID)
	}
	atomic.AddInt64(&w.metrics.DuplicateCount, 1)
	return nil
}

// runSubmitStream submits a workflow and streams to completion
func (w *Worker) runSubmitStream(ctx context.Context) {
	// Generate unique prompt
	prompt := fmt.Sprintf("Load test query %d at %d", w.id, time.Now().UnixNano())
	idempotencyKey := fmt.Sprintf("loadtest-%d-%d", w.id, time.Now().UnixNano())

	// Submit workflow
	submitStart := time.Now()
	jobID, duplicate, err := w.submitWorkflow(ctx, prompt, idempotencyKey)
	if err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] Submit error: %v", w.id, err)
		}
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}
	w.metrics.AddSubmitLatency(time.Since(submitStart))

	if duplicate {
		atomic.AddInt64(&w.metrics.DuplicateCount, 1)
		return
	}

	// Connect to stream
	streamStart := time.Now()
	err = w.streamToCompletion(ctx, jobID)
	if err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] Stream error: %v", w.id, err)
		}
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}
	w.metrics.AddStreamLatency(time.Since(streamStart))
	atomic.AddInt64(&w.metrics.SuccessCount, 1)
}

// runSubmitCancel submits a workflow and cancels it mid-execution
func (w *Worker) runSubmitCancel(ctx context.Context) {
	prompt := fmt.Sprintf("Cancel test query %d at %d", w.id, time.Now().UnixNano())
	idempotencyKey := fmt.Sprintf("loadtest-cancel-%d-%d", w.id, time.Now().UnixNano())

	// Submit workflow
	submitStart := time.Now()
	jobID, duplicate, err := w.submitWorkflow(ctx, prompt, idempotencyKey)
	if err != nil {
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}
	w.metrics.AddSubmitLatency(time.Since(submitStart))

	if duplicate {
		atomic.AddInt64(&w.metrics.DuplicateCount, 1)
		return
	}

	// Wait a short time then cancel
	time.Sleep(time.Duration(100+rand.Intn(500)) * time.Millisecond)

	err = w.cancelJob(ctx, jobID)
	if err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] Cancel error: %v", w.id, err)
		}
		// Not necessarily an error - job might have completed
	}
	atomic.AddInt64(&w.metrics.CancelCount, 1)
}

// runReconnect tests WebSocket reconnection with resume_from
func (w *Worker) runReconnect(ctx context.Context) {
	prompt := fmt.Sprintf("Reconnect test query %d at %d", w.id, time.Now().UnixNano())
	idempotencyKey := fmt.Sprintf("loadtest-reconnect-%d-%d", w.id, time.Now().UnixNano())

	// Submit workflow
	jobID, duplicate, err := w.submitWorkflow(ctx, prompt, idempotencyKey)
	if err != nil {
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}

	if duplicate {
		atomic.AddInt64(&w.metrics.DuplicateCount, 1)
		return
	}

	// Connect and collect some events
	lastSequence, events1, err := w.streamPartial(ctx, jobID, 0, 3)
	if err != nil {
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}

	// Disconnect briefly
	time.Sleep(100 * time.Millisecond)

	// Reconnect with resume_from
	_, events2, err := w.streamPartial(ctx, jobID, lastSequence, 100)
	if err != nil {
		atomic.AddInt64(&w.metrics.ReconnectFailures, 1)
		return
	}
	atomic.AddInt64(&w.metrics.ReconnectSuccesses, 1)

	// Check for duplicate events (indicates resume_from didn't work)
	seen := make(map[int64]bool)
	for _, seq := range events1 {
		seen[seq] = true
	}
	for _, seq := range events2 {
		if seen[seq] {
			atomic.AddInt64(&w.metrics.EventLossCount, 1)
			if w.config.Verbose {
				log.Printf("[Worker %d] Duplicate event %d detected after reconnect", w.id, seq)
			}
		}
	}

	atomic.AddInt64(&w.metrics.SuccessCount, 1)
}

// runDurability tests node recovery and status persistence
func (w *Worker) runDurability(ctx context.Context) {
	prompt := fmt.Sprintf("Durability test query %d at %d", w.id, time.Now().UnixNano())
	idempotencyKey := fmt.Sprintf("loadtest-durability-%d-%d", w.id, time.Now().UnixNano())

	// Submit workflow
	submitStart := time.Now()
	jobID, duplicate, err := w.submitWorkflow(ctx, prompt, idempotencyKey)
	if err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] Submit error: %v", w.id, err)
		}
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}
	w.metrics.AddSubmitLatency(time.Since(submitStart))

	if duplicate {
		atomic.AddInt64(&w.metrics.DuplicateCount, 1)
		return
	}

	// Stream to completion
	err = w.streamToCompletion(ctx, jobID)
	if err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] Stream error: %v", w.id, err)
		}
		atomic.AddInt64(&w.metrics.ErrorCount, 1)
		return
	}

	// Now verify durability by fetching job details
	atomic.AddInt64(&w.metrics.DurabilityChecks, 1)

	job, err := w.fetchJobDetails(ctx, jobID)
	if err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] Fetch job error: %v", w.id, err)
		}
		atomic.AddInt64(&w.metrics.DurabilityFailures, 1)
		return
	}

	// Verify job is in terminal state
	status, _ := job["status"].(string)
	if status != "completed" && status != "failed" && status != "cancelled" {
		if w.config.Verbose {
			log.Printf("[Worker %d] Job %s not in terminal state: %s", w.id, jobID, status)
		}
		atomic.AddInt64(&w.metrics.DurabilityFailures, 1)
		return
	}

	// Verify nodes exist and have execution IDs
	nodes, _ := job["nodes"].([]interface{})
	if len(nodes) == 0 {
		if w.config.Verbose {
			log.Printf("[Worker %d] Job %s has no nodes persisted", w.id, jobID)
		}
		atomic.AddInt64(&w.metrics.DurabilityFailures, 1)
		return
	}

	for _, n := range nodes {
		node, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		nodeStatus, _ := node["status"].(string)
		if nodeStatus != "completed" && nodeStatus != "failed" && nodeStatus != "cancelled" {
			if w.config.Verbose {
				log.Printf("[Worker %d] Node not in terminal state: %s", w.id, nodeStatus)
			}
			atomic.AddInt64(&w.metrics.DurabilityFailures, 1)
			return
		}
	}

	// Test terminal snapshot: reconnect to completed job
	err = w.verifyTerminalSnapshot(ctx, jobID)
	if err != nil {
		if w.config.Verbose {
			log.Printf("[Worker %d] Snapshot error: %v", w.id, err)
		}
		atomic.AddInt64(&w.metrics.SnapshotFailures, 1)
	} else {
		atomic.AddInt64(&w.metrics.SnapshotSuccesses, 1)
	}

	atomic.AddInt64(&w.metrics.SuccessCount, 1)
}

// fetchJobDetails retrieves job information via API
func (w *Worker) fetchJobDetails(ctx context.Context, jobID string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", w.config.BaseURL+"/api/jobs/"+jobID, nil)
	if err != nil {
		return nil, err
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch job returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// verifyTerminalSnapshot fetches a completed job's stream endpoint and verifies snapshot is returned
// Note: Server returns JSON (not WebSocket) for completed jobs
func (w *Worker) verifyTerminalSnapshot(ctx context.Context, jobID string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", w.config.BaseURL+"/api/jobs/"+jobID+"/stream", nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var snapshot map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}

	// Verify snapshot has required fields
	complete, _ := snapshot["complete"].(bool)
	if !complete {
		return fmt.Errorf("snapshot not marked complete")
	}

	status, _ := snapshot["status"].(string)
	if status != "completed" && status != "failed" && status != "cancelled" {
		return fmt.Errorf("unexpected status in snapshot: %s", status)
	}

	// Verify nodes are included
	nodes, _ := snapshot["nodes"].([]interface{})
	if len(nodes) == 0 {
		return fmt.Errorf("snapshot missing nodes")
	}

	return nil
}

// submitWorkflow submits a workflow and returns the job ID
func (w *Worker) submitWorkflow(ctx context.Context, prompt, idempotencyKey string) (string, bool, error) {
	workflow := map[string]interface{}{
		"id":   "loadtest-workflow",
		"name": "loadtest",
		"context": map[string]interface{}{
			"user_prompt": prompt,
		},
		"nodes": []map[string]interface{}{
			{
				"id":              "node1",
				"type":            "prompt",
				"model":           "google/gemini-2.5-flash-lite",
				"prompt":          prompt,
				"temperature":     0.0,
				"max_tokens":      128,
				"timeout_seconds": 30,
				"retry_policy": map[string]interface{}{
					"max_attempts":     1,
					"backoff_ms":       0,
					"backoff_multiply": 1,
					"max_backoff_ms":   0,
				},
			},
		},
		"edges": []map[string]interface{}{},
	}

	body := map[string]interface{}{
		"workflow":        workflow,
		"idempotency_key": idempotencyKey,
	}

	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", w.config.BaseURL+"/api/workflows/submit", strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if len(body) > 0 {
			return "", false, fmt.Errorf("submit returned status %d: %s", resp.StatusCode, string(body))
		}
		return "", false, fmt.Errorf("submit returned status %d", resp.StatusCode)
	}

	var result struct {
		JobID     string `json:"job_id"`
		Duplicate bool   `json:"duplicate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, err
	}

	return result.JobID, result.Duplicate, nil
}

// streamToCompletion connects to WebSocket and waits for completion
func (w *Worker) streamToCompletion(ctx context.Context, jobID string) error {
	url := strings.Replace(w.config.BaseURL, "http", "ws", 1) + "/api/jobs/" + jobID + "/stream"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, message, err := conn.ReadMessage()
		if err != nil {
			// Check if it was a normal close
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, 4000) {
				return nil
			}
			return err
		}

		var event map[string]interface{}
		if err := json.Unmarshal(message, &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		if eventType == "complete" || eventType == "cancelled" || eventType == "error" {
			return nil
		}
	}
}

// streamPartial connects and collects a limited number of events
func (w *Worker) streamPartial(ctx context.Context, jobID string, resumeFrom int64, maxEvents int) (int64, []int64, error) {
	url := strings.Replace(w.config.BaseURL, "http", "ws", 1) + "/api/jobs/" + jobID + "/stream"
	if resumeFrom > 0 {
		url = fmt.Sprintf("%s?resume_from=%d", url, resumeFrom)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = conn.Close() }()

	var lastSeq int64
	var sequences []int64
	eventCount := 0

	for eventCount < maxEvents {
		select {
		case <-ctx.Done():
			return lastSeq, sequences, ctx.Err()
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, message, err := conn.ReadMessage()
		if err != nil {
			// Timeout is expected for partial read
			if strings.Contains(err.Error(), "timeout") {
				return lastSeq, sequences, nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, 4000) {
				return lastSeq, sequences, nil
			}
			return lastSeq, sequences, err
		}

		var event map[string]interface{}
		if err := json.Unmarshal(message, &event); err != nil {
			continue
		}

		if seq, ok := event["sequence"].(float64); ok {
			lastSeq = int64(seq)
			sequences = append(sequences, lastSeq)
		}

		eventType, _ := event["type"].(string)
		if eventType == "complete" || eventType == "cancelled" || eventType == "error" {
			return lastSeq, sequences, nil
		}

		eventCount++
	}

	return lastSeq, sequences, nil
}

// cancelJob cancels a running job
func (w *Worker) cancelJob(ctx context.Context, jobID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", w.config.BaseURL+"/api/jobs/"+jobID+"/cancel", nil)
	if err != nil {
		return err
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("cancel returned status %d", resp.StatusCode)
	}

	return nil
}

func (w *Worker) doOpenAIJSON(ctx context.Context, method, path string, body interface{}, idempotencyKey string, out interface{}) error {
	req, err := w.newOpenAIRequest(ctx, method, path, body, idempotencyKey)
	if err != nil {
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s returned status %d: %s", method, path, resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}

func (w *Worker) doOpenAIStream(ctx context.Context, path string, body interface{}, idempotencyKey string) error {
	req, err := w.newOpenAIRequest(ctx, http.MethodPost, path, body, idempotencyKey)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s stream returned status %d: %s", path, resp.StatusCode, string(body))
	}

	seenData := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		seenData = true
		if data == "[DONE]" {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !seenData {
		return fmt.Errorf("%s stream returned no data events", path)
	}
	return nil
}

func (w *Worker) newOpenAIRequest(ctx context.Context, method, path string, body interface{}, idempotencyKey string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(jsonBody))
	}
	req, err := http.NewRequestWithContext(ctx, method, w.config.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.config.OpenAIAPIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		req.Header.Set("Idempotency-Key", strings.TrimSpace(idempotencyKey))
	}
	return req, nil
}

func (w *Worker) openAIModel() string {
	if strings.TrimSpace(w.config.OpenAIModel) != "" {
		return strings.TrimSpace(w.config.OpenAIModel)
	}
	return "gpt-4o-mini"
}

func (c *Config) openAIModelForPrint() string {
	if strings.TrimSpace(c.OpenAIModel) != "" {
		return strings.TrimSpace(c.OpenAIModel)
	}
	return "gpt-4o-mini"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func main() {
	appenv.LoadLocalEnv()

	// Parse flags
	baseURL := flag.String("url", appenv.BackendURL(), "Base URL of the server")
	concurrency := flag.Int("concurrency", 10, "Number of concurrent workers")
	duration := flag.Duration("duration", 30*time.Second, "Duration of the test")
	scenario := flag.String("scenario", "mixed", "Test scenario: submit_stream, submit_cancel, reconnect, durability, mixed, openai")
	openAIAPIKey := flag.String("openai-api-key", firstNonEmpty(os.Getenv("CONSORTIUM_OPENAI_API_KEY"), os.Getenv("OPENAI_API_KEY")), "OpenAI-compatible API key for scenario=openai")
	openAIModel := flag.String("openai-model", firstNonEmpty(os.Getenv("CONSORTIUM_OPENAI_MODEL"), "gpt-4o-mini"), "OpenAI-compatible model for scenario=openai")
	verbose := flag.Bool("verbose", false, "Verbose output")
	flag.Parse()

	// Validate scenario
	validScenarios := map[string]bool{
		ScenarioSubmitStream: true,
		ScenarioSubmitCancel: true,
		ScenarioReconnect:    true,
		ScenarioDurability:   true,
		ScenarioMixed:        true,
		ScenarioOpenAI:       true,
	}
	if !validScenarios[*scenario] {
		log.Fatalf("Invalid scenario: %s. Valid options: submit_stream, submit_cancel, reconnect, durability, mixed, openai", *scenario)
	}
	if *scenario == ScenarioOpenAI && strings.TrimSpace(*openAIAPIKey) == "" {
		log.Fatal("scenario=openai requires --openai-api-key or CONSORTIUM_OPENAI_API_KEY/OPENAI_API_KEY")
	}

	config := &Config{
		BaseURL:      *baseURL,
		Concurrency:  *concurrency,
		Duration:     *duration,
		Scenario:     *scenario,
		Verbose:      *verbose,
		OpenAIAPIKey: *openAIAPIKey,
		OpenAIModel:  *openAIModel,
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("CONSORTIUM LOAD TEST")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("URL:         %s\n", config.BaseURL)
	fmt.Printf("Concurrency: %d\n", config.Concurrency)
	fmt.Printf("Duration:    %v\n", config.Duration)
	fmt.Printf("Scenario:    %s\n", config.Scenario)
	if config.Scenario == ScenarioOpenAI {
		fmt.Printf("OpenAIModel: %s\n", config.openAIModelForPrint())
	}
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("Starting load test...")

	metrics := NewMetrics()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), config.Duration)
	defer cancel()

	// Handle interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\nInterrupted, stopping...")
		cancel()
	}()

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < config.Concurrency; i++ {
		worker := NewWorker(i, config, metrics)
		wg.Add(1)
		go worker.Run(ctx, &wg)
	}

	// Wait for completion
	wg.Wait()

	// Print results
	metrics.PrintSummary()
}
