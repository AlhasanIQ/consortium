import type React from 'react';
import type { NodeData } from '../../../types/workflow';

interface PreviewBadgeProps {
  config: NodeData['config'];
}

const PreviewBadge: React.FC<PreviewBadgeProps> = ({ config }) => {
  if (config.aggregationInternalState !== 'expanded') {
    return null;
  }

  return (
    <div
      style={{
        fontSize: '10px',
        lineHeight: 1.35,
        opacity: 0.9,
        marginTop: '6px',
        padding: '4px 6px',
        borderRadius: '4px',
        backgroundColor: 'rgba(15, 23, 42, 0.24)',
        border: '1px solid rgba(255,255,255,0.22)',
      }}
    >
      <div style={{ fontWeight: 700 }}>Compiled preview</div>
      {config.sourceWorkflowId && <div>{config.sourceWorkflowId}</div>}
    </div>
  );
};

export default PreviewBadge;
