import { Handle, type Node, type NodeProps, Position } from '@xyflow/react';
import type React from 'react';
import { useWorkflowStore } from '../../../stores/workflowStore';
import type { NodeData } from '../../../types/workflow';
import ExecutionBadge from './ExecutionBadge';

// Human-readable names for aggregation methods
const aggregationLabels: Record<string, string> = {
  collect: 'Collect',
  judge: 'Judge',
  scoring: 'Scoring',
  synthesis: 'Synthesis',
  peer_matrix: 'Peer Matrix',
};

type ResultNodeType = Node<NodeData, 'result'>;

const ResultNode: React.FC<NodeProps<ResultNodeType>> = ({ data, selected, id }) => {
  const executionState = useWorkflowStore((state) => state.nodeExecutionStates[id]);
  const replaySelectedNodeId = useWorkflowStore((state) => state.replaySelectedNodeId);
  const replayNodes = useWorkflowStore((state) => state.replayNodes);
  const isReplayFocused =
    replaySelectedNodeId != null && (replaySelectedNodeId === id || replaySelectedNodeId.split('__')[0] === id);

  // Count subnodes for this node during replay
  const subnodes = replayNodes.filter((s) => s.node_id !== id && s.node_id.startsWith(`${id}__`));
  const subnodeCompleted = subnodes.filter((s) => s.status === 'completed').length;
  const subnodeFailed = subnodes.filter((s) => s.status === 'failed').length;

  const getBackgroundColor = () => {
    if (!executionState) return '#9C27B0'; // Purple for initial state
    if (executionState.status === 'idle') return '#9C27B0';
    if (executionState.status === 'running') return '#FFB74D'; // Orange for running
    if (executionState.status === 'completed') return '#66BB6A'; // Green for completed
    if (executionState.status === 'error') return '#EF5350'; // Red for error
    return '#9C27B0';
  };

  const getBorderColor = () => {
    if (selected) {
      if (executionState?.status === 'running') return '#F57C00';
      if (executionState?.status === 'completed') return '#43A047';
      if (executionState?.status === 'error') return '#E53935';
      return '#7B1FA2';
    }
    if (executionState?.status === 'running') return '#FB8C00';
    if (executionState?.status === 'completed') return '#4CAF50';
    if (executionState?.status === 'error') return '#F44336';
    return '#AB47BC';
  };

  const aggregationMethod = data.config.aggregationMethod || 'collect';
  const aggregationLabel = aggregationLabels[aggregationMethod] || aggregationMethod;
  const isCompiledAggregationInternal =
    data.config.aggregationInternalState === 'expanded' || data.config.aggregationInternalState === 'forked';

  return (
    <div
      data-testid="workflow-node-result"
      data-node-id={id}
      style={{
        padding: '15px',
        borderRadius: '8px',
        backgroundColor: getBackgroundColor(),
        color: 'white',
        border: `2px solid ${getBorderColor()}`,
        minWidth: '150px',
        boxShadow: isReplayFocused
          ? '0 0 0 3px #FFD54F, 0 0 16px 4px rgba(255, 213, 79, 0.5)'
          : selected
            ? '0 4px 8px rgba(0,0,0,0.3)'
            : '0 2px 4px rgba(0,0,0,0.2)',
        position: 'relative',
        transition: 'all 0.3s ease',
      }}
    >
      <Handle
        type="target"
        position={Position.Top}
        style={{
          background: '#7B1FA2',
          width: '10px',
          height: '10px',
          border: '2px solid white',
        }}
      />
      {isCompiledAggregationInternal && (
        <Handle
          type="source"
          position={Position.Bottom}
          style={{
            background: '#7B1FA2',
            width: '10px',
            height: '10px',
            border: '2px solid white',
          }}
        />
      )}
      {/* Status Badge */}
      {executionState && <ExecutionBadge status={executionState.status} />}

      <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>{data.label}</div>
      {data.config.name && (
        <div style={{ fontSize: '12px', opacity: 0.9, marginBottom: '3px' }}>{data.config.name}</div>
      )}
      {/* Reference ID badge */}
      <div
        style={{
          fontSize: '10px',
          fontFamily: 'monospace',
          backgroundColor: 'rgba(0,0,0,0.2)',
          padding: '2px 6px',
          borderRadius: '3px',
          marginBottom: '4px',
          display: 'inline-block',
        }}
      >
        {`{{${data.config.alias || id}}}`}
      </div>
      {aggregationMethod !== 'collect' && (
        <div
          style={{
            fontSize: '10px',
            opacity: 0.8,
            marginTop: '4px',
            padding: '2px 6px',
            backgroundColor: 'rgba(255,255,255,0.2)',
            borderRadius: '4px',
            display: 'inline-block',
          }}
        >
          {aggregationLabel}
        </div>
      )}

      {/* Subnode indicator during replay */}
      {subnodes.length > 0 && (
        <div
          style={{
            marginTop: '6px',
            paddingTop: '6px',
            borderTop: '1px solid rgba(255,255,255,0.3)',
            fontSize: '9px',
            display: 'flex',
            alignItems: 'center',
            gap: '4px',
            flexWrap: 'wrap',
          }}
        >
          <span style={{ opacity: 0.9 }}>
            {subnodeCompleted}/{subnodes.length} subnodes
          </span>
          {/* Status dots */}
          <div style={{ display: 'flex', gap: '2px' }}>
            {subnodes.map((s) => (
              <div
                key={s.node_id}
                style={{
                  width: '6px',
                  height: '6px',
                  borderRadius: '50%',
                  backgroundColor:
                    s.status === 'completed'
                      ? 'rgba(255,255,255,0.9)'
                      : s.status === 'failed'
                        ? '#ffcdd2'
                        : s.status === 'running'
                          ? '#FFE082'
                          : 'rgba(255,255,255,0.3)',
                }}
                title={`${s.node_name || s.node_label || s.node_id}: ${s.status}`}
              />
            ))}
          </div>
          {subnodeFailed > 0 && <span style={{ color: '#ffcdd2', fontWeight: 600 }}>{subnodeFailed} failed</span>}
        </div>
      )}
    </div>
  );
};

export default ResultNode;
