import { afterEach, beforeEach, describe, expect, it, vi } from 'bun:test';
import { type EnsembleJobSnapshot, useEnsembleStore } from './ensembleStore';

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

    it('patches the latest history entry with authoritative completion data', () => {
      useEnsembleStore.setState({
        history: [
          {
            id: 'history-1',
            jobId: 'job-123',
            prompt: 'question',
            synthesizedResponse: '',
            timestamp: new Date('2026-01-01T00:00:00Z'),
            totalCost: 0,
            totalTokens: 0,
            totalLatency: 100,
          },
        ],
      });

      useEnsembleStore.getState().setExecutionComplete('job-123', {
        finalOutput: 'authoritative answer',
        totalCost: 0.42,
        totalTokens: 17,
      });

      const state = useEnsembleStore.getState();
      expect(state.history[0]).toMatchObject({
        synthesizedResponse: 'authoritative answer',
        totalCost: 0.42,
        totalTokens: 17,
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

  describe('applyJobSnapshot()', () => {
    it('authoritatively projects reconnect node state and is idempotent at terminal completion', () => {
      useEnsembleStore.setState({
        prompt: 'question',
        executionState: { status: 'streaming', jobId: 'job-reconnect', idempotencyKey: 'key-reconnect' },
        isExecuting: true,
        phase: 'thinking',
        history: [],
        particles: [],
        agents: [
          {
            id: 'agent-a',
            model: 'provider/model-a',
            provider: 'provider',
            displayName: 'Agent A',
            color: '#111111',
            status: 'streaming',
            output: '',
            streamingText: 'stale partial',
            tokens: 1,
            cost: 0.01,
            latencyMs: 0,
          },
          {
            id: 'agent-b',
            model: 'provider/model-b',
            provider: 'provider',
            displayName: 'Agent B',
            color: '#222222',
            status: 'thinking',
            output: '',
            streamingText: '',
            tokens: 0,
            cost: 0,
            latencyMs: 0,
          },
        ],
      });

      const runningSnapshot: EnsembleJobSnapshot = {
        id: 'job-reconnect',
        status: 'running',
        cost: 0.3,
        tokens_total: 9,
        nodes: [
          {
            node_id: 'agent-a',
            node_type: 'prompt',
            status: 'completed',
            output: 'authoritative A',
            cost: 0.2,
            tokens_input: 2,
            tokens_output: 3,
            latency_ms: 40,
          },
          {
            node_id: 'agent-b',
            node_type: 'prompt',
            status: 'failed',
            error_message: 'provider failed',
            cost: 0.1,
            tokens_input: 4,
          },
          { node_id: 'result-final', node_type: 'result', status: 'running' },
        ],
      };

      useEnsembleStore.getState().applyJobSnapshot(runningSnapshot, 'job-reconnect', 'synthesis');

      let state = useEnsembleStore.getState();
      expect(state.executionState.status).toBe('streaming');
      expect(state.isExecuting).toBe(true);
      expect(state.phase).toBe('aggregating');
      expect(state.totalCost).toBe(0.3);
      expect(state.totalTokens).toBe(9);
      expect(state.agents[0]).toMatchObject({
        status: 'done',
        output: 'authoritative A',
        streamingText: 'authoritative A',
        tokens: 5,
        cost: 0.2,
      });
      expect(state.agents[1]).toMatchObject({ status: 'error', error: 'provider failed', tokens: 4, cost: 0.1 });
      expect(state.history).toHaveLength(0);

      const completedSnapshot: EnsembleJobSnapshot = {
        ...runningSnapshot,
        status: 'completed',
        result_text: 'authoritative final',
        cost: 0.35,
        tokens_total: 12,
        nodes: [
          ...(runningSnapshot.nodes ?? []).slice(0, 2),
          {
            node_id: 'result-final',
            node_type: 'result',
            status: 'completed',
            output: 'authoritative final',
          },
        ],
      };

      useEnsembleStore.getState().applyJobSnapshot(completedSnapshot, 'job-reconnect', 'synthesis');
      useEnsembleStore.getState().applyJobSnapshot(completedSnapshot, 'job-reconnect', 'synthesis');

      state = useEnsembleStore.getState();
      expect(state.executionState).toEqual({ status: 'complete', jobId: 'job-reconnect' });
      expect(state.history).toHaveLength(1);
      expect(state.history[0]).toMatchObject({
        jobId: 'job-reconnect',
        synthesizedResponse: 'authoritative final',
        totalCost: 0.35,
        totalTokens: 12,
        aggregationMethod: 'synthesis',
      });
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
