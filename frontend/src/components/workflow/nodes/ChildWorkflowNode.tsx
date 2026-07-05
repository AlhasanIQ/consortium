import { Handle, type Node, type NodeProps, Position } from '@xyflow/react';
import type React from 'react';
import { useWorkflowStore } from '../../../stores/workflowStore';
import type { NodeData } from '../../../types/workflow';
import ExecutionBadge from './ExecutionBadge';

type ChildWorkflowNodeType = Node<NodeData>;

const ChildWorkflowNode: React.FC<NodeProps<ChildWorkflowNodeType>> = ({ data, selected, id }) => {
  const executionState = useWorkflowStore((state) => state.nodeExecutionStates[id]);
  const replaySelectedNodeId = useWorkflowStore((state) => state.replaySelectedNodeId);
  const isReplayFocused =
    replaySelectedNodeId != null && (replaySelectedNodeId === id || replaySelectedNodeId.split('__')[0] === id);
  const isWorkflowRef = data.type === 'workflow_ref';
  const baseColor = isWorkflowRef ? '#0891B2' : '#7C3AED';
  const idleBorderColor = isWorkflowRef ? '#06B6D4' : '#8B5CF6';
  const selectedBorderColor = isWorkflowRef ? '#0E7490' : '#6D28D9';

  const getBackgroundColor = () => {
    if (!executionState) return baseColor;
    if (executionState.status === 'idle') return baseColor;
    if (executionState.status === 'running') return '#FFB74D';
    if (executionState.status === 'retrying') return '#FFA726';
    if (executionState.status === 'completed') return '#66BB6A';
    if (executionState.status === 'error') return '#EF5350';
    return baseColor;
  };

  const getBorderColor = () => {
    if (selected) {
      if (executionState?.status === 'running') return '#F57C00';
      if (executionState?.status === 'retrying') return '#EF6C00';
      if (executionState?.status === 'completed') return '#43A047';
      if (executionState?.status === 'error') return '#E53935';
      return selectedBorderColor;
    }
    if (executionState?.status === 'running') return '#FB8C00';
    if (executionState?.status === 'retrying') return '#F57C00';
    if (executionState?.status === 'completed') return '#4CAF50';
    if (executionState?.status === 'error') return '#F44336';
    return idleBorderColor;
  };

  return (
    <div
      data-testid={isWorkflowRef ? 'workflow-node-workflow-ref' : 'workflow-node-child-workflow'}
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
          background: selectedBorderColor,
          width: '10px',
          height: '10px',
          border: '2px solid white',
        }}
      />
      {executionState && (
        <ExecutionBadge
          status={executionState.status}
          retryAttempt={executionState.retryAttempt}
          retryMaxAttempts={executionState.retryMaxAttempts}
        />
      )}

      <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>{data.label}</div>
      {data.config.name && (
        <div style={{ fontSize: '13px', opacity: 0.95, marginBottom: '3px' }}>{data.config.name}</div>
      )}
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
      {isWorkflowRef && (data.config.workflowRefId || data.config.workflowId) && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>
          use: {data.config.workflowRefId || data.config.workflowId}
        </div>
      )}
      {isWorkflowRef && data.config.outputKey && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>output: {data.config.outputKey}</div>
      )}
      {!isWorkflowRef && data.config.childWorkflowId && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>child: {data.config.childWorkflowId}</div>
      )}
      {!isWorkflowRef && data.config.childOutputKey && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>output: {data.config.childOutputKey}</div>
      )}

      {executionState?.metrics && (
        <div
          style={{
            marginTop: '8px',
            paddingTop: '8px',
            borderTop: '1px solid rgba(255,255,255,0.3)',
            fontSize: '9px',
            opacity: 0.9,
          }}
        >
          {executionState.metrics.latency_ms && <div>⚡ {executionState.metrics.latency_ms}ms</div>}
          {executionState.metrics.cost !== undefined && <div>💰 ${executionState.metrics.cost.toFixed(4)}</div>}
        </div>
      )}

      <Handle
        type="source"
        position={Position.Bottom}
        style={{
          background: selectedBorderColor,
          width: '10px',
          height: '10px',
          border: '2px solid white',
        }}
      />
    </div>
  );
};

export default ChildWorkflowNode;
