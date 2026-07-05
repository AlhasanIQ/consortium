import type React from 'react';
import type { AgentState, EnsembleState } from '../../stores/ensembleStore';

interface DataRingProps {
  centerX: number;
  centerY: number;
  radius: number;
  agents: AgentState[];
  agentPositions: Array<{ x: number; y: number }>;
  phase: EnsembleState['phase'];
}

const DataRing: React.FC<DataRingProps> = ({ centerX, centerY, radius, agents, agentPositions, phase }) => {
  // Check if any activity is happening (agents working OR aggregating)
  const isActive = phase !== 'idle' && phase !== 'complete';
  const isAggregating = phase === 'aggregating';

  return (
    <svg
      role="img"
      aria-labelledby="data-ring-title"
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        width: '100%',
        height: '100%',
        pointerEvents: 'none',
        zIndex: 5,
      }}
    >
      <style>
        {`
          @keyframes pulse {
            0%, 100% { opacity: 0.4; transform-origin: center; }
            50% { opacity: 0.8; }
          }
          @keyframes rotateDash {
            from { stroke-dashoffset: 0; }
            to { stroke-dashoffset: 24; }
          }
        `}
      </style>
      <title id="data-ring-title">Agent connection ring visualization</title>
      <defs>
        {/* Gradient for the ring */}
        <linearGradient id="ringGradient" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="#8B5CF6" stopOpacity="0.3" />
          <stop offset="50%" stopColor="#6D28D9" stopOpacity="0.5" />
          <stop offset="100%" stopColor="#8B5CF6" stopOpacity="0.3" />
        </linearGradient>

        {/* Glow filter */}
        <filter id="glow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="3" result="coloredBlur" />
          <feMerge>
            <feMergeNode in="coloredBlur" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>

        {/* Aggregation gradient (gold/amber) */}
        <linearGradient id="aggregateGradient" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="#F59E0B" stopOpacity="0.4" />
          <stop offset="50%" stopColor="#D97706" stopOpacity="0.6" />
          <stop offset="100%" stopColor="#F59E0B" stopOpacity="0.4" />
        </linearGradient>
      </defs>

      {/* Aggregation pulse ring - shown during aggregation phase */}
      {isAggregating && (
        <circle
          cx={centerX}
          cy={centerY}
          r={radius - 8}
          fill="none"
          stroke="url(#aggregateGradient)"
          strokeWidth="3"
          opacity={0.7}
          style={{
            animation: 'pulse 1.5s ease-in-out infinite',
          }}
        />
      )}

      {/* Main ring circle */}
      <circle
        cx={centerX}
        cy={centerY}
        r={radius}
        fill="none"
        stroke={isAggregating ? 'url(#aggregateGradient)' : 'url(#ringGradient)'}
        strokeWidth="2"
        strokeDasharray={isActive ? '8,4' : '4,4'}
        opacity={isActive ? 0.6 : 0.4}
        style={{
          transition: 'opacity 0.3s ease, stroke 0.3s ease',
          animation: isActive ? 'rotateDash 1s linear infinite' : undefined,
        }}
      />

      {/* Connection dots on ring where agents connect */}
      {agentPositions.map((pos, i) => {
        const agent = agents[i];
        const isAgentActive = agent.status !== 'idle';

        return (
          <circle
            key={agent.id}
            cx={pos.x}
            cy={pos.y}
            r={isAgentActive ? 6 : 4}
            fill={agent.color}
            opacity={isAgentActive ? 0.8 : 0.3}
            filter={isAgentActive ? 'url(#glow)' : undefined}
            style={{
              transition: 'all 0.3s ease',
            }}
          />
        );
      })}
    </svg>
  );
};

export default DataRing;
