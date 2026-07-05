import { useCallback, useRef, useState } from 'react';
import {
  EventCancelled,
  EventComplete,
  EventError,
  EventJobCreated,
  EventNodeComplete,
  EventNodeFailed,
  EventNodeRetryBackoff,
  EventNodeRetryExhausted,
  EventNodeRetryStart,
  EventNodeStart,
} from '../types/events';
import type { WorkflowFileFormat } from '../types/workflowFile';
import { buildWebSocketURL } from '../utils/network';

export interface ExecutionMessageData {
  tokens_input?: number;
  tokens_output?: number;
  cost?: number;
  latency_ms?: number;
  total_tokens?: number;
  total_cost?: number;
  total_latency?: number;
  output_name?: string;
  output_value?: unknown;
  outputs?: Record<string, unknown>;
  attempt?: number;
  max_attempts?: number;
  backoff_ms?: number;
  error_code?: string;
}

export interface ExecutionMessage {
  type: string;
  message: string;
  job_id?: string;
  node_id?: string;
  output?: string;
  error?: string;
  code?: string; // Machine-readable error code
  timestamp: string;
  data?: ExecutionMessageData;
}

export interface ExecutionResult {
  total_tokens?: number;
  total_cost?: number;
  total_latency?: number;
  outputs?: Record<string, unknown>;
}

export interface ExecutionState {
  isExecuting: boolean;
  isCancelled: boolean;
  messages: ExecutionMessage[];
  jobId?: string;
  currentNode?: string;
  error?: string;
  errorCode?: string;
  result?: ExecutionResult;
}

export interface ExecuteWorkflowRequest {
  workflowFile: WorkflowFileFormat;
  inputValues?: Record<string, unknown>;
}

export function useWorkflowExecution() {
  const [executionState, setExecutionState] = useState<ExecutionState>({
    isExecuting: false,
    isCancelled: false,
    messages: [],
  });

  const wsRef = useRef<WebSocket | null>(null);

  const execute = useCallback(async ({ workflowFile, inputValues = {} }: ExecuteWorkflowRequest) => {
    const applyTerminalState = (
      status: string,
      detail?: { result_text?: string; tokens_total?: number; cost?: number; error_message?: string },
    ) => {
      const isCancelled = status === 'cancelled';
      const isFailed = status === 'failed';
      setExecutionState((prev) => ({
        ...prev,
        isExecuting: false,
        isCancelled,
        error: isFailed
          ? detail?.error_message || 'Workflow failed'
          : isCancelled
            ? detail?.error_message || 'Workflow cancelled'
            : undefined,
        errorCode: isCancelled ? 'CANCELLED' : undefined,
        result:
          status === 'completed'
            ? {
                total_tokens: detail?.tokens_total,
                total_cost: detail?.cost,
                outputs: detail?.result_text ? { final_output: detail.result_text } : undefined,
              }
            : prev.result,
      }));
    };

    // Clear previous state
    setExecutionState({
      isExecuting: true,
      isCancelled: false,
      messages: [],
    });

    // Phase 1: Submit workflow to get a job ID
    let jobId: string;
    try {
      const resp = await fetch('/api/workflows/submit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ workflow_file: workflowFile, input_values: inputValues }),
      });

      if (!resp.ok) {
        const errBody = await resp.json().catch(() => ({ error: resp.statusText }));
        setExecutionState((prev) => ({
          ...prev,
          isExecuting: false,
          error: errBody.error || 'Failed to submit workflow',
          errorCode: errBody.code,
        }));
        return;
      }

      const result = await resp.json();
      jobId = result.job_id;
    } catch (err) {
      setExecutionState((prev) => ({
        ...prev,
        isExecuting: false,
        error: `Submit failed: ${err instanceof Error ? err.message : String(err)}`,
      }));
      return;
    }

    // Update state with job ID immediately
    setExecutionState((prev) => ({ ...prev, jobId }));

    // If submit deduplicated to an already-terminal run, avoid WS and show result.
    try {
      const detailResp = await fetch(`/api/jobs/${jobId}`);
      if (detailResp.ok) {
        const detail = await detailResp.json();
        if (detail?.status === 'completed' || detail?.status === 'failed' || detail?.status === 'cancelled') {
          applyTerminalState(detail.status, detail);
          return;
        }
      }
    } catch {
      // Continue with WS path.
    }

    // Phase 2: Connect to stream to execute and receive events
    const ws = new WebSocket(buildWebSocketURL(`/api/jobs/${jobId}/stream`));
    wsRef.current = ws;
    let attemptedFallback = false;

    ws.onopen = () => {
      console.log('Stream connected for job:', jobId);
    };

    ws.onmessage = (event) => {
      const parsed = JSON.parse(event.data);

      // Stream snapshots are emitted on subscribe/reconnect.
      if (parsed?.type === 'snapshot' && parsed?.job) {
        const status = String(parsed.job.status || '');
        if (status === 'completed' || status === 'failed' || status === 'cancelled') {
          applyTerminalState(status, {
            result_text: parsed.job.result_text,
            tokens_total: parsed.job.tokens_total,
            cost: parsed.job.cost,
            error_message: parsed.job.error,
          });
        }
        return;
      }

      const msg: ExecutionMessage = {
        ...parsed,
        timestamp: parsed?.timestamp || new Date().toISOString(),
      };
      console.log('Execution message:', msg);

      setExecutionState((prev) => {
        const newMessages = [...prev.messages, msg];
        const update: ExecutionState = {
          ...prev,
          messages: newMessages,
        };

        if (msg.job_id && update.jobId !== msg.job_id) {
          update.jobId = msg.job_id;
        }

        // Handle events using canonical event types
        switch (msg.type) {
          case EventJobCreated:
            // Capture job ID from job_created event
            update.jobId = msg.job_id;
            break;

          case EventNodeStart:
            update.currentNode = msg.node_id;
            break;

          case EventNodeComplete:
            // Node completed, clear current node
            if (update.currentNode === msg.node_id) {
              update.currentNode = undefined;
            }
            break;

          case EventNodeRetryBackoff:
            // Node failed, waiting before retrying
            update.currentNode = msg.node_id;
            break;

          case EventNodeRetryStart:
            // New retry attempt starting
            update.currentNode = msg.node_id;
            break;

          case EventNodeRetryExhausted:
            // All retry attempts exhausted
            break;

          case EventNodeFailed:
            // Node failed - record error but don't stop execution yet
            // The workflow may continue or fail depending on backend logic
            update.error = msg.error || msg.message;
            update.errorCode = msg.code;
            break;

          case EventComplete:
            update.isExecuting = false;
            update.result = msg.data;
            break;

          case EventError:
            update.isExecuting = false;
            update.error = msg.error || msg.message;
            update.errorCode = msg.code;
            break;

          case EventCancelled:
            update.isExecuting = false;
            update.isCancelled = true;
            update.error = msg.error || msg.message || 'Workflow cancelled';
            update.errorCode = msg.code || 'CANCELLED';
            break;
        }

        return update;
      });
    };

    ws.onerror = (error) => {
      console.error('WebSocket error:', error);
      if (attemptedFallback) {
        return;
      }
      attemptedFallback = true;

      void (async () => {
        try {
          const detailResp = await fetch(`/api/jobs/${jobId}`);
          if (detailResp.ok) {
            const detail = await detailResp.json();
            if (detail?.status === 'completed' || detail?.status === 'failed' || detail?.status === 'cancelled') {
              applyTerminalState(detail.status, detail);
              return;
            }
          }
        } catch {
          // Fall through to generic socket error.
        }

        setExecutionState((prev) => ({
          ...prev,
          isExecuting: false,
          error: 'WebSocket connection error',
        }));
      })();
    };

    ws.onclose = () => {
      console.log('WebSocket disconnected');
      setExecutionState((prev) => ({
        ...prev,
        isExecuting: false,
      }));
    };
  }, []);

  const cancel = useCallback(async () => {
    // Get the current job ID before closing WebSocket
    const jobId = wsRef.current ? executionState.jobId : undefined;

    if (wsRef.current) {
      wsRef.current.close(1000, 'User cancelled');
      wsRef.current = null;
    }

    // Call backend cancel API if we have a job ID
    if (jobId) {
      try {
        console.log('[WorkflowExecution] Cancelling job:', jobId);
        const response = await fetch(`/api/jobs/${jobId}/cancel`, {
          method: 'POST',
        });

        if (response.ok) {
          console.log('[WorkflowExecution] Job cancelled successfully');
        } else {
          const errorText = await response.text();
          console.warn('[WorkflowExecution] Cancel request failed:', response.status, errorText);
        }
      } catch (err) {
        console.warn('[WorkflowExecution] Cancel request error:', err);
      }
    }

    setExecutionState((prev) => ({
      ...prev,
      isExecuting: false,
      isCancelled: true,
    }));
  }, [executionState.jobId]);

  const clear = useCallback(() => {
    setExecutionState({
      isExecuting: false,
      isCancelled: false,
      messages: [],
    });
  }, []);

  return {
    executionState,
    execute,
    cancel,
    clear,
  };
}
