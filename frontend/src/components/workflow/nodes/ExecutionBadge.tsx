import type React from 'react';

interface ExecutionBadgeProps {
  status: 'idle' | 'running' | 'completed' | 'error' | 'retrying';
  retryAttempt?: number;
  retryMaxAttempts?: number;
}

const ExecutionBadge: React.FC<ExecutionBadgeProps> = ({ status, retryAttempt, retryMaxAttempts }) => {
  if (status === 'idle') return null;

  const getIcon = () => {
    switch (status) {
      case 'running':
        return '⏳';
      case 'retrying':
        return '↻';
      case 'completed':
        return '✔';
      case 'error':
        return '✖';
      default:
        return '';
    }
  };

  const getColor = () => {
    switch (status) {
      case 'running':
        return '#FF9800';
      case 'retrying':
        return '#FFA726';
      case 'completed':
        return '#4CAF50';
      case 'error':
        return '#F44336';
      default:
        return '#999';
    }
  };

  const showRetryBadge = retryAttempt != null && retryMaxAttempts != null && retryAttempt > 1;

  return (
    <>
      <div
        style={{
          position: 'absolute',
          top: '-8px',
          right: '-8px',
          width: '24px',
          height: '24px',
          borderRadius: '50%',
          backgroundColor: 'white',
          color: getColor(),
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: '14px',
          fontWeight: 'bold',
          boxShadow: '0 2px 6px rgba(0,0,0,0.3)',
          border: `2px solid ${getColor()}`,
          animation: status === 'running' || status === 'retrying' ? 'pulse 1.5s ease-in-out infinite' : 'none',
          zIndex: 10,
        }}
      >
        {getIcon()}
      </div>
      {showRetryBadge && (
        <div
          style={{
            position: 'absolute',
            top: '-8px',
            left: '-8px',
            padding: '1px 5px',
            borderRadius: '8px',
            backgroundColor: '#FFA726',
            color: 'white',
            fontSize: '9px',
            fontWeight: 'bold',
            boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
            zIndex: 10,
          }}
        >
          {retryAttempt}/{retryMaxAttempts}
        </div>
      )}
      <style>{`
        @keyframes pulse {
          0%, 100% {
            transform: scale(1);
            opacity: 1;
          }
          50% {
            transform: scale(1.15);
            opacity: 0.85;
          }
        }
      `}</style>
    </>
  );
};

export default ExecutionBadge;
