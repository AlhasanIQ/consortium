package jobs

import (
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// DefaultManagerConfig — default values
// ---------------------------------------------------------------------------

func TestDefaultManagerConfig_Defaults(t *testing.T) {
	// Ensure no env vars from the test environment interfere.
	clearConfigEnvVars(t)

	cfg := DefaultManagerConfig()

	if cfg.MaxConcurrentWorkflows != 150 {
		t.Errorf("MaxConcurrentWorkflows = %d, want 150", cfg.MaxConcurrentWorkflows)
	}
	if cfg.MaxParallelNodesPerWF != 32 {
		t.Errorf("MaxParallelNodesPerWF = %d, want 32", cfg.MaxParallelNodesPerWF)
	}
	if cfg.AdmissionTimeoutSeconds != 30 {
		t.Errorf("AdmissionTimeoutSeconds = %d, want 30", cfg.AdmissionTimeoutSeconds)
	}
	if cfg.WorkerCount != 300 {
		t.Errorf("WorkerCount = %d, want 300", cfg.WorkerCount)
	}
	if cfg.WorkerInitialCount != 10 {
		t.Errorf("WorkerInitialCount = %d, want 10", cfg.WorkerInitialCount)
	}
	if cfg.WorkerPollInterval != 100*time.Millisecond {
		t.Errorf("WorkerPollInterval = %v, want 100ms", cfg.WorkerPollInterval)
	}
	if cfg.WorkerClaimErrorBackoffMax != 2*time.Second {
		t.Errorf("WorkerClaimErrorBackoffMax = %v, want 2s", cfg.WorkerClaimErrorBackoffMax)
	}
	if cfg.WorkerIdleBackoffMax != time.Second {
		t.Errorf("WorkerIdleBackoffMax = %v, want 1s", cfg.WorkerIdleBackoffMax)
	}
	if cfg.ReasoningTraceTTL != 6*time.Hour {
		t.Errorf("ReasoningTraceTTL = %v, want 6h", cfg.ReasoningTraceTTL)
	}
	if cfg.ReasoningPurgeInterval != 30*time.Minute {
		t.Errorf("ReasoningPurgeInterval = %v, want 30m", cfg.ReasoningPurgeInterval)
	}
	if !cfg.PauseAdmissionOnTerminalFailure {
		t.Error("PauseAdmissionOnTerminalFailure should be true by default")
	}
}

// ---------------------------------------------------------------------------
// DefaultManagerConfig — env var overrides
// ---------------------------------------------------------------------------

func TestDefaultManagerConfig_EnvVarOverrides(t *testing.T) {
	clearConfigEnvVars(t)

	setenv(t, "MAX_CONCURRENT_WORKFLOWS", "50")
	setenv(t, "MAX_PARALLEL_NODES_PER_WORKFLOW", "8")
	setenv(t, "WORKER_COUNT", "200")
	setenv(t, "WORKER_INITIAL_COUNT", "5")
	setenv(t, "WORKER_POLL_INTERVAL_MS", "250")
	setenv(t, "WORKER_CLAIM_ERROR_BACKOFF_MAX_MS", "5000")
	setenv(t, "WORKER_IDLE_BACKOFF_MAX_MS", "3000")
	setenv(t, "REASONING_TRACE_TTL_SECONDS", "3600")
	setenv(t, "REASONING_PURGE_INTERVAL_SECONDS", "900")
	setenv(t, "PAUSE_ADMISSION_ON_TERMINAL_FAILURE", "false")

	cfg := DefaultManagerConfig()

	if cfg.MaxConcurrentWorkflows != 50 {
		t.Errorf("MaxConcurrentWorkflows = %d, want 50", cfg.MaxConcurrentWorkflows)
	}
	if cfg.MaxParallelNodesPerWF != 8 {
		t.Errorf("MaxParallelNodesPerWF = %d, want 8", cfg.MaxParallelNodesPerWF)
	}
	if cfg.WorkerCount != 200 {
		t.Errorf("WorkerCount = %d, want 200", cfg.WorkerCount)
	}
	if cfg.WorkerInitialCount != 5 {
		t.Errorf("WorkerInitialCount = %d, want 5", cfg.WorkerInitialCount)
	}
	if cfg.WorkerPollInterval != 250*time.Millisecond {
		t.Errorf("WorkerPollInterval = %v, want 250ms", cfg.WorkerPollInterval)
	}
	if cfg.WorkerClaimErrorBackoffMax != 5*time.Second {
		t.Errorf("WorkerClaimErrorBackoffMax = %v, want 5s", cfg.WorkerClaimErrorBackoffMax)
	}
	if cfg.WorkerIdleBackoffMax != 3*time.Second {
		t.Errorf("WorkerIdleBackoffMax = %v, want 3s", cfg.WorkerIdleBackoffMax)
	}
	if cfg.ReasoningTraceTTL != time.Hour {
		t.Errorf("ReasoningTraceTTL = %v, want 1h", cfg.ReasoningTraceTTL)
	}
	if cfg.ReasoningPurgeInterval != 15*time.Minute {
		t.Errorf("ReasoningPurgeInterval = %v, want 15m", cfg.ReasoningPurgeInterval)
	}
	if cfg.PauseAdmissionOnTerminalFailure {
		t.Error("PauseAdmissionOnTerminalFailure should be false when env=false")
	}
}

func TestDefaultManagerConfig_InvalidEnvVarsIgnored(t *testing.T) {
	clearConfigEnvVars(t)

	// Non-numeric / invalid values should be silently ignored; defaults persist.
	setenv(t, "MAX_CONCURRENT_WORKFLOWS", "not-a-number")
	setenv(t, "WORKER_COUNT", "")
	setenv(t, "WORKER_POLL_INTERVAL_MS", "abc")
	setenv(t, "PAUSE_ADMISSION_ON_TERMINAL_FAILURE", "maybe")

	cfg := DefaultManagerConfig()

	if cfg.MaxConcurrentWorkflows != 150 {
		t.Errorf("MaxConcurrentWorkflows = %d, want 150 (invalid env ignored)", cfg.MaxConcurrentWorkflows)
	}
	if cfg.WorkerCount != 300 {
		t.Errorf("WorkerCount = %d, want 300 (empty env ignored)", cfg.WorkerCount)
	}
	if cfg.WorkerPollInterval != 100*time.Millisecond {
		t.Errorf("WorkerPollInterval = %v, want 100ms (invalid env ignored)", cfg.WorkerPollInterval)
	}
	if !cfg.PauseAdmissionOnTerminalFailure {
		t.Error("PauseAdmissionOnTerminalFailure should remain true (invalid env ignored)")
	}
}

func TestDefaultManagerConfig_ZeroEnvVarsIgnored(t *testing.T) {
	clearConfigEnvVars(t)

	// Zero values should be rejected (env vars must be > 0 to be accepted).
	setenv(t, "MAX_CONCURRENT_WORKFLOWS", "0")
	setenv(t, "WORKER_COUNT", "0")
	setenv(t, "WORKER_INITIAL_COUNT", "0")

	cfg := DefaultManagerConfig()

	if cfg.MaxConcurrentWorkflows != 150 {
		t.Errorf("MaxConcurrentWorkflows = %d, want 150 (zero env ignored)", cfg.MaxConcurrentWorkflows)
	}
	if cfg.WorkerCount != 300 {
		t.Errorf("WorkerCount = %d, want 300 (zero env ignored)", cfg.WorkerCount)
	}
	if cfg.WorkerInitialCount != 10 {
		t.Errorf("WorkerInitialCount = %d, want 10 (zero env ignored)", cfg.WorkerInitialCount)
	}
}

// ---------------------------------------------------------------------------
// normalizeManagerConfig
// ---------------------------------------------------------------------------

func TestNormalizeManagerConfig_NilInput(t *testing.T) {
	cfg := normalizeManagerConfig(nil)
	if cfg == nil {
		t.Fatal("expected non-nil config from nil input")
	}
	if cfg.MaxConcurrentWorkflows != 150 {
		t.Errorf("MaxConcurrentWorkflows = %d, want 150", cfg.MaxConcurrentWorkflows)
	}
	if cfg.MaxParallelNodesPerWF != 32 {
		t.Errorf("MaxParallelNodesPerWF = %d, want 32", cfg.MaxParallelNodesPerWF)
	}
	if cfg.WorkerCount != 300 {
		t.Errorf("WorkerCount = %d, want 300", cfg.WorkerCount)
	}
}

func TestNormalizeManagerConfig_ZeroValues(t *testing.T) {
	cfg := normalizeManagerConfig(&ManagerConfig{})

	if cfg.MaxConcurrentWorkflows != 150 {
		t.Errorf("MaxConcurrentWorkflows = %d, want 150", cfg.MaxConcurrentWorkflows)
	}
	if cfg.MaxParallelNodesPerWF != 32 {
		t.Errorf("MaxParallelNodesPerWF = %d, want 32", cfg.MaxParallelNodesPerWF)
	}
	if cfg.AdmissionTimeoutSeconds != 30 {
		t.Errorf("AdmissionTimeoutSeconds = %d, want 30", cfg.AdmissionTimeoutSeconds)
	}
	if cfg.WorkerCount != 300 {
		t.Errorf("WorkerCount = %d, want 300", cfg.WorkerCount)
	}
	if cfg.WorkerInitialCount != 10 {
		t.Errorf("WorkerInitialCount = %d, want 10", cfg.WorkerInitialCount)
	}
	if cfg.WorkerPollInterval != 100*time.Millisecond {
		t.Errorf("WorkerPollInterval = %v, want 100ms", cfg.WorkerPollInterval)
	}
	if cfg.WorkerClaimErrorBackoffMax != 2*time.Second {
		t.Errorf("WorkerClaimErrorBackoffMax = %v, want 2s", cfg.WorkerClaimErrorBackoffMax)
	}
	if cfg.WorkerIdleBackoffMax != time.Second {
		t.Errorf("WorkerIdleBackoffMax = %v, want 1s", cfg.WorkerIdleBackoffMax)
	}
	if cfg.ReasoningTraceTTL != 6*time.Hour {
		t.Errorf("ReasoningTraceTTL = %v, want 6h", cfg.ReasoningTraceTTL)
	}
	if cfg.ReasoningPurgeInterval != 30*time.Minute {
		t.Errorf("ReasoningPurgeInterval = %v, want 30m", cfg.ReasoningPurgeInterval)
	}
}

func TestNormalizeManagerConfig_WorkerCountClamped(t *testing.T) {
	// WorkerCount must be >= MaxConcurrentWorkflows to prevent deadlock.
	cfg := normalizeManagerConfig(&ManagerConfig{
		MaxConcurrentWorkflows: 200,
		WorkerCount:            50, // less than MaxConcurrentWorkflows
	})
	if cfg.WorkerCount != 200 {
		t.Errorf("WorkerCount = %d, want 200 (clamped to MaxConcurrentWorkflows)", cfg.WorkerCount)
	}
}

func TestNormalizeManagerConfig_WorkerInitialCountClamped(t *testing.T) {
	// WorkerInitialCount must not exceed WorkerCount.
	cfg := normalizeManagerConfig(&ManagerConfig{
		MaxConcurrentWorkflows: 10,
		WorkerCount:            20,
		WorkerInitialCount:     50, // exceeds WorkerCount
	})
	if cfg.WorkerInitialCount != 20 {
		t.Errorf("WorkerInitialCount = %d, want 20 (clamped to WorkerCount)", cfg.WorkerInitialCount)
	}
}

func TestNormalizeManagerConfig_ValidValuesPreserved(t *testing.T) {
	input := &ManagerConfig{
		MaxConcurrentWorkflows:          10,
		MaxParallelNodesPerWF:           4,
		AdmissionTimeoutSeconds:         60,
		WorkerCount:                     20,
		WorkerInitialCount:              5,
		WorkerPollInterval:              50 * time.Millisecond,
		WorkerClaimErrorBackoffMax:      500 * time.Millisecond,
		WorkerIdleBackoffMax:            750 * time.Millisecond,
		ReasoningTraceTTL:               1 * time.Hour,
		ReasoningPurgeInterval:          10 * time.Minute,
		PauseAdmissionOnTerminalFailure: false,
	}

	cfg := normalizeManagerConfig(input)

	if cfg.MaxConcurrentWorkflows != 10 {
		t.Errorf("MaxConcurrentWorkflows = %d, want 10", cfg.MaxConcurrentWorkflows)
	}
	if cfg.MaxParallelNodesPerWF != 4 {
		t.Errorf("MaxParallelNodesPerWF = %d, want 4", cfg.MaxParallelNodesPerWF)
	}
	if cfg.AdmissionTimeoutSeconds != 60 {
		t.Errorf("AdmissionTimeoutSeconds = %d, want 60", cfg.AdmissionTimeoutSeconds)
	}
	if cfg.WorkerCount != 20 {
		t.Errorf("WorkerCount = %d, want 20", cfg.WorkerCount)
	}
	if cfg.WorkerInitialCount != 5 {
		t.Errorf("WorkerInitialCount = %d, want 5", cfg.WorkerInitialCount)
	}
	if cfg.WorkerPollInterval != 50*time.Millisecond {
		t.Errorf("WorkerPollInterval = %v, want 50ms", cfg.WorkerPollInterval)
	}
	if cfg.WorkerClaimErrorBackoffMax != 500*time.Millisecond {
		t.Errorf("WorkerClaimErrorBackoffMax = %v, want 500ms", cfg.WorkerClaimErrorBackoffMax)
	}
	if cfg.WorkerIdleBackoffMax != 750*time.Millisecond {
		t.Errorf("WorkerIdleBackoffMax = %v, want 750ms", cfg.WorkerIdleBackoffMax)
	}
	if cfg.ReasoningTraceTTL != time.Hour {
		t.Errorf("ReasoningTraceTTL = %v, want 1h", cfg.ReasoningTraceTTL)
	}
	if cfg.ReasoningPurgeInterval != 10*time.Minute {
		t.Errorf("ReasoningPurgeInterval = %v, want 10m", cfg.ReasoningPurgeInterval)
	}
	if cfg.PauseAdmissionOnTerminalFailure {
		t.Error("PauseAdmissionOnTerminalFailure should be false")
	}
}

func TestNormalizeManagerConfig_NegativeValues(t *testing.T) {
	cfg := normalizeManagerConfig(&ManagerConfig{
		MaxConcurrentWorkflows:     -5,
		MaxParallelNodesPerWF:      -1,
		AdmissionTimeoutSeconds:    -10,
		WorkerCount:                -100,
		WorkerInitialCount:         -3,
		WorkerPollInterval:         -1 * time.Second,
		WorkerClaimErrorBackoffMax: -500 * time.Millisecond,
		WorkerIdleBackoffMax:       -500 * time.Millisecond,
		ReasoningTraceTTL:          -1 * time.Hour,
		ReasoningPurgeInterval:     -10 * time.Minute,
	})

	if cfg.MaxConcurrentWorkflows < 1 {
		t.Errorf("MaxConcurrentWorkflows = %d, want >= 1", cfg.MaxConcurrentWorkflows)
	}
	if cfg.MaxParallelNodesPerWF < 1 {
		t.Errorf("MaxParallelNodesPerWF = %d, want >= 1", cfg.MaxParallelNodesPerWF)
	}
	if cfg.AdmissionTimeoutSeconds < 1 {
		t.Errorf("AdmissionTimeoutSeconds = %d, want >= 1", cfg.AdmissionTimeoutSeconds)
	}
	if cfg.WorkerCount < 1 {
		t.Errorf("WorkerCount = %d, want >= 1", cfg.WorkerCount)
	}
	if cfg.WorkerInitialCount < 1 {
		t.Errorf("WorkerInitialCount = %d, want >= 1", cfg.WorkerInitialCount)
	}
	if cfg.WorkerPollInterval <= 0 {
		t.Errorf("WorkerPollInterval = %v, want > 0", cfg.WorkerPollInterval)
	}
	if cfg.WorkerClaimErrorBackoffMax <= 0 {
		t.Errorf("WorkerClaimErrorBackoffMax = %v, want > 0", cfg.WorkerClaimErrorBackoffMax)
	}
	if cfg.WorkerIdleBackoffMax <= 0 {
		t.Errorf("WorkerIdleBackoffMax = %v, want > 0", cfg.WorkerIdleBackoffMax)
	}
	if cfg.ReasoningTraceTTL <= 0 {
		t.Errorf("ReasoningTraceTTL = %v, want > 0", cfg.ReasoningTraceTTL)
	}
	if cfg.ReasoningPurgeInterval <= 0 {
		t.Errorf("ReasoningPurgeInterval = %v, want > 0", cfg.ReasoningPurgeInterval)
	}
}

func TestNextWorkerIdleBackoff(t *testing.T) {
	poll := 100 * time.Millisecond
	maxBackoff := time.Second

	cases := []struct {
		streak int
		want   time.Duration
	}{
		{streak: 0, want: poll},
		{streak: 1, want: poll},
		{streak: 2, want: 200 * time.Millisecond},
		{streak: 3, want: 400 * time.Millisecond},
		{streak: 4, want: 800 * time.Millisecond},
		{streak: 5, want: maxBackoff},
		{streak: 10, want: maxBackoff},
	}

	for _, tc := range cases {
		got := nextWorkerIdleBackoff(tc.streak, poll, maxBackoff)
		if got != tc.want {
			t.Fatalf("nextWorkerIdleBackoff(%d) = %v, want %v", tc.streak, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// clearConfigEnvVars unsets all env vars read by DefaultManagerConfig, restoring
// them via t.Cleanup so tests are isolated.
func clearConfigEnvVars(t *testing.T) {
	t.Helper()
	vars := []string{
		"MAX_CONCURRENT_WORKFLOWS",
		"MAX_PARALLEL_NODES_PER_WORKFLOW",
		"WORKER_COUNT",
		"WORKER_INITIAL_COUNT",
		"WORKER_POLL_INTERVAL_MS",
		"WORKER_CLAIM_ERROR_BACKOFF_MAX_MS",
		"WORKER_IDLE_BACKOFF_MAX_MS",
		"REASONING_TRACE_TTL_SECONDS",
		"REASONING_PURGE_INTERVAL_SECONDS",
		"PAUSE_ADMISSION_ON_TERMINAL_FAILURE",
	}
	for _, v := range vars {
		old, set := os.LookupEnv(v)
		_ = os.Unsetenv(v)
		if set {
			t.Cleanup(func() { _ = os.Setenv(v, old) })
		}
	}
}

// setenv sets an env var for the duration of a test and restores the old value.
func setenv(t *testing.T, key, value string) {
	t.Helper()
	old, set := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("setenv(%q, %q): %v", key, value, err)
	}
	t.Cleanup(func() {
		if set {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
