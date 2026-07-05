import type React from 'react';
import type { ExecutionState } from '../../hooks/useWorkflowExecution';
import { getErrorInfo } from '../../utils/errorMessages';
import { getBorderColor, getMessageColor } from './executionPanelUtils';

interface LogTabProps {
  executionState: ExecutionState;
  onClear: () => void;
  replayJobId: string | null;
}

export const LogTab: React.FC<LogTabProps> = ({ executionState, onClear, replayJobId }) => {
  const { messages, isExecuting, error, errorCode, result } = executionState;

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: '10px' }}>
        {messages.length > 0 && !isExecuting && (
          <button
            type="button"
            onClick={onClear}
            style={{
              padding: '5px 10px',
              fontSize: '12px',
              backgroundColor: '#666',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Clear
          </button>
        )}
      </div>

      {messages.length === 0 && !isExecuting && (
        <div style={{ color: '#999', textAlign: 'center', marginTop: '50px' }}>
          {replayJobId ? (
            <>
              Replay mode is active.
              <br />
              <br />
              Open the Replay tab to view node-by-node details.
            </>
          ) : (
            <>
              No execution in progress.
              <br />
              <br />
              Click "Execute" to run the workflow.
            </>
          )}
        </div>
      )}

      <div style={{ flex: 1, overflowY: 'auto' }}>
        {messages.map((msg) => (
          <div
            key={`${msg.type}-${msg.timestamp}`}
            style={{
              marginBottom: '12px',
              padding: '10px',
              backgroundColor: getMessageColor(msg.type),
              borderRadius: '4px',
              borderLeft: `4px solid ${getBorderColor(msg.type)}`,
            }}
          >
            <div style={{ fontSize: '11px', color: '#666', marginBottom: '4px' }}>
              {new Date(msg.timestamp).toLocaleTimeString()}
              {msg.node_id && (
                <span style={{ marginLeft: '8px' }} title={msg.node_id}>
                  • Node: {msg.node_id.substring(0, 8)}...
                </span>
              )}
            </div>
            <div style={{ fontSize: '13px', fontWeight: msg.type === 'error' ? 'bold' : 'normal' }}>{msg.message}</div>
            {msg.output && (
              <div
                style={{
                  marginTop: '8px',
                  padding: '8px',
                  backgroundColor: 'white',
                  borderRadius: '3px',
                  fontSize: '12px',
                  fontFamily: 'monospace',
                  whiteSpace: 'pre-wrap',
                  maxHeight: '150px',
                  overflowY: 'auto',
                }}
              >
                {msg.output}
              </div>
            )}
            {msg.error &&
              (() => {
                const info = getErrorInfo(msg.code);
                return (
                  <div
                    style={{
                      marginTop: '8px',
                      padding: '8px',
                      backgroundColor: '#ffebee',
                      borderRadius: '3px',
                      fontSize: '12px',
                      color: '#c62828',
                    }}
                  >
                    {info && <div style={{ fontWeight: 'bold' }}>{info.label}</div>}
                    <div>{msg.error}</div>
                    {info && <div style={{ color: '#666', marginTop: '4px' }}>{info.action}</div>}
                  </div>
                );
              })()}
            {msg.data && msg.type === 'node_complete' && (
              <div style={{ marginTop: '8px', fontSize: '11px', color: '#666' }}>
                <div>
                  Tokens: {msg.data.tokens_input} in / {msg.data.tokens_output} out
                </div>
                <div>Cost: ${msg.data.cost?.toFixed(6) || '0.000000'}</div>
                <div>Latency: {msg.data.latency_ms}ms</div>
              </div>
            )}
            {msg.data && msg.type === 'complete' && (
              <div style={{ marginTop: '8px', fontSize: '12px', fontWeight: 'bold', color: '#2e7d32' }}>
                <div>Total Tokens: {msg.data.total_tokens}</div>
                <div>Total Cost: ${msg.data.total_cost?.toFixed(6) || '0.000000'}</div>
                <div>Total Time: {msg.data.total_latency}ms</div>
                {msg.data.outputs && Object.keys(msg.data.outputs).length > 0 && (
                  <div style={{ marginTop: '8px', paddingTop: '8px', borderTop: '1px solid #c8e6c9' }}>
                    <div style={{ marginBottom: '4px' }}>Outputs:</div>
                    {Object.entries(msg.data.outputs).map(([name, value]) => (
                      <div key={name} style={{ marginLeft: '8px', fontSize: '11px' }}>
                        <strong>{name}:</strong> {JSON.stringify(value)}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
            {msg.data && msg.type === 'output' && (
              <div
                style={{
                  marginTop: '8px',
                  fontSize: '11px',
                  color: '#666',
                  backgroundColor: '#f3e5f5',
                  padding: '8px',
                  borderRadius: '4px',
                }}
              >
                <div style={{ fontWeight: 'bold', marginBottom: '4px' }}>Output: {msg.data.output_name}</div>
                <div style={{ fontFamily: 'monospace', fontSize: '10px' }}>{JSON.stringify(msg.data.output_value)}</div>
              </div>
            )}
          </div>
        ))}
      </div>

      {isExecuting && (
        <div
          style={{
            marginTop: '15px',
            padding: '12px',
            backgroundColor: '#e3f2fd',
            borderRadius: '4px',
            textAlign: 'center',
            fontSize: '13px',
          }}
        >
          <div style={{ marginBottom: '8px' }}>Executing workflow...</div>
          <div
            style={{
              height: '4px',
              backgroundColor: '#90caf9',
              borderRadius: '2px',
              animation: 'pulse 1.5s ease-in-out infinite',
            }}
          />
        </div>
      )}

      {error &&
        !isExecuting &&
        (() => {
          const info = getErrorInfo(errorCode);
          return (
            <div
              style={{
                marginTop: '15px',
                padding: '12px',
                backgroundColor: '#ffebee',
                borderRadius: '4px',
                color: '#c62828',
                fontSize: '13px',
              }}
            >
              {info && <div style={{ fontWeight: 'bold', marginBottom: '4px' }}>{info.label}</div>}
              <div>{error}</div>
              {info && <div style={{ marginTop: '6px', color: '#666', fontSize: '12px' }}>{info.action}</div>}
            </div>
          );
        })()}

      {result && !isExecuting && (
        <div
          style={{
            marginTop: '15px',
            padding: '12px',
            backgroundColor: '#e8f5e9',
            borderRadius: '4px',
            color: '#2e7d32',
            fontSize: '13px',
          }}
        >
          Workflow completed successfully
        </div>
      )}

      <style>{`
        @keyframes pulse {
          0%, 100% {
            opacity: 1;
          }
          50% {
            opacity: 0.5;
          }
        }
      `}</style>
    </>
  );
};
