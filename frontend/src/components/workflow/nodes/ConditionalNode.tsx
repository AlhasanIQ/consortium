import { Handle, type Node, type NodeProps, Position } from '@xyflow/react';
import type React from 'react';
import { useWorkflowStore } from '../../../stores/workflowStore';
import type { NodeData } from '../../../types/workflow';
import ExecutionBadge from './ExecutionBadge';
import PreviewBadge from './PreviewBadge';

type ConditionalNodeType = Node<NodeData, 'conditional'>;

const ConditionalNode: React.FC<NodeProps<ConditionalNodeType>> = ({ data, selected, id }) => {
  const executionState = useWorkflowStore((state) => state.nodeExecutionStates[id]);
  const replaySelectedNodeId = useWorkflowStore((state) => state.replaySelectedNodeId);
  const isReplayFocused =
    replaySelectedNodeId != null && (replaySelectedNodeId === id || replaySelectedNodeId.split('__')[0] === id);

  const getBackgroundColor = () => {
    if (!executionState) return '#FF9800'; // Orange for initial state
    if (executionState.status === 'idle') return '#FF9800';
    if (executionState.status === 'running') return '#FFA726'; // Lighter orange for running
    if (executionState.status === 'completed') return '#66BB6A'; // Green for completed
    if (executionState.status === 'error') return '#EF5350'; // Red for error
    return '#FF9800';
  };

  const getBorderColor = () => {
    if (selected) {
      if (executionState?.status === 'running') return '#EF6C00';
      if (executionState?.status === 'completed') return '#43A047';
      if (executionState?.status === 'error') return '#E53935';
      return '#E65100';
    }
    if (executionState?.status === 'running') return '#F57C00';
    if (executionState?.status === 'completed') return '#4CAF50';
    if (executionState?.status === 'error') return '#F44336';
    return '#FB8C00';
  };

  return (
    <div
      data-testid="workflow-node-conditional"
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
          background: '#F57C00',
          width: '10px',
          height: '10px',
          border: '2px solid white',
        }}
      />
      {/* Status Badge */}
      {executionState && <ExecutionBadge status={executionState.status} />}

      <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>{data.label}</div>
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
      {data.config.condition && <div style={{ fontSize: '12px', opacity: 0.9 }}>{data.config.condition}</div>}
      <PreviewBadge config={data.config} />
      <Handle
        type="source"
        position={Position.Bottom}
        id="true"
        style={{
          background: '#4CAF50',
          width: '10px',
          height: '10px',
          border: '2px solid white',
          left: '30%',
        }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        id="false"
        style={{
          background: '#F44336',
          width: '10px',
          height: '10px',
          border: '2px solid white',
          left: '70%',
        }}
      />
    </div>
  );
};

export default ConditionalNode;
