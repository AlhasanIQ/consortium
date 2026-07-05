import { afterEach, beforeEach, describe, expect, it, vi } from 'bun:test';
import { useEnsembleStore } from './ensembleStore';

describe('EnsembleStore State Machine', () => {
  beforeEach(() => {
    vi.spyOn(console, 'log').mockImplementation(() => {});
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(console, 'error').mockImplementation(() => {});

    // Reset store to initial state before each test
    useEnsembleStore.setState({
      executionState: { status: 'idle' },
      isExecuting: false,
      phase: 'idle',
      jobId: null,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('canExecute()', () => {
    it('returns true when status is idle', () => {
      useEnsembleStore.setState({ executionState: { status: 'idle' } });
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });

    it('returns true when status is complete', () => {
      useEnsembleStore.setState({ executionState: { status: 'complete', jobId: 'job-123' } });
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });

    it('returns true when status is error', () => {
      useEnsembleStore.setState({ executionState: { status: 'error', message: 'Test error' } });
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });

    it('returns true when status is cancelled', () => {
      useEnsembleStore.setState({ executionState: { status: 'cancelled', jobId: 'job-123' } });
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });

    it('returns false when status is submitting', () => {
      useEnsembleStore.setState({ executionState: { status: 'submitting', idempotencyKey: 'key-123' } });
      expect(useEnsembleStore.getState().canExecute()).toBe(false);
    });

    it('returns false when status is streaming', () => {
      useEnsembleStore.setState({
        executionState: { status: 'streaming', jobId: 'job-123', idempotencyKey: 'key-123' },
      });
      expect(useEnsembleStore.getState().canExecute()).toBe(false);
    });
  });

  describe('startSubmitting()', () => {
    it('transitions from idle to submitting', () => {
      const result = useEnsembleStore.getState().startSubmitting('key-123');
      expect(result).toBe(true);
      expect(useEnsembleStore.getState().executionState).toEqual({
        status: 'submitting',
        idempotencyKey: 'key-123',
      });
      expect(useEnsembleStore.getState().isExecuting).toBe(true);
      expect(useEnsembleStore.getState().phase).toBe('sending');
    });

    it('transitions from complete to submitting', () => {
      useEnsembleStore.setState({ executionState: { status: 'complete', jobId: 'old-job' } });
      const result = useEnsembleStore.getState().startSubmitting('key-456');
      expect(result).toBe(true);
      expect(useEnsembleStore.getState().executionState.status).toBe('submitting');
    });

    it('transitions from error to submitting', () => {
      useEnsembleStore.setState({ executionState: { status: 'error', message: 'Previous error' } });
      const result = useEnsembleStore.getState().startSubmitting('key-789');
      expect(result).toBe(true);
      expect(useEnsembleStore.getState().executionState.status).toBe('submitting');
    });

    it('transitions from cancelled to submitting', () => {
      useEnsembleStore.setState({ executionState: { status: 'cancelled', jobId: 'cancelled-job' } });
      const result = useEnsembleStore.getState().startSubmitting('key-abc');
      expect(result).toBe(true);
      expect(useEnsembleStore.getState().executionState.status).toBe('submitting');
    });

    it('rejects transition from submitting', () => {
      useEnsembleStore.setState({ executionState: { status: 'submitting', idempotencyKey: 'existing-key' } });
      const result = useEnsembleStore.getState().startSubmitting('new-key');
      expect(result).toBe(false);
      // State should remain unchanged
      expect(useEnsembleStore.getState().executionState).toEqual({
        status: 'submitting',
        idempotencyKey: 'existing-key',
      });
    });

    it('rejects transition from streaming', () => {
      useEnsembleStore.setState({
        executionState: { status: 'streaming', jobId: 'job-123', idempotencyKey: 'key-123' },
      });
      const result = useEnsembleStore.getState().startSubmitting('new-key');
      expect(result).toBe(false);
    });
  });

  describe('setStreaming()', () => {
    it('transitions from submitting to streaming', () => {
      useEnsembleStore.setState({ executionState: { status: 'submitting', idempotencyKey: 'key-123' } });
      useEnsembleStore.getState().setStreaming('job-456');
      expect(useEnsembleStore.getState().executionState).toEqual({
        status: 'streaming',
        jobId: 'job-456',
        idempotencyKey: 'key-123',
      });
      expect(useEnsembleStore.getState().jobId).toBe('job-456');
    });

    it('does not transition from non-submitting states', () => {
      useEnsembleStore.setState({ executionState: { status: 'idle' } });
      useEnsembleStore.getState().setStreaming('job-789');
      // State should remain unchanged
      expect(useEnsembleStore.getState().executionState.status).toBe('idle');
    });
  });

  describe('setExecutionComplete()', () => {
    it('transitions to complete state', () => {
      useEnsembleStore.setState({
        executionState: { status: 'streaming', jobId: 'job-123', idempotencyKey: 'key-123' },
      });
      useEnsembleStore.getState().setExecutionComplete('job-123');
      expect(useEnsembleStore.getState().executionState).toEqual({
        status: 'complete',
        jobId: 'job-123',
      });
    });
  });

  describe('setExecutionCancelled()', () => {
    it('transitions to cancelled state', () => {
      useEnsembleStore.setState({
        executionState: { status: 'streaming', jobId: 'job-123', idempotencyKey: 'key-123' },
        isExecuting: true,
        phase: 'thinking',
      });
      useEnsembleStore.getState().setExecutionCancelled('job-123');
      expect(useEnsembleStore.getState().executionState).toEqual({
        status: 'cancelled',
        jobId: 'job-123',
      });
      expect(useEnsembleStore.getState().isExecuting).toBe(false);
      expect(useEnsembleStore.getState().phase).toBe('idle');
    });

    it('allows re-execution after cancellation', () => {
      useEnsembleStore.setState({ executionState: { status: 'cancelled', jobId: 'old-job' } });
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
      const result = useEnsembleStore.getState().startSubmitting('new-key');
      expect(result).toBe(true);
    });
  });

  describe('setExecutionError()', () => {
    it('transitions to error state with message', () => {
      useEnsembleStore.setState({
        executionState: { status: 'streaming', jobId: 'job-123', idempotencyKey: 'key-123' },
        isExecuting: true,
        phase: 'thinking',
      });
      useEnsembleStore.getState().setExecutionError('Something went wrong', 'job-123');
      expect(useEnsembleStore.getState().executionState).toEqual({
        status: 'error',
        message: 'Something went wrong',
        jobId: 'job-123',
      });
      expect(useEnsembleStore.getState().isExecuting).toBe(false);
      expect(useEnsembleStore.getState().phase).toBe('idle');
    });

    it('allows re-execution after error', () => {
      useEnsembleStore.setState({ executionState: { status: 'error', message: 'Previous error' } });
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });
  });

  describe('State Machine Flow', () => {
    it('follows the complete happy path: idle -> submitting -> streaming -> complete', () => {
      // Start in idle
      expect(useEnsembleStore.getState().executionState.status).toBe('idle');
      expect(useEnsembleStore.getState().canExecute()).toBe(true);

      // Transition to submitting
      const submitted = useEnsembleStore.getState().startSubmitting('key-1');
      expect(submitted).toBe(true);
      expect(useEnsembleStore.getState().executionState.status).toBe('submitting');
      expect(useEnsembleStore.getState().canExecute()).toBe(false);

      // Transition to streaming
      useEnsembleStore.getState().setStreaming('job-1');
      expect(useEnsembleStore.getState().executionState.status).toBe('streaming');
      expect(useEnsembleStore.getState().canExecute()).toBe(false);

      // Transition to complete
      useEnsembleStore.getState().setExecutionComplete('job-1');
      expect(useEnsembleStore.getState().executionState.status).toBe('complete');
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });

    it('follows the cancel path: idle -> submitting -> streaming -> cancelled', () => {
      // Start in idle and go to streaming
      useEnsembleStore.getState().startSubmitting('key-2');
      useEnsembleStore.getState().setStreaming('job-2');
      expect(useEnsembleStore.getState().executionState.status).toBe('streaming');

      // Cancel
      useEnsembleStore.getState().setExecutionCancelled('job-2');
      expect(useEnsembleStore.getState().executionState.status).toBe('cancelled');
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });

    it('follows the error path: idle -> submitting -> streaming -> error', () => {
      // Start in idle and go to streaming
      useEnsembleStore.getState().startSubmitting('key-3');
      useEnsembleStore.getState().setStreaming('job-3');

      // Error
      useEnsembleStore.getState().setExecutionError('Connection lost', 'job-3');
      expect(useEnsembleStore.getState().executionState.status).toBe('error');
      expect(useEnsembleStore.getState().canExecute()).toBe(true);
    });
  });
});
