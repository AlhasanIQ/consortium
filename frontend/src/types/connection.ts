// Connection state machine types for WebSocket resilience

export type ConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'recovered' | 'failed';

export interface ConnectionState {
  status: ConnectionStatus;
  attempt: number;
  maxAttempts: number;
  lastError?: string;
  lastConnectedAt?: number;
  lastSequence: number; // For event deduplication
}

export interface ReconnectOptions {
  baseDelay: number; // Default: 1000ms
  maxDelay: number; // Default: 30000ms
  maxAttempts: number; // Default: 5
  jitterFactor: number; // Default: 0.3 (30% jitter)
}

export const DEFAULT_RECONNECT_OPTIONS: ReconnectOptions = {
  baseDelay: 1000,
  maxDelay: 30000,
  maxAttempts: 5,
  jitterFactor: 0.3,
};

// WebSocket close codes from backend
export const WS_CLOSE_CODES = {
  NORMAL: 4000,
  FAILED: 4001,
  CANCELLED: 4002,
  SERVER_SHUTDOWN: 4003,
  RECONNECT_REQUIRED: 4004,
  POOL_EXHAUSTED: 4005,
} as const;

export type WSCloseCode = (typeof WS_CLOSE_CODES)[keyof typeof WS_CLOSE_CODES];

// Check if a close code indicates we should attempt reconnection
export function shouldReconnect(code: number): boolean {
  // Standard WebSocket codes
  if (code === 1000) return false; // Normal closure
  if (code === 1001) return true; // Going away - server might be restarting

  // Custom codes from backend
  if (code === WS_CLOSE_CODES.NORMAL) return false;
  if (code === WS_CLOSE_CODES.FAILED) return false;
  if (code === WS_CLOSE_CODES.CANCELLED) return false;
  if (code === WS_CLOSE_CODES.SERVER_SHUTDOWN) return true;
  if (code === WS_CLOSE_CODES.RECONNECT_REQUIRED) return true;
  if (code === WS_CLOSE_CODES.POOL_EXHAUSTED) return true;

  // Unknown codes - attempt reconnection
  return true;
}

// Get human-readable reason for close code
export function getCloseReason(code: number): string {
  switch (code) {
    case 1000:
      return 'Normal closure';
    case 1001:
      return 'Server going away';
    case WS_CLOSE_CODES.NORMAL:
      return 'Workflow completed';
    case WS_CLOSE_CODES.FAILED:
      return 'Workflow failed';
    case WS_CLOSE_CODES.CANCELLED:
      return 'Workflow cancelled';
    case WS_CLOSE_CODES.SERVER_SHUTDOWN:
      return 'Server shutdown';
    case WS_CLOSE_CODES.RECONNECT_REQUIRED:
      return 'Reconnect required';
    case WS_CLOSE_CODES.POOL_EXHAUSTED:
      return 'Server at capacity';
    default:
      return `Unknown (${code})`;
  }
}
