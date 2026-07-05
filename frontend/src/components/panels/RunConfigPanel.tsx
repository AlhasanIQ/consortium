import type React from 'react';
import { useState } from 'react';
import type { NormalizedConfig } from '../../types/events';

interface RunConfigPanelProps {
  config: NormalizedConfig;
}

const RunConfigPanel: React.FC<RunConfigPanelProps> = ({ config }) => {
  const [expanded, setExpanded] = useState(true);
  const shortHash = config.config_hash ? config.config_hash.slice(0, 12) : '';

  return (
    <div
      style={{
        backgroundColor: 'white',
        borderRadius: '8px',
        border: '1px solid #e0e0e0',
        marginBottom: '8px',
      }}
    >
      {/* Header */}
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        style={{
          width: '100%',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          padding: '8px 10px',
          border: 'none',
          backgroundColor: 'transparent',
          cursor: 'pointer',
        }}
      >
        <span style={{ fontSize: '11px', fontWeight: 'bold', color: '#333' }}>Run Configuration</span>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
          {shortHash && (
            <span
              style={{
                fontFamily: 'monospace',
                fontSize: '10px',
                color: '#666',
                backgroundColor: '#f0f0f0',
                padding: '1px 5px',
                borderRadius: '3px',
              }}
              title={config.config_hash}
            >
              {shortHash}
            </span>
          )}
          <span style={{ fontSize: '10px', color: '#999' }}>{expanded ? '\u25B2' : '\u25BC'}</span>
        </div>
      </button>

      {expanded && (
        <div style={{ padding: '0 10px 10px', fontSize: '10px' }}>
          {/* Nodes table */}
          {config.nodes.length > 0 && (
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid #e0e0e0' }}>
                  <th style={{ textAlign: 'left', padding: '3px 2px', color: '#666', fontWeight: 600 }}>Node</th>
                  <th style={{ textAlign: 'left', padding: '3px 2px', color: '#666', fontWeight: 600 }}>Model</th>
                  <th style={{ textAlign: 'right', padding: '3px 2px', color: '#666', fontWeight: 600 }}>Temp</th>
                  <th style={{ textAlign: 'right', padding: '3px 2px', color: '#666', fontWeight: 600 }}>Tokens</th>
                  <th style={{ textAlign: 'right', padding: '3px 2px', color: '#666', fontWeight: 600 }}>Timeout</th>
                </tr>
              </thead>
              <tbody>
                {config.nodes.map((node) => (
                  <tr key={node.node_id} style={{ borderBottom: '1px solid #f0f0f0' }}>
                    <td
                      style={{
                        padding: '3px 2px',
                        color: '#333',
                        fontFamily: 'monospace',
                        maxWidth: '80px',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                      title={node.node_id}
                    >
                      {node.node_id}
                    </td>
                    <td
                      style={{
                        padding: '3px 2px',
                        color: '#333',
                        maxWidth: '80px',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                      title={node.model || '-'}
                    >
                      {node.model || '-'}
                    </td>
                    <td style={{ padding: '3px 2px', textAlign: 'right', color: '#333' }}>{node.temperature}</td>
                    <td style={{ padding: '3px 2px', textAlign: 'right', color: '#333' }}>
                      {node.max_tokens.toLocaleString()}
                    </td>
                    <td style={{ padding: '3px 2px', textAlign: 'right', color: '#333' }}>{node.timeout_seconds}s</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {/* Cost limits */}
          {config.limits && (config.limits.max_cost_usd || config.limits.max_tokens) && (
            <div style={{ marginTop: '6px', color: '#666' }}>
              <span style={{ fontWeight: 600 }}>Limits: </span>
              {config.limits.max_cost_usd ? `$${config.limits.max_cost_usd} max cost` : ''}
              {config.limits.max_cost_usd && config.limits.max_tokens ? ' | ' : ''}
              {config.limits.max_tokens ? `${config.limits.max_tokens.toLocaleString()} max tokens` : ''}
            </div>
          )}

          {/* Empty state */}
          {config.nodes.length === 0 && (
            <div style={{ color: '#999', textAlign: 'center', padding: '8px' }}>No node configuration data.</div>
          )}
        </div>
      )}
    </div>
  );
};

export default RunConfigPanel;
