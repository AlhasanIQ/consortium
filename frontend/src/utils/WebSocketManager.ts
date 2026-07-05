import type { ConnectionState, ReconnectOptions } from '../types/connection';
import { DEFAULT_RECONNECT_OPTIONS, shouldReconnect } from '../types/connection';
import { buildWebSocketURL } from './network';

export interface WebSocketEvent {
  type: string;
  job_id?: string;
  node_id?: string;
  sequence?: number;
  [key: string]: unknown;
}

export interface WebSocketManagerCallbacks {
  onMessage: (event: WebSocketEvent) => void;
  onStateChange: (state: ConnectionState) => void;
  onReconnecting?: (attempt: number, maxAttempts: number) => void;
  onRecovered?: () => void;
}

/**
 * WebSocketManager provides resilient WebSocket connections with:
 * - Exponential backoff + jitter for reconnection
 * - Snapshot recovery on reconnect
 * - Sequence tracking for event deduplication
 * - Connection state management
 */
export class WebSocketManager {
  private ws: WebSocket | null = null;
  private jobId: string;
  private options: ReconnectOptions;
  private callbacks: WebSocketManagerCallbacks;
  private state: ConnectionState;
  private reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  private wasConnected = false;

  constructor(jobId: string, callbacks: WebSocketManagerCallbacks, options: Partial<ReconnectOptions> = {}) {
    this.jobId = jobId;
    this.callbacks = callbacks;
    this.options = { ...DEFAULT_RECONNECT_OPTIONS, ...options };
    this.state = {
      status: 'disconnected',
      attempt: 0,
      maxAttempts: this.options.maxAttempts,
      lastSequence: 0,
    };
  }

  /**
   * Connect to the WebSocket server
   * @param resumeFrom Optional sequence number to resume from
   */
  connect(resumeFrom?: number): void {
    if (this.ws?.readyState === WebSocket.OPEN || this.ws?.readyState === WebSocket.CONNECTING) {
      return;
    }

    this.clearReconnectTimeout();

    const baseUrl = buildWebSocketURL(`/api/jobs/${this.jobId}/stream`);
    const url = resumeFrom !== undefined ? `${baseUrl}?resume_from=${resumeFrom}` : baseUrl;

    this.updateState({ status: 'connecting' });

    try {
      this.ws = new WebSocket(url);
      this.setupEventHandlers();
    } catch (error) {
      console.error('[WebSocketManager] Failed to create WebSocket:', error);
      this.handleConnectionFailure(error instanceof Error ? error.message : 'Connection failed');
    }
  }

  /**
   * Disconnect from the WebSocket server
   */
  disconnect(): void {
    this.clearReconnectTimeout();

    if (this.ws) {
      this.ws.onclose = null; // Prevent reconnect
      this.ws.close(1000, 'Client disconnect');
      this.ws = null;
    }

    this.updateState({ status: 'disconnected', attempt: 0 });
  }

  /**
   * Get the current connection state
   */
  getState(): ConnectionState {
    return { ...this.state };
  }

  /**
   * Get the last processed sequence number
   */
  getLastSequence(): number {
    return this.state.lastSequence;
  }

  private setupEventHandlers(): void {
    if (!this.ws) return;

    this.ws.onopen = () => {
      console.log('[WebSocketManager] Connected to job:', this.jobId);

      const wasReconnect = this.wasConnected;
      this.wasConnected = true;

      this.updateState({
        status: wasReconnect ? 'recovered' : 'connected',
        attempt: 0,
        lastConnectedAt: Date.now(),
        lastError: undefined,
      });

      if (wasReconnect) {
        this.callbacks.onRecovered?.();

        // Transition to 'connected' after brief recovery indication
        setTimeout(() => {
          if (this.state.status === 'recovered') {
            this.updateState({ status: 'connected' });
          }
        }, 2000);
      }
    };

    this.ws.onmessage = (event) => {
      try {
        const data: WebSocketEvent = JSON.parse(event.data);

        // Handle sequence-based deduplication
        if (data.sequence !== undefined) {
          if (data.sequence <= this.state.lastSequence) {
            console.log('[WebSocketManager] Dropping duplicate event:', data.sequence);
            return;
          }
          this.state.lastSequence = data.sequence;
        }

        this.callbacks.onMessage(data);
      } catch (error) {
        console.error('[WebSocketManager] Failed to parse message:', error);
      }
    };

    this.ws.onerror = (error) => {
      console.error('[WebSocketManager] WebSocket error:', error);
    };

    this.ws.onclose = (event) => {
      console.log('[WebSocketManager] WebSocket closed:', event.code, event.reason);
      this.ws = null;

      // Check if we should attempt reconnection
      if (shouldReconnect(event.code) && this.state.attempt < this.options.maxAttempts) {
        this.scheduleReconnect();
      } else {
        // Final closure - no reconnection
        this.updateState({
          status: event.code === 1000 || event.code === 4000 ? 'disconnected' : 'failed',
          lastError: event.reason || undefined,
        });
      }
    };
  }

  private scheduleReconnect(): void {
    const nextAttempt = this.state.attempt + 1;
    const delay = this.getBackoffDelay(nextAttempt);

    console.log(
      `[WebSocketManager] Scheduling reconnect attempt ${nextAttempt}/${this.options.maxAttempts} in ${delay}ms`,
    );

    this.updateState({
      status: 'reconnecting',
      attempt: nextAttempt,
    });

    this.callbacks.onReconnecting?.(nextAttempt, this.options.maxAttempts);

    this.reconnectTimeout = setTimeout(() => {
      this.fetchSnapshotAndReconnect();
    }, delay);
  }

  private async fetchSnapshotAndReconnect(): Promise<void> {
    try {
      // Fetch current job state
      const response = await fetch(`/api/jobs/${this.jobId}`);

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const snapshot = await response.json();

      // Update lastSequence from snapshot
      if (snapshot.snapshot_sequence !== undefined) {
        this.state.lastSequence = snapshot.snapshot_sequence;
      }

      // Check if job is already complete
      if (snapshot.complete || ['completed', 'failed', 'cancelled'].includes(snapshot.status)) {
        console.log('[WebSocketManager] Job already complete, no reconnect needed');
        this.updateState({ status: 'disconnected' });
        return;
      }

      // Reconnect with resume_from
      this.connect(this.state.lastSequence);
    } catch (error) {
      console.error('[WebSocketManager] Failed to fetch snapshot:', error);
      this.handleConnectionFailure(error instanceof Error ? error.message : 'Snapshot fetch failed');
    }
  }

  private handleConnectionFailure(error: string): void {
    if (this.state.attempt < this.options.maxAttempts) {
      this.scheduleReconnect();
    } else {
      this.updateState({
        status: 'failed',
        lastError: error,
      });
    }
  }

  private getBackoffDelay(attempt: number): number {
    // Exponential backoff: delay = min(base * 2^attempt, maxDelay)
    const exponentialDelay = Math.min(this.options.baseDelay * 2 ** (attempt - 1), this.options.maxDelay);

    // Add jitter to prevent thundering herd
    const jitter = exponentialDelay * this.options.jitterFactor * Math.random();

    return Math.floor(exponentialDelay + jitter);
  }

  private clearReconnectTimeout(): void {
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
  }

  private updateState(partial: Partial<ConnectionState>): void {
    this.state = { ...this.state, ...partial };
    this.callbacks.onStateChange(this.getState());
  }
}
