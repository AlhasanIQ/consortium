export function buildWebSocketURL(path: string): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}${normalizedPath}`;
}
