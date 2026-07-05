import type { Node, NodeProps } from '@xyflow/react';
import type React from 'react';
import type { NodeData } from '../../../types/workflow';

type AggregationFrameNodeType = Node<NodeData, 'aggregation_frame'>;

const AggregationFrameNode: React.FC<NodeProps<AggregationFrameNodeType>> = ({ data, id }) => {
  const llmJobs = data.config.aggregationFrameLLMJobCount ?? 0;
  const conditionalLLMJobs = data.config.aggregationFrameConditionalLLMJobCount ?? 0;
  const topLevelLLMJobs = data.config.aggregationFrameTopLevelLLMJobCount ?? Math.max(0, llmJobs - conditionalLLMJobs);
  const operations = data.config.aggregationFrameOperationCount ?? 0;
  const method = data.config.aggregationFrameMethod;
  const sourceWorkflowID = data.config.aggregationFrameSourceWorkflowId;
  const title =
    data.config.aggregationFrameTitle ||
    (method ? `${method} compiled preview` : data.label || 'Compiled aggregation preview');
  const llmJobLabel =
    conditionalLLMJobs > 0
      ? `${llmJobs} LLM jobs (${topLevelLLMJobs} top-level + ${conditionalLLMJobs} conditional)`
      : `${llmJobs} LLM jobs`;

  return (
    <div
      data-testid="aggregation-frame-node"
      data-node-id={id}
      style={{
        width: '100%',
        height: '100%',
        minWidth: '260px',
        minHeight: '180px',
        border: '2px dashed #0f766e',
        borderRadius: '8px',
        backgroundColor: 'rgba(240, 253, 250, 0.5)',
        boxShadow: 'inset 0 0 0 1px rgba(15, 118, 110, 0.12)',
        pointerEvents: 'auto',
        cursor: 'move',
        color: '#0f172a',
        padding: '12px',
        boxSizing: 'border-box',
      }}
    >
      <div
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '8px',
          padding: '6px 8px',
          borderRadius: '6px',
          backgroundColor: 'rgba(255,255,255,0.86)',
          border: '1px solid rgba(15, 118, 110, 0.2)',
          fontSize: '11px',
          fontWeight: 700,
        }}
      >
        <span>{title}</span>
        <span style={{ color: '#0f766e' }}>{llmJobLabel}</span>
        <span style={{ color: '#475569' }}>{operations} ops</span>
      </div>
      {sourceWorkflowID && (
        <div style={{ marginTop: '8px', fontSize: '11px', color: '#475569' }}>{sourceWorkflowID}</div>
      )}
    </div>
  );
};

export default AggregationFrameNode;
