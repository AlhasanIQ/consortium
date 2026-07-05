import type React from 'react';
import { useCallback, useEffect, useState } from 'react';
import { ConnectionStatusBanner } from '../components/common/ConnectionStatusBanner';
import { DuplicateRunDialog } from '../components/common/DuplicateRunDialog';
import PromptInput from '../components/ensemble/PromptInput';
import ResultsTimeline from '../components/ensemble/ResultsTimeline';
import RoundTable from '../components/ensemble/RoundTable';
import { WorkflowSelector } from '../components/ensemble/WorkflowSelector';
import { useEnsembleWebSocket } from '../hooks/useEnsembleWebSocket';
import { initEnsembleAgents, useEnsembleStore } from '../stores/ensembleStore';

const Ensemble: React.FC = () => {
  const { execute, executeForceNew, cancel, retry, lastDuplicate, clearDuplicate, streamExistingJob } =
    useEnsembleWebSocket();

  const handleOpenExisting = useCallback(() => {
    const jobId = lastDuplicate?.jobId;
    clearDuplicate();
    if (jobId) {
      void streamExistingJob(jobId);
    }
  }, [clearDuplicate, lastDuplicate, streamExistingJob]);

  const handleStartNew = useCallback(() => {
    clearDuplicate();
    executeForceNew();
  }, [clearDuplicate, executeForceNew]);

  const handleCloseDialog = useCallback(() => {
    clearDuplicate();
  }, [clearDuplicate]);
  const {
    isExecuting,
    reset,
    agents,
    history,
    backendHistory,
    historyLoading,
    phase,
    selectedWorkflowId,
    fetchBackendHistory,
    loadDraft,
    connectionState,
  } = useEnsembleStore();

  const socketOnline = connectionState.status === 'connected' || connectionState.status === 'recovered';
  const socketConnecting = connectionState.status === 'connecting' || connectionState.status === 'reconnecting';
  const socketLabel = socketConnecting ? 'Connecting' : socketOnline ? 'Live' : 'Disconnected';

  // Mobile detection
  const [isMobile, setIsMobile] = useState(window.innerWidth < 640);

  // Collapsible history panel state
  const [historyCollapsed, setHistoryCollapsed] = useState(false);

  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < 640);
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // Set page title
  useEffect(() => {
    document.title = 'Consortium - Ensemble';
  }, []);

  useEffect(() => {
    void initEnsembleAgents();
  }, []);

  // Load any saved draft on mount
  useEffect(() => {
    loadDraft();
  }, [loadDraft]);

  // Fetch backend history on mount
  useEffect(() => {
    fetchBackendHistory();
  }, [fetchBackendHistory]);

  // Determine if we should show split layout (ring left, timeline right)
  // Show split layout when: we have local history, backend history, OR loading history
  const hasResults = history.length > 0 || backendHistory.length > 0 || historyLoading || phase === 'complete';

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isExecuting) {
        cancel();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isExecuting, cancel]);

  return (
    <div
      style={{
        height: '100vh',
        background: 'linear-gradient(135deg, #0a0a0f 0%, #1a1a2e 50%, #0f0f1a 100%)',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
      }}
    >
      {/* Connection status banner */}
      <ConnectionStatusBanner onRetry={retry} />

      {/* Header with model selector */}
      <header
        style={{
          flexShrink: 0,
          padding: isMobile ? '8px 12px' : '12px 24px',
          borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: isMobile ? '8px' : '0',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: isMobile ? '8px' : '16px' }}>
          <h1
            style={{
              fontSize: isMobile ? '14px' : '18px',
              fontWeight: '600',
              color: '#fff',
              margin: 0,
              letterSpacing: '1px',
            }}
          >
            {isMobile ? 'Consortium' : 'The Consortium'}
          </h1>
          {isExecuting && (
            <div
              title={`WebSocket: ${connectionState.status}`}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '6px',
                padding: isMobile ? '2px 6px' : '3px 8px',
                borderRadius: '999px',
                background: socketOnline
                  ? 'rgba(16, 185, 129, 0.16)'
                  : socketConnecting
                    ? 'rgba(245, 158, 11, 0.16)'
                    : 'rgba(239, 68, 68, 0.16)',
                border: socketOnline
                  ? '1px solid rgba(16, 185, 129, 0.4)'
                  : socketConnecting
                    ? '1px solid rgba(245, 158, 11, 0.4)'
                    : '1px solid rgba(239, 68, 68, 0.4)',
              }}
            >
              <span
                style={{
                  width: isMobile ? '6px' : '7px',
                  height: isMobile ? '6px' : '7px',
                  borderRadius: '50%',
                  background: socketOnline ? '#10B981' : socketConnecting ? '#F59E0B' : '#EF4444',
                  animation: socketOnline ? 'pulse 2s ease-in-out infinite' : undefined,
                }}
              />
              {!isMobile && <span style={{ fontSize: '10px', color: '#d1d5db' }}>{socketLabel}</span>}
            </div>
          )}
          {!isMobile && <WorkflowSelector />}
        </div>

        {/* Model selector pills */}
        <div style={{ display: 'flex', gap: isMobile ? '4px' : '8px', alignItems: 'center' }}>
          {agents.map((agent) => (
            <div
              key={agent.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: isMobile ? '3px' : '6px',
                padding: isMobile ? '3px 6px' : '4px 10px',
                background: `${agent.color}22`,
                borderRadius: isMobile ? '8px' : '10px',
                border: `1px solid ${agent.color}44`,
              }}
            >
              <span
                style={{
                  width: isMobile ? '5px' : '6px',
                  height: isMobile ? '5px' : '6px',
                  borderRadius: '50%',
                  background: agent.color,
                }}
              />
              {!isMobile && <span style={{ fontSize: '11px', color: '#ccc' }}>{agent.displayName}</span>}
            </div>
          ))}
          {!isMobile && (
            <div style={{ width: '1px', height: '16px', background: 'rgba(255,255,255,0.1)', margin: '0 8px' }} />
          )}

          {/* Cancel button (when executing) */}
          {isExecuting && (
            <button
              type="button"
              onClick={cancel}
              style={{
                padding: isMobile ? '3px 8px' : '4px 10px',
                background: 'rgba(239, 68, 68, 0.2)',
                border: '1px solid rgba(239, 68, 68, 0.4)',
                borderRadius: '8px',
                color: '#EF4444',
                fontSize: isMobile ? '10px' : '11px',
                cursor: 'pointer',
              }}
            >
              {isMobile ? '✕' : 'Cancel'}
            </button>
          )}

          {/* Reset button */}
          {!isExecuting && (
            <button
              type="button"
              onClick={reset}
              style={{
                padding: isMobile ? '3px 8px' : '4px 10px',
                background: 'rgba(255,255,255,0.05)',
                border: '1px solid rgba(255,255,255,0.1)',
                borderRadius: '8px',
                color: '#666',
                fontSize: isMobile ? '10px' : '11px',
                cursor: 'pointer',
              }}
            >
              Reset
            </button>
          )}

          {/* Navigation links - hide on mobile */}
          {!isMobile && (
            <>
              <a
                href={`/workflow/${selectedWorkflowId}`}
                style={{
                  padding: '4px 10px',
                  background: 'rgba(139, 92, 246, 0.2)',
                  border: '1px solid rgba(139, 92, 246, 0.4)',
                  borderRadius: '8px',
                  color: '#8B5CF6',
                  fontSize: '11px',
                  textDecoration: 'none',
                }}
              >
                Edit Workflow
              </a>
              <a
                href="/admin"
                style={{
                  padding: '4px 10px',
                  background: 'rgba(14, 165, 233, 0.18)',
                  border: '1px solid rgba(14, 165, 233, 0.38)',
                  borderRadius: '8px',
                  color: '#38BDF8',
                  fontSize: '11px',
                  textDecoration: 'none',
                }}
              >
                Admin Panel
              </a>
            </>
          )}
        </div>
      </header>

      {/* Main content area */}
      <div
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: isMobile && hasResults ? 'column' : 'row',
          overflow: 'hidden',
          minHeight: 0,
        }}
      >
        {hasResults ? (
          isMobile ? (
            // Mobile: vertical stack (ring on top, timeline below)
            <>
              <div
                style={{
                  flexShrink: 0,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  padding: '10px',
                  borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
                }}
              >
                <RoundTable compact />
              </div>
              <div
                style={{
                  flex: 1,
                  overflow: 'auto',
                }}
              >
                <ResultsTimeline />
              </div>
            </>
          ) : (
            // Desktop: Split layout - Ring left, Timeline right (collapsible)
            <>
              <div
                style={{
                  width: historyCollapsed ? '100%' : '35%',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  borderRight: historyCollapsed ? 'none' : '1px solid rgba(255, 255, 255, 0.08)',
                  padding: '20px',
                  overflow: 'visible',
                  transition: 'width 0.3s ease',
                }}
              >
                <RoundTable compact={!historyCollapsed} />
              </div>
              {!historyCollapsed && (
                <div
                  style={{
                    flex: 1,
                    overflow: 'auto',
                    position: 'relative',
                  }}
                >
                  <ResultsTimeline />
                </div>
              )}
              {/* Collapse/Expand toggle button */}
              <button
                type="button"
                onClick={() => setHistoryCollapsed(!historyCollapsed)}
                style={{
                  position: 'absolute',
                  right: historyCollapsed ? '20px' : 'calc(65% - 40px)',
                  top: '50%',
                  transform: 'translateY(-50%)',
                  zIndex: 100,
                  padding: '8px',
                  backgroundColor: 'transparent',
                  border: 'none',
                  color: '#8B5CF6',
                  cursor: 'pointer',
                  fontSize: '16px',
                  transition: 'right 0.3s ease',
                }}
                title={historyCollapsed ? 'Show history' : 'Hide history'}
              >
                {historyCollapsed ? '◀' : '▶'}
              </button>
            </>
          )
        ) : (
          // Full-width ring centered
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <RoundTable />
          </div>
        )}
      </div>

      {/* Prompt Input - fixed at bottom */}
      <PromptInput onSubmit={execute} />

      {/* Duplicate run dialog */}
      <DuplicateRunDialog
        isOpen={lastDuplicate !== null}
        jobId={lastDuplicate?.jobId ?? ''}
        onOpenExisting={handleOpenExisting}
        onStartNew={handleStartNew}
        onClose={handleCloseDialog}
      />

      {/* CSS Animations */}
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.5; }
        }

        @keyframes pulse-fast {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.3; }
        }

        @keyframes spin-slow {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }

        @keyframes blink {
          0%, 50% { opacity: 1; }
          51%, 100% { opacity: 0; }
        }

        @keyframes slideIn {
          from {
            opacity: 0;
            transform: translateX(20px) translateY(-50%);
          }
          to {
            opacity: 1;
            transform: translateX(0) translateY(-50%);
          }
        }

        .pulse { animation: pulse 2s ease-in-out infinite; }
        .pulse-fast { animation: pulse-fast 0.8s ease-in-out infinite; }
        .spin-slow { animation: spin-slow 2s linear infinite; display: inline-block; }
        .blink { animation: blink 1s step-end infinite; }

        /* Connection banner animations */
        @keyframes slide-in-from-top {
          from {
            opacity: 0;
            transform: translateX(-50%) translateY(-100%);
          }
          to {
            opacity: 1;
            transform: translateX(-50%) translateY(0);
          }
        }
        .animate-in.slide-in-from-top {
          animation: slide-in-from-top 0.3s ease-out forwards;
        }
        @keyframes zoom-in-95 {
          from {
            opacity: 0;
            transform: scale(0.95);
          }
          to {
            opacity: 1;
            transform: scale(1);
          }
        }
        .animate-in.zoom-in-95 {
          animation: zoom-in-95 0.2s ease-out forwards;
        }

        /* Scrollbar styling */
        ::-webkit-scrollbar {
          width: 6px;
        }
        ::-webkit-scrollbar-track {
          background: rgba(255,255,255,0.02);
          border-radius: 3px;
        }
        ::-webkit-scrollbar-thumb {
          background: rgba(255,255,255,0.1);
          border-radius: 3px;
        }
        ::-webkit-scrollbar-thumb:hover {
          background: rgba(255,255,255,0.2);
        }
      `}</style>
    </div>
  );
};

export default Ensemble;
