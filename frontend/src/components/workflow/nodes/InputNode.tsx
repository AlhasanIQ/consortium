import { Handle, type Node, type NodeProps, Position } from '@xyflow/react';
import type React from 'react';
import { useWorkflowStore } from '../../../stores/workflowStore';
import type { NodeData } from '../../../types/workflow';
import ExecutionBadge from './ExecutionBadge';

type InputNodeType = Node<NodeData, 'input'>;

const InputNode: React.FC<NodeProps<InputNodeType>> = ({ data, selected, id }) => {
  const executionState = useWorkflowStore((state) => state.nodeExecutionStates[id]);
  const replaySelectedNodeId = useWorkflowStore((state) => state.replaySelectedNodeId);
  const isReplayFocused =
    replaySelectedNodeId != null && (replaySelectedNodeId === id || replaySelectedNodeId.split('__')[0] === id);

  const getBackgroundColor = () => {
    if (!executionState) return '#4CAF50'; // Green for initial state
    if (executionState.status === 'idle') return '#4CAF50';
    if (executionState.status === 'running') return '#FFB74D'; // Orange for running
    if (executionState.status === 'completed') return '#66BB6A'; // Lighter green for completed
    if (executionState.status === 'error') return '#EF5350'; // Red for error
    return '#4CAF50';
  };

  const getBorderColor = () => {
    if (selected) {
      if (executionState?.status === 'running') return '#F57C00';
      if (executionState?.status === 'completed') return '#2E7D32';
      if (executionState?.status === 'error') return '#E53935';
      return '#1B5E20';
    }
    if (executionState?.status === 'running') return '#FB8C00';
    if (executionState?.status === 'completed') return '#388E3C';
    if (executionState?.status === 'error') return '#F44336';
    return '#4CAF50';
  };

  return (
    <div
      data-testid="workflow-node-input"
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
      {/* Status Badge */}
      {executionState && <ExecutionBadge status={executionState.status} />}

      <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>{data.label}</div>
      {data.config.name && (
        <div style={{ fontSize: '12px', opacity: 0.9, marginBottom: '3px' }}>{data.config.name}</div>
      )}
      {/* Show schema variable names - these are what's actually referenceable */}
      {data.config.schema && Object.keys(data.config.schema).length > 0 ? (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px' }}>
          {Object.keys(data.config.schema).map((key) => (
            <div
              key={key}
              style={{
                fontSize: '10px',
                fontFamily: 'monospace',
                backgroundColor: 'rgba(0,0,0,0.2)',
                padding: '2px 6px',
                borderRadius: '3px',
                maxWidth: '100%',
                overflowWrap: 'anywhere',
                wordBreak: 'break-word',
              }}
            >
              {`{{${key}}}`}
            </div>
          ))}
        </div>
      ) : (
        <div
          style={{
            fontSize: '10px',
            fontFamily: 'monospace',
            backgroundColor: 'rgba(0,0,0,0.2)',
            padding: '2px 6px',
            borderRadius: '3px',
            display: 'inline-block',
            opacity: 0.6,
          }}
        >
          No schema defined
        </div>
      )}
      <Handle
        type="source"
        position={Position.Bottom}
        style={{
          background: '#388E3C',
          width: '10px',
          height: '10px',
          border: '2px solid white',
        }}
      />
    </div>
  );
};

export default InputNode;
