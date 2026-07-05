import { useBackendStatus } from '../../hooks/useBackendStatus';

export function BackendOfflineBanner() {
  const isOnline = useBackendStatus();

  if (isOnline) return null;

  return (
    <div
      className="fixed top-4 left-1/2 z-[100] flex -translate-x-1/2 items-center gap-2 rounded-lg bg-red-50 px-4 py-2 text-sm font-medium text-red-600 shadow-lg dark:bg-red-900/30 dark:text-red-400"
      role="alert"
      aria-live="assertive"
    >
      <span>✕</span>
      <span>Backend server is offline</span>
    </div>
  );
}

export default BackendOfflineBanner;
