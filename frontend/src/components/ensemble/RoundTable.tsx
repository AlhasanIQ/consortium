import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { useEnsembleStore } from '../../stores/ensembleStore';
import AgentNode from './AgentNode';
import DataRing from './DataRing';
import ResponsePanel from './ResponsePanel';

interface RoundTableProps {
  compact?: boolean;
}

// Human-friendly phase labels
const phaseLabels: Record<string, string> = {
  idle: '',
  sending: 'Sending...',
  thinking: 'Agents Working',
  aggregating: 'Processing Results',
  complete: 'Complete',
};

// Method-specific aggregation labels
const aggregationLabels: Record<string, string> = {
  collect: 'Collecting',
  judge: 'Judging',
  scoring: 'Scoring',
  synthesis: 'Synthesizing',
  peer_matrix: 'Peer Evaluating',
};

const RoundTable: React.FC<RoundTableProps> = ({ compact = false }) => {
  const { agents, selectedAgentId, phase, aggregationMethod } = useEnsembleStore();

  // Mobile detection
  const [isMobile, setIsMobile] = useState(window.innerWidth < 640);

  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < 640);
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // Calculate layout dimensions based on compact mode and mobile
  const containerSize = isMobile
    ? Math.min(window.innerWidth - 40, 300)
    : compact
      ? Math.min(window.innerWidth * 0.3, window.innerHeight * 0.6, 350)
      : Math.min(window.innerWidth * 0.6, window.innerHeight * 0.6, 550);

  const centerX = containerSize / 2;
  const centerY = containerSize / 2;
  const ringRadius = containerSize * 0.38; // Circle radius for agents

  // Gap between card edge and the dot on the ring
  const cardGap = isMobile ? 6 : compact ? 8 : 10;

  // Calculate positions and angles for agents
  // Cards are positioned radially outward from the ring with zone-based edge anchoring
  const agentLayout = useMemo(() => {
    return agents.map((_, index) => {
      // Start from top (-90 degrees) and go clockwise
      const angleDeg = -90 + (360 / agents.length) * index;
      const angleRad = (angleDeg * Math.PI) / 180;
      const cos = Math.cos(angleRad);
      const sin = Math.sin(angleRad);

      // Dot position: exactly on the ring
      const dotX = centerX + ringRadius * cos;
      const dotY = centerY + ringRadius * sin;

      // Card anchor position: cardGap pixels outward from ring along the radial direction
      // This ensures the card edge is always the same distance from the ring
      const cardX = centerX + (ringRadius + cardGap) * cos;
      const cardY = centerY + (ringRadius + cardGap) * sin;

      // Normalize angle to -180 to 180 range for zone detection
      let normalizedAngle = angleDeg;
      while (normalizedAngle > 180) normalizedAngle -= 360;
      while (normalizedAngle <= -180) normalizedAngle += 360;

      // Determine anchor transform based on zone
      // This anchors the card at the edge closest to the ring
      let transform: string;

      if (normalizedAngle > -135 && normalizedAngle <= -45) {
        // Top zone: anchor at bottom-center
        transform = 'translate(-50%, -100%)';
      } else if (normalizedAngle > -45 && normalizedAngle <= 45) {
        // Right zone: anchor at left-center
        transform = 'translate(0%, -50%)';
      } else if (normalizedAngle > 45 && normalizedAngle <= 135) {
        // Bottom zone: anchor at top-center
        transform = 'translate(-50%, 0%)';
      } else {
        // Left zone: anchor at right-center
        transform = 'translate(-100%, -50%)';
      }

      return {
        dot: { x: dotX, y: dotY },
        card: { x: cardX, y: cardY },
        transform,
      };
    });
  }, [agents, centerX, centerY, ringRadius, cardGap]);

  return (
    <div
      style={{
        position: 'relative',
        width: containerSize,
        height: containerSize,
        margin: '0 auto',
        transition: 'all 0.5s ease',
      }}
    >
      {/* CSS animations */}
      <style>
        {`
          @keyframes pulse {
            0%, 100% { opacity: 0.6; transform: scale(1); }
            50% { opacity: 1; transform: scale(1.1); }
          }
        `}
      </style>

      {/* Background glow */}
      <div
        style={{
          position: 'absolute',
          left: '50%',
          top: '50%',
          transform: 'translate(-50%, -50%)',
          width: ringRadius * 2 + 100,
          height: ringRadius * 2 + 100,
          borderRadius: '50%',
          background: 'radial-gradient(circle, rgba(139, 92, 246, 0.05) 0%, transparent 70%)',
          pointerEvents: 'none',
        }}
      />

      {/* Data flow ring and connections */}
      <DataRing
        centerX={centerX}
        centerY={centerY}
        radius={ringRadius}
        agents={agents}
        agentPositions={agentLayout.map((a) => a.dot)}
        phase={phase}
      />

      {/* Center phase indicator */}
      {phase !== 'idle' && phase !== 'complete' && (
        <div
          style={{
            position: 'absolute',
            left: '50%',
            top: '50%',
            transform: 'translate(-50%, -50%)',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 8,
            pointerEvents: 'none',
            zIndex: 10,
          }}
        >
          {/* Pulsing indicator */}
          <div
            style={{
              width: isMobile ? 12 : 16,
              height: isMobile ? 12 : 16,
              borderRadius: '50%',
              background: phase === 'aggregating' ? '#F59E0B' : '#8B5CF6',
              boxShadow: `0 0 20px ${phase === 'aggregating' ? '#F59E0B' : '#8B5CF6'}`,
              animation: 'pulse 1.5s ease-in-out infinite',
            }}
          />
          {/* Phase label */}
          <div
            style={{
              fontSize: isMobile ? 10 : 12,
              fontWeight: 500,
              color: phase === 'aggregating' ? '#F59E0B' : '#A78BFA',
              textTransform: 'uppercase',
              letterSpacing: '0.05em',
              whiteSpace: 'nowrap',
            }}
          >
            {phase === 'aggregating' && aggregationMethod
              ? aggregationLabels[aggregationMethod] || 'Processing'
              : phaseLabels[phase] || ''}
          </div>
        </div>
      )}

      {/* Agent nodes */}
      {agents.map((agent, index) => (
        <AgentNode
          key={agent.id}
          agent={agent}
          position={agentLayout[index].card}
          transform={agentLayout[index].transform}
          mobile={isMobile}
        />
      ))}

      {/* Connection dots on ring (above agent cards) */}
      {agents.map((agent, index) => {
        const pos = agentLayout[index].dot;
        const isActive = agent.status !== 'idle';
        const dotSize = isMobile ? (isActive ? 10 : 6) : isActive ? 12 : 8;
        return (
          <div
            key={`dot-${agent.id}`}
            style={{
              position: 'absolute',
              left: pos.x,
              top: pos.y,
              transform: 'translate(-50%, -50%)',
              width: dotSize,
              height: dotSize,
              borderRadius: '50%',
              background: agent.color,
              border: `${isMobile ? 1 : 2}px solid #1a1a2e`,
              boxShadow: isActive ? `0 0 ${isMobile ? 6 : 10}px ${agent.color}` : 'none',
              zIndex: 25,
              transition: 'all 0.3s ease',
              pointerEvents: 'none',
            }}
          />
        );
      })}

      {/* Response panel (slides in when agent selected) */}
      {selectedAgentId && <ResponsePanel />}
    </div>
  );
};

export default RoundTable;
