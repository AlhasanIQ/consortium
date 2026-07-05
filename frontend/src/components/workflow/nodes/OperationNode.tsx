import { Handle, type Node, type NodeProps, Position } from '@xyflow/react';
import type React from 'react';
import { useWorkflowStore } from '../../../stores/workflowStore';
import type { NodeData } from '../../../types/workflow';
import ExecutionBadge from './ExecutionBadge';
import PreviewBadge from './PreviewBadge';

type OperationNodeType = Node<NodeData, 'operation'>;

const OperationNode: React.FC<NodeProps<OperationNodeType>> = ({ data, selected, id }) => {
  const executionState = useWorkflowStore((state) => state.nodeExecutionStates[id]);
  const isExpanded = data.config.aggregationInternalState === 'expanded';
  const isForked = data.config.aggregationInternalState === 'forked';
  const borderColor = selected ? '#334155' : '#64748B';
  const backgroundColor = isExpanded ? '#475569' : '#334155';

  return (
    <div
      data-testid="workflow-node-operation"
      data-node-id={id}
      style={{
        minWidth: '180px',
        padding: '12px',
        borderRadius: '8px',
        backgroundColor,
        color: 'white',
        border: `2px solid ${borderColor}`,
        boxShadow: selected ? '0 4px 8px rgba(0,0,0,0.28)' : '0 2px 4px rgba(0,0,0,0.18)',
        opacity: isExpanded ? 0.86 : 1,
        position: 'relative',
      }}
    >
      <Handle
        type="target"
        position={Position.Top}
        style={{ background: '#334155', width: '10px', height: '10px', border: '2px solid white' }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        style={{ background: '#334155', width: '10px', height: '10px', border: '2px solid white' }}
      />
      {executionState && <ExecutionBadge status={executionState.status} />}

      <div style={{ fontWeight: 700, marginBottom: '5px' }}>{data.label || 'Operation'}</div>
      <div
        style={{
          fontSize: '10px',
          fontFamily: 'monospace',
          backgroundColor: 'rgba(0,0,0,0.24)',
          padding: '2px 6px',
          borderRadius: '3px',
          marginBottom: '6px',
          display: 'inline-block',
        }}
      >
        {data.config.operationType || 'operation'}
      </div>
      {isForked && data.config.sourceWorkflowId && (
        <div style={{ fontSize: '10px', opacity: 0.82 }}>{data.config.sourceWorkflowId}</div>
      )}
      <PreviewBadge config={data.config} />
    </div>
  );
};

export default OperationNode;
