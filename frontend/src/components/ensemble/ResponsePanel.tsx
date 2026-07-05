import type React from 'react';
import { useEnsembleStore } from '../../stores/ensembleStore';
import MarkdownRenderer from '../common/MarkdownRenderer';

const ResponsePanel: React.FC = () => {
  const { selectedAgentId, agents, setSelectedAgent } = useEnsembleStore();

  const selectedAgent = agents.find((a) => a.id === selectedAgentId);

  if (!selectedAgent) return null;

  const content = selectedAgent.output || selectedAgent.streamingText;

  return (
    <div
      style={{
        position: 'fixed',
        right: '20px',
        top: '50%',
        transform: 'translateY(-50%)',
        width: '350px',
        maxHeight: '70vh',
        background: 'linear-gradient(145deg, #1a1a2e 0%, #0f0f1a 100%)',
        borderRadius: '16px',
        border: `2px solid ${selectedAgent.color}`,
        boxShadow: `0 0 30px ${selectedAgent.color}33`,
        zIndex: 100,
        overflow: 'hidden',
        animation: 'slideIn 0.3s ease',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '16px',
          borderBottom: '1px solid rgba(255,255,255,0.1)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <div>
          <div
            style={{
              fontSize: '16px',
              fontWeight: '600',
              color: '#fff',
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
            }}
          >
            <span
              style={{
                width: '10px',
                height: '10px',
                borderRadius: '50%',
                background: selectedAgent.color,
              }}
            />
            {selectedAgent.displayName}
          </div>
          <div style={{ fontSize: '11px', color: '#666', marginTop: '2px' }}>{selectedAgent.model}</div>
        </div>

        <button
          type="button"
          onClick={() => setSelectedAgent(null)}
          style={{
            background: 'rgba(255,255,255,0.1)',
            border: 'none',
            borderRadius: '8px',
            width: '28px',
            height: '28px',
            color: '#888',
            cursor: 'pointer',
            fontSize: '14px',
          }}
        >
          ×
        </button>
      </div>

      {/* Status bar - only show when not idle */}
      {selectedAgent.status !== 'idle' && (
        <div
          style={{
            padding: '10px 16px',
            background: 'rgba(255,255,255,0.02)',
            display: 'flex',
            gap: '16px',
            fontSize: '11px',
            color: '#888',
            borderBottom: '1px solid rgba(255,255,255,0.05)',
          }}
        >
          <span>
            Status:{' '}
            <span
              style={{
                color:
                  selectedAgent.status === 'done'
                    ? '#10B981'
                    : selectedAgent.status === 'error'
                      ? '#EF4444'
                      : selectedAgent.color,
              }}
            >
              {selectedAgent.status === 'done' ? 'Complete' : selectedAgent.status}
            </span>
          </span>
          {selectedAgent.latencyMs > 0 && <span>{selectedAgent.latencyMs}ms</span>}
          {selectedAgent.tokens > 0 && <span>{selectedAgent.tokens} tokens</span>}
          {selectedAgent.cost > 0 && <span>${selectedAgent.cost.toFixed(5)}</span>}
        </div>
      )}

      {/* Content */}
      <div
        style={{
          padding: '16px',
          maxHeight: 'calc(70vh - 130px)',
          overflow: 'auto',
          fontSize: '13px',
          color: '#ccc',
          lineHeight: '1.6',
        }}
      >
        {content ? (
          <>
            <MarkdownRenderer content={content} />
            {selectedAgent.status === 'streaming' && <span className="blink">|</span>}
          </>
        ) : selectedAgent.status === 'error' ? (
          <div style={{ color: '#EF4444' }}>{selectedAgent.error}</div>
        ) : selectedAgent.status === 'idle' ? (
          <div style={{ color: '#666', fontStyle: 'italic' }}>Ready. Submit a prompt to start.</div>
        ) : (
          <div style={{ color: '#666', fontStyle: 'italic' }}>Processing...</div>
        )}
      </div>

      {/* Copy button */}
      {content && (
        <div
          style={{
            padding: '12px 16px',
            borderTop: '1px solid rgba(255,255,255,0.05)',
          }}
        >
          <button
            type="button"
            onClick={() => navigator.clipboard.writeText(content)}
            style={{
              width: '100%',
              padding: '8px',
              background: 'rgba(255,255,255,0.05)',
              border: '1px solid rgba(255,255,255,0.1)',
              borderRadius: '8px',
              color: '#888',
              fontSize: '12px',
              cursor: 'pointer',
              transition: 'all 0.2s ease',
            }}
          >
            Copy Response
          </button>
        </div>
      )}
    </div>
  );
};

export default ResponsePanel;
