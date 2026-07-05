import type React from 'react';
import { type AgentState, useEnsembleStore } from '../../stores/ensembleStore';

interface AgentNodeProps {
  agent: AgentState;
  position: { x: number; y: number };
  transform: string;
  mobile?: boolean;
}

const AgentNode: React.FC<AgentNodeProps> = ({ agent, position, transform, mobile = false }) => {
  const { selectedAgentId, setSelectedAgent, aggregationDetails, phase } = useEnsembleStore();
  const isSelected = selectedAgentId === agent.id;
  const isWinner = phase === 'complete' && aggregationDetails?.winner === agent.id;
  const agentScore = aggregationDetails?.scores?.[agent.id];

  const getStatusIndicator = () => {
    switch (agent.status) {
      case 'idle':
        return { icon: '○', label: 'Ready', animation: '' };
      case 'receiving':
        return { icon: '◐', label: 'Receiving...', animation: 'spin-slow' };
      case 'thinking':
        return { icon: '◑', label: 'Thinking...', animation: 'pulse' };
      case 'streaming':
        return { icon: '◉', label: 'Streaming...', animation: 'pulse-fast' };
      case 'done':
        return { icon: '●', label: 'Complete', animation: '' };
      case 'error':
        return { icon: '✕', label: 'Error', animation: '' };
      default:
        return { icon: '○', label: '', animation: '' };
    }
  };

  const status = getStatusIndicator();

  const getGlowStyle = () => {
    if (isWinner) {
      return `0 0 30px rgba(251, 191, 36, 0.6), 0 0 60px rgba(251, 191, 36, 0.3)`;
    }
    if (agent.status === 'error') {
      return `0 0 20px rgba(239, 68, 68, 0.6)`;
    }
    if (agent.status === 'streaming' || agent.status === 'thinking') {
      return `0 0 30px ${agent.color}99, 0 0 60px ${agent.color}44`;
    }
    if (agent.status === 'done') {
      return `0 0 20px ${agent.color}66`;
    }
    if (isSelected) {
      return `0 0 25px ${agent.color}88`;
    }
    return `0 0 10px rgba(0,0,0,0.3)`;
  };

  const getBorderColor = () => {
    if (isWinner) {
      return '#FCD34D'; // Gold for winner
    }
    if (agent.status === 'error') {
      return '#EF4444';
    }
    return agent.color;
  };

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => setSelectedAgent(isSelected ? null : agent.id)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          setSelectedAgent(isSelected ? null : agent.id);
        }
      }}
      style={{
        position: 'absolute',
        left: position.x,
        top: position.y,
        transform: transform,
        cursor: 'pointer',
        zIndex: isSelected ? 20 : 10,
      }}
    >
      {/* Main card */}
      <div
        style={{
          padding: mobile ? '8px 12px' : '12px 16px',
          borderRadius: mobile ? '10px' : '12px',
          background: isWinner
            ? `linear-gradient(145deg, #2a2518 0%, #1f1a10 100%)`
            : `linear-gradient(145deg, #1a1a2e 0%, #16162a 100%)`,
          border: `${mobile ? 1 : 2}px solid ${getBorderColor()}`,
          boxShadow: getGlowStyle(),
          transition: 'all 0.3s ease',
          transform: isSelected ? 'scale(1.05)' : 'scale(1)',
          whiteSpace: 'nowrap',
        }}
      >
        {/* Winner badge */}
        {isWinner && !mobile && (
          <div
            style={{
              position: 'absolute',
              top: '-8px',
              right: '-8px',
              fontSize: '16px',
              filter: 'drop-shadow(0 2px 4px rgba(0,0,0,0.5))',
            }}
          >
            +
          </div>
        )}

        {/* Model name */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: '6px',
            fontSize: mobile ? '11px' : '13px',
            fontWeight: '600',
            color: isWinner ? '#FCD34D' : '#fff',
            marginBottom: agent.status === 'idle' ? '0' : '4px',
            textAlign: 'center',
          }}
        >
          {agent.displayName}
          {isWinner && mobile && <span style={{ fontSize: '10px' }}>+</span>}
        </div>

        {/* Status indicator - only show when not idle */}
        {agent.status !== 'idle' && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: mobile ? '2px' : '4px',
              fontSize: mobile ? '8px' : '10px',
              color: agent.status === 'error' ? '#EF4444' : '#888',
              marginBottom: mobile ? '4px' : '6px',
            }}
          >
            <span
              className={status.animation}
              style={{
                color: agent.status === 'error' ? '#EF4444' : agent.color,
                fontSize: mobile ? '10px' : '12px',
              }}
            >
              {status.icon}
            </span>
            {!mobile && <span>{status.label}</span>}
          </div>
        )}

        {/* Streaming preview - hide on mobile */}
        {!mobile && agent.status === 'streaming' && agent.streamingText && (
          <div
            style={{
              fontSize: '9px',
              color: '#aaa',
              maxHeight: '30px',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              lineHeight: '1.3',
              marginBottom: '6px',
              padding: '4px',
              background: 'rgba(255,255,255,0.05)',
              borderRadius: '4px',
            }}
          >
            {agent.streamingText.slice(-60)}
            <span className="blink">|</span>
          </div>
        )}

        {/* Metrics (when done) */}
        {agent.status === 'done' && (
          <div
            style={{
              display: 'flex',
              justifyContent: 'center',
              gap: mobile ? '4px' : '8px',
              fontSize: mobile ? '8px' : '9px',
              color: isWinner ? 'rgba(252, 211, 77, 0.8)' : '#666',
            }}
          >
            {agentScore !== undefined && (
              <span
                style={{
                  fontWeight: 500,
                  color: isWinner ? '#FCD34D' : '#888',
                }}
              >
                {agentScore.toFixed(1)}
              </span>
            )}
            <span>{(agent.latencyMs / 1000).toFixed(1)}s</span>
            {!mobile && <span>${agent.cost.toFixed(3)}</span>}
          </div>
        )}

        {/* Error message - simplified on mobile */}
        {agent.status === 'error' && agent.error && !mobile && (
          <div
            style={{
              fontSize: '9px',
              color: '#EF4444',
              padding: '4px',
              background: 'rgba(239,68,68,0.1)',
              borderRadius: '4px',
              maxHeight: '30px',
              overflow: 'hidden',
            }}
          >
            {agent.error.slice(0, 50)}
          </div>
        )}
      </div>
    </div>
  );
};

export default AgentNode;
