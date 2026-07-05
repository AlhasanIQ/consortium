import { useSyncExternalStore } from 'react';

let online = true;
const listeners = new Set<() => void>();

function notify() {
  for (const l of listeners) l();
}

export function setBackendOnline(value: boolean) {
  if (online === value) return;
  online = value;
  notify();
}

export function useBackendStatus(): boolean {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => online,
  );
}
