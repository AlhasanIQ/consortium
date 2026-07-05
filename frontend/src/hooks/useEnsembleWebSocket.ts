import { useCallback, useEffect, useRef, useState } from 'react';
import { generateIdempotencyKey, useEnsembleStore } from '../stores/ensembleStore';
import type { ConnectionState } from '../types/connection';
import {
  type AggregationDetails,
  type AggregationMethod,
  DEFAULT_PROMPT_MAX_TOKENS,
  DEFAULT_PROMPT_TEMPERATURE,
  DEFAULT_PROMPT_TIMEOUT_SECONDS,
  type EvaluationMatrix,
  resolveRetryPolicy,
} from '../types/workflow';
import { buildExecutionRequest, loadWorkflowById } from '../utils/ensembleSync';
import { type WebSocketEvent, WebSocketManager, type WebSocketManagerCallbacks } from '../utils/WebSocketManager';

interface EnsembleMessage extends WebSocketEvent {
  type: string;
  job_id?: string;
  node_id?: string;
  node_type?: string;
  model?: string;
  chunk?: string;
  output?: string;
  tokens?: number;
  latency_ms?: number;
  cost?: number;
  total_cost?: number;
  error?: string;
  message?: string;
  timestamp?: string;
  aggregation_method?: AggregationMethod;
  data?: {
    cost?: number;
    latency_ms?: number;
    tokens_input?: number;
    tokens_output?: number;
    total_cost?: number;
    aggregation_method?: AggregationMethod;
    aggregation_winner?: string;
    aggregation_scores?: Record<string, number>;
    aggregation_reasoning?: string;
    eval_matrix?: EvaluationMatrix;
    node_type?: string;
    [key: string]: unknown;
  };
}

interface SubmitResponse {
  job_id: string;
  workflow_id: string;
  duplicate: boolean;
  dedup_reason?: string;
  dedup_source_job_id?: string;
  dedup_source_status?: string;
  dedup_bypassed?: boolean;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
}

export interface DuplicateInfo {
  jobId: string;
  reason?: string;
  sourceStatus?: string;
}

const API_BASE = '/api';

export function useEnsembleWebSocket() {
  const wsManagerRef = useRef<WebSocketManager | null>(null);
  const currentIdempotencyKeyRef = useRef<string | null>(null);
  const [lastDuplicate, setLastDuplicate] = useState<DuplicateInfo | null>(null);

  const {
    prompt,
    agents,
    selectedWorkflowId,
    availableWorkflows,
    startExecution,
    updateAgentStatus,
    appendAgentStream,
    completeAgent,
    failAgent,
    startAggregation,
    appendAggregationStream,
    completeAggregation,
    reset,
    canExecute,
    startSubmitting,
    setStreaming,
    setExecutionError,
    setExecutionComplete,
    setExecutionCancelled,
    setConnectionState,
    updateLastSequence,
    saveDraft,
    clearDraft,
  } = useEnsembleStore();

  // Auto-save draft when prompt changes
  useEffect(() => {
    if (prompt.trim()) {
      const timeoutId = setTimeout(() => {
        saveDraft();
      }, 1000); // Debounce by 1 second
      return () => clearTimeout(timeoutId);
    }
  }, [prompt, saveDraft]);

  // Helper to get the aggregation method for the selected workflow
  const getSelectedWorkflowAggregationMethod = useCallback((): AggregationMethod => {
    const selectedWorkflow = availableWorkflows.find((w) => w.id === selectedWorkflowId);
    return selectedWorkflow?.aggregation_method || 'collect';
  }, [availableWorkflows, selectedWorkflowId]);

  // Map node_id to agent
  const findAgentByNodeId = useCallback(
    (nodeId: string) => {
      const normalized = nodeId.toLowerCase();

      // Try direct match first
      let agent = agents.find((a) => a.id === nodeId);
      if (agent) return agent;

      // Try matching by display name
      agent = agents.find((a) => normalized.includes(a.displayName.toLowerCase()));
      if (agent) return agent;

      // Try matching by model name
      agent = agents.find((a) => {
        const modelLower = a.model.toLowerCase();
        return modelLower.includes(normalized) || normalized.includes(modelLower.split('/').pop() || '');
      });

      return agent;
    },
    [agents],
  );

  // Handle incoming WebSocket message
  const handleMessage = useCallback(
    (msg: EnsembleMessage) => {
      console.log('[Ensemble] Message:', msg);

      switch (msg.type) {
        case 'job_created':
        case 'status':
        case 'snapshot':
          break;

        case 'node_start': {
          const nodeType = msg.data?.node_type as string | undefined;
          const isResultNode =
            nodeType === 'result' ||
            msg.node_id === 'synthesize' ||
            msg.node_id?.startsWith('result') ||
            msg.node_id?.includes('-result');
          if (isResultNode) {
            const aggregationMethod =
              msg.aggregation_method || msg.data?.aggregation_method || getSelectedWorkflowAggregationMethod();
            startAggregation(aggregationMethod);
          } else if (msg.node_id) {
            const agent = findAgentByNodeId(msg.node_id);
            if (agent) {
              updateAgentStatus(agent.id, 'thinking');
            }
          }
          break;
        }

        case 'step_streaming':
        case 'streaming': {
          const streamNodeType = msg.data?.node_type as string | undefined;
          const isStreamResultNode =
            streamNodeType === 'result' ||
            msg.node_id === 'synthesize' ||
            msg.node_id?.startsWith('result') ||
            msg.node_id?.includes('-result');
          if (isStreamResultNode) {
            if (msg.chunk) {
              appendAggregationStream(msg.chunk);
            }
          } else if (msg.node_id) {
            const agent = findAgentByNodeId(msg.node_id);
            if (agent && msg.chunk) {
              appendAgentStream(agent.id, msg.chunk, msg.tokens);
            }
          }
          break;
        }

        case 'node_complete': {
          const completeNodeType = msg.data?.node_type as string | undefined;
          const isCompleteResultNode =
            completeNodeType === 'result' ||
            msg.node_id === 'synthesize' ||
            msg.node_id?.startsWith('result') ||
            msg.node_id?.includes('-result');
          if (isCompleteResultNode) {
            const aggregationCost = msg.data?.cost ?? msg.cost ?? 0;
            const aggregationTokensInput = (msg.data?.tokens_input as number) ?? 0;
            const aggregationTokensOutput = (msg.data?.tokens_output as number) ?? 0;
            const aggregationTokens = aggregationTokensInput + aggregationTokensOutput;
            const store = useEnsembleStore.getState();
            const finalOutput = msg.output || (msg.data?.output as string) || store.synthesisStreaming;
            const totalCost = store.totalCost + aggregationCost;
            const totalTokens = store.totalTokens + aggregationTokens;

            const aggregationDetails: AggregationDetails | undefined =
              msg.data?.aggregation_method || msg.data?.aggregation_winner
                ? {
                    method: msg.data?.aggregation_method || store.aggregationMethod || 'collect',
                    winner: msg.data?.aggregation_winner,
                    scores: msg.data?.aggregation_scores,
                    reasoning: msg.data?.aggregation_reasoning,
                    evalMatrix: msg.data?.eval_matrix,
                  }
                : undefined;

            const jobId = store.executionState.status === 'streaming' ? store.executionState.jobId : store.jobId;
            completeAggregation(finalOutput, totalCost, totalTokens, aggregationDetails);
            if (jobId) {
              setExecutionComplete(jobId);
              clearDraft(); // Clear draft on successful completion
            }
          } else if (msg.node_id) {
            const agent = findAgentByNodeId(msg.node_id);
            if (agent) {
              const latencyMs = msg.data?.latency_ms ?? msg.latency_ms ?? 0;
              const cost = msg.data?.cost ?? msg.cost ?? 0;
              const tokensInput = (msg.data?.tokens_input as number) ?? 0;
              const tokensOutput = (msg.data?.tokens_output as number) ?? 0;
              const totalTokens = tokensInput + tokensOutput;
              completeAgent(agent.id, msg.output || (msg.data?.output as string) || '', latencyMs, cost, totalTokens);
            }
          }
          break;
        }

        case 'node_failed':
        case 'error':
          if (msg.node_id) {
            const agent = findAgentByNodeId(msg.node_id);
            if (agent) {
              failAgent(agent.id, msg.error || msg.message || 'Unknown error');
            }
          } else {
            const store = useEnsembleStore.getState();
            const jobId = store.executionState.status === 'streaming' ? store.executionState.jobId : store.jobId;
            setExecutionError(msg.error || msg.message || 'Unknown error', jobId || undefined);
          }
          break;

        case 'cancelled': {
          console.log('[Ensemble] Workflow cancelled:', msg.message);
          const store = useEnsembleStore.getState();
          const jobId = store.executionState.status === 'streaming' ? store.executionState.jobId : store.jobId;
          if (jobId) {
            setExecutionCancelled(jobId);
          }
          break;
        }

        case 'complete': {
          const store = useEnsembleStore.getState();
          const jobId = store.executionState.status === 'streaming' ? store.executionState.jobId : store.jobId;
          if (jobId) {
            setExecutionComplete(jobId, {
              finalOutput: msg.output || (msg.data?.final_output as string) || undefined,
              totalCost: (msg.data?.total_cost as number) || undefined,
              totalTokens: (msg.data?.total_tokens as number) || undefined,
            });
            clearDraft();
          }
          break;
        }

        default:
          console.log('[Ensemble] Unknown message type:', msg.type);
      }
    },
    [
      findAgentByNodeId,
      getSelectedWorkflowAggregationMethod,
      updateAgentStatus,
      appendAgentStream,
      completeAgent,
      failAgent,
      startAggregation,
      appendAggregationStream,
      completeAggregation,
      setExecutionError,
      setExecutionComplete,
      setExecutionCancelled,
      clearDraft,
    ],
  );

  // Handle connection state changes
  const handleStateChange = useCallback(
    (state: ConnectionState) => {
      setConnectionState(state);

      // Update last sequence from connection state
      if (state.lastSequence !== undefined) {
        updateLastSequence(state.lastSequence);
      }
    },
    [setConnectionState, updateLastSequence],
  );

  // Handle reconnection recovery
  const handleRecovered = useCallback(() => {
    console.log('[Ensemble] Connection recovered');
  }, []);

  // Handle reconnection attempts
  const handleReconnecting = useCallback((attempt: number, maxAttempts: number) => {
    console.log(`[Ensemble] Reconnecting attempt ${attempt}/${maxAttempts}`);
  }, []);

  const resetExecutionToIdle = useCallback(() => {
    useEnsembleStore.setState({
      executionState: { status: 'idle' },
      isExecuting: false,
      phase: 'idle',
    });
  }, []);

  // Helper to process completed job data
  const processCompletedJob = useCallback(
    async (jobData: Record<string, unknown>, jobId: string) => {
      const terminalStatus = String(jobData.status || '').toLowerCase();
      const nodes = (jobData.nodes || []) as Array<{
        node_id: string;
        status?: string;
        output?: string;
        error_message?: string;
        latency_ms?: number;
        cost?: number;
        tokens_input?: number;
        tokens_output?: number;
      }>;
      let synthesisOutput = '';

      const currentAgents = useEnsembleStore.getState().agents;

      const findAgent = (nodeId: string) => {
        const normalized = nodeId.toLowerCase();
        let agent = currentAgents.find((a) => a.id === nodeId);
        if (agent) return agent;
        agent = currentAgents.find((a) => normalized.includes(a.displayName.toLowerCase()));
        if (agent) return agent;
        agent = currentAgents.find((a) => {
          const modelLower = a.model.toLowerCase();
          return modelLower.includes(normalized) || normalized.includes(modelLower.split('/').pop() || '');
        });
        return agent;
      };

      // Replay the final node states into the UI.
      for (const node of nodes) {
        if (node.node_id === 'synthesize') {
          synthesisOutput = node.output || '';
        } else {
          const agent = findAgent(node.node_id);
          if (agent) {
            const totalTokens = (node.tokens_input || 0) + (node.tokens_output || 0);
            const nodeStatus = (node.status || '').toLowerCase();
            if (nodeStatus === 'failed' || nodeStatus === 'cancelled') {
              failAgent(agent.id, node.error_message || 'Node failed');
            } else if (nodeStatus === 'completed') {
              completeAgent(agent.id, node.output || '', node.latency_ms || 0, node.cost || 0, totalTokens);
            } else {
              updateAgentStatus(agent.id, 'thinking');
            }
          }
        }
      }

      if (terminalStatus === 'completed') {
        startAggregation(getSelectedWorkflowAggregationMethod());
        const finalOutput = synthesisOutput || (jobData.result_text as string) || '';
        completeAggregation(finalOutput, (jobData.cost as number) || 0);
        setExecutionComplete(jobId);
        clearDraft();
        return;
      }

      if (terminalStatus === 'cancelled') {
        setExecutionCancelled(jobId);
        return;
      }

      setExecutionError(
        (jobData.error_message as string) || (jobData.error as string) || 'Workflow failed',
        jobId || undefined,
      );
    },
    [
      updateAgentStatus,
      completeAgent,
      failAgent,
      startAggregation,
      getSelectedWorkflowAggregationMethod,
      completeAggregation,
      setExecutionComplete,
      setExecutionCancelled,
      setExecutionError,
      clearDraft,
    ],
  );

  const execute = useCallback(
    async (forceNewRun = false) => {
      if (!prompt.trim()) return;

      if (!canExecute()) {
        console.warn('[Ensemble] Cannot execute: execution already in progress');
        return;
      }

      const idempotencyKey = generateIdempotencyKey(prompt);
      currentIdempotencyKeyRef.current = idempotencyKey;

      if (!startSubmitting(idempotencyKey)) {
        console.warn('[Ensemble] Failed to start submission');
        return;
      }

      // Disconnect any existing WebSocket
      if (wsManagerRef.current) {
        wsManagerRef.current.disconnect();
        wsManagerRef.current = null;
      }

      // Build the workflow request
      let workflowRequest: Record<string, unknown> | undefined;
      try {
        const workflow = await loadWorkflowById(selectedWorkflowId);
        if (workflow) {
          workflowRequest = buildExecutionRequest(workflow, prompt);
          console.log('[Ensemble] Using workflow:', selectedWorkflowId);
        }
      } catch (err) {
        console.warn('[Ensemble] Failed to load workflow, falling back to dynamic build:', err);
      }

      // Fallback: build workflow dynamically
      if (!workflowRequest) {
        // Backend requires these explicitly on every prompt node (no implicit
        // defaults). The dynamic ensemble has no per-node config, so apply the
        // shared defaults uniformly.
        const promptDefaults = {
          temperature: DEFAULT_PROMPT_TEMPERATURE,
          max_tokens: DEFAULT_PROMPT_MAX_TOKENS,
          timeout_seconds: DEFAULT_PROMPT_TIMEOUT_SECONDS,
          retry_policy: resolveRetryPolicy({}),
        };

        const agentNodes = agents.map((a) => ({
          id: a.id,
          type: 'prompt',
          model: a.model,
          prompt: prompt,
          ...promptDefaults,
        }));

        const edges = agents.map((a) => ({
          source: a.id,
          target: 'synthesize',
        }));

        workflowRequest = {
          id: 'dynamic-ensemble',
          name: 'ensemble-query',
          context: {
            user_prompt: prompt,
          },
          nodes: [
            ...agentNodes,
            {
              id: 'synthesize',
              type: 'prompt',
              model: agents[0]?.model || 'anthropic/claude-4-sonnet-20250522',
              ...promptDefaults,
              prompt: `You are a synthesis agent. Multiple AI models have responded to the same question. Combine their responses into a single, coherent answer. Highlight areas of agreement and note any interesting differences or unique insights.

Question: ${prompt}

${agents.map((a) => `### ${a.displayName} Response:\n{{${a.id}}}`).join('\n\n')}

Please provide a synthesized response that captures the best insights from all models:`,
            },
          ],
          edges,
        };
      }

      // Submit workflow
      let jobId: string;
      try {
        console.log('[Ensemble] Submitting workflow with idempotency key:', idempotencyKey.slice(-12));
        const response = await fetch(`${API_BASE}/workflows/submit`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            workflow: workflowRequest,
            idempotency_key: idempotencyKey,
            force_new_run: forceNewRun,
          }),
        });

        if (response.status === 503) {
          const errorBody = await response.json().catch(() => ({ code: 'UNKNOWN' }));
          const msg =
            errorBody.code === 'POOL_EXHAUSTED'
              ? 'Server is at capacity. Please try again shortly.'
              : 'Server temporarily unavailable. Please try again.';
          throw new Error(msg);
        }

        if (!response.ok) {
          const errorText = await response.text();
          throw new Error(`Submit failed: ${response.status} - ${errorText}`);
        }

        const result: SubmitResponse = await response.json();
        jobId = result.job_id;

        if (result.duplicate) {
          // Contract violation guard: failed/cancelled should never be dedup hits
          if (result.status === 'failed' || result.status === 'cancelled') {
            if (forceNewRun) {
              setExecutionError('Server returned an invalid dedup response for a forced run request.');
              return;
            }
            console.warn(
              '[Ensemble] Contract violation: dedup returned',
              result.status,
              'job. Retrying with force_new_run.',
            );
            resetExecutionToIdle();
            void execute(true);
            return;
          }
          setLastDuplicate({
            jobId,
            reason: result.dedup_reason,
            sourceStatus: result.dedup_source_status,
          });
          console.log(
            '[Ensemble] Duplicate submission detected, using existing job:',
            jobId,
            'status:',
            result.status,
            'reason:',
            result.dedup_reason,
          );
          // Reset execution state so user can choose "Start New" or "View Existing"
          resetExecutionToIdle();
          return;
        } else {
          setLastDuplicate(null);
          console.log('[Ensemble] New job created:', jobId);
        }

        // Handle already-completed jobs
        if (result.status === 'completed' || result.status === 'failed' || result.status === 'cancelled') {
          console.log('[Ensemble] Job already finished, fetching results via HTTP');
          startExecution(jobId);

          const jobResponse = await fetch(`${API_BASE}/jobs/${jobId}`);
          if (jobResponse.ok) {
            const jobData = await jobResponse.json();
            await processCompletedJob(jobData, jobId);
          } else {
            setExecutionError('Failed to fetch completed job results');
          }
          return;
        }

        startExecution(jobId);
        setStreaming(jobId);
      } catch (err) {
        console.error('[Ensemble] Submit failed:', err);
        setExecutionError(err instanceof Error ? err.message : 'Submit failed');
        return;
      }

      // Create WebSocket manager with reconnection support
      const callbacks: WebSocketManagerCallbacks = {
        onMessage: handleMessage,
        onStateChange: handleStateChange,
        onReconnecting: handleReconnecting,
        onRecovered: handleRecovered,
      };

      const wsManager = new WebSocketManager(jobId, callbacks, {
        maxAttempts: 5,
        baseDelay: 1000,
        maxDelay: 30000,
        jitterFactor: 0.3,
      });

      wsManagerRef.current = wsManager;
      wsManager.connect();
    },
    [
      prompt,
      agents,
      selectedWorkflowId,
      canExecute,
      startSubmitting,
      startExecution,
      setStreaming,
      setExecutionError,
      handleMessage,
      handleStateChange,
      handleReconnecting,
      handleRecovered,
      processCompletedJob,
      resetExecutionToIdle,
    ],
  );

  const cancel = useCallback(async () => {
    const state = useEnsembleStore.getState().executionState;
    const jobId = state.status === 'streaming' ? state.jobId : null;

    currentIdempotencyKeyRef.current = null;

    // Disconnect WebSocket manager
    if (wsManagerRef.current) {
      wsManagerRef.current.disconnect();
      wsManagerRef.current = null;
    }

    if (jobId) {
      try {
        console.log('[Ensemble] Cancelling job:', jobId);
        const response = await fetch(`${API_BASE}/jobs/${jobId}/cancel`, {
          method: 'POST',
        });

        if (response.ok) {
          console.log('[Ensemble] Job cancelled successfully');
          setExecutionCancelled(jobId);
        } else {
          const errorText = await response.text();
          console.warn('[Ensemble] Cancel request failed:', response.status, errorText);
          setExecutionCancelled(jobId);
        }
      } catch (err) {
        console.warn('[Ensemble] Cancel request error:', err);
        setExecutionCancelled(jobId);
      }
    } else {
      reset();
    }
  }, [reset, setExecutionCancelled]);

  // Retry after connection failure
  const retry = useCallback(() => {
    const state = useEnsembleStore.getState();
    const executionState = state.executionState;

    if (executionState.status === 'streaming' && wsManagerRef.current) {
      // Reconnect existing WebSocket manager
      const lastSequence = wsManagerRef.current.getLastSequence();
      wsManagerRef.current.connect(lastSequence);
    } else {
      // Re-execute the workflow
      execute();
    }
  }, [execute]);

  // Clean up on unmount
  useEffect(() => {
    return () => {
      if (wsManagerRef.current) {
        wsManagerRef.current.disconnect();
        wsManagerRef.current = null;
      }
    };
  }, []);

  const executeForceNew = useCallback(() => execute(true), [execute]);
  const clearDuplicate = useCallback(() => setLastDuplicate(null), []);

  // Stream an existing dedup job (used when user clicks "View Existing")
  const streamExistingJob = useCallback(
    async (jobId: string) => {
      try {
        const jobRes = await fetch(`${API_BASE}/jobs/${jobId}`);
        if (jobRes.ok) {
          const jobData = await jobRes.json();
          if (jobData.status === 'completed' || jobData.status === 'failed' || jobData.status === 'cancelled') {
            startExecution(jobId);
            await processCompletedJob(jobData, jobId);
            return;
          }
        }
      } catch {
        // Best-effort precheck; proceed to stream.
      }

      startExecution(jobId);
      setStreaming(jobId);

      const callbacks: WebSocketManagerCallbacks = {
        onMessage: handleMessage,
        onStateChange: handleStateChange,
        onReconnecting: handleReconnecting,
        onRecovered: handleRecovered,
      };

      const wsManager = new WebSocketManager(jobId, callbacks, {
        maxAttempts: 5,
        baseDelay: 1000,
        maxDelay: 30000,
        jitterFactor: 0.3,
      });

      wsManagerRef.current = wsManager;
      wsManager.connect();
    },
    [
      startExecution,
      setStreaming,
      processCompletedJob,
      handleMessage,
      handleStateChange,
      handleReconnecting,
      handleRecovered,
    ],
  );

  return {
    execute,
    executeForceNew,
    cancel,
    retry,
    lastDuplicate,
    clearDuplicate,
    streamExistingJob,
  };
}
