import { afterEach, describe, expect, it, vi } from 'bun:test';
import { cancelJobAfterServerAcknowledgement } from './useEnsembleWebSocket';

describe('Ensemble cancellation acknowledgement', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('does not run the cancellation transition when the backend rejects the request', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 409,
      text: async () => 'job is no longer cancellable',
    } as Response);
    const onConfirmed = vi.fn();

    let failure: unknown;
    try {
      await cancelJobAfterServerAcknowledgement('job-running', onConfirmed);
    } catch (error) {
      failure = error;
    }

    expect(failure).toBeInstanceOf(Error);
    expect((failure as Error).message).toBe('Cancel request failed: 409 - job is no longer cancellable');
    expect(onConfirmed).not.toHaveBeenCalled();
  });

  it('runs the cancellation transition only after backend acknowledgement', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
    } as Response);
    const onConfirmed = vi.fn();

    await cancelJobAfterServerAcknowledgement('job-running', onConfirmed);

    expect(fetchMock).toHaveBeenCalledWith('/api/jobs/job-running/cancel', { method: 'POST' });
    expect(onConfirmed).toHaveBeenCalledTimes(1);
  });
});
