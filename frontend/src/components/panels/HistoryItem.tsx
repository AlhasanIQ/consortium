import type React from 'react';
import type { JobSummary } from '../../hooks/useExecutionHistory';
import { statusColors } from './executionPanelUtils';
import { ExternalLinkIcon, PlayIcon } from './PanelIcons';

export const HistoryItem: React.FC<{
  job: JobSummary;
  onReplay: (jobId: string) => void;
  isLoading: boolean;
  compareMode?: boolean;
  selectedForCompare?: string[];
  onToggleCompare?: (jobId: string) => void;
}> = ({ job, onReplay, isLoading, compareMode, selectedForCompare, onToggleCompare }) => {
  return (
    <div
      style={{
        padding: '10px',
        backgroundColor: 'white',
        borderRadius: '6px',
        marginBottom: '8px',
        border: '1px solid #e0e0e0',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        {compareMode && onToggleCompare && (
          <div style={{ display: 'flex', alignItems: 'center', marginRight: '8px', paddingTop: '2px' }}>
            <input
              type="checkbox"
              checked={selectedForCompare?.includes(job.id) ?? false}
              onChange={() => onToggleCompare(job.id)}
              disabled={(selectedForCompare?.length ?? 0) >= 2 && !selectedForCompare?.includes(job.id)}
              style={{ cursor: 'pointer', width: '14px', height: '14px' }}
            />
          </div>
        )}
        <div style={{ flex: 1 }}>
          <div
            style={{
              fontSize: '12px',
              color: '#333',
              marginBottom: '4px',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              maxWidth: compareMode ? '170px' : '200px',
            }}
            title={job.description}
          >
            {job.description || 'No description'}
          </div>
          <div style={{ display: 'flex', gap: '8px', fontSize: '10px', color: '#666' }}>
            <span
              style={{
                padding: '2px 6px',
                borderRadius: '3px',
                backgroundColor: statusColors[job.status] || '#757575',
                color: 'white',
              }}
            >
              {job.status}
            </span>
            <span>${job.cost?.toFixed(5) || '0'}</span>
            <span>{job.tokens_total || 0} tokens</span>
          </div>
          <div
            style={{
              display: 'flex',
              gap: '6px',
              alignItems: 'center',
              fontSize: '10px',
              color: '#999',
              marginTop: '4px',
            }}
          >
            <span>{new Date(job.created_at).toLocaleString()}</span>
            {job.config_hash && (
              <span
                style={{
                  fontFamily: 'monospace',
                  fontSize: '9px',
                  color: '#888',
                  backgroundColor: '#f0f0f0',
                  padding: '0 4px',
                  borderRadius: '2px',
                }}
                title={`Config: ${job.config_hash}`}
              >
                {job.config_hash.slice(0, 8)}
              </span>
            )}
            {job.dag_hash && (
              <span
                style={{
                  fontFamily: 'monospace',
                  fontSize: '9px',
                  color: '#666',
                  backgroundColor: '#e8f4e8',
                  padding: '0 4px',
                  borderRadius: '2px',
                }}
                title={`DAG: ${job.dag_hash}`}
              >
                {job.dag_hash.slice(0, 8)}
              </span>
            )}
            {job.run_number && job.run_number > 1 && (
              <span
                style={{
                  fontSize: '9px',
                  color: '#e65100',
                  backgroundColor: '#fff3e0',
                  padding: '0 4px',
                  borderRadius: '2px',
                }}
              >
                run #{job.run_number}
              </span>
            )}
          </div>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
          <button
            type="button"
            onClick={() => onReplay(job.id)}
            disabled={isLoading || job.status === 'running' || job.status === 'pending'}
            style={{
              padding: '4px 8px',
              fontSize: '10px',
              backgroundColor: isLoading ? '#ccc' : '#1976d2',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: isLoading || job.status === 'running' ? 'default' : 'pointer',
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
            }}
            title="Replay on canvas"
          >
            <PlayIcon size={10} />
            Replay
          </button>
          <a
            href={`/admin/jobs/${job.id}`}
            target="_blank"
            rel="noopener noreferrer"
            style={{
              padding: '4px 8px',
              fontSize: '10px',
              backgroundColor: '#666',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
              textDecoration: 'none',
            }}
            title="View in Admin Panel"
          >
            <ExternalLinkIcon size={10} />
            Admin
          </a>
        </div>
      </div>
    </div>
  );
};
