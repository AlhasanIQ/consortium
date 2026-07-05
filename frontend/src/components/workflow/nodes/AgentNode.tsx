import { Handle, type Node, type NodeProps, Position } from '@xyflow/react';
import type React from 'react';
import { useWorkflowStore } from '../../../stores/workflowStore';
import type { NodeData } from '../../../types/workflow';
import ExecutionBadge from './ExecutionBadge';
import PreviewBadge from './PreviewBadge';

type AgentNodeType = Node<NodeData, 'agent' | 'agent_run' | 'novo_run' | 'contract_extract'>;

const AgentNode: React.FC<NodeProps<AgentNodeType>> = ({ data, selected, id }) => {
  const executionState = useWorkflowStore((state) => state.nodeExecutionStates[id]);
  const replaySelectedNodeId = useWorkflowStore((state) => state.replaySelectedNodeId);
  const isReplayFocused =
    replaySelectedNodeId != null && (replaySelectedNodeId === id || replaySelectedNodeId.split('__')[0] === id);
  const isExpandedPreview = data.config.aggregationInternalState === 'expanded';
  const modelRoute = data.config.provider
    ? `${data.config.provider} / ${data.config.model || 'default'}`
    : isExpandedPreview && data.config.model
      ? data.config.model
      : '';

  const getBackgroundColor = () => {
    const idleColor = data.type === 'novo_run' ? '#BE123C' : data.type === 'agent_run' ? '#0F766E' : '#2196F3';
    if (!executionState) return idleColor;
    if (executionState.status === 'idle') return idleColor;
    if (executionState.status === 'running') return '#FFB74D'; // Orange for running
    if (executionState.status === 'retrying') return '#FFA726'; // Amber for retrying
    if (executionState.status === 'completed') return '#66BB6A'; // Green for completed
    if (executionState.status === 'error') return '#EF5350'; // Red for error
    return data.type === 'novo_run' ? '#BE123C' : data.type === 'agent_run' ? '#0F766E' : '#2196F3';
  };

  const getBorderColor = () => {
    if (selected) {
      if (executionState?.status === 'running') return '#F57C00';
      if (executionState?.status === 'retrying') return '#EF6C00';
      if (executionState?.status === 'completed') return '#43A047';
      if (executionState?.status === 'error') return '#E53935';
      return '#1976D2';
    }
    if (executionState?.status === 'running') return '#FB8C00';
    if (executionState?.status === 'retrying') return '#F57C00';
    if (executionState?.status === 'completed') return '#4CAF50';
    if (executionState?.status === 'error') return '#F44336';
    return '#42A5F5';
  };

  return (
    <div
      data-testid="workflow-node-agent"
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
          background: '#1976D2',
          width: '10px',
          height: '10px',
          border: '2px solid white',
        }}
      />
      {/* Status Badge */}
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
      {data.type === 'agent_run' && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>Novomo / {data.config.harness || 'claude-code'}</div>
      )}
      {data.type === 'novo_run' && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>Novo wake / {data.config.identity || 'sde-novo'}</div>
      )}
      {modelRoute && data.type !== 'agent_run' && data.type !== 'novo_run' && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>{modelRoute}</div>
      )}
      {(data.type === 'agent_run' || data.type === 'novo_run') && data.config.timeoutSeconds && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>Timeout: {data.config.timeoutSeconds}s</div>
      )}
      {data.config.maxTokens && data.type !== 'agent_run' && data.type !== 'novo_run' && (
        <div style={{ fontSize: '10px', opacity: 0.85 }}>Max tokens: {data.config.maxTokens}</div>
      )}

      {/* Execution metrics */}
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

      <PreviewBadge config={data.config} />

      <Handle
        type="source"
        position={Position.Bottom}
        style={{
          background: '#1976D2',
          width: '10px',
          height: '10px',
          border: '2px solid white',
        }}
      />
    </div>
  );
};

export default AgentNode;
