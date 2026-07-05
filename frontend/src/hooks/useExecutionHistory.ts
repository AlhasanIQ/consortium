import { useCallback, useEffect, useState } from 'react';

const API_BASE = '/api';

export interface JobSummary {
  id: string;
  workflow_id: string;
  description: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  tokens_total: number;
  cost: number;
  created_at: string;
  node_count: number;
  aggregation_method?: string;
  config_hash?: string;
  run_number?: number;
  dag_hash?: string;
  workflow_execution_id?: string;
}

export interface JobNode {
  node_id: string;
  node_label?: string;
  node_name?: string;
  node_type: string;
  node_order?: number;
  status: string;
  prompt?: string;
  output?: string;
  error_message?: string;
  tokens_input: number;
  tokens_output: number;
  cost: number;
  latency_ms: number;
  model?: string;
  execution_uid?: string;
  attempt_number?: number;
  started_at?: string | null;
  completed_at?: string | null;
  metadata?: string;
}

export interface JobDetail extends JobSummary {
  result_text?: string;
  tokens_input: number;
  tokens_output: number;
  updated_at: string;
  nodes: JobNode[];
}

export interface UseExecutionHistoryOptions {
  limit?: number;
  workflowId?: string;
  autoFetch?: boolean;
}

export interface UseExecutionHistoryResult {
  jobs: JobSummary[];
  isLoading: boolean;
  error: Error | null;
  refresh: () => void;
  fetchMore: () => void;
  hasMore: boolean;
}

export function useExecutionHistory(options: UseExecutionHistoryOptions = {}): UseExecutionHistoryResult {
  const { limit = 20, workflowId, autoFetch = true } = options;

  const [jobs, setJobs] = useState<JobSummary[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);

  const fetchJobs = useCallback(
    async (appendMode = false, nextCursor?: string) => {
      setIsLoading(true);
      setError(null);

      try {
        const params = new URLSearchParams();
        params.set('limit', String(limit));
        if (nextCursor) {
          params.set('cursor', nextCursor);
        }
        if (workflowId) {
          params.set('workflow_id', workflowId);
        }

        const response = await fetch(`${API_BASE}/jobs?${params.toString()}`);
        if (!response.ok) {
          throw new Error(`Failed to fetch jobs: ${response.statusText}`);
        }

        const data = await response.json();

        if (appendMode) {
          setJobs((prev) => [...prev, ...data.jobs]);
        } else {
          setJobs(data.jobs || []);
        }

        setCursor(data.pagination?.cursor || null);
        setHasMore(data.pagination?.has_more || false);
      } catch (err) {
        setError(err instanceof Error ? err : new Error('Unknown error'));
      } finally {
        setIsLoading(false);
      }
    },
    [limit, workflowId],
  );

  const refresh = useCallback(() => {
    setCursor(null);
    fetchJobs(false);
  }, [fetchJobs]);

  const fetchMore = useCallback(() => {
    if (cursor && hasMore && !isLoading) {
      fetchJobs(true, cursor);
    }
  }, [cursor, hasMore, isLoading, fetchJobs]);

  useEffect(() => {
    if (autoFetch) {
      fetchJobs(false);
    }
  }, [autoFetch, fetchJobs]);

  return {
    jobs,
    isLoading,
    error,
    refresh,
    fetchMore,
    hasMore,
  };
}

// Fetch a single job's details
export async function fetchJobDetail(jobId: string): Promise<JobDetail> {
  const response = await fetch(`${API_BASE}/jobs/${jobId}`);
  if (!response.ok) {
    throw new Error(`Failed to fetch job: ${response.statusText}`);
  }
  return response.json();
}
