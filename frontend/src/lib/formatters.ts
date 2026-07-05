export function durationLabel(ms: number): string {
  if (ms <= 0) return '0ms';
  if (ms < 1000) return `${ms.toFixed(0)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`;
  const mins = Math.floor(ms / 60_000);
  const secs = ((ms % 60_000) / 1000).toFixed(0);
  return `${mins}m ${secs}s`;
}

export function formatCost(n: number): string {
  if (n === 0) return '$0.00';
  if (n < 0.0001) return `$${n.toFixed(8)}`;
  if (n < 0.01) return `$${n.toFixed(6)}`;
  return `$${n.toFixed(4)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

export function relativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = Date.now();
  const diffMs = now - date.getTime();
  if (diffMs < 0) return 'just now';

  const seconds = Math.floor(diffMs / 1000);
  if (seconds < 60) return `${seconds}s ago`;

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;

  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;

  return date.toLocaleDateString();
}

export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export function truncateId(id: string, length = 8): string {
  return id.length > length ? id.slice(0, length) : id;
}

/** Check whether a node metadata JSON string contains replayed=true. */
export function isNodeReplayed(metadata?: string): boolean {
  if (!metadata) return false;
  try {
    return JSON.parse(metadata)?.replayed === true;
  } catch {
    return false;
  }
}

/** Extract extraction_method from node metadata JSON string (set by contract_extract nodes). */
export function getExtractionMethod(metadata?: string): string | null {
  if (!metadata) return null;
  try {
    const m = JSON.parse(metadata)?.extraction_method;
    if (m === 'regex' || m === 'llm_fallback') return m;
    return null;
  } catch {
    return null;
  }
}
