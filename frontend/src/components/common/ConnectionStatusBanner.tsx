import { useEnsembleStore } from '../../stores/ensembleStore';
import type { ConnectionStatus } from '../../types/connection';

interface ConnectionStatusBannerProps {
  onRetry?: () => void;
}

const statusConfig: Record<
  ConnectionStatus,
  { message: string; bgColor: string; textColor: string; icon: string; show: boolean }
> = {
  disconnected: {
    message: 'Disconnected',
    bgColor: 'bg-gray-100 dark:bg-gray-800',
    textColor: 'text-gray-600 dark:text-gray-400',
    icon: '○',
    show: false, // Don't show when idle
  },
  connecting: {
    message: 'Connecting...',
    bgColor: 'bg-blue-50 dark:bg-blue-900/30',
    textColor: 'text-blue-600 dark:text-blue-400',
    icon: '◌',
    show: true,
  },
  connected: {
    message: 'Connected',
    bgColor: 'bg-green-50 dark:bg-green-900/30',
    textColor: 'text-green-600 dark:text-green-400',
    icon: '●',
    show: false, // Don't show when stable
  },
  reconnecting: {
    message: 'Reconnecting...',
    bgColor: 'bg-yellow-50 dark:bg-yellow-900/30',
    textColor: 'text-yellow-600 dark:text-yellow-400',
    icon: '◐',
    show: true,
  },
  recovered: {
    message: 'Connection recovered',
    bgColor: 'bg-green-50 dark:bg-green-900/30',
    textColor: 'text-green-600 dark:text-green-400',
    icon: '✓',
    show: true,
  },
  failed: {
    message: 'Connection failed',
    bgColor: 'bg-red-50 dark:bg-red-900/30',
    textColor: 'text-red-600 dark:text-red-400',
    icon: '✕',
    show: true,
  },
};

export function ConnectionStatusBanner({ onRetry }: ConnectionStatusBannerProps) {
  const connectionState = useEnsembleStore((state) => state.connectionState);
  const { status, attempt, maxAttempts, lastError } = connectionState;

  const config = statusConfig[status];

  // Don't render if this status shouldn't be shown
  if (!config.show) {
    return null;
  }

  return (
    <div
      className={`fixed top-4 left-1/2 -translate-x-1/2 z-50 px-4 py-2 rounded-lg shadow-lg
        ${config.bgColor} ${config.textColor}
        flex items-center gap-2 text-sm font-medium
        animate-in slide-in-from-top duration-300`}
      role="status"
      aria-live="polite"
    >
      <span className="animate-pulse">{config.icon}</span>
      <span>
        {config.message}
        {status === 'reconnecting' && ` (${attempt}/${maxAttempts})`}
      </span>
      {lastError && <span className="text-xs opacity-75">: {lastError}</span>}
      {status === 'failed' && onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="ml-2 px-2 py-1 text-xs bg-red-100 dark:bg-red-800 hover:bg-red-200 dark:hover:bg-red-700
            rounded transition-colors"
        >
          Retry
        </button>
      )}
    </div>
  );
}

export default ConnectionStatusBanner;
