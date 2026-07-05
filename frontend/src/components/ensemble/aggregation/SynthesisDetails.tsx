import { useState } from 'react';
import type { AgentState } from '../../../stores/ensembleStore';
import type { AggregationDetails } from '../../../types/workflow';

interface SynthesisDetailsProps {
  details: AggregationDetails;
  agents: AgentState[];
  collapsed?: boolean;
}

export function SynthesisDetails({ details, agents, collapsed = true }: SynthesisDetailsProps) {
  const [isExpanded, setIsExpanded] = useState(!collapsed);

  return (
    <div
      style={{
        backgroundColor: 'rgba(139, 92, 246, 0.1)',
        border: '1px solid rgba(139, 92, 246, 0.3)',
        borderRadius: '8px',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <button
        type="button"
        onClick={() => setIsExpanded(!isExpanded)}
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '10px 14px',
          backgroundColor: 'transparent',
          border: 'none',
          cursor: 'pointer',
          color: '#fff',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          <span
            style={{
              fontSize: '10px',
              padding: '3px 8px',
              borderRadius: '4px',
              backgroundColor: '#8B5CF6',
              color: '#fff',
              textTransform: 'uppercase',
              letterSpacing: '0.5px',
              fontWeight: 600,
            }}
          >
            Synthesis
          </span>
          <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)' }}>
            Combined {agents.length} agent responses
          </span>
        </div>
        <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.5)' }}>{isExpanded ? '[-]' : '[+]'}</span>
      </button>

      {/* Expanded content */}
      {isExpanded && (
        <div style={{ padding: '0 14px 14px' }}>
          {/* Contributing agents */}
          <div style={{ marginBottom: '12px' }}>
            <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)', marginBottom: '8px' }}>
              Contributing agents:
            </div>
            <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
              {agents.map((agent) => (
                <div
                  key={agent.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '6px',
                    padding: '6px 10px',
                    backgroundColor: 'rgba(255,255,255,0.05)',
                    borderRadius: '4px',
                    border: `1px solid ${agent.color}33`,
                  }}
                >
                  <span
                    style={{
                      width: '8px',
                      height: '8px',
                      borderRadius: '50%',
                      backgroundColor: agent.color,
                    }}
                  />
                  <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.8)' }}>{agent.displayName}</span>
                  {agent.status === 'done' && (
                    <span style={{ fontSize: '10px', color: 'rgba(16, 185, 129, 0.8)' }}>*</span>
                  )}
                </div>
              ))}
            </div>
          </div>

          {/* Synthesis info */}
          <div
            style={{
              padding: '10px',
              backgroundColor: 'rgba(0,0,0,0.2)',
              borderRadius: '4px',
              fontSize: '12px',
              lineHeight: 1.5,
              color: 'rgba(255,255,255,0.7)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px' }}>
              <span style={{ fontSize: '14px' }}>*</span>
              <span>All agent responses were combined into a unified answer</span>
            </div>
            <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)' }}>
              The synthesis agent identifies areas of agreement, notes differences, and provides a coherent combined
              response.
            </div>
          </div>

          {/* Synthesis reasoning if available */}
          {details.reasoning && (
            <div style={{ marginTop: '12px' }}>
              <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)', marginBottom: '6px' }}>
                Synthesis notes:
              </div>
              <div
                style={{
                  padding: '10px',
                  backgroundColor: 'rgba(0,0,0,0.2)',
                  borderRadius: '4px',
                  fontSize: '12px',
                  lineHeight: 1.5,
                  color: 'rgba(255,255,255,0.8)',
                  maxHeight: '150px',
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                }}
              >
                {details.reasoning}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
