import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { type JobDetail, useExecutionHistory } from '../../hooks/useExecutionHistory';
import type { ExecutionState } from '../../hooks/useWorkflowExecution';
import { type ReplayJobInfo, type ReplayNodeResult, useWorkflowStore } from '../../stores/workflowStore';
import ConfigPanel from './ConfigPanel';
import DiffView from './DiffView';
import { getBaseNodeId } from './executionPanelUtils';
import { HistoryTab } from './HistoryTab';
import { LogTab } from './LogTab';
import { ReplayTab } from './ReplayTab';

type TabId = 'config' | 'log' | 'history' | 'replay' | 'diff';

interface InspectorPanelProps {
  executionState: ExecutionState;
  onClear: () => void;
  workflowId?: string;
}

const tabButtonStyle = (isActive: boolean): React.CSSProperties => ({
  flex: 1,
  padding: '8px',
  fontSize: '13px',
  backgroundColor: isActive ? 'white' : 'transparent',
  border: 'none',
  borderBottom: isActive ? '2px solid #1976d2' : '2px solid transparent',
  color: isActive ? '#1976d2' : '#666',
  cursor: 'pointer',
  fontWeight: isActive ? 'bold' : 'normal',
});

const InspectorPanel: React.FC<InspectorPanelProps> = ({ executionState, onClear, workflowId }) => {
  const { result, isExecuting } = executionState;
  const [activeTab, setActiveTab] = useState<TabId>('log');
  const [diffData, setDiffData] = useState<{ left: JobDetail; right: JobDetail } | null>(null);

  const {
    jobs,
    isLoading: historyLoading,
    refresh,
    hasMore,
    fetchMore,
  } = useExecutionHistory({
    workflowId,
    limit: 10,
  });

  const {
    enterReplayMode,
    replayJobId,
    replayJob,
    replayNodes,
    replayNodeResults,
    replaySelectedNodeId,
    requestReplayFocus,
    setReplaySelectedNodeId,
    nodes,
    selectedNodeId,
  } = useWorkflowStore();

  const nodeIdSet = useMemo(() => new Set(nodes.map((node) => node.id)), [nodes]);
  const selectedBaseId = useMemo(
    () => (replaySelectedNodeId ? getBaseNodeId(replaySelectedNodeId) : null),
    [replaySelectedNodeId],
  );

  const hasSelectedNode = selectedNodeId !== null;

  // Refresh history when execution completes
  useEffect(() => {
    if (result && !isExecuting) {
      refresh();
    }
  }, [result, isExecuting, refresh]);

  // Auto-switch to Config when a node is selected (not in replay mode)
  useEffect(() => {
    if (hasSelectedNode && !replayJobId) {
      setActiveTab('config');
    }
  }, [hasSelectedNode, replayJobId]);

  // Switch away from Config when node deselected
  useEffect(() => {
    if (!hasSelectedNode && activeTab === 'config') {
      setActiveTab(replayJobId ? 'replay' : 'log');
    }
  }, [hasSelectedNode, activeTab, replayJobId]);

  // Auto-switch to Log when execution starts
  useEffect(() => {
    if (isExecuting) {
      setActiveTab('log');
    }
  }, [isExecuting]);

  // Auto-switch away from Replay/Diff when exiting replay mode
  useEffect(() => {
    if (!replayJobId && (activeTab === 'replay' || activeTab === 'diff')) {
      setActiveTab('history');
    }
  }, [replayJobId, activeTab]);

  // Auto-switch away from Diff when diff closed
  useEffect(() => {
    if (!diffData && activeTab === 'diff') {
      setActiveTab('history');
    }
  }, [diffData, activeTab]);

  // Sync canvas selection -> replay selection
  useEffect(() => {
    if (!replayJobId || !selectedNodeId) return;
    const baseId = getBaseNodeId(selectedNodeId);
    if (selectedBaseId === baseId) return;
    const hasNodes = replayNodes.some((node) => getBaseNodeId(node.node_id) === baseId);
    if (hasNodes) {
      setReplaySelectedNodeId(baseId);
    }
  }, [replayJobId, selectedNodeId, replayNodes, selectedBaseId, setReplaySelectedNodeId]);

  const handleEnterReplay = (job: ReplayJobInfo, nodeResults: ReplayNodeResult[]) => {
    enterReplayMode(job, nodeResults);
  };

  const handleDiffLoaded = (left: JobDetail, right: JobDetail) => {
    setDiffData({ left, right });
  };

  const closeDiff = () => {
    setDiffData(null);
    setActiveTab('history');
  };

  return (
    <div
      style={{
        width: '380px',
        padding: '20px',
        backgroundColor: '#f5f5f5',
        borderLeft: '1px solid #ddd',
        overflowY: 'auto',
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
      }}
    >
      {/* Tab navigation */}
      <div style={{ display: 'flex', marginBottom: '15px', borderBottom: '1px solid #ddd' }}>
        {hasSelectedNode && (
          <button type="button" onClick={() => setActiveTab('config')} style={tabButtonStyle(activeTab === 'config')}>
            Config
          </button>
        )}
        <button type="button" onClick={() => setActiveTab('log')} style={tabButtonStyle(activeTab === 'log')}>
          Log
        </button>
        <button type="button" onClick={() => setActiveTab('history')} style={tabButtonStyle(activeTab === 'history')}>
          History {jobs.length > 0 && `(${jobs.length})`}
        </button>
        {replayJobId && (
          <button type="button" onClick={() => setActiveTab('replay')} style={tabButtonStyle(activeTab === 'replay')}>
            Replay
          </button>
        )}
        {diffData && (
          <button type="button" onClick={() => setActiveTab('diff')} style={tabButtonStyle(activeTab === 'diff')}>
            Diff
          </button>
        )}
      </div>

      {activeTab === 'config' && hasSelectedNode && <ConfigPanel />}

      {activeTab === 'log' && <LogTab executionState={executionState} onClear={onClear} replayJobId={replayJobId} />}

      {activeTab === 'history' && (
        <HistoryTab
          jobs={jobs}
          historyLoading={historyLoading}
          hasMore={hasMore}
          fetchMore={fetchMore}
          onEnterReplay={handleEnterReplay}
          onSetActiveTab={setActiveTab}
          onDiffLoaded={handleDiffLoaded}
          onDiffError={() => {}}
        />
      )}

      {activeTab === 'replay' && (
        <ReplayTab
          replayJobId={replayJobId}
          replayJob={replayJob}
          replayNodes={replayNodes}
          replayNodeResults={replayNodeResults}
          replaySelectedNodeId={replaySelectedNodeId}
          requestReplayFocus={requestReplayFocus}
          nodeIdSet={nodeIdSet}
          selectedBaseId={selectedBaseId}
        />
      )}

      {activeTab === 'diff' && diffData && (
        <div style={{ flex: 1, overflowY: 'auto' }}>
          <DiffView left={diffData.left} right={diffData.right} onClose={closeDiff} />
        </div>
      )}
    </div>
  );
};

export default InspectorPanel;
