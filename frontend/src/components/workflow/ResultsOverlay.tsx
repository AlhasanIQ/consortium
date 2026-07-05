import type React from 'react';
import { useState } from 'react';
import type { ExecutionState } from '../../hooks/useWorkflowExecution';
import MarkdownRenderer from '../common/MarkdownRenderer';

interface ResultsOverlayProps {
  executionState: ExecutionState;
  onClose?: () => void;
}

const ResultsOverlay: React.FC<ResultsOverlayProps> = ({ executionState, onClose }) => {
  const [isMinimized, setIsMinimized] = useState(false);
  const { messages, isExecuting, result } = executionState;

  // Get the last few messages for display
  const recentMessages = messages.slice(-5);
  const completedNodes = messages.filter((m) => m.type === 'node_complete').length;
  const totalNodes = messages.filter((m) => m.type === 'node_start').length;

  if (messages.length === 0 && !isExecuting) {
    return null;
  }

  if (isMinimized) {
    return (
      <div
        id="workflow-execution-results-minimized"
        role="button"
        tabIndex={0}
        style={{
          position: 'absolute',
          bottom: '20px',
          right: '20px',
          backgroundColor: 'rgba(255, 255, 255, 0.95)',
          borderRadius: '12px',
          padding: '12px 20px',
          boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
          border: '1px solid #ddd',
          cursor: 'pointer',
          zIndex: 1000,
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
        }}
        onClick={() => setIsMinimized(false)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setIsMinimized(false);
          }
        }}
      >
        {isExecuting ? (
          <>
            <div
              style={{
                width: '12px',
                height: '12px',
                borderRadius: '50%',
                backgroundColor: '#2196F3',
                animation: 'pulse 1.5s ease-in-out infinite',
              }}
            />
            <span style={{ fontSize: '14px', fontWeight: '500' }}>
              Executing... {completedNodes}/{totalNodes} nodes
            </span>
          </>
        ) : result ? (
          <>
            <div
              style={{
                width: '12px',
                height: '12px',
                borderRadius: '50%',
                backgroundColor: '#4CAF50',
              }}
            />
            <span style={{ fontSize: '14px', fontWeight: '500' }}>✓ Workflow completed</span>
          </>
        ) : (
          <span style={{ fontSize: '14px', fontWeight: '500' }}>View results</span>
        )}
      </div>
    );
  }

  return (
    <div
      id="workflow-execution-results"
      style={{
        position: 'absolute',
        bottom: '20px',
        right: '20px',
        width: '400px',
        maxHeight: '500px',
        backgroundColor: 'rgba(255, 255, 255, 0.98)',
        borderRadius: '12px',
        boxShadow: '0 8px 24px rgba(0,0,0,0.2)',
        border: '1px solid #ddd',
        overflow: 'hidden',
        zIndex: 1000,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '16px 20px',
          borderBottom: '1px solid #e0e0e0',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          backgroundColor: '#f8f9fa',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          <h3 style={{ margin: 0, fontSize: '16px', fontWeight: '600' }}>Execution Results</h3>
          {isExecuting && (
            <div
              style={{
                fontSize: '12px',
                color: '#666',
                backgroundColor: '#e3f2fd',
                padding: '4px 8px',
                borderRadius: '4px',
              }}
            >
              {completedNodes}/{totalNodes} nodes
            </div>
          )}
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <button
            type="button"
            onClick={() => setIsMinimized(true)}
            style={{
              background: 'none',
              border: 'none',
              fontSize: '18px',
              cursor: 'pointer',
              color: '#666',
              padding: '4px 8px',
            }}
            title="Minimize"
          >
            −
          </button>
          {onClose && !isExecuting && (
            <button
              type="button"
              onClick={onClose}
              style={{
                background: 'none',
                border: 'none',
                fontSize: '18px',
                cursor: 'pointer',
                color: '#666',
                padding: '4px 8px',
              }}
              title="Close"
            >
              ×
            </button>
          )}
        </div>
      </div>

      {/* Content */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '16px',
          maxHeight: '400px',
        }}
      >
        {/* Progress indicator */}
        {isExecuting && (
          <div
            style={{
              marginBottom: '16px',
              padding: '12px',
              backgroundColor: '#e3f2fd',
              borderRadius: '8px',
              borderLeft: '4px solid #2196F3',
            }}
          >
            <div style={{ fontSize: '13px', marginBottom: '8px', color: '#1976D2' }}>⏳ Executing workflow...</div>
            <div
              style={{
                height: '4px',
                backgroundColor: '#bbdefb',
                borderRadius: '2px',
                overflow: 'hidden',
              }}
            >
              <div
                style={{
                  height: '100%',
                  width: totalNodes > 0 ? `${(completedNodes / totalNodes) * 100}%` : '0%',
                  backgroundColor: '#2196F3',
                  transition: 'width 0.3s ease',
                }}
              />
            </div>
          </div>
        )}

        {/* Recent messages */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {recentMessages.map((msg) => (
            <div
              key={`${msg.type}-${msg.timestamp}`}
              style={{
                padding: '12px',
                backgroundColor: getMessageBgColor(msg.type),
                borderRadius: '8px',
                borderLeft: `4px solid ${getMessageBorderColor(msg.type)}`,
                fontSize: '13px',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '6px' }}>
                <span style={{ fontSize: '11px', color: '#666', fontWeight: '500' }}>
                  {getMessageIcon(msg.type)} {msg.type.replace('_', ' ').toUpperCase()}
                </span>
                <span style={{ fontSize: '11px', color: '#999' }}>{new Date(msg.timestamp).toLocaleTimeString()}</span>
              </div>
              <div style={{ color: '#333' }}>{msg.message}</div>

              {msg.output && (
                <div
                  style={{
                    marginTop: '8px',
                    padding: '8px',
                    backgroundColor: 'rgba(255,255,255,0.9)',
                    borderRadius: '4px',
                    fontSize: '12px',
                    maxHeight: '100px',
                    overflowY: 'auto',
                  }}
                >
                  <MarkdownRenderer content={msg.output} />
                </div>
              )}

              {msg.data && msg.type === 'node_complete' && (
                <div style={{ marginTop: '8px', fontSize: '11px', color: '#666' }}>
                  <div style={{ display: 'flex', gap: '12px', flexWrap: 'wrap' }}>
                    <span>
                      🔢 {msg.data.tokens_input}→{msg.data.tokens_output} tokens
                    </span>
                    <span>💰 ${msg.data.cost?.toFixed(4) || '0'}</span>
                    <span>⚡ {msg.data.latency_ms}ms</span>
                  </div>
                </div>
              )}

              {msg.data && msg.type === 'output' && (
                <div
                  style={{
                    marginTop: '8px',
                    padding: '8px',
                    backgroundColor: 'rgba(156, 39, 176, 0.1)',
                    borderRadius: '4px',
                    fontSize: '11px',
                  }}
                >
                  <div style={{ fontWeight: 'bold', marginBottom: '4px' }}>📤 {msg.data.output_name}</div>
                  <div style={{ fontFamily: 'monospace', fontSize: '10px' }}>
                    {JSON.stringify(msg.data.output_value)}
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>

        {/* Summary for completed workflows */}
        {result && !isExecuting && (
          <div
            style={{
              marginTop: '16px',
              padding: '16px',
              backgroundColor: '#e8f5e9',
              borderRadius: '8px',
              borderLeft: '4px solid #4CAF50',
            }}
          >
            <div style={{ fontSize: '14px', fontWeight: '600', color: '#2e7d32', marginBottom: '8px' }}>
              ✓ Workflow Completed Successfully
            </div>
            <div style={{ fontSize: '12px', color: '#666' }}>
              <div>Total tokens: {result.total_tokens || 0}</div>
              <div>Total cost: ${result.total_cost?.toFixed(6) || '0.000000'}</div>
              <div>Total time: {result.total_latency || 0}ms</div>
            </div>
            {result.outputs && Object.keys(result.outputs).length > 0 && (
              <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid #c8e6c9' }}>
                <div style={{ fontSize: '12px', fontWeight: '600', marginBottom: '6px' }}>Final Outputs:</div>
                {Object.entries(result.outputs).map(([name, value]) => (
                  <div
                    key={name}
                    style={{
                      fontSize: '12px',
                      backgroundColor: 'rgba(255,255,255,0.7)',
                      padding: '8px',
                      borderRadius: '4px',
                      marginTop: '6px',
                    }}
                  >
                    <div style={{ fontWeight: 'bold', marginBottom: '6px' }}>{name}:</div>
                    <MarkdownRenderer content={typeof value === 'string' ? value : JSON.stringify(value, null, 2)} />
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      <style>{`
        @keyframes pulse {
          0%, 100% {
            opacity: 1;
            transform: scale(1);
          }
          50% {
            opacity: 0.6;
            transform: scale(1.1);
          }
        }

        /* Markdown content styling */
        .markdown-content h1,
        .markdown-content h2,
        .markdown-content h3,
        .markdown-content h4,
        .markdown-content h5,
        .markdown-content h6 {
          margin-top: 0.8em;
          margin-bottom: 0.4em;
          font-weight: 600;
          line-height: 1.25;
        }

        .markdown-content h1 { font-size: 1.5em; border-bottom: 1px solid #eaecef; padding-bottom: 0.3em; }
        .markdown-content h2 { font-size: 1.3em; border-bottom: 1px solid #eaecef; padding-bottom: 0.3em; }
        .markdown-content h3 { font-size: 1.15em; }
        .markdown-content h4 { font-size: 1em; }
        .markdown-content h5 { font-size: 0.9em; }
        .markdown-content h6 { font-size: 0.85em; color: #6a737d; }

        .markdown-content p {
          margin-top: 0;
          margin-bottom: 0.8em;
        }

        .markdown-content ul,
        .markdown-content ol {
          margin-top: 0;
          margin-bottom: 0.8em;
          padding-left: 2em;
        }

        .markdown-content li {
          margin-bottom: 0.25em;
        }

        .markdown-content code {
          padding: 0.2em 0.4em;
          margin: 0;
          font-size: 85%;
          background-color: rgba(27,31,35,0.05);
          border-radius: 3px;
          font-family: 'SFMono-Regular', 'Consolas', 'Liberation Mono', 'Menlo', monospace;
        }

        .markdown-content pre {
          padding: 8px 12px;
          overflow: auto;
          font-size: 85%;
          line-height: 1.45;
          background-color: #f6f8fa;
          border-radius: 4px;
          margin-bottom: 0.8em;
        }

        .markdown-content pre code {
          padding: 0;
          margin: 0;
          background-color: transparent;
          border: 0;
        }

        .markdown-content blockquote {
          padding: 0 1em;
          color: #6a737d;
          border-left: 0.25em solid #dfe2e5;
          margin: 0 0 0.8em 0;
        }

        .markdown-content table {
          border-spacing: 0;
          border-collapse: collapse;
          margin-bottom: 0.8em;
          width: 100%;
        }

        .markdown-content table th,
        .markdown-content table td {
          padding: 6px 13px;
          border: 1px solid #dfe2e5;
        }

        .markdown-content table th {
          font-weight: 600;
          background-color: #f6f8fa;
        }

        .markdown-content table tr:nth-child(2n) {
          background-color: #f6f8fa;
        }

        .markdown-content a {
          color: #0366d6;
          text-decoration: none;
        }

        .markdown-content a:hover {
          text-decoration: underline;
        }

        .markdown-content hr {
          height: 0.25em;
          padding: 0;
          margin: 1em 0;
          background-color: #e1e4e8;
          border: 0;
        }

        .markdown-content img {
          max-width: 100%;
          box-sizing: content-box;
        }
      `}</style>
    </div>
  );
};

function getMessageIcon(type: string): string {
  switch (type) {
    case 'node_start':
      return '▶️';
    case 'node_complete':
      return '✓';
    case 'error':
      return '❌';
    case 'complete':
      return '🎉';
    case 'output':
      return '📤';
    default:
      return '•';
  }
}

function getMessageBgColor(type: string): string {
  switch (type) {
    case 'error':
      return '#ffebee';
    case 'complete':
      return '#e8f5e9';
    case 'node_complete':
      return '#e3f2fd';
    case 'node_start':
      return '#fff9c4';
    case 'output':
      return '#f3e5f5';
    default:
      return '#f5f5f5';
  }
}

function getMessageBorderColor(type: string): string {
  switch (type) {
    case 'error':
      return '#c62828';
    case 'complete':
      return '#4CAF50';
    case 'node_complete':
      return '#2196F3';
    case 'node_start':
      return '#FFC107';
    case 'output':
      return '#9C27B0';
    default:
      return '#999';
  }
}

export default ResultsOverlay;
