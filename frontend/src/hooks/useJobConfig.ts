import { useCallback, useEffect, useState } from 'react';
import type { ConfigDiff, NormalizedConfig } from '../types/events';

const API_BASE = '/api';

export interface UseJobConfigResult {
  config: NormalizedConfig | null;
  isLoading: boolean;
  error: Error | null;
}

export function useJobConfig(jobId: string | null): UseJobConfigResult {
  const [config, setConfig] = useState<NormalizedConfig | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    if (!jobId) {
      setConfig(null);
      return;
    }

    let cancelled = false;
    setIsLoading(true);
    setError(null);

    fetch(`${API_BASE}/jobs/${jobId}/config`)
      .then((res) => {
        if (!res.ok) throw new Error(`Failed to fetch config: ${res.statusText}`);
        return res.json();
      })
      .then((data: NormalizedConfig) => {
        if (!cancelled) setConfig(data);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err : new Error('Unknown error'));
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [jobId]);

  return { config, isLoading, error };
}

export function useFetchConfigDiff(): {
  diff: ConfigDiff | null;
  isLoading: boolean;
  error: Error | null;
  fetchDiff: (jobIdA: string, jobIdB: string) => void;
  clearDiff: () => void;
} {
  const [diff, setDiff] = useState<ConfigDiff | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const fetchDiff = useCallback((jobIdA: string, jobIdB: string) => {
    setIsLoading(true);
    setError(null);

    fetch(`${API_BASE}/jobs/${jobIdA}/diff/${jobIdB}`)
      .then((res) => {
        if (!res.ok) throw new Error(`Failed to fetch diff: ${res.statusText}`);
        return res.json();
      })
      .then((data: ConfigDiff) => {
        setDiff(data);
      })
      .catch((err) => {
        setError(err instanceof Error ? err : new Error('Unknown error'));
      })
      .finally(() => {
        setIsLoading(false);
      });
  }, []);

  const clearDiff = useCallback(() => {
    setDiff(null);
    setError(null);
  }, []);

  return { diff, isLoading, error, fetchDiff, clearDiff };
}
