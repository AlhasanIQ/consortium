package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime/durable"
)

// DeduplicationWindowSeconds is the time window for request hash deduplication
// Set to 3 minutes to catch rapid duplicate submissions while allowing intentional re-runs
const DeduplicationWindowSeconds = 180

// DedupReason indicates the reason for deduplication
type DedupReason string

const (
	DedupReasonIdempotencyKey DedupReason = "idempotency_key"
	DedupReasonRequestHash    DedupReason = "request_hash"
)

// ErrPoolExhausted is returned when the admission pool has no available slots
// and the wait timeout has elapsed.
var ErrPoolExhausted = errors.New("admission pool exhausted")

// ErrJobStateConflict is returned when a job operation is invalid for the current status.
var ErrJobStateConflict = errors.New("job state conflict")

// IsAdmissionError returns true if err is an admission pool exhaustion error.
func IsAdmissionError(err error) bool {
	return errors.Is(err, ErrPoolExhausted)
}

// Manager handles all job lifecycle operations with automatic database tracking.
// It tracks running jobs, supports cancellation, and enforces bounded concurrency
// via an admission pool.
type Manager struct {
	storage  *storage.Storage
	registry *providers.Registry
	executor *workflow.Executor
	config   *ManagerConfig

	// Durable execution runtime used for all scheduling/execution.
	durableRuntime *durable.DAGRuntime

	// Admission pool: buffered channel semaphore that limits concurrent workflows.
	admissionPool chan struct{}

	// Job cancellation support
	// Maps jobID -> cancel function for running jobs
	runningJobs sync.Map // map[string]context.CancelFunc

	// In-process terminal notifications for synchronous waiters. The database
	// remains the source of truth; these channels only avoid waiting for the
	// fallback polling interval when this process completes a job.
	completionMu      sync.Mutex
	completionWaiters map[string][]chan struct{}

	// Background worker lifecycle
	workerMu        sync.Mutex
	workersStarted  atomic.Bool
	workersStopping atomic.Bool
	workerCancel    context.CancelFunc
	workerCtx       context.Context
	workerWg        sync.WaitGroup
	workerNextID    atomic.Int64
	activeWorkers   atomic.Int64
	busyWorkers     atomic.Int64

	// Startup recovery toggle. Durable jobs left in "running" after a restart
	// are only scanned/resumed during initial startup recovery.
	startupRecoveryPending atomic.Bool

	// Admission pause state gates new root submissions when systemic terminal
	// failures are detected (for example, auth/credits issues).
	admissionPaused atomic.Bool
	admissionMu     sync.RWMutex
	admissionReason *AdmissionPauseReason

	// Factory used by explicit admin/conctl Novomo stop actions. Production
	// uses Novomo environment configuration; tests can inject a fake without
	// standing up an HTTP server.
	novomoStopClientFactory func() (novomoStopClient, error)
}

// NewManagerWithConfig creates a new job manager with explicit configuration.
func NewManagerWithConfig(store *storage.Storage, registry *providers.Registry, config *ManagerConfig) *Manager {
	if config == nil {
		config = DefaultManagerConfig()
	} else {
		config = normalizeManagerConfig(config)
	}
	llmClient := providers.NewClient(registry, NewStorageNodeLogger(store))
	executor := workflow.NewExecutor(llmClient)

	// Build durable runtime activity handlers from registered node runners.
	activityRegistry := runtime.NewActivityHandlerRegistry()
	durable.RegisterNodeRunners(activityRegistry, executor.RunnerRegistry())
	dagRuntime := durable.NewDAGRuntime(store, activityRegistry)

	return &Manager{
		storage:        store,
		registry:       registry,
		executor:       executor,
		config:         config,
		durableRuntime: dagRuntime,
		admissionPool:  make(chan struct{}, config.MaxConcurrentWorkflows),
	}
}

// NewManager creates a new job manager with default configuration.
func NewManager(storage *storage.Storage, registry *providers.Registry) *Manager {
	return NewManagerWithConfig(storage, registry, DefaultManagerConfig())
}

// AcquireSlot blocks until a workflow execution slot is available, the context
// is cancelled, or the admission timeout elapses.
func (m *Manager) AcquireSlot(ctx context.Context) error {
	timeout := time.Duration(m.config.AdmissionTimeoutSeconds) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case m.admissionPool <- struct{}{}:
		return nil
	case <-timer.C:
		return ErrPoolExhausted
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ReleaseSlot returns a workflow execution slot to the admission pool.
// Must be called via defer after a successful AcquireSlot.
func (m *Manager) ReleaseSlot() {
	<-m.admissionPool
}

// PoolStats returns the current number of active workflows and total pool capacity.
func (m *Manager) PoolStats() (active int, capacity int) {
	return len(m.admissionPool), cap(m.admissionPool)
}

// WorkerStats returns current worker-pool counters and admission-pool usage.
func (m *Manager) WorkerStats() WorkerStats {
	admissionActive, admissionCapacity := m.PoolStats()
	activeWorkers := int(m.activeWorkers.Load())
	busyWorkers := int(m.busyWorkers.Load())
	idleWorkers := activeWorkers - busyWorkers
	if idleWorkers < 0 {
		idleWorkers = 0
	}
	admissionUtilization := float64(0)
	if admissionCapacity > 0 {
		admissionUtilization = float64(admissionActive) / float64(admissionCapacity)
	}
	return WorkerStats{
		WorkerInitial:        m.config.WorkerInitialCount,
		WorkerMax:            m.config.WorkerCount,
		ActiveWorkers:        activeWorkers,
		BusyWorkers:          busyWorkers,
		IdleWorkers:          idleWorkers,
		AdmissionActive:      admissionActive,
		AdmissionCapacity:    admissionCapacity,
		AdmissionUtilization: admissionUtilization,
	}
}

// WorkerStats captures live worker-pool counters. WorkerMax is capacity; it is
// not the number of active polling workers.
type WorkerStats struct {
	WorkerInitial        int
	WorkerMax            int
	ActiveWorkers        int
	BusyWorkers          int
	IdleWorkers          int
	AdmissionActive      int
	AdmissionCapacity    int
	AdmissionUtilization float64
}

// IsRunning returns true if the job is currently executing in this process.
func (m *Manager) IsRunning(jobID string) bool {
	_, ok := m.runningJobs.Load(jobID)
	return ok
}

// Config returns the manager's configuration.
func (m *Manager) Config() *ManagerConfig {
	return m.config
}

// CancelJob cancels a running job by triggering its context cancellation
// Returns error if job not found, already completed, or not running
func (m *Manager) CancelJob(jobID string) error {
	// Check if job exists and is running
	job, err := m.storage.GetExecution(jobID)
	if err != nil {
		return fmt.Errorf("job %s not found: %w", jobID, err)
	}

	// Pending/paused jobs can be cancelled immediately before worker claim.
	if job.Status == events.JobStatusPending || job.Status == events.JobStatusPaused {
		prevStatus := job.Status
		job.Status = events.JobStatusCancelled
		job.ErrorMessage = "Job cancelled by user request"
		if updateErr := m.storage.UpdateExecution(job); updateErr != nil {
			return fmt.Errorf("failed to cancel queued job %s: %w", jobID, updateErr)
		}
		log.Printf("❌ Job %s cancelled while %s", jobID, prevStatus)
		m.notifyCompletionWaiters(jobID)
		return nil
	}

	// Running jobs require active cancellation via context cancel.
	if job.Status != events.JobStatusRunning {
		return fmt.Errorf(
			"cannot cancel job %s: status is %s (must be '%s', '%s', or '%s'): %w",
			jobID, job.Status, events.JobStatusPending, events.JobStatusPaused, events.JobStatusRunning, ErrJobStateConflict,
		)
	}

	// Lookup cancel function for this job
	cancelFunc, ok := m.runningJobs.Load(jobID)
	if !ok {
		// Job is marked running but not in our map - might have just completed
		// Refresh job status from DB
		if refreshedJob, refreshErr := m.storage.GetExecution(jobID); refreshErr == nil && refreshedJob.Status != events.JobStatusRunning {
			return fmt.Errorf("job %s is no longer running (status: %s): %w", jobID, refreshedJob.Status, ErrJobStateConflict)
		}
		return fmt.Errorf("job %s not found in running jobs map: %w", jobID, ErrJobStateConflict)
	}

	// Cancel the context - this will trigger cancellation in the workflow
	if cancel, ok := cancelFunc.(context.CancelFunc); ok {
		cancel()
		log.Printf("❌ Job %s cancellation requested by user", jobID)
	} else {
		return fmt.Errorf("invalid cancel function type for job %s", jobID)
	}

	// Update job status to cancelled
	// Note: The workflow execution will also update this, but we do it here for immediate feedback
	job.Status = events.JobStatusCancelled
	job.ErrorMessage = "Job cancelled by user request"
	if updateErr := m.storage.UpdateExecution(job); updateErr != nil {
		log.Printf("Warning: Failed to update job status for cancelled job %s: %v", jobID, updateErr)
		// Don't return error - cancellation signal was sent successfully
	}
	m.notifyCompletionWaiters(jobID)

	return nil
}
