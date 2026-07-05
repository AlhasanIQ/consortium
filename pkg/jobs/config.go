package jobs

import (
	"os"
	"strconv"
	"time"
)

// ManagerConfig holds configuration for the job Manager.
type ManagerConfig struct {
	// MaxConcurrentWorkflows is the maximum number of workflows that can execute
	// simultaneously. Controlled by MAX_CONCURRENT_WORKFLOWS env var.
	MaxConcurrentWorkflows int

	// MaxParallelNodesPerWF is the maximum number of goroutines spawned per
	// DAG execution level within a single workflow. Controlled by
	// MAX_PARALLEL_NODES_PER_WORKFLOW env var.
	MaxParallelNodesPerWF int

	// AdmissionTimeoutSeconds is how long AcquireSlot will wait before
	// returning ErrPoolExhausted.
	AdmissionTimeoutSeconds int

	// WorkerCount is the maximum number of background goroutines that can poll
	// for and execute durable jobs. Workers are not started automatically —
	// call Manager.StartWorkers() to begin processing.
	// For parent/child chains, set this comfortably above MaxConcurrentWorkflows
	// so blocked parent jobs don't starve child-job execution.
	WorkerCount int

	// WorkerInitialCount is how many workers are started immediately on boot.
	// Additional workers are started iteratively as demand saturates the active
	// pool, up to WorkerCount.
	WorkerInitialCount int

	// WorkerPollInterval is how long a worker waits between poll attempts
	// when no work is available.
	WorkerPollInterval time.Duration

	// WorkerClaimErrorBackoffMax bounds exponential backoff when transient
	// claim/recovery database errors occur.
	WorkerClaimErrorBackoffMax time.Duration

	// WorkerIdleBackoffMax bounds exponential backoff when workers poll an empty
	// durable queue. This keeps idle workers from generating constant DB churn.
	WorkerIdleBackoffMax time.Duration

	// ReasoningTraceTTL controls how long verbose debug reasoning traces are retained.
	ReasoningTraceTTL time.Duration

	// ReasoningPurgeInterval controls how often verbose reasoning traces are purged.
	ReasoningPurgeInterval time.Duration

	// PauseAdmissionOnTerminalFailure pauses new root admissions automatically
	// when systemic terminal failures are detected (for example, auth/credits).
	PauseAdmissionOnTerminalFailure bool
}

// DefaultManagerConfig returns a ManagerConfig populated from environment
// variables with sensible defaults.
func DefaultManagerConfig() *ManagerConfig {
	cfg := &ManagerConfig{
		MaxConcurrentWorkflows:          150,
		MaxParallelNodesPerWF:           32,
		AdmissionTimeoutSeconds:         30,
		WorkerCount:                     300,
		WorkerInitialCount:              10,
		WorkerPollInterval:              100 * time.Millisecond,
		WorkerClaimErrorBackoffMax:      2 * time.Second,
		WorkerIdleBackoffMax:            1 * time.Second,
		ReasoningTraceTTL:               6 * time.Hour,
		ReasoningPurgeInterval:          30 * time.Minute,
		PauseAdmissionOnTerminalFailure: true,
	}
	if v := os.Getenv("MAX_CONCURRENT_WORKFLOWS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConcurrentWorkflows = n
		}
	}
	if v := os.Getenv("MAX_PARALLEL_NODES_PER_WORKFLOW"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxParallelNodesPerWF = n
		}
	}
	if v := os.Getenv("WORKER_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.WorkerCount = n
		}
	}
	if v := os.Getenv("WORKER_INITIAL_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.WorkerInitialCount = n
		}
	}
	if v := os.Getenv("WORKER_POLL_INTERVAL_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.WorkerPollInterval = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv("WORKER_CLAIM_ERROR_BACKOFF_MAX_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.WorkerClaimErrorBackoffMax = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv("WORKER_IDLE_BACKOFF_MAX_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.WorkerIdleBackoffMax = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv("REASONING_TRACE_TTL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ReasoningTraceTTL = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("REASONING_PURGE_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ReasoningPurgeInterval = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("PAUSE_ADMISSION_ON_TERMINAL_FAILURE"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.PauseAdmissionOnTerminalFailure = parsed
		}
	}
	return normalizeManagerConfig(cfg)
}

func normalizeManagerConfig(cfg *ManagerConfig) *ManagerConfig {
	if cfg == nil {
		cfg = &ManagerConfig{}
	}

	if cfg.MaxConcurrentWorkflows < 1 {
		cfg.MaxConcurrentWorkflows = 150
	}
	if cfg.MaxParallelNodesPerWF < 1 {
		cfg.MaxParallelNodesPerWF = 32
	}
	if cfg.AdmissionTimeoutSeconds < 1 {
		cfg.AdmissionTimeoutSeconds = 30
	}

	if cfg.WorkerCount < 1 {
		cfg.WorkerCount = 300
	}
	// Keep max worker capacity at least as large as root admission capacity.
	if cfg.WorkerCount < cfg.MaxConcurrentWorkflows {
		cfg.WorkerCount = cfg.MaxConcurrentWorkflows
	}

	if cfg.WorkerInitialCount < 1 {
		cfg.WorkerInitialCount = 10
	}
	if cfg.WorkerInitialCount > cfg.WorkerCount {
		cfg.WorkerInitialCount = cfg.WorkerCount
	}

	if cfg.WorkerPollInterval <= 0 {
		cfg.WorkerPollInterval = 100 * time.Millisecond
	}
	if cfg.WorkerClaimErrorBackoffMax <= 0 {
		cfg.WorkerClaimErrorBackoffMax = 2 * time.Second
	}
	if cfg.WorkerIdleBackoffMax <= 0 {
		cfg.WorkerIdleBackoffMax = time.Second
	}
	if cfg.WorkerIdleBackoffMax < cfg.WorkerPollInterval {
		cfg.WorkerIdleBackoffMax = cfg.WorkerPollInterval
	}
	if cfg.ReasoningTraceTTL <= 0 {
		cfg.ReasoningTraceTTL = 6 * time.Hour
	}
	if cfg.ReasoningPurgeInterval <= 0 {
		cfg.ReasoningPurgeInterval = 30 * time.Minute
	}

	return cfg
}
