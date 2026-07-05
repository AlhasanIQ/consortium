import type React from 'react';
import { useEffect, useRef } from 'react';
import { useEnsembleStore } from '../../stores/ensembleStore';

interface PromptInputProps {
  onSubmit: () => void;
}

const PromptInput: React.FC<PromptInputProps> = ({ onSubmit }) => {
  const { prompt, setPrompt, isExecuting } = useEnsembleStore();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-focus when not executing
  useEffect(() => {
    if (!isExecuting && textareaRef.current) {
      textareaRef.current.focus();
    }
  }, [isExecuting]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (prompt.trim() && !isExecuting) {
        onSubmit();
      }
    }
  };

  return (
    <div
      style={{
        padding: '16px 24px',
        background: 'linear-gradient(180deg, rgba(15, 15, 26, 0.95) 0%, #0f0f1a 100%)',
        borderTop: '1px solid rgba(255, 255, 255, 0.1)',
      }}
    >
      <div
        style={{
          display: 'flex',
          gap: '12px',
          maxWidth: '800px',
          margin: '0 auto',
          alignItems: 'flex-end',
        }}
      >
        <textarea
          ref={textareaRef}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask the council..."
          disabled={isExecuting}
          rows={2}
          style={{
            flex: 1,
            background: 'rgba(255, 255, 255, 0.05)',
            border: '1px solid rgba(255, 255, 255, 0.15)',
            borderRadius: '12px',
            padding: '12px 16px',
            color: '#fff',
            fontSize: '15px',
            resize: 'none',
            outline: 'none',
            fontFamily: 'inherit',
            lineHeight: '1.5',
            opacity: isExecuting ? 0.5 : 1,
            transition: 'border-color 0.2s ease',
          }}
          onFocus={(e) => {
            e.target.style.borderColor = 'rgba(139, 92, 246, 0.5)';
          }}
          onBlur={(e) => {
            e.target.style.borderColor = 'rgba(255, 255, 255, 0.15)';
          }}
        />
        <button
          type="button"
          onClick={() => onSubmit()}
          disabled={!prompt.trim() || isExecuting}
          style={{
            padding: '12px 24px',
            background: isExecuting ? 'rgba(139, 92, 246, 0.3)' : 'linear-gradient(135deg, #8B5CF6 0%, #6D28D9 100%)',
            border: 'none',
            borderRadius: '12px',
            color: '#fff',
            fontSize: '14px',
            fontWeight: '500',
            cursor: isExecuting || !prompt.trim() ? 'not-allowed' : 'pointer',
            opacity: !prompt.trim() ? 0.5 : 1,
            transition: 'all 0.2s ease',
            boxShadow: isExecuting ? 'none' : '0 4px 15px rgba(139, 92, 246, 0.3)',
            whiteSpace: 'nowrap',
          }}
        >
          {isExecuting ? 'Processing...' : 'Consult'}
        </button>
      </div>
      <div
        style={{
          textAlign: 'center',
          marginTop: '8px',
          fontSize: '11px',
          color: '#444',
        }}
      >
        Press Enter to submit, Shift+Enter for new line
      </div>
    </div>
  );
};

export default PromptInput;
