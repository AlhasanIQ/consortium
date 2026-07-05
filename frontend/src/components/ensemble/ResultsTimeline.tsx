import type React from 'react';
import { type AgentState, type HistoryEntry, useEnsembleStore } from '../../stores/ensembleStore';
import MarkdownRenderer from '../common/MarkdownRenderer';
import { AggregationDetailsPanel } from './AggregationDetailsPanel';

// External link icon component
const ExternalLinkIcon: React.FC<{ size?: number }> = ({ size = 12 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    aria-hidden="true"
  >
    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
    <polyline points="15 3 21 3 21 9" />
    <line x1="10" y1="14" x2="21" y2="3" />
  </svg>
);

// Refresh icon component
const RefreshIcon: React.FC<{ size?: number; spinning?: boolean }> = ({ size = 14, spinning = false }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    aria-hidden="true"
    style={spinning ? { animation: 'spin 1s linear infinite' } : undefined}
  >
    <path d="M21 2v6h-6M3 22v-6h6M21 13a9 9 0 1 1-3-7.7L21 8M3 11a9 9 0 1 1 3 7.7L3 16" />
  </svg>
);

const formatTime = (date: Date): string => {
  return date.toLocaleTimeString('en-US', {
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  });
};

interface TimelineEntryProps {
  entry: HistoryEntry;
  agents: AgentState[];
}

const TimelineEntry: React.FC<TimelineEntryProps> = ({ entry, agents }) => {
  // Aggregation method colors
  const methodColors: Record<string, string> = {
    judge: '#3B82F6',
    scoring: '#F59E0B',
    synthesis: '#8B5CF6',
    peer_matrix: '#06B6D4',
    collect: '#6B7280',
  };

  // Status colors
  const statusColors: Record<string, string> = {
    completed: '#22C55E',
    failed: '#EF4444',
    running: '#3B82F6',
    pending: '#F59E0B',
    cancelled: '#6B7280',
  };

  const isFailed = entry.status === 'failed';

  return (
    <div
      style={{
        background: isFailed ? 'rgba(239, 68, 68, 0.05)' : 'rgba(255, 255, 255, 0.03)',
        borderRadius: '12px',
        padding: '16px',
        marginBottom: '12px',
        border: isFailed ? '1px solid rgba(239, 68, 68, 0.3)' : '1px solid rgba(255, 255, 255, 0.08)',
      }}
    >
      {/* User prompt */}
      <div
        style={{
          marginBottom: '12px',
          paddingBottom: '12px',
          borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
        }}
      >
        <div
          style={{
            fontSize: '11px',
            color: '#666',
            marginBottom: '4px',
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
          }}
        >
          You
        </div>
        <div
          style={{
            fontSize: '14px',
            color: '#999',
            fontStyle: 'italic',
          }}
        >
          "{entry.prompt}"
        </div>
      </div>

      {/* Synthesized response */}
      <div>
        <div
          style={{
            fontSize: '11px',
            color: isFailed ? '#EF4444' : '#8B5CF6',
            marginBottom: '8px',
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}
        >
          Consortium
          {entry.status && entry.status !== 'completed' && (
            <span
              style={{
                fontSize: '9px',
                padding: '2px 6px',
                borderRadius: '4px',
                backgroundColor: statusColors[entry.status] || '#6B7280',
                color: '#fff',
                textTransform: 'uppercase',
              }}
            >
              {entry.status}
            </span>
          )}
          {entry.aggregationMethod && !isFailed && (
            <span
              style={{
                fontSize: '9px',
                padding: '2px 6px',
                borderRadius: '4px',
                backgroundColor: methodColors[entry.aggregationMethod] || '#6B7280',
                color: '#fff',
                textTransform: 'uppercase',
              }}
            >
              {entry.aggregationMethod.replace('_', ' ')}
            </span>
          )}
        </div>
        {isFailed && !entry.synthesizedResponse ? (
          <div
            style={{
              fontSize: '14px',
              color: '#EF4444',
              fontStyle: 'italic',
            }}
          >
            Execution failed. View details in admin panel.
          </div>
        ) : (
          <MarkdownRenderer
            content={entry.synthesizedResponse}
            style={{
              fontSize: '14px',
              color: '#e0e0e0',
            }}
          />
        )}
      </div>

      {/* Aggregation details */}
      {entry.aggregationDetails && <AggregationDetailsPanel details={entry.aggregationDetails} agents={agents} />}

      {/* Metadata */}
      <div
        style={{
          marginTop: '12px',
          paddingTop: '12px',
          borderTop: '1px solid rgba(255, 255, 255, 0.08)',
          display: 'flex',
          gap: '16px',
          fontSize: '11px',
          color: '#555',
          alignItems: 'center',
        }}
      >
        <span>{formatTime(entry.timestamp)}</span>
        <span>{entry.totalTokens.toLocaleString()} tokens</span>
        <span>${entry.totalCost.toFixed(5)}</span>
        {entry.totalLatency > 0 && <span>{(entry.totalLatency / 1000).toFixed(1)}s</span>}
        {entry.jobId && (
          <a
            href={`/admin/jobs/${entry.jobId}`}
            target="_blank"
            rel="noopener noreferrer"
            className="admin-link"
            style={{
              marginLeft: 'auto',
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
              color: '#666',
              textDecoration: 'none',
              fontSize: '10px',
            }}
          >
            View in Admin
            <ExternalLinkIcon size={10} />
          </a>
        )}
      </div>
    </div>
  );
};

// Method-specific aggregation labels
const aggregationLabels: Record<string, string> = {
  collect: 'Collecting responses',
  judge: 'Judging responses',
  scoring: 'Scoring responses',
  synthesis: 'Synthesizing',
  peer_matrix: 'Peer evaluating',
};

const ResultsTimeline: React.FC = () => {
  const {
    history,
    backendHistory,
    historyLoading,
    fetchBackendHistory,
    synthesizedOutput,
    synthesisStreaming,
    prompt,
    phase,
    totalCost,
    totalTokens,
    totalLatency,
    isAggregating,
    aggregationMethod,
    aggregationDetails,
    agents,
    jobId,
  } = useEnsembleStore();

  // Merge local and backend history, avoiding duplicates by jobId
  const localJobIds = new Set(history.map((h) => h.jobId).filter(Boolean));
  const mergedHistory = [...history, ...backendHistory.filter((h) => !localJobIds.has(h.jobId))].sort(
    (a, b) => b.timestamp.getTime() - a.timestamp.getTime(),
  );

  // Show current result if complete (before it's added to history)
  const showCurrentResult = phase === 'complete' && synthesizedOutput;

  // Show aggregation phase immediately (even before streaming starts)
  const showAggregating = isAggregating;

  // Show streaming content in real-time
  const showStreamingContent = isAggregating && synthesisStreaming;

  if (mergedHistory.length === 0 && !showCurrentResult && !showAggregating) {
    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100%',
          color: '#444',
          fontSize: '14px',
          gap: '12px',
        }}
      >
        {historyLoading ? (
          <>
            <RefreshIcon size={20} spinning />
            Loading history...
          </>
        ) : (
          <>
            Results will appear here
            <button
              type="button"
              onClick={fetchBackendHistory}
              style={{
                background: 'transparent',
                border: '1px solid #333',
                borderRadius: '6px',
                padding: '6px 12px',
                color: '#666',
                cursor: 'pointer',
                fontSize: '12px',
                display: 'flex',
                alignItems: 'center',
                gap: '6px',
              }}
            >
              <RefreshIcon size={12} />
              Refresh
            </button>
          </>
        )}
      </div>
    );
  }

  return (
    <div
      style={{
        padding: '16px 24px',
        overflowY: 'auto',
        height: '100%',
      }}
    >
      <div
        style={{
          maxWidth: '800px',
          margin: '0 auto',
        }}
      >
        {/* Live aggregation/streaming */}
        {showAggregating && (
          <div
            style={{
              background: 'rgba(139, 92, 246, 0.05)',
              borderRadius: '12px',
              padding: '16px',
              marginBottom: '12px',
              border: '1px solid rgba(139, 92, 246, 0.2)',
            }}
          >
            <div
              style={{
                marginBottom: '12px',
                paddingBottom: '12px',
                borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
              }}
            >
              <div
                style={{
                  fontSize: '11px',
                  color: '#666',
                  marginBottom: '4px',
                  textTransform: 'uppercase',
                  letterSpacing: '0.5px',
                }}
              >
                You
              </div>
              <div
                style={{
                  fontSize: '14px',
                  color: '#999',
                  fontStyle: 'italic',
                }}
              >
                "{prompt}"
              </div>
            </div>
            <div>
              <div
                style={{
                  fontSize: '11px',
                  color: '#8B5CF6',
                  marginBottom: '8px',
                  textTransform: 'uppercase',
                  letterSpacing: '0.5px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                }}
              >
                Consortium
                <span style={{ fontSize: '10px', opacity: 0.7, animation: 'pulse 1.5s ease-in-out infinite' }}>
                  {aggregationMethod ? aggregationLabels[aggregationMethod] || 'Processing' : 'Processing'}...
                </span>
              </div>
              {showStreamingContent ? (
                <>
                  <MarkdownRenderer
                    content={synthesisStreaming}
                    style={{
                      fontSize: '14px',
                      color: '#e0e0e0',
                    }}
                  />
                  <span className="blink" style={{ color: '#8B5CF6' }}>
                    |
                  </span>
                </>
              ) : (
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '8px',
                    color: '#666',
                    fontSize: '13px',
                  }}
                >
                  <div
                    style={{
                      width: 8,
                      height: 8,
                      borderRadius: '50%',
                      background: '#8B5CF6',
                      animation: 'pulse 1s ease-in-out infinite',
                    }}
                  />
                  Waiting for results...
                </div>
              )}
            </div>
          </div>
        )}

        {/* Current result (if complete but not yet in history) */}
        {showCurrentResult && (
          <TimelineEntry
            entry={{
              id: 'current',
              jobId: jobId ?? undefined,
              prompt,
              synthesizedResponse: synthesizedOutput,
              timestamp: new Date(),
              totalCost,
              totalTokens,
              totalLatency,
              aggregationMethod: aggregationMethod ?? undefined,
              aggregationDetails: aggregationDetails ?? undefined,
            }}
            agents={agents}
          />
        )}

        {/* History header with refresh */}
        {mergedHistory.length > 0 && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              marginBottom: '12px',
              paddingBottom: '8px',
              borderBottom: '1px solid rgba(255, 255, 255, 0.05)',
            }}
          >
            <span style={{ fontSize: '11px', color: '#555', textTransform: 'uppercase', letterSpacing: '0.5px' }}>
              History
            </span>
            <button
              type="button"
              onClick={fetchBackendHistory}
              disabled={historyLoading}
              style={{
                background: 'transparent',
                border: 'none',
                padding: '4px',
                color: historyLoading ? '#333' : '#555',
                cursor: historyLoading ? 'default' : 'pointer',
                display: 'flex',
                alignItems: 'center',
              }}
              title="Refresh history"
            >
              <RefreshIcon size={12} spinning={historyLoading} />
            </button>
          </div>
        )}

        {/* History entries */}
        {mergedHistory.map((entry) => (
          <TimelineEntry key={entry.id} entry={entry} agents={agents} />
        ))}
      </div>
    </div>
  );
};

export default ResultsTimeline;
