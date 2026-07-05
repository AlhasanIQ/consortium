import { useState } from 'react';
import type { AgentState } from '../../../stores/ensembleStore';
import type { AggregationDetails } from '../../../types/workflow';

interface JudgeDetailsProps {
  details: AggregationDetails;
  agents: AgentState[];
  collapsed?: boolean;
}

export function JudgeDetails({ details, agents, collapsed = true }: JudgeDetailsProps) {
  const [isExpanded, setIsExpanded] = useState(!collapsed);

  const winnerAgent = agents.find((a) => a.id === details.winner);

  return (
    <div
      style={{
        backgroundColor: 'rgba(59, 130, 246, 0.1)',
        border: '1px solid rgba(59, 130, 246, 0.3)',
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
              backgroundColor: '#3B82F6',
              color: '#fff',
              textTransform: 'uppercase',
              letterSpacing: '0.5px',
              fontWeight: 600,
            }}
          >
            Judge
          </span>
          {winnerAgent && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ fontSize: '16px' }}>+</span>
              <span
                style={{
                  width: '8px',
                  height: '8px',
                  borderRadius: '50%',
                  backgroundColor: winnerAgent.color,
                }}
              />
              <span style={{ fontSize: '13px', fontWeight: 500 }}>{winnerAgent.displayName}</span>
              <span style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)' }}>selected as best</span>
            </div>
          )}
        </div>
        <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.5)' }}>{isExpanded ? '[-]' : '[+]'}</span>
      </button>

      {/* Expanded content */}
      {isExpanded && (
        <div style={{ padding: '0 14px 14px' }}>
          {/* Winner highlight */}
          {winnerAgent && (
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '12px',
                padding: '12px',
                backgroundColor: 'rgba(251, 191, 36, 0.1)',
                border: '1px solid rgba(251, 191, 36, 0.3)',
                borderRadius: '6px',
                marginBottom: '12px',
              }}
            >
              <span style={{ fontSize: '20px' }}>+</span>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px' }}>
                  <span
                    style={{
                      width: '10px',
                      height: '10px',
                      borderRadius: '50%',
                      backgroundColor: winnerAgent.color,
                    }}
                  />
                  <span style={{ fontSize: '14px', fontWeight: 600 }}>{winnerAgent.displayName}</span>
                  <span
                    style={{
                      fontSize: '10px',
                      padding: '2px 6px',
                      borderRadius: '3px',
                      backgroundColor: 'rgba(251, 191, 36, 0.3)',
                      color: '#FCD34D',
                    }}
                  >
                    WINNER
                  </span>
                </div>
                <span style={{ fontSize: '11px', color: 'rgba(255,255,255,0.6)' }}>{winnerAgent.model}</span>
              </div>
            </div>
          )}

          {/* Other agents */}
          <div style={{ marginBottom: '12px' }}>
            <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)', marginBottom: '8px' }}>
              Other responses:
            </div>
            <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
              {agents
                .filter((a) => a.id !== details.winner)
                .map((agent) => (
                  <div
                    key={agent.id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '6px',
                      padding: '6px 10px',
                      backgroundColor: 'rgba(255,255,255,0.05)',
                      borderRadius: '4px',
                    }}
                  >
                    <span
                      style={{
                        width: '6px',
                        height: '6px',
                        borderRadius: '50%',
                        backgroundColor: agent.color,
                      }}
                    />
                    <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.7)' }}>{agent.displayName}</span>
                  </div>
                ))}
            </div>
          </div>

          {/* Judge reasoning */}
          {details.reasoning && (
            <div>
              <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)', marginBottom: '6px' }}>
                Judge's reasoning:
              </div>
              <div
                style={{
                  padding: '10px',
                  backgroundColor: 'rgba(0,0,0,0.2)',
                  borderRadius: '4px',
                  fontSize: '12px',
                  lineHeight: 1.5,
                  color: 'rgba(255,255,255,0.8)',
                  maxHeight: '200px',
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
