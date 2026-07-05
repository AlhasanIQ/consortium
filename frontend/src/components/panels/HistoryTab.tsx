import type React from 'react';
import { useState } from 'react';
import { fetchJobDetail, type JobSummary } from '../../hooks/useExecutionHistory';
import type { ReplayJobInfo, ReplayNodeResult } from '../../stores/workflowStore';
import { HistoryItem } from './HistoryItem';

interface HistoryTabProps {
  jobs: JobSummary[];
  historyLoading: boolean;
  hasMore: boolean;
  fetchMore: () => void;
  onEnterReplay: (job: ReplayJobInfo, nodes: ReplayNodeResult[]) => void;
  onSetActiveTab: (tab: 'replay' | 'diff') => void;
  onDiffLoaded: (
    left: Awaited<ReturnType<typeof fetchJobDetail>>,
    right: Awaited<ReturnType<typeof fetchJobDetail>>,
  ) => void;
  onDiffError: () => void;
}

export const HistoryTab: React.FC<HistoryTabProps> = ({
  jobs,
  historyLoading,
  hasMore,
  fetchMore,
  onEnterReplay,
  onSetActiveTab,
  onDiffLoaded,
  onDiffError,
}) => {
  const [replayLoading, setReplayLoading] = useState(false);
  const [compareMode, setCompareMode] = useState(false);
  const [selectedForCompare, setSelectedForCompare] = useState<string[]>([]);
  const [localDiffLoading, setLocalDiffLoading] = useState(false);

  const handleReplay = async (jobId: string) => {
    setReplayLoading(true);
    try {
      const jobDetail = await fetchJobDetail(jobId);
      const replayJobInfo: ReplayJobInfo = {
        id: jobDetail.id,
        status: jobDetail.status,
        description: jobDetail.description,
        created_at: jobDetail.created_at,
        updated_at: jobDetail.updated_at,
        tokens_input: jobDetail.tokens_input,
        tokens_output: jobDetail.tokens_output,
        tokens_total: jobDetail.tokens_input + jobDetail.tokens_output,
        cost: jobDetail.cost,
        result_text: jobDetail.result_text,
      };
      onEnterReplay(replayJobInfo, jobDetail.nodes as ReplayNodeResult[]);
      onSetActiveTab('replay');
    } catch (err) {
      console.error('Failed to load job for replay:', err);
      alert('Failed to load job details for replay');
    } finally {
      setReplayLoading(false);
    }
  };

  const handleToggleCompare = (jobId: string) => {
    setSelectedForCompare((prev) => {
      if (prev.includes(jobId)) {
        return prev.filter((id) => id !== jobId);
      }
      if (prev.length >= 2) return prev;
      return [...prev, jobId];
    });
  };

  const handleCompareSelected = async () => {
    if (selectedForCompare.length !== 2) return;
    setLocalDiffLoading(true);
    try {
      const [leftDetail, rightDetail] = await Promise.all([
        fetchJobDetail(selectedForCompare[0]),
        fetchJobDetail(selectedForCompare[1]),
      ]);
      onDiffLoaded(leftDetail, rightDetail);
      onSetActiveTab('diff');
    } catch (err) {
      console.error('Failed to load job details for comparison:', err);
      alert('Failed to load job details for comparison');
      onDiffError();
    } finally {
      setLocalDiffLoading(false);
    }
  };

  const isLoading = localDiffLoading;

  return (
    <div style={{ flex: 1, overflowY: 'auto' }}>
      {jobs.length > 1 && (
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            marginBottom: '10px',
          }}
        >
          <button
            type="button"
            onClick={() => {
              setCompareMode(!compareMode);
              if (compareMode) {
                setSelectedForCompare([]);
              }
            }}
            style={{
              padding: '4px 10px',
              fontSize: '11px',
              backgroundColor: compareMode ? '#1976d2' : '#f5f5f5',
              color: compareMode ? 'white' : '#333',
              border: compareMode ? 'none' : '1px solid #ddd',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            {compareMode ? 'Cancel Compare' : 'Compare'}
          </button>
          {compareMode && selectedForCompare.length === 2 && (
            <button
              type="button"
              onClick={handleCompareSelected}
              disabled={isLoading}
              style={{
                padding: '4px 10px',
                fontSize: '11px',
                backgroundColor: isLoading ? '#ccc' : '#2e7d32',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: isLoading ? 'default' : 'pointer',
              }}
            >
              {isLoading ? 'Loading...' : 'Compare Selected'}
            </button>
          )}
          {compareMode && selectedForCompare.length > 0 && selectedForCompare.length < 2 && (
            <span style={{ fontSize: '10px', color: '#666' }}>Select {2 - selectedForCompare.length} more</span>
          )}
        </div>
      )}
      {historyLoading ? (
        <div style={{ textAlign: 'center', padding: '20px', color: '#666' }}>Loading history...</div>
      ) : jobs.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '20px', color: '#999' }}>
          No execution history for this workflow.
        </div>
      ) : (
        <>
          {jobs.map((job) => (
            <HistoryItem
              key={job.id}
              job={job}
              onReplay={handleReplay}
              isLoading={replayLoading}
              compareMode={compareMode}
              selectedForCompare={selectedForCompare}
              onToggleCompare={handleToggleCompare}
            />
          ))}
          {hasMore && (
            <button
              type="button"
              onClick={fetchMore}
              style={{
                width: '100%',
                padding: '10px',
                marginTop: '8px',
                backgroundColor: '#f5f5f5',
                border: '1px solid #ddd',
                borderRadius: '6px',
                cursor: 'pointer',
                fontSize: '12px',
                color: '#666',
              }}
            >
              Load More
            </button>
          )}
        </>
      )}
    </div>
  );
};
