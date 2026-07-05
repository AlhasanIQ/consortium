package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// ErrWorkersNotStarted is returned when ExecuteWorkflow is called without
// calling StartWorkers() first.
var ErrWorkersNotStarted = errors.New("manager: workers not started — call StartWorkers() before ExecuteWorkflow")

// StartWorkers launches background goroutines that poll for and execute
// pending durable jobs. Workers also resume durable jobs left in "running"
// state after a process restart.
//
// Call StopWorkers to gracefully shut down.
func (m *Manager) StartWorkers() {
	m.workerMu.Lock()
	defer m.workerMu.Unlock()

	if m.workerCancel != nil {
		log.Printf("⚙️ [Workers] already started")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.workersStopping.Store(false)
	m.workerCancel = cancel
	m.workerCtx = ctx
	m.workersStarted.Store(true)
	m.workerNextID.Store(0)
	m.activeWorkers.Store(0)
	m.busyWorkers.Store(0)
	m.startupRecoveryPending.Store(true)

	initial := m.config.WorkerInitialCount
	if initial < 1 {
		initial = 1
	}
	if initial > m.config.WorkerCount {
		initial = m.config.WorkerCount
	}
	for i := 0; i < initial; i++ {
		_ = m.startWorkerLocked(ctx)
	}

	m.workerWg.Add(1)
	go m.workerAutoscaleLoop(ctx)
	m.workerWg.Add(1)
	go m.reasoningPurgeLoop(ctx)
	log.Printf("⚙️ [Workers] started %d background workers (initial=%d max=%d)",
		m.activeWorkers.Load(), initial, m.config.WorkerCount)
}

// StopWorkers signals all background workers to stop and waits for them to
// drain. Blocks until all workers have finished or ctx is cancelled.
func (m *Manager) StopWorkers(ctx context.Context) {
	m.workerMu.Lock()
	if m.workerCancel == nil {
		m.workerMu.Unlock()
		return
	}
	m.workersStopping.Store(true)
	m.workerCancel()
	m.workerMu.Unlock()

	done := make(chan struct{})
	go func() {
		m.workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.workerMu.Lock()
		m.workerCancel = nil
		m.workerCtx = nil
		m.workersStarted.Store(false)
		m.workersStopping.Store(false)
		m.startupRecoveryPending.Store(false)
		m.activeWorkers.Store(0)
		m.busyWorkers.Store(0)
		m.workerMu.Unlock()
		log.Printf("⚙️ [Workers] all workers stopped")
	case <-ctx.Done():
		m.workerMu.Lock()
		m.workersStopping.Store(false)
		m.workerMu.Unlock()
		log.Printf("⚙️ [Workers] stop timed out, some workers may still be running")
	}
}

func (m *Manager) startWorkerLocked(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	if m.workerCancel == nil {
		return false
	}
	if m.workersStopping.Load() {
		return false
	}
	if int(m.activeWorkers.Load()) >= m.config.WorkerCount {
		return false
	}

	workerID := int(m.workerNextID.Add(1) - 1)
	m.activeWorkers.Add(1)
	m.workerWg.Add(1)
	go m.workerLoop(ctx, workerID)
	return true
}

func (m *Manager) maybeScaleWorkers(reason string) bool {
	active := m.activeWorkers.Load()
	busy := m.busyWorkers.Load()
	if busy < active {
		return false
	}
	if int(active) >= m.config.WorkerCount {
		return false
	}

	m.workerMu.Lock()
	defer m.workerMu.Unlock()
	if m.workerCancel == nil || m.workerCtx == nil || m.workerCtx.Err() != nil {
		return false
	}
	active = m.activeWorkers.Load()
	busy = m.busyWorkers.Load()
	if busy < active {
		return false
	}
	if int(active) >= m.config.WorkerCount {
		return false
	}
	if !m.startWorkerLocked(m.workerCtx) {
		return false
	}
	log.Printf("⚙️ [Workers] scaled active workers to %d/%d (%s)", m.activeWorkers.Load(), m.config.WorkerCount, reason)
	return true
}

func (m *Manager) workerAutoscaleLoop(ctx context.Context) {
	defer m.workerWg.Done()

	interval := m.config.WorkerPollInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	nextErrLog := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		active := m.activeWorkers.Load()
		if active < 1 {
			_ = m.maybeScaleWorkers("autoscale_no_active_workers")
			continue
		}
		if int(active) >= m.config.WorkerCount {
			continue
		}
		if m.busyWorkers.Load() < active {
			continue
		}

		pending, err := m.storage.CountPendingDurableJobs(ctx)
		if err != nil {
			now := time.Now()
			if nextErrLog.IsZero() || now.After(nextErrLog) {
				log.Printf("⚠️ [Workers] autoscale pending-count check failed: %v", err)
				nextErrLog = now.Add(3 * time.Second)
			}
			continue
		}
		if pending <= 0 {
			continue
		}
		_ = m.maybeScaleWorkers("autoscale_pending_backlog")
	}
}

// workerLoop is the main loop for a single background worker.
func (m *Manager) workerLoop(ctx context.Context, workerID int) {
	defer func() {
		m.activeWorkers.Add(-1)
		m.workerWg.Done()
	}()

	claimErrStreak := 0
	idleStreak := 0
	nextClaimErrLog := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		claimed, err := m.processNextJob(ctx)
		if err != nil {
			idleStreak = 0
			if storage.IsRetryableSQLiteError(err) {
				claimErrStreak++
				backoff := m.workerClaimErrorBackoff(claimErrStreak)
				now := time.Now()
				if claimErrStreak == 1 || now.After(nextClaimErrLog) {
					log.Printf("⚠️ [Worker %d] transient claim/recovery error (streak=%d, backoff=%s): %v", workerID, claimErrStreak, backoff, err)
					nextClaimErrLog = now.Add(3 * time.Second)
				}
				if !waitWithContext(ctx, backoff) {
					return
				}
				continue
			}
			claimErrStreak = 0
			log.Printf("⚙️ [Worker %d] claim/recovery error: %v", workerID, err)
			if !waitWithContext(ctx, m.config.WorkerPollInterval) {
				return
			}
			continue
		}
		claimErrStreak = 0
		if !claimed {
			idleStreak++
			backoff := nextWorkerIdleBackoff(idleStreak, m.config.WorkerPollInterval, m.config.WorkerIdleBackoffMax)
			// No work available — wait before polling again.
			if !waitWithContext(ctx, backoff) {
				return
			}
			continue
		}
		idleStreak = 0
		// If we processed a job, loop immediately to check for more.
	}
}

// reasoningPurgeLoop periodically redacts/removes expired verbose reasoning artifacts.
func (m *Manager) reasoningPurgeLoop(ctx context.Context) {
	defer m.workerWg.Done()
	interval := m.config.ReasoningPurgeInterval
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ttl := m.config.ReasoningTraceTTL
			if ttl <= 0 {
				ttl = 6 * time.Hour
			}
			count, err := m.storage.PurgeExpiredReasoningTraces(ctx, ttl)
			if err != nil {
				log.Printf("⚠️ [Workers] reasoning trace purge failed: %v", err)
				continue
			}
			if count > 0 {
				log.Printf("⚙️ [Workers] purged %d expired reasoning traces", count)
			}
		}
	}
}

// processNextJob attempts to claim and execute one job.
// Returns true if a job was found (regardless of execution outcome).
func (m *Manager) processNextJob(ctx context.Context) (bool, error) {
	// 1. Prioritize child jobs without consuming additional admission slots.
	// Parent jobs already hold slots while waiting on child completion.
	childJob, err := m.storage.ClaimPendingDurableChildJob(ctx)
	if err != nil {
		return false, fmt.Errorf("child claim: %w", err)
	}
	if childJob != nil {
		return m.executeClaimedJob(ctx, childJob), nil
	}

	// 2. While admission is paused, do not claim new root jobs.
	if paused, _ := m.AdmissionState(); paused {
		return false, nil
	}

	// 3. Root jobs use admission slots.
	if !m.tryAcquireSlot() {
		return false, nil
	}
	defer m.ReleaseSlot()

	// 4. Startup-only recovery of durable jobs left in "running" after restart.
	// Once drained, workers stop scanning running rows in the hot poll path.
	if m.startupRecoveryPending.Load() {
		resumable, err := m.claimStartupResumableJob(ctx)
		if err != nil {
			return false, fmt.Errorf("startup resumable claim: %w", err)
		}
		if resumable != nil {
			return m.executeClaimedJob(ctx, resumable), nil
		}
	}

	// 5. Claim a pending durable root job.
	job, err := m.storage.ClaimPendingDurableRootJob(ctx)
	if err != nil {
		return false, fmt.Errorf("root claim: %w", err)
	}
	if job == nil {
		return false, nil
	}

	return m.executeClaimedJob(ctx, job), nil
}

func (m *Manager) claimStartupResumableJob(ctx context.Context) (*storage.WorkflowExecution, error) {
	if !m.startupRecoveryPending.Load() {
		return nil, nil
	}

	var excludeIDs []string
	m.runningJobs.Range(func(key, _ interface{}) bool {
		excludeIDs = append(excludeIDs, key.(string))
		return true
	})

	resumable, err := m.storage.ListResumableDurableRunningJobs(ctx, excludeIDs, 1)
	if err != nil {
		return nil, err
	}
	if len(resumable) == 0 {
		if m.startupRecoveryPending.CompareAndSwap(true, false) {
			log.Printf("⚙️ [Workers] startup resumable recovery complete; hot-loop scan disabled")
		}
		return nil, nil
	}
	return resumable[0], nil
}

func (m *Manager) tryAcquireSlot() bool {
	select {
	case m.admissionPool <- struct{}{}:
		return true
	default:
		return false
	}
}

func (m *Manager) executeClaimedJob(ctx context.Context, job *storage.WorkflowExecution) bool {
	// Atomically reserve the job to prevent another worker from processing it.
	// Store a real cancel function immediately so CancelJob can reliably stop the
	// execution even if cancellation arrives right after claim.
	jobCtx, jobCancel := context.WithCancel(ctx)
	if _, loaded := m.runningJobs.LoadOrStore(job.ID, jobCancel); loaded {
		jobCancel()
		return false // Another worker already owns this job.
	}
	defer m.notifyCompletionWaiters(job.ID)
	m.busyWorkers.Add(1)
	_ = m.maybeScaleWorkers("worker_saturated")
	defer func() {
		m.busyWorkers.Add(-1)
		m.runningJobs.Delete(job.ID)
		jobCancel()
	}()

	// Re-load status after reservation. Another worker may have finished this job
	// between resumable-job discovery and our reservation attempt.
	latestJob, err := m.storage.GetExecution(job.ID)
	if err != nil {
		log.Printf("⚙️ [Worker] failed to reload job %s after reservation: %v", job.ID, err)
		return false
	}
	switch latestJob.Status {
	case events.JobStatusCompleted, events.JobStatusFailed, events.JobStatusCancelled:
		return true
	}
	job = latestJob

	// Load workflow from stored request data.
	wf, err := m.loadWorkflowFromJob(job)
	if err != nil {
		log.Printf("⚙️ [Worker] failed to load workflow for job %s: %v", job.ID, err)
		m.failJob(job, fmt.Sprintf("worker: %v", err))
		return true
	}
	replayReq, err := m.loadReplayRequestForJob(jobCtx, job.ID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// Caller-initiated cancellation can race with this optional replay lookup.
			// Continue without replay so durable runtime records a cancelled outcome.
			log.Printf("⚙️ [Worker] replay request lookup interrupted for job %s: %v", job.ID, err)
		} else {
			log.Printf("⚙️ [Worker] failed to load replay request for job %s: %v", job.ID, err)
			m.failJob(job, fmt.Sprintf("worker replay load: %v", err))
			return true
		}
	}

	// Execute via durable runtime.
	log.Printf("⚙️ [Worker] executing job %s (workflow: %s)", job.ID, job.WorkflowID)
	result, err := m.executeDurable(jobCtx, job, wf, nil, replayReq)
	if result != nil && !result.Success {
		m.maybePauseAdmissionForFailure(job.ID, result.ErrorCode, result.Error)
	}

	if err != nil {
		log.Printf("⚙️ [Worker] job %s error: %v", job.ID, err)
	} else if result != nil {
		log.Printf("⚙️ [Worker] job %s done (success=%v)", job.ID, result.Success)
	}
	return true
}

func (m *Manager) workerClaimErrorBackoff(streak int) time.Duration {
	base := m.config.WorkerPollInterval
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	maxBackoff := m.config.WorkerClaimErrorBackoffMax
	if maxBackoff <= 0 {
		maxBackoff = 2 * time.Second
	}
	if streak <= 1 {
		if base > maxBackoff {
			return maxBackoff
		}
		return base
	}
	backoff := base
	for i := 1; i < streak; i++ {
		if backoff >= maxBackoff {
			return maxBackoff
		}
		backoff *= 2
		if backoff >= maxBackoff {
			return maxBackoff
		}
	}
	return backoff
}

func nextWorkerIdleBackoff(streak int, pollInterval, maxBackoff time.Duration) time.Duration {
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	if maxBackoff <= 0 {
		maxBackoff = time.Second
	}
	if maxBackoff < pollInterval {
		maxBackoff = pollInterval
	}
	if streak <= 1 {
		return pollInterval
	}
	backoff := pollInterval
	for i := 1; i < streak; i++ {
		if backoff >= maxBackoff {
			return maxBackoff
		}
		backoff *= 2
		if backoff >= maxBackoff {
			return maxBackoff
		}
	}
	return backoff
}

func waitWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// loadWorkflowFromJob reconstructs a *workflow.Workflow from the job's stored
// RequestData JSON.
func (m *Manager) loadWorkflowFromJob(job *storage.WorkflowExecution) (*workflow.Workflow, error) {
	if job.RequestData == "" {
		return nil, fmt.Errorf("job %s has no request_data", job.ID)
	}
	var wf workflow.Workflow
	if err := json.Unmarshal([]byte(job.RequestData), &wf); err != nil {
		return nil, fmt.Errorf("unmarshal request_data for job %s: %w", job.ID, err)
	}
	return &wf, nil
}

// failJob marks a job as failed with the given error message.
func (m *Manager) failJob(job *storage.WorkflowExecution, errMsg string) {
	job.Status = events.JobStatusFailed
	job.ErrorMessage = errMsg
	if err := m.storage.UpdateExecution(job); err != nil {
		log.Printf("⚙️ [Worker] failed to mark job %s as failed: %v", job.ID, err)
	}
}
