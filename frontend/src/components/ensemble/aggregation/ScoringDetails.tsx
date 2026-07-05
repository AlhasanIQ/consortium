import { useState } from 'react';
import type { AgentState } from '../../../stores/ensembleStore';
import type { AggregationDetails } from '../../../types/workflow';

interface ScoringDetailsProps {
  details: AggregationDetails;
  agents: AgentState[];
  collapsed?: boolean;
}

export function ScoringDetails({ details, agents, collapsed = true }: ScoringDetailsProps) {
  const [isExpanded, setIsExpanded] = useState(!collapsed);

  const winnerAgent = agents.find((a) => a.id === details.winner);

  // Sort agents by score (highest first), then by ID alphabetically for deterministic tie-breaking (matches backend)
  const sortedAgents = [...agents].sort((a, b) => {
    const scoreA = details.scores?.[a.id] || 0;
    const scoreB = details.scores?.[b.id] || 0;
    if (scoreB !== scoreA) return scoreB - scoreA;
    return a.id.localeCompare(b.id);
  });

  // Find max score for normalization
  const maxScore = Math.max(...Object.values(details.scores || {}), 1);

  return (
    <div
      style={{
        backgroundColor: 'rgba(245, 158, 11, 0.1)',
        border: '1px solid rgba(245, 158, 11, 0.3)',
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
              backgroundColor: '#F59E0B',
              color: '#fff',
              textTransform: 'uppercase',
              letterSpacing: '0.5px',
              fontWeight: 600,
            }}
          >
            Scoring
          </span>
          {winnerAgent && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span
                style={{
                  width: '8px',
                  height: '8px',
                  borderRadius: '50%',
                  backgroundColor: winnerAgent.color,
                }}
              />
              <span style={{ fontSize: '13px', fontWeight: 500 }}>{winnerAgent.displayName}</span>
              <span style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)' }}>
                scored {details.scores?.[winnerAgent.id]?.toFixed(1) || 'N/A'}
              </span>
            </div>
          )}
        </div>
        <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.5)' }}>{isExpanded ? '[-]' : '[+]'}</span>
      </button>

      {/* Expanded content */}
      {isExpanded && (
        <div style={{ padding: '0 14px 14px' }}>
          {/* Score bars */}
          <div style={{ marginBottom: '12px' }}>
            <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)', marginBottom: '10px' }}>
              Scores (0-10 scale):
            </div>
            {sortedAgents.map((agent) => {
              const score = details.scores?.[agent.id] || 0;
              const percentage = (score / maxScore) * 100;
              const isWinner = agent.id === details.winner;

              return (
                <div key={agent.id} style={{ marginBottom: '8px' }}>
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      marginBottom: '4px',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                      {isWinner && <span style={{ fontSize: '12px' }}>+</span>}
                      <span
                        style={{
                          width: '8px',
                          height: '8px',
                          borderRadius: '50%',
                          backgroundColor: agent.color,
                        }}
                      />
                      <span
                        style={{
                          fontSize: '12px',
                          fontWeight: isWinner ? 600 : 400,
                          color: isWinner ? '#fff' : 'rgba(255,255,255,0.8)',
                        }}
                      >
                        {agent.displayName}
                      </span>
                      {isWinner && (
                        <span
                          style={{
                            fontSize: '9px',
                            padding: '2px 5px',
                            borderRadius: '3px',
                            backgroundColor: 'rgba(251, 191, 36, 0.3)',
                            color: '#FCD34D',
                          }}
                        >
                          WINNER
                        </span>
                      )}
                    </div>
                    <span
                      style={{
                        fontSize: '12px',
                        fontWeight: 500,
                        color: isWinner ? '#FCD34D' : 'rgba(255,255,255,0.7)',
                      }}
                    >
                      {score.toFixed(1)}
                    </span>
                  </div>
                  <div
                    style={{
                      height: '6px',
                      backgroundColor: 'rgba(255,255,255,0.1)',
                      borderRadius: '3px',
                      overflow: 'hidden',
                    }}
                  >
                    <div
                      style={{
                        width: `${percentage}%`,
                        height: '100%',
                        backgroundColor: isWinner ? '#FCD34D' : agent.color,
                        borderRadius: '3px',
                        transition: 'width 0.3s ease',
                      }}
                    />
                  </div>
                </div>
              );
            })}
          </div>

          {/* Scoring reasoning */}
          {details.reasoning && (
            <div>
              <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)', marginBottom: '6px' }}>
                Scoring breakdown:
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
