import { afterEach, beforeEach, describe, expect, it, vi } from 'bun:test';
import { type WebSocketEvent, type WebSocketJobSnapshot, WebSocketManager } from './WebSocketManager';

type FakeSocket = {
  url: string;
  readyState: number;
  onopen: (() => void) | null;
  onmessage: ((event: { data: string }) => void) | null;
  onerror: ((event: unknown) => void) | null;
  onclose: ((event: { code: number; reason: string }) => void) | null;
  close: ReturnType<typeof vi.fn>;
  open: () => void;
  message: (data: WebSocketEvent | string) => void;
  closeFromServer: (code: number, reason?: string) => void;
};

const sockets: FakeSocket[] = [];

class TestWebSocket implements FakeSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;

  url: string;
  readyState = TestWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  close = vi.fn(() => {
    this.readyState = TestWebSocket.CLOSED;
  });

  constructor(url: string) {
    this.url = url;
    sockets.push(this);
  }

  open() {
    this.readyState = TestWebSocket.OPEN;
    this.onopen?.();
  }

  message(data: WebSocketEvent | string) {
    this.onmessage?.({ data: typeof data === 'string' ? data : JSON.stringify(data) });
  }

  closeFromServer(code: number, reason = '') {
    this.readyState = TestWebSocket.CLOSED;
    this.onclose?.({ code, reason });
  }
}

function installBrowserGlobals() {
  Object.assign(globalThis, {
    WebSocket: TestWebSocket,
    window: { location: { protocol: 'http:', host: 'localhost:3000' } },
  });
}

describe('WebSocketManager', () => {
  beforeEach(() => {
    sockets.length = 0;
    installBrowserGlobals();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('delivers each sequence once and drops stale events before callbacks', () => {
    const messages: WebSocketEvent[] = [];
    const manager = new WebSocketManager('job-1', {
      onMessage: (message) => messages.push(message),
      onStateChange: () => {},
    });

    manager.connect();
    const socket = sockets[0];
    socket.open();
    socket.message({ type: 'node_start', data: { sequence: 2 } });
    socket.message({ type: 'duplicate', data: { sequence: 2 } });
    socket.message({ type: 'stale', data: { sequence: 1 } });
    socket.message({ type: 'node_complete', data: { sequence: 3 } });

    expect(messages.map((message) => message.type)).toEqual(['node_start', 'node_complete']);
    expect(manager.getLastSequence()).toBe(3);
  });

  it('applies the authoritative snapshot before resume and ignores stale replay events', async () => {
    const states: string[] = [];
    const reconnecting: Array<[number, number]> = [];
    const messages: WebSocketEvent[] = [];
    const snapshots: WebSocketJobSnapshot[] = [];
    const socketCountsAtSnapshot: number[] = [];
    const manager = new WebSocketManager(
      'job-2',
      {
        onMessage: (message) => messages.push(message),
        onStateChange: (state) => states.push(state.status),
        onSnapshot: (snapshot) => {
          snapshots.push(snapshot);
          socketCountsAtSnapshot.push(sockets.length);
        },
        onReconnecting: (attempt, maxAttempts) => reconnecting.push([attempt, maxAttempts]),
      },
      { baseDelay: 10, maxDelay: 10, maxAttempts: 2, jitterFactor: 0 },
    );
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'job-2',
        status: 'running',
        snapshot_sequence: 7,
        nodes: [{ node_id: 'agent-1', status: 'completed', output: 'from snapshot' }],
      }),
    } as Response);

    manager.connect();
    sockets[0].open();
    sockets[0].message({ type: 'node_start', node_id: 'agent-1', data: { sequence: 2 } });
    sockets[0].closeFromServer(4004, 'resume required');

    expect(manager.getState()).toMatchObject({ status: 'reconnecting', attempt: 1 });
    expect(reconnecting).toEqual([[1, 2]]);

    vi.advanceTimersByTime(10);
    await Promise.resolve();
    await Promise.resolve();

    expect(fetchMock).toHaveBeenCalledWith('/api/jobs/job-2');
    expect(snapshots).toEqual([
      {
        id: 'job-2',
        status: 'running',
        snapshot_sequence: 7,
        nodes: [{ node_id: 'agent-1', status: 'completed', output: 'from snapshot' }],
      },
    ]);
    expect(socketCountsAtSnapshot).toEqual([1]);
    expect(manager.getLastSequence()).toBe(7);
    expect(sockets).toHaveLength(2);
    expect(sockets[1].url).toBe('ws://localhost:3000/api/jobs/job-2/stream?resume_from=7');
    expect(states).toContain('connecting');

    sockets[1].open();
    sockets[1].message({ type: 'snapshot', snapshot_sequence: 7, job: { id: 'job-2', status: 'running' } });
    sockets[1].message({ type: 'stale-replay', data: { sequence: 6 } });
    sockets[1].message({ type: 'node_complete', node_id: 'agent-2', data: { sequence: 8 } });

    expect(messages.map((message) => message.type)).toEqual(['node_start', 'snapshot', 'node_complete']);
    expect(manager.getLastSequence()).toBe(8);
  });

  it('stops reconnecting when the snapshot says the job is terminal', async () => {
    const snapshots: WebSocketJobSnapshot[] = [];
    const manager = new WebSocketManager(
      'job-3',
      { onMessage: () => {}, onStateChange: () => {}, onSnapshot: (snapshot) => snapshots.push(snapshot) },
      { baseDelay: 1, maxDelay: 1, maxAttempts: 1, jitterFactor: 0 },
    );
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'completed', complete: true, snapshot_sequence: 12 }),
    } as Response);

    manager.connect();
    sockets[0].closeFromServer(1001, 'server going away');
    vi.advanceTimersByTime(1);
    await Promise.resolve();
    await Promise.resolve();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(snapshots).toEqual([{ status: 'completed', complete: true, snapshot_sequence: 12 }]);
    expect(manager.getState()).toMatchObject({ status: 'disconnected', attempt: 1, lastSequence: 12 });
    expect(sockets).toHaveLength(1);
  });
});
