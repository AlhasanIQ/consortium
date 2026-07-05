import { useState } from 'react';
import type { AgentState } from '../../../stores/ensembleStore';
import type { AggregationDetails } from '../../../types/workflow';

interface PeerMatrixDetailsProps {
  details: AggregationDetails;
  agents: AgentState[];
  collapsed?: boolean;
}

// Color scale for scores (1-10)
function getScoreColor(score: number): string {
  if (score >= 8) return 'rgba(16, 185, 129, 0.8)'; // Green
  if (score >= 6) return 'rgba(251, 191, 36, 0.8)'; // Yellow
  if (score >= 4) return 'rgba(249, 115, 22, 0.8)'; // Orange
  return 'rgba(239, 68, 68, 0.8)'; // Red
}

// Get bias indicator
function getBiasIndicator(bias: number): { label: string; color: string } | null {
  if (bias > 0.5) return { label: 'Generous', color: '#10B981' };
  if (bias < -0.5) return { label: 'Harsh', color: '#EF4444' };
  return null;
}

export function PeerMatrixDetails({ details, agents, collapsed = true }: PeerMatrixDetailsProps) {
  const [isExpanded, setIsExpanded] = useState(!collapsed);
  const [showNormalized, setShowNormalized] = useState(false); // Default to raw scores - more intuitive

  const winnerAgent = agents.find((a) => a.id === details.winner);
  const evalMatrix = details.evalMatrix;

  // Get sorted agent IDs from scores or agents list
  const agentIds = agents.map((a) => a.id);

  // Helper to get agent by ID
  const getAgent = (id: string) => agents.find((a) => a.id === id);

  // Get scores matrix to use
  const scoresMatrix = showNormalized
    ? evalMatrix?.normalized_scores || evalMatrix?.raw_scores
    : evalMatrix?.raw_scores;

  // Compute final averaged scores from raw or normalized matrix
  const computeFinalScores = (matrix: Record<string, Record<string, number>> | undefined) => {
    if (!matrix) return details.scores || {};
    const totals: Record<string, number> = {};
    const counts: Record<string, number> = {};
    for (const reviewerScores of Object.values(matrix)) {
      for (const [candidateId, score] of Object.entries(reviewerScores)) {
        totals[candidateId] = (totals[candidateId] || 0) + score;
        counts[candidateId] = (counts[candidateId] || 0) + 1;
      }
    }
    const averages: Record<string, number> = {};
    for (const [id, total] of Object.entries(totals)) {
      averages[id] = counts[id] > 0 ? total / counts[id] : 0;
    }
    return averages;
  };

  // Use raw final scores when showing raw, otherwise use backend's normalized final scores
  const displayScores = showNormalized ? details.scores || {} : computeFinalScores(evalMatrix?.raw_scores);

  return (
    <div
      style={{
        backgroundColor: 'rgba(6, 182, 212, 0.1)',
        border: '1px solid rgba(6, 182, 212, 0.3)',
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
              backgroundColor: '#06B6D4',
              color: '#fff',
              textTransform: 'uppercase',
              letterSpacing: '0.5px',
              fontWeight: 600,
            }}
          >
            Peer Matrix
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
              <span style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)' }}>highest peer rating</span>
            </div>
          )}
        </div>
        <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.5)' }}>{isExpanded ? '[-]' : '[+]'}</span>
      </button>

      {/* Expanded content */}
      {isExpanded && (
        <div style={{ padding: '0 14px 14px' }}>
          {/* Final scores bar */}
          <div style={{ marginBottom: '16px' }}>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '10px',
              }}
            >
              <span style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)' }}>
                Final averaged scores {showNormalized ? '(normalized)' : '(raw)'}:
              </span>
            </div>
            {Object.entries(displayScores)
              .sort(([idA, a], [idB, b]) => {
                // Sort by score descending, then by ID ascending for deterministic tie-breaking (matches backend)
                if (b !== a) return b - a;
                return idA.localeCompare(idB);
              })
              .map(([agentId, score]) => {
                const agent = getAgent(agentId);
                const isWinner = agentId === details.winner;
                const maxScore = Math.max(...Object.values(displayScores), 1);
                const percentage = (score / maxScore) * 100;

                return (
                  <div key={agentId} style={{ marginBottom: '8px' }}>
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
                            backgroundColor: agent?.color || '#6B7280',
                          }}
                        />
                        <span
                          style={{
                            fontSize: '12px',
                            fontWeight: isWinner ? 600 : 400,
                          }}
                        >
                          {agent?.displayName || agentId}
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
                      <span style={{ fontSize: '12px', fontWeight: 500, color: isWinner ? '#FCD34D' : '#fff' }}>
                        {score.toFixed(2)}
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
                          backgroundColor: isWinner ? '#FCD34D' : agent?.color || '#6B7280',
                          borderRadius: '3px',
                        }}
                      />
                    </div>
                  </div>
                );
              })}
          </div>

          {/* Evaluation Matrix Heatmap */}
          {scoresMatrix && Object.keys(scoresMatrix).length > 0 && (
            <div style={{ marginBottom: '16px' }}>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  marginBottom: '10px',
                }}
              >
                <span style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)' }}>Evaluation matrix (N×N):</span>
                <button
                  type="button"
                  onClick={() => setShowNormalized(!showNormalized)}
                  style={{
                    fontSize: '10px',
                    padding: '3px 8px',
                    borderRadius: '4px',
                    backgroundColor: showNormalized ? 'rgba(6, 182, 212, 0.3)' : 'rgba(255,255,255,0.1)',
                    border: 'none',
                    color: showNormalized ? '#06B6D4' : 'rgba(255,255,255,0.6)',
                    cursor: 'pointer',
                  }}
                >
                  {showNormalized ? 'Normalized' : 'Raw'}
                </button>
              </div>

              {/* Heatmap grid */}
              <div style={{ overflowX: 'auto' }}>
                <table
                  style={{
                    borderCollapse: 'collapse',
                    fontSize: '11px',
                    width: '100%',
                  }}
                >
                  <thead>
                    <tr>
                      <th
                        style={{
                          padding: '6px',
                          textAlign: 'left',
                          color: 'rgba(255,255,255,0.5)',
                          fontWeight: 400,
                          borderBottom: '1px solid rgba(255,255,255,0.1)',
                        }}
                      >
                        Reviewer / Candidate
                      </th>
                      {agentIds.map((candidateId) => {
                        const agent = getAgent(candidateId);
                        return (
                          <th
                            key={candidateId}
                            style={{
                              padding: '6px',
                              textAlign: 'center',
                              borderBottom: '1px solid rgba(255,255,255,0.1)',
                            }}
                          >
                            <span
                              style={{
                                display: 'inline-block',
                                width: '10px',
                                height: '10px',
                                borderRadius: '50%',
                                backgroundColor: agent?.color || '#6B7280',
                              }}
                            />
                          </th>
                        );
                      })}
                    </tr>
                  </thead>
                  <tbody>
                    {agentIds.map((reviewerId) => {
                      const reviewer = getAgent(reviewerId);
                      const reviewerScores = scoresMatrix[reviewerId] || {};
                      const bias = evalMatrix?.reviewer_bias?.[reviewerId];
                      const biasIndicator = bias !== undefined ? getBiasIndicator(bias) : null;

                      return (
                        <tr key={reviewerId}>
                          <td
                            style={{
                              padding: '6px',
                              borderBottom: '1px solid rgba(255,255,255,0.05)',
                            }}
                          >
                            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                              <span
                                style={{
                                  width: '8px',
                                  height: '8px',
                                  borderRadius: '50%',
                                  backgroundColor: reviewer?.color || '#6B7280',
                                }}
                              />
                              <span style={{ color: 'rgba(255,255,255,0.8)' }}>
                                {reviewer?.displayName || reviewerId}
                              </span>
                              {biasIndicator && (
                                <span
                                  style={{
                                    fontSize: '9px',
                                    padding: '1px 4px',
                                    borderRadius: '2px',
                                    backgroundColor: `${biasIndicator.color}22`,
                                    color: biasIndicator.color,
                                  }}
                                >
                                  {biasIndicator.label}
                                </span>
                              )}
                            </div>
                          </td>
                          {agentIds.map((candidateId) => {
                            const isSelf = reviewerId === candidateId;
                            const score = reviewerScores[candidateId];

                            return (
                              <td
                                key={candidateId}
                                style={{
                                  padding: '4px',
                                  textAlign: 'center',
                                  borderBottom: '1px solid rgba(255,255,255,0.05)',
                                }}
                              >
                                {isSelf ? (
                                  <span
                                    style={{
                                      display: 'inline-block',
                                      width: '28px',
                                      height: '24px',
                                      lineHeight: '24px',
                                      backgroundColor: 'rgba(255,255,255,0.05)',
                                      borderRadius: '3px',
                                      color: 'rgba(255,255,255,0.3)',
                                    }}
                                  >
                                    -
                                  </span>
                                ) : score !== undefined ? (
                                  <span
                                    style={{
                                      display: 'inline-block',
                                      width: '28px',
                                      height: '24px',
                                      lineHeight: '24px',
                                      backgroundColor: getScoreColor(score),
                                      borderRadius: '3px',
                                      color: '#fff',
                                      fontWeight: 500,
                                    }}
                                  >
                                    {score.toFixed(1)}
                                  </span>
                                ) : (
                                  <span
                                    style={{
                                      display: 'inline-block',
                                      width: '28px',
                                      height: '24px',
                                      lineHeight: '24px',
                                      backgroundColor: 'rgba(239, 68, 68, 0.2)',
                                      borderRadius: '3px',
                                      color: 'rgba(239, 68, 68, 0.6)',
                                    }}
                                  >
                                    !
                                  </span>
                                )}
                              </td>
                            );
                          })}
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>

              {/* Legend */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '16px',
                  marginTop: '10px',
                  fontSize: '10px',
                  color: 'rgba(255,255,255,0.5)',
                }}
              >
                <span>Score scale:</span>
                <div style={{ display: 'flex', gap: '8px' }}>
                  <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                    <span
                      style={{
                        width: '12px',
                        height: '12px',
                        borderRadius: '2px',
                        backgroundColor: 'rgba(239, 68, 68, 0.8)',
                      }}
                    />
                    1-4
                  </span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                    <span
                      style={{
                        width: '12px',
                        height: '12px',
                        borderRadius: '2px',
                        backgroundColor: 'rgba(249, 115, 22, 0.8)',
                      }}
                    />
                    4-6
                  </span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                    <span
                      style={{
                        width: '12px',
                        height: '12px',
                        borderRadius: '2px',
                        backgroundColor: 'rgba(251, 191, 36, 0.8)',
                      }}
                    />
                    6-8
                  </span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                    <span
                      style={{
                        width: '12px',
                        height: '12px',
                        borderRadius: '2px',
                        backgroundColor: 'rgba(16, 185, 129, 0.8)',
                      }}
                    />
                    8-10
                  </span>
                </div>
              </div>

              {/* Invalid count */}
              {evalMatrix?.invalid_count !== undefined && evalMatrix.invalid_count > 0 && (
                <div
                  style={{
                    marginTop: '8px',
                    fontSize: '10px',
                    color: 'rgba(239, 68, 68, 0.8)',
                  }}
                >
                  Warning: {evalMatrix.invalid_count} evaluation(s) failed
                </div>
              )}
            </div>
          )}

          {/* Reasoning */}
          {details.reasoning && (
            <div>
              <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.5)', marginBottom: '6px' }}>
                Evaluation summary:
              </div>
              <div
                style={{
                  padding: '10px',
                  backgroundColor: 'rgba(0,0,0,0.2)',
                  borderRadius: '4px',
                  fontSize: '12px',
                  lineHeight: 1.5,
                  color: 'rgba(255,255,255,0.8)',
                  maxHeight: '100px',
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
