import { Handle, type Node, type NodeProps, Position } from '@xyflow/react';
import type React from 'react';
import { useWorkflowStore } from '../../../stores/workflowStore';
import type { NodeData } from '../../../types/workflow';
import { buildAggregationPlan } from '../../../utils/aggregationPlan';
import ExecutionBadge from './ExecutionBadge';

const modelOwnershipLabels: Record<string, string> = {
  none: 'No model',
  owned: 'Owns evaluator model',
  conditional: 'Conditional model',
  inherited: 'Inherits reviewer models',
};

type AggregationNodeType = Node<NodeData, 'aggregation'>;

const AggregationNode: React.FC<NodeProps<AggregationNodeType>> = ({ data, selected, id }) => {
  const executionState = useWorkflowStore((state) => state.nodeExecutionStates[id]);
  const edges = useWorkflowStore((state) => state.edges);
  const replaySelectedNodeId = useWorkflowStore((state) => state.replaySelectedNodeId);
  const replayNodes = useWorkflowStore((state) => state.replayNodes);
  const inputIds = edges.filter((edge) => edge.target === id).map((edge) => edge.source);
  const method = data.config.aggregationMethod || 'collect';
  const sourceWorkflowId = data.config.aggregationWorkflowId || data.config.sourceWorkflowId;
  const boundaryType = data.config.boundaryType || (sourceWorkflowId ? 'compiled_workflow_ref' : undefined);
  const plan = buildAggregationPlan({
    method,
    inputIds,
    config: data.config.aggregationConfig,
  });
  const isReplayFocused =
    replaySelectedNodeId != null &&
    (replaySelectedNodeId === id ||
      replaySelectedNodeId.startsWith(`${id}__agg__`) ||
      replaySelectedNodeId.startsWith(`${id}--`));
  const subnodes = replayNodes.filter(
    (node) => node.node_id !== id && (node.node_id.startsWith(`${id}__agg__`) || node.node_id.startsWith(`${id}--`)),
  );

  const borderColor = selected ? '#0F766E' : '#14B8A6';
  const backgroundColor = executionState?.status === 'running' ? '#F59E0B' : '#0D9488';

  return (
    <div
      data-testid="workflow-node-aggregation"
      data-node-id={id}
      aria-label={`${data.label} aggregation node`}
      role="button"
      tabIndex={0}
      style={{
        width: '260px',
        padding: '12px',
        borderRadius: '8px',
        backgroundColor,
        color: 'white',
        border: `2px solid ${borderColor}`,
        boxShadow: isReplayFocused
          ? '0 0 0 3px #FFD54F, 0 0 16px 4px rgba(255, 213, 79, 0.5)'
          : selected
            ? '0 4px 8px rgba(0,0,0,0.3)'
            : '0 2px 4px rgba(0,0,0,0.2)',
        position: 'relative',
      }}
    >
      <Handle
        type="target"
        position={Position.Top}
        style={{ background: '#0F766E', width: '10px', height: '10px', border: '2px solid white' }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        style={{ background: '#0F766E', width: '10px', height: '10px', border: '2px solid white' }}
      />
      {executionState && <ExecutionBadge status={executionState.status} />}

      <div style={{ fontWeight: 700, marginBottom: '5px' }}>{data.label}</div>
      <div
        style={{
          fontSize: '10px',
          fontFamily: 'monospace',
          backgroundColor: 'rgba(0,0,0,0.2)',
          padding: '2px 6px',
          borderRadius: '3px',
          marginBottom: '8px',
          display: 'inline-block',
        }}
      >
        {`{{${data.config.alias || id}}}`}
      </div>
      <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap', marginBottom: '8px' }}>
        <span
          style={{ fontSize: '10px', padding: '2px 6px', borderRadius: '4px', background: 'rgba(255,255,255,0.22)' }}
        >
          {plan.title}
        </span>
        {sourceWorkflowId && (
          <span style={{ fontSize: '10px', padding: '2px 6px', borderRadius: '4px', background: 'rgba(0,0,0,0.18)' }}>
            {sourceWorkflowId}
          </span>
        )}
        {boundaryType && (
          <span style={{ fontSize: '10px', padding: '2px 6px', borderRadius: '4px', background: 'rgba(0,0,0,0.18)' }}>
            {boundaryType}
          </span>
        )}
        <span style={{ fontSize: '10px', padding: '2px 6px', borderRadius: '4px', background: 'rgba(0,0,0,0.18)' }}>
          {modelOwnershipLabels[plan.modelOwnership]}
        </span>
      </div>
      {subnodes.length > 0 && (
        <div
          style={{ marginTop: '8px', borderTop: '1px solid rgba(255,255,255,0.3)', paddingTop: '6px', fontSize: '9px' }}
        >
          {subnodes.filter((node) => node.status === 'completed').length}/{subnodes.length} runtime subnodes
        </div>
      )}
    </div>
  );
};

export default AggregationNode;
