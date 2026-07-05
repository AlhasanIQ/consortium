import type React from 'react';
import { useEffect, useState } from 'react';
import type { JobDetail, JobNode } from '../../hooks/useExecutionHistory';
import type { ConfigDiff, DiffCategory, FieldDiff } from '../../types/events';

interface DiffViewProps {
  left: JobDetail;
  right: JobDetail;
  onClose: () => void;
}

const statusColors: Record<string, string> = {
  completed: '#2e7d32',
  failed: '#c62828',
  running: '#1976d2',
  pending: '#f57c00',
  cancelled: '#757575',
};

function formatDeltaTokens(a: number, b: number): { text: string; color: string } | null {
  const delta = b - a;
  if (delta === 0) return null;
  const sign = delta > 0 ? '+' : '';
  // For tokens, lower is better, so negative delta (B < A) is green
  const color = delta < 0 ? '#2e7d32' : '#c62828';
  return { text: `${sign}${delta.toLocaleString()}`, color };
}

function formatDeltaCost(a: number, b: number): { text: string; color: string } | null {
  const delta = b - a;
  if (Math.abs(delta) < 0.0000005) return null;
  const sign = delta > 0 ? '+' : '';
  // For cost, lower is better, so negative delta (B < A) is green
  const color = delta < 0 ? '#2e7d32' : '#c62828';
  return { text: `${sign}$${delta.toFixed(6)}`, color };
}

function formatDeltaLatency(a: number, b: number): { text: string; color: string } | null {
  const delta = b - a;
  if (delta === 0) return null;
  const sign = delta > 0 ? '+' : '';
  // For latency, lower is better
  const color = delta < 0 ? '#2e7d32' : '#c62828';
  return { text: `${sign}${Math.round(delta)}ms`, color };
}

interface NodePair {
  nodeId: string;
  left?: JobNode;
  right?: JobNode;
}

function matchNodes(leftNodes: JobNode[], rightNodes: JobNode[]): NodePair[] {
  const pairs: NodePair[] = [];
  const rightMap = new Map<string, JobNode>();
  for (const node of rightNodes) {
    rightMap.set(node.node_id, node);
  }
  const matchedRight = new Set<string>();

  for (const leftNode of leftNodes) {
    const rightNode = rightMap.get(leftNode.node_id);
    if (rightNode) {
      matchedRight.add(leftNode.node_id);
      pairs.push({ nodeId: leftNode.node_id, left: leftNode, right: rightNode });
    } else {
      pairs.push({ nodeId: leftNode.node_id, left: leftNode });
    }
  }

  for (const rightNode of rightNodes) {
    if (!matchedRight.has(rightNode.node_id)) {
      pairs.push({ nodeId: rightNode.node_id, right: rightNode });
    }
  }

  return pairs;
}

const CollapsibleOutput: React.FC<{
  label: string;
  content: string;
  defaultExpanded?: boolean;
}> = ({ label, content, defaultExpanded = false }) => {
  const [expanded, setExpanded] = useState(defaultExpanded);

  return (
    <div style={{ marginTop: '4px' }}>
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        style={{
          padding: '2px 6px',
          fontSize: '10px',
          backgroundColor: '#f5f5f5',
          color: '#333',
          border: '1px solid #ddd',
          borderRadius: '4px',
          cursor: 'pointer',
        }}
      >
        {expanded ? 'Hide' : 'Show'} {label}
      </button>
      {expanded && (
        <div
          style={{
            marginTop: '4px',
            padding: '6px',
            backgroundColor: '#f7f7f7',
            borderRadius: '4px',
            fontSize: '10px',
            fontFamily: 'monospace',
            whiteSpace: 'pre-wrap',
            maxHeight: '120px',
            overflowY: 'auto',
            wordBreak: 'break-word',
          }}
        >
          {content}
        </div>
      )}
    </div>
  );
};

const categoryColors: Record<DiffCategory, string> = {
  structure: '#7b1fa2',
  model: '#1565c0',
  prompt: '#e65100',
  decoding: '#2e7d32',
  aggregation: '#6a1b9a',
  limits: '#c62828',
};

function formatDiffValue(val: unknown): string {
  if (val === null || val === undefined) return '(none)';
  if (typeof val === 'string') {
    if (val.length > 200) return `${val.slice(0, 200)}...`;
    return val;
  }
  return String(val);
}

const ConfigDiffSection: React.FC<{ configDiff: ConfigDiff }> = ({ configDiff }) => {
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set());

  const toggleExpand = (path: string) => {
    setExpandedPaths((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  if (configDiff.identical) {
    return (
      <div
        style={{
          backgroundColor: 'white',
          borderRadius: '8px',
          border: '1px solid #e0e0e0',
          padding: '10px',
        }}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span style={{ fontSize: '11px', fontWeight: 'bold', color: '#333' }}>Configuration</span>
          <span
            style={{
              padding: '2px 6px',
              borderRadius: '3px',
              backgroundColor: '#e8f5e9',
              color: '#2e7d32',
              fontSize: '9px',
              fontWeight: 600,
            }}
          >
            Same Config
          </span>
        </div>
        <div style={{ fontSize: '10px', color: '#666', marginTop: '4px', fontFamily: 'monospace' }}>
          {configDiff.left_hash.slice(0, 12)}
        </div>
      </div>
    );
  }

  // Group diffs by category
  const byCategory = new Map<string, FieldDiff[]>();
  for (const d of configDiff.diffs) {
    const existing = byCategory.get(d.category) || [];
    existing.push(d);
    byCategory.set(d.category, existing);
  }

  return (
    <div
      style={{
        backgroundColor: 'white',
        borderRadius: '8px',
        border: '1px solid #e0e0e0',
        padding: '10px',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
        <span style={{ fontSize: '11px', fontWeight: 'bold', color: '#333' }}>Configuration</span>
        <span
          style={{
            padding: '2px 6px',
            borderRadius: '3px',
            backgroundColor: '#fff3e0',
            color: '#e65100',
            fontSize: '9px',
            fontWeight: 600,
          }}
        >
          Config Changed ({configDiff.diffs.length})
        </span>
      </div>

      {Array.from(byCategory.entries()).map(([category, diffs]) => (
        <div key={category} style={{ marginBottom: '6px' }}>
          <div
            style={{
              fontSize: '9px',
              fontWeight: 600,
              color: categoryColors[category as DiffCategory] || '#333',
              textTransform: 'uppercase',
              letterSpacing: '0.5px',
              marginBottom: '2px',
            }}
          >
            {category}
          </div>
          {diffs.map((d) => {
            const isLongText =
              (typeof d.left === 'string' && d.left.length > 80) ||
              (typeof d.right === 'string' && d.right.length > 80);
            const isExpanded = expandedPaths.has(d.path);

            return (
              <div
                key={d.path}
                style={{
                  fontSize: '10px',
                  padding: '3px 4px',
                  borderRadius: '3px',
                  backgroundColor: '#fafafa',
                  marginBottom: '2px',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontFamily: 'monospace', color: '#555' }}>{d.path}</span>
                  {isLongText && (
                    <button
                      type="button"
                      onClick={() => toggleExpand(d.path)}
                      style={{
                        padding: '0 4px',
                        fontSize: '9px',
                        backgroundColor: '#f0f0f0',
                        border: '1px solid #ddd',
                        borderRadius: '3px',
                        cursor: 'pointer',
                        color: '#666',
                      }}
                    >
                      {isExpanded ? 'Less' : 'More'}
                    </button>
                  )}
                </div>
                <div style={{ display: 'flex', gap: '4px', alignItems: 'baseline', marginTop: '2px' }}>
                  <span style={{ color: '#c62828' }}>
                    {isLongText && !isExpanded ? `${formatDiffValue(d.left).slice(0, 80)}...` : formatDiffValue(d.left)}
                  </span>
                  <span style={{ color: '#999' }}>{'\u2192'}</span>
                  <span style={{ color: '#2e7d32' }}>
                    {isLongText && !isExpanded
                      ? `${formatDiffValue(d.right).slice(0, 80)}...`
                      : formatDiffValue(d.right)}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
};

const DiffView: React.FC<DiffViewProps> = ({ left, right, onClose }) => {
  const nodePairs = matchNodes(left.nodes, right.nodes);
  const [configDiff, setConfigDiff] = useState<ConfigDiff | null>(null);

  // Fetch config diff on mount
  useEffect(() => {
    if (!left.id || !right.id) return;
    fetch(`/api/jobs/${left.id}/diff/${right.id}`)
      .then((res) => {
        if (res.ok) return res.json();
        return null;
      })
      .then((data: ConfigDiff | null) => {
        if (data) setConfigDiff(data);
      })
      .catch(() => {
        // Config diff is best-effort; don't block the view
      });
  }, [left.id, right.id]);

  const tokensDelta = formatDeltaTokens(
    left.tokens_input + left.tokens_output,
    right.tokens_input + right.tokens_output,
  );
  const costDelta = formatDeltaCost(left.cost, right.cost);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
      {/* Header */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          padding: '10px',
          backgroundColor: 'white',
          borderRadius: '8px',
          border: '1px solid #e0e0e0',
        }}
      >
        <div style={{ fontSize: '12px', fontWeight: 'bold', color: '#333' }}>Comparing Runs</div>
        <button
          type="button"
          onClick={onClose}
          style={{
            padding: '4px 8px',
            fontSize: '12px',
            backgroundColor: 'transparent',
            color: '#666',
            border: '1px solid #ddd',
            borderRadius: '4px',
            cursor: 'pointer',
            lineHeight: 1,
          }}
        >
          X
        </button>
      </div>

      {/* Config diff section */}
      {configDiff && <ConfigDiffSection configDiff={configDiff} />}

      {/* Summary comparison table */}
      <div
        style={{
          backgroundColor: 'white',
          borderRadius: '8px',
          border: '1px solid #e0e0e0',
          padding: '10px',
        }}
      >
        <div style={{ fontSize: '11px', fontWeight: 'bold', color: '#333', marginBottom: '8px' }}>
          Summary Comparison
        </div>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '10px' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid #e0e0e0' }}>
              <th style={{ textAlign: 'left', padding: '4px 2px', color: '#666', fontWeight: 600 }}>Metric</th>
              <th style={{ textAlign: 'right', padding: '4px 2px', color: '#666', fontWeight: 600 }}>Run A</th>
              <th style={{ textAlign: 'right', padding: '4px 2px', color: '#666', fontWeight: 600 }}>Run B</th>
              <th style={{ textAlign: 'right', padding: '4px 2px', color: '#666', fontWeight: 600 }}>Delta</th>
            </tr>
          </thead>
          <tbody>
            {/* Status */}
            <tr style={{ borderBottom: '1px solid #f0f0f0' }}>
              <td style={{ padding: '4px 2px', color: '#333' }}>Status</td>
              <td style={{ padding: '4px 2px', textAlign: 'right' }}>
                <span
                  style={{
                    padding: '1px 5px',
                    borderRadius: '3px',
                    backgroundColor: statusColors[left.status] || '#757575',
                    color: 'white',
                    fontSize: '9px',
                  }}
                >
                  {left.status}
                </span>
              </td>
              <td style={{ padding: '4px 2px', textAlign: 'right' }}>
                <span
                  style={{
                    padding: '1px 5px',
                    borderRadius: '3px',
                    backgroundColor: statusColors[right.status] || '#757575',
                    color: 'white',
                    fontSize: '9px',
                  }}
                >
                  {right.status}
                </span>
              </td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#999' }}>-</td>
            </tr>
            {/* Tokens */}
            <tr style={{ borderBottom: '1px solid #f0f0f0' }}>
              <td style={{ padding: '4px 2px', color: '#333' }}>Tokens</td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#333' }}>
                {(left.tokens_input + left.tokens_output).toLocaleString()}
              </td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#333' }}>
                {(right.tokens_input + right.tokens_output).toLocaleString()}
              </td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: tokensDelta?.color || '#999' }}>
                {tokensDelta?.text || '-'}
              </td>
            </tr>
            {/* Cost */}
            <tr style={{ borderBottom: '1px solid #f0f0f0' }}>
              <td style={{ padding: '4px 2px', color: '#333' }}>Cost</td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#333' }}>
                ${left.cost?.toFixed(6) || '0.000000'}
              </td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#333' }}>
                ${right.cost?.toFixed(6) || '0.000000'}
              </td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: costDelta?.color || '#999' }}>
                {costDelta?.text || '-'}
              </td>
            </tr>
            {/* Nodes */}
            <tr>
              <td style={{ padding: '4px 2px', color: '#333' }}>Nodes</td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#333' }}>{left.node_count}</td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#333' }}>{right.node_count}</td>
              <td style={{ padding: '4px 2px', textAlign: 'right', color: '#999' }}>
                {left.node_count === right.node_count
                  ? '-'
                  : `${right.node_count > left.node_count ? '+' : ''}${right.node_count - left.node_count}`}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      {/* Per-node comparison */}
      <div style={{ fontSize: '11px', fontWeight: 'bold', color: '#333' }}>Node Comparison</div>
      {nodePairs.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '12px', color: '#999', fontSize: '11px' }}>
          No nodes to compare.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          {nodePairs.map((pair) => {
            const onlyInA = pair.left && !pair.right;
            const onlyInB = !pair.left && pair.right;
            const node = pair.left || pair.right;
            const nodeName = node?.node_name || node?.node_label || pair.nodeId;

            // For matched pairs, compute diffs
            const outputsMatch = pair.left && pair.right && (pair.left.output || '') === (pair.right.output || '');
            const pairTokensDelta =
              pair.left && pair.right
                ? formatDeltaTokens(
                    pair.left.tokens_input + pair.left.tokens_output,
                    pair.right.tokens_input + pair.right.tokens_output,
                  )
                : null;
            const pairCostDelta = pair.left && pair.right ? formatDeltaCost(pair.left.cost, pair.right.cost) : null;
            const pairLatencyDelta =
              pair.left && pair.right ? formatDeltaLatency(pair.left.latency_ms, pair.right.latency_ms) : null;

            return (
              <div
                key={pair.nodeId}
                style={{
                  backgroundColor: 'white',
                  borderRadius: '8px',
                  border: `1px solid ${onlyInA || onlyInB ? '#ffe082' : '#e0e0e0'}`,
                  padding: '10px',
                }}
              >
                {/* Node header */}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div style={{ fontSize: '11px', fontWeight: 600, color: '#333' }}>{nodeName}</div>
                  {onlyInA && (
                    <span
                      style={{
                        padding: '2px 6px',
                        borderRadius: '3px',
                        backgroundColor: '#fff3e0',
                        color: '#e65100',
                        fontSize: '9px',
                        fontWeight: 600,
                      }}
                    >
                      Only in Run A
                    </span>
                  )}
                  {onlyInB && (
                    <span
                      style={{
                        padding: '2px 6px',
                        borderRadius: '3px',
                        backgroundColor: '#fff3e0',
                        color: '#e65100',
                        fontSize: '9px',
                        fontWeight: 600,
                      }}
                    >
                      Only in Run B
                    </span>
                  )}
                  {pair.left && pair.right && outputsMatch && (
                    <span
                      style={{
                        padding: '2px 6px',
                        borderRadius: '3px',
                        backgroundColor: '#e8f5e9',
                        color: '#2e7d32',
                        fontSize: '9px',
                        fontWeight: 600,
                      }}
                    >
                      Identical
                    </span>
                  )}
                  {pair.left && pair.right && !outputsMatch && (
                    <span
                      style={{
                        padding: '2px 6px',
                        borderRadius: '3px',
                        backgroundColor: '#ffebee',
                        color: '#c62828',
                        fontSize: '9px',
                        fontWeight: 600,
                      }}
                    >
                      Different
                    </span>
                  )}
                </div>
                <div style={{ fontSize: '10px', color: '#777', fontFamily: 'monospace', marginTop: '2px' }}>
                  {pair.nodeId}
                </div>

                {/* Matched pair: status comparison */}
                {pair.left && pair.right && (
                  <div style={{ marginTop: '6px' }}>
                    <div style={{ display: 'flex', gap: '8px', fontSize: '10px', alignItems: 'center' }}>
                      <span style={{ color: '#666' }}>Status:</span>
                      <span
                        style={{
                          padding: '1px 5px',
                          borderRadius: '3px',
                          backgroundColor: statusColors[pair.left.status] || '#757575',
                          color: 'white',
                          fontSize: '9px',
                        }}
                      >
                        {pair.left.status}
                      </span>
                      <span style={{ color: '#999' }}>vs</span>
                      <span
                        style={{
                          padding: '1px 5px',
                          borderRadius: '3px',
                          backgroundColor: statusColors[pair.right.status] || '#757575',
                          color: 'white',
                          fontSize: '9px',
                        }}
                      >
                        {pair.right.status}
                      </span>
                    </div>

                    {/* Tokens comparison */}
                    <div style={{ display: 'flex', gap: '6px', fontSize: '10px', marginTop: '4px', color: '#555' }}>
                      <span>
                        Tokens: {(pair.left.tokens_input + pair.left.tokens_output).toLocaleString()} vs{' '}
                        {(pair.right.tokens_input + pair.right.tokens_output).toLocaleString()}
                      </span>
                      {pairTokensDelta && (
                        <span style={{ color: pairTokensDelta.color, fontWeight: 600 }}>({pairTokensDelta.text})</span>
                      )}
                    </div>

                    {/* Cost comparison */}
                    <div style={{ display: 'flex', gap: '6px', fontSize: '10px', marginTop: '2px', color: '#555' }}>
                      <span>
                        Cost: ${pair.left.cost?.toFixed(6)} vs ${pair.right.cost?.toFixed(6)}
                      </span>
                      {pairCostDelta && (
                        <span style={{ color: pairCostDelta.color, fontWeight: 600 }}>({pairCostDelta.text})</span>
                      )}
                    </div>

                    {/* Latency comparison */}
                    <div style={{ display: 'flex', gap: '6px', fontSize: '10px', marginTop: '2px', color: '#555' }}>
                      <span>
                        Latency: {Math.round(pair.left.latency_ms)}ms vs {Math.round(pair.right.latency_ms)}ms
                      </span>
                      {pairLatencyDelta && (
                        <span style={{ color: pairLatencyDelta.color, fontWeight: 600 }}>
                          ({pairLatencyDelta.text})
                        </span>
                      )}
                    </div>

                    {/* Output diff */}
                    {!outputsMatch && (
                      <div style={{ marginTop: '6px' }}>
                        {pair.left.output && <CollapsibleOutput label="Run A Output" content={pair.left.output} />}
                        {pair.right.output && <CollapsibleOutput label="Run B Output" content={pair.right.output} />}
                      </div>
                    )}
                  </div>
                )}

                {/* Only in one run: show details */}
                {(onlyInA || onlyInB) && node && (
                  <div style={{ marginTop: '6px', fontSize: '10px', color: '#555' }}>
                    <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                      <span>Status:</span>
                      <span
                        style={{
                          padding: '1px 5px',
                          borderRadius: '3px',
                          backgroundColor: statusColors[node.status] || '#757575',
                          color: 'white',
                          fontSize: '9px',
                        }}
                      >
                        {node.status}
                      </span>
                    </div>
                    <div style={{ marginTop: '2px' }}>
                      Tokens: {(node.tokens_input + node.tokens_output).toLocaleString()} | Cost: $
                      {node.cost?.toFixed(6)} | Latency: {Math.round(node.latency_ms)}ms
                    </div>
                    {node.output && <CollapsibleOutput label="Output" content={node.output} />}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default DiffView;
