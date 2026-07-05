import type React from 'react';
import { useEnsembleStore } from '../../stores/ensembleStore';

interface CenterNodeProps {
  size: number;
}

// Human-friendly aggregation method labels
const aggregationLabels: Record<string, string> = {
  collect: 'Collecting...',
  judge: 'Judging...',
  scoring: 'Scoring...',
  synthesis: 'Synthesizing...',
  peer_matrix: 'Peer evaluating...',
};

const CenterNode: React.FC<CenterNodeProps> = ({ size }) => {
  const { isExecuting, isAggregating, aggregationMethod, phase, synthesisStreaming, agents } = useEnsembleStore();

  const getPhaseLabel = () => {
    switch (phase) {
      case 'sending':
        return 'Sending to models...';
      case 'thinking': {
        const done = agents.filter((a) => a.status === 'done').length;
        return `${done}/${agents.length} responses`;
      }
      case 'aggregating':
        return aggregationMethod ? aggregationLabels[aggregationMethod] || 'Processing...' : 'Processing...';
      case 'complete':
        return 'Complete';
      default:
        return '';
    }
  };

  const innerSize = size * 0.9;

  return (
    <div
      style={{
        position: 'absolute',
        left: '50%',
        top: '50%',
        transform: 'translate(-50%, -50%)',
        zIndex: 30,
      }}
    >
      {/* Outer glow ring */}
      <div
        style={{
          position: 'absolute',
          left: '50%',
          top: '50%',
          transform: 'translate(-50%, -50%)',
          width: size,
          height: size,
          borderRadius: '50%',
          background: 'transparent',
          border: `2px solid ${isExecuting ? '#8B5CF6' : '#333'}`,
          boxShadow: isExecuting
            ? '0 0 40px rgba(139, 92, 246, 0.4), inset 0 0 40px rgba(139, 92, 246, 0.1)'
            : '0 0 20px rgba(0,0,0,0.5)',
          transition: 'all 0.5s ease',
        }}
      />

      {/* Main container */}
      <div
        style={{
          width: innerSize,
          height: innerSize,
          borderRadius: '50%',
          background: 'linear-gradient(145deg, #1a1a2e 0%, #0f0f1a 100%)',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '20px',
          boxSizing: 'border-box',
          position: 'relative',
          overflow: 'hidden',
        }}
      >
        {/* Logo / Title */}
        <div
          style={{
            fontSize: '16px',
            fontWeight: '600',
            color: isExecuting ? '#8B5CF6' : '#666',
            letterSpacing: '2px',
            textTransform: 'uppercase',
            transition: 'color 0.3s ease',
          }}
        >
          Consortium
        </div>

        {/* Phase indicator */}
        {phase !== 'idle' && (
          <div
            style={{
              marginTop: '8px',
              fontSize: '12px',
              color: phase === 'complete' ? '#10B981' : '#8B5CF6',
              fontWeight: '500',
            }}
          >
            {getPhaseLabel()}
          </div>
        )}

        {/* Aggregation streaming preview */}
        {isAggregating && synthesisStreaming && (
          <div
            style={{
              marginTop: '12px',
              width: '80%',
              maxHeight: '60px',
              overflow: 'hidden',
              fontSize: '11px',
              color: '#888',
              textAlign: 'center',
              lineHeight: '1.4',
            }}
          >
            {synthesisStreaming.slice(0, 100)}
            {synthesisStreaming.length > 100 && '...'}
            <span className="blink">|</span>
          </div>
        )}

        {/* Idle state hint */}
        {phase === 'idle' && (
          <div
            style={{
              marginTop: '8px',
              fontSize: '11px',
              color: '#444',
            }}
          >
            Ready
          </div>
        )}
      </div>
    </div>
  );
};

export default CenterNode;
