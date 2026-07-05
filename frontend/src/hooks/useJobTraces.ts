import { useCallback, useEffect, useState } from 'react';
import type { TraceNodeGroup, TraceResponse } from '../types/events';

const API_BASE = '/api';

export function useJobTraces(jobId: string | null): {
  traceGroups: TraceNodeGroup[];
  isLoading: boolean;
} {
  const [traceGroups, setTraceGroups] = useState<TraceNodeGroup[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  const fetchTraces = useCallback(async (id: string) => {
    setIsLoading(true);
    try {
      const response = await fetch(`${API_BASE}/jobs/${id}/trace`);
      if (!response.ok) {
        throw new Error(`Failed to fetch traces: ${response.statusText}`);
      }
      const data: TraceResponse = await response.json();
      setTraceGroups(data.node_groups || []);
    } catch (err) {
      console.error('Failed to fetch job traces:', err);
      setTraceGroups([]);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (jobId) {
      fetchTraces(jobId);
    } else {
      setTraceGroups([]);
    }
  }, [jobId, fetchTraces]);

  return { traceGroups, isLoading };
}
