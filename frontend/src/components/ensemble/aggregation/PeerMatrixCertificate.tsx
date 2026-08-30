export interface PeerMatrixEvaluationPair {
  reviewer_id: string;
  candidate_id: string;
}

export interface PeerMatrixScoreBound {
  observed_average?: number;
  lower_bound?: number;
  upper_bound?: number;
  valid_reviews: number;
  invalid_reviews: number;
  remaining_reviews: number;
  eliminated?: boolean;
  dominated_by?: string;
}

export interface PeerMatrixCertificateData {
  mode: string;
  proof_version: string;
  certified: boolean;
  winner?: string;
  score_min: number;
  score_max: number;
  normalization: string;
  tie_break: string;
  total_evaluations: number;
  completed_evaluations: number;
  skipped_evaluations: number;
  savings_ratio: number;
  rounds_completed: number;
  winner_lower_bound?: number;
  strongest_challenger_upper_bound?: number;
  guaranteed_margin?: number;
  bounds: Record<string, PeerMatrixScoreBound>;
  skipped_pairs?: PeerMatrixEvaluationPair[];
}

interface PeerMatrixCertificateProps {
  certificate: PeerMatrixCertificateData;
  getAgentName: (id: string) => string;
}

function formatScore(value: number | undefined): string {
  return value === undefined ? '—' : value.toFixed(2);
}

export function PeerMatrixCertificate({ certificate, getAgentName }: PeerMatrixCertificateProps) {
  const savingsPercent = Math.max(0, Math.min(100, certificate.savings_ratio * 100));
  const bounds = Object.entries(certificate.bounds || {}).sort(([idA, a], [idB, b]) => {
    if (idA === certificate.winner) return -1;
    if (idB === certificate.winner) return 1;
    if (Boolean(a.eliminated) !== Boolean(b.eliminated)) return a.eliminated ? 1 : -1;
    return idA.localeCompare(idB);
  });

  return (
    <div
      style={{
        marginBottom: '16px',
        padding: '12px',
        borderRadius: '6px',
        border: certificate.certified ? '1px solid rgba(16, 185, 129, 0.35)' : '1px solid rgba(6, 182, 212, 0.3)',
        backgroundColor: certificate.certified ? 'rgba(16, 185, 129, 0.08)' : 'rgba(6, 182, 212, 0.06)',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          justifyContent: 'space-between',
          gap: '12px',
          marginBottom: '10px',
        }}
      >
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '7px', marginBottom: '3px' }}>
            <span
              style={{
                padding: '2px 6px',
                borderRadius: '3px',
                fontSize: '9px',
                fontWeight: 700,
                letterSpacing: '0.5px',
                backgroundColor: certificate.certified ? 'rgba(16, 185, 129, 0.22)' : 'rgba(6, 182, 212, 0.2)',
                color: certificate.certified ? '#6EE7B7' : '#67E8F9',
              }}
            >
              {certificate.certified ? 'CERTIFIED' : 'PROGRESSIVE'}
            </span>
            <span style={{ fontSize: '12px', fontWeight: 600 }}>Bounded winner proof</span>
          </div>
          <div style={{ fontSize: '10px', color: 'rgba(255,255,255,0.45)' }}>
            {certificate.proof_version} · {certificate.normalization} · {certificate.tie_break}
          </div>
        </div>
        {certificate.certified && certificate.winner && (
          <div style={{ textAlign: 'right' }}>
            <div style={{ fontSize: '10px', color: 'rgba(255,255,255,0.45)' }}>Locked winner</div>
            <div style={{ fontSize: '13px', fontWeight: 700, color: '#6EE7B7' }}>
              {getAgentName(certificate.winner)}
            </div>
          </div>
        )}
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(110px, 1fr))',
          gap: '6px',
          marginBottom: '10px',
        }}
      >
        <ProofStat label="Reviews" value={`${certificate.completed_evaluations}/${certificate.total_evaluations}`} />
        <ProofStat label="Skipped" value={`${certificate.skipped_evaluations} (${savingsPercent.toFixed(1)}%)`} />
        <ProofStat label="Rounds" value={String(certificate.rounds_completed)} />
        <ProofStat label="Guaranteed margin" value={formatScore(certificate.guaranteed_margin)} />
      </div>

      {certificate.winner_lower_bound !== undefined && (
        <div
          style={{
            marginBottom: '10px',
            padding: '7px 8px',
            borderRadius: '4px',
            backgroundColor: 'rgba(0,0,0,0.16)',
            fontSize: '10px',
            color: 'rgba(255,255,255,0.65)',
          }}
        >
          Winner lower bound <strong style={{ color: '#fff' }}>{formatScore(certificate.winner_lower_bound)}</strong>
          {certificate.strongest_challenger_upper_bound !== undefined && (
            <>
              {' '}
              vs strongest challenger upper bound{' '}
              <strong style={{ color: '#fff' }}>{formatScore(certificate.strongest_challenger_upper_bound)}</strong>
            </>
          )}
        </div>
      )}

      {bounds.length > 0 && (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '10px' }}>
            <thead>
              <tr style={{ color: 'rgba(255,255,255,0.45)' }}>
                <th style={headerCellStyle}>Candidate</th>
                <th style={numericHeaderCellStyle}>Observed</th>
                <th style={numericHeaderCellStyle}>Lower</th>
                <th style={numericHeaderCellStyle}>Upper</th>
                <th style={numericHeaderCellStyle}>Reviews</th>
                <th style={headerCellStyle}>Status</th>
              </tr>
            </thead>
            <tbody>
              {bounds.map(([candidateId, bound]) => {
                const isWinner = candidateId === certificate.winner;
                return (
                  <tr key={candidateId} style={{ borderTop: '1px solid rgba(255,255,255,0.06)' }}>
                    <td style={{ ...bodyCellStyle, fontWeight: isWinner ? 700 : 400, color: isWinner ? '#6EE7B7' : '#fff' }}>
                      {getAgentName(candidateId)}
                    </td>
                    <td style={numericBodyCellStyle}>{formatScore(bound.observed_average)}</td>
                    <td style={numericBodyCellStyle}>{formatScore(bound.lower_bound)}</td>
                    <td style={numericBodyCellStyle}>{formatScore(bound.upper_bound)}</td>
                    <td style={numericBodyCellStyle}>
                      {bound.valid_reviews} valid
                      {bound.invalid_reviews > 0 ? ` / ${bound.invalid_reviews} invalid` : ''}
                    </td>
                    <td style={bodyCellStyle}>
                      {isWinner && certificate.certified
                        ? 'locked'
                        : bound.eliminated
                          ? `dominated${bound.dominated_by ? ` by ${getAgentName(bound.dominated_by)}` : ''}`
                          : `${bound.remaining_reviews} remaining`}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function ProofStat({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ padding: '6px 7px', borderRadius: '4px', backgroundColor: 'rgba(0,0,0,0.14)' }}>
      <div style={{ fontSize: '9px', color: 'rgba(255,255,255,0.42)', marginBottom: '2px' }}>{label}</div>
      <div style={{ fontSize: '11px', fontWeight: 600 }}>{value}</div>
    </div>
  );
}

const headerCellStyle = {
  textAlign: 'left' as const,
  padding: '5px 6px',
  fontWeight: 500,
};

const numericHeaderCellStyle = {
  ...headerCellStyle,
  textAlign: 'right' as const,
};

const bodyCellStyle = {
  padding: '5px 6px',
  color: 'rgba(255,255,255,0.72)',
  whiteSpace: 'nowrap' as const,
};

const numericBodyCellStyle = {
  ...bodyCellStyle,
  textAlign: 'right' as const,
  fontVariantNumeric: 'tabular-nums',
};
