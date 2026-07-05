import { useCallback } from 'react';

interface DuplicateRunDialogProps {
  isOpen: boolean;
  jobId: string;
  onOpenExisting: () => void;
  onStartNew: () => void;
  onClose: () => void;
}

export function DuplicateRunDialog({ isOpen, jobId, onOpenExisting, onStartNew, onClose }: DuplicateRunDialogProps) {
  const handleBackdropClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) {
        onClose();
      }
    },
    [onClose],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    },
    [onClose],
  );

  if (!isOpen) {
    return null;
  }

  return (
    <div
      onClick={handleBackdropClick}
      onKeyDown={handleKeyDown}
      role="dialog"
      aria-modal="true"
      aria-labelledby="duplicate-dialog-title"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 50,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'rgba(0, 0, 0, 0.5)',
        backdropFilter: 'blur(4px)',
      }}
    >
      <div
        style={{
          background: '#1e1e2e',
          borderRadius: '12px',
          boxShadow: '0 25px 50px rgba(0, 0, 0, 0.5)',
          padding: '24px',
          maxWidth: '420px',
          width: '100%',
          margin: '0 16px',
          border: '1px solid rgba(255, 255, 255, 0.1)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: '16px' }}>
          <div
            style={{
              flexShrink: 0,
              width: '40px',
              height: '40px',
              borderRadius: '50%',
              background: 'rgba(234, 179, 8, 0.15)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <svg
              width="20"
              height="20"
              fill="none"
              viewBox="0 0 24 24"
              stroke="#EAB308"
              role="img"
              aria-labelledby="warning-icon-title"
            >
              <title id="warning-icon-title">Warning</title>
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
              />
            </svg>
          </div>
          <div style={{ flex: 1 }}>
            <h3 id="duplicate-dialog-title" style={{ fontSize: '16px', fontWeight: 600, color: '#f0f0f0', margin: 0 }}>
              Duplicate Run Detected
            </h3>
            <p style={{ marginTop: '8px', fontSize: '13px', color: '#999', lineHeight: '1.5' }}>
              A run with this prompt already exists. Would you like to view the existing run or start a new one?
            </p>
            <p style={{ marginTop: '4px', fontSize: '11px', color: '#666', fontFamily: 'monospace' }}>
              Job ID: {jobId.slice(-12)}
            </p>
          </div>
        </div>

        <div style={{ marginTop: '20px', display: 'flex', gap: '10px', justifyContent: 'flex-end' }}>
          <button
            type="button"
            onClick={onClose}
            style={{
              padding: '8px 16px',
              fontSize: '13px',
              fontWeight: 500,
              color: '#999',
              background: 'rgba(255, 255, 255, 0.05)',
              border: '1px solid rgba(255, 255, 255, 0.1)',
              borderRadius: '8px',
              cursor: 'pointer',
            }}
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onOpenExisting}
            style={{
              padding: '8px 16px',
              fontSize: '13px',
              fontWeight: 500,
              color: '#8B5CF6',
              background: 'rgba(139, 92, 246, 0.15)',
              border: '1px solid rgba(139, 92, 246, 0.3)',
              borderRadius: '8px',
              cursor: 'pointer',
            }}
          >
            View Existing
          </button>
          <button
            type="button"
            onClick={onStartNew}
            style={{
              padding: '8px 16px',
              fontSize: '13px',
              fontWeight: 500,
              color: '#fff',
              background: 'linear-gradient(135deg, #8B5CF6 0%, #6D28D9 100%)',
              border: 'none',
              borderRadius: '8px',
              cursor: 'pointer',
            }}
          >
            Start New
          </button>
        </div>
      </div>
    </div>
  );
}

export default DuplicateRunDialog;
