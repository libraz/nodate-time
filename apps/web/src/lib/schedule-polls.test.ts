import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { schedulePollApi } from './schedule-polls';

const fetchMock = vi.fn();
const realFetch = globalThis.fetch;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const pollBody = {
  id: 'p1',
  etag: 'v2.r3',
  calendarId: 'c1',
  title: 'Lunch',
  timezone: 'UTC',
  status: 'open',
  canManage: true,
  canRespond: true,
  slots: null,
  myAnswers: null,
  respondedCount: 0,
  memberCount: 0,
};

const sent = (i = 0) => fetchMock.mock.calls[i]?.[0] as Request;

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockResolvedValue(jsonResponse(pollBody));
  globalThis.fetch = fetchMock as never;
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe('schedulePollApi writes', () => {
  it('sends the poll etag as If-Match on PATCH without version fields', async () => {
    await schedulePollApi.update('c1', 'p1', 'v2.r3', { title: 'Dinner' });
    expect(sent().method).toBe('PATCH');
    expect(sent().headers.get('If-Match')).toBe('v2.r3');
    expect(await sent().json()).toEqual({ title: 'Dinner' });
  });

  it('sends the poll etag as If-Match on finalize without version fields', async () => {
    await schedulePollApi.finalize('c1', 'p1', 'v2.r3', { slotId: 's1', acknowledged: true });
    expect(sent().url).toMatch(/\/schedule-polls\/p1\/finalize$/);
    expect(sent().headers.get('If-Match')).toBe('v2.r3');
    expect(await sent().json()).toEqual({ slotId: 's1', acknowledged: true });
  });

  it('sends the poll etag as If-Match on cancel with no body', async () => {
    await schedulePollApi.cancel('c1', 'p1', 'v2.r3');
    expect(sent().url).toMatch(/\/schedule-polls\/p1\/cancel$/);
    expect(sent().headers.get('If-Match')).toBe('v2.r3');
    expect(await sent().text()).toBe('');
  });

  it('reads the caller answer from GET .../response and PUTs it back with that etag', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ etag: 'a4', answers: [{ slotId: 's1', value: 'yes' }] }),
    );
    const mine = await schedulePollApi.getResponse('c1', 'p1');
    expect(sent().url).toMatch(/\/schedule-polls\/p1\/response$/);
    expect(mine).toEqual({ etag: 'a4', answers: [{ slotId: 's1', value: 'yes' }] });

    fetchMock.mockResolvedValueOnce(jsonResponse({ etag: 'a5', answers: null }));
    const saved = await schedulePollApi.respond('c1', 'p1', mine.etag, [
      { slotId: 's1', value: 'maybe' },
    ]);
    expect(sent(1).method).toBe('PUT');
    expect(sent(1).headers.get('If-Match')).toBe('a4');
    expect(await sent(1).json()).toEqual({ answers: [{ slotId: 's1', value: 'maybe' }] });
    expect(saved).toEqual({ etag: 'a5', answers: [] });
  });

  it('rejects a stale write with the server error code', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ status: 412, code: 'SCHEDULE_POLL.STALE', message: 'stale' }, 412),
    );
    await expect(schedulePollApi.cancel('c1', 'p1', 'v1.r0')).rejects.toMatchObject({
      status: 412,
      code: 'SCHEDULE_POLL.STALE',
    });
  });

  it('lists the caller actions from /user/schedule-poll-actions', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse([]));
    await schedulePollApi.actions();
    expect(new URL(sent().url).pathname).toBe('/user/schedule-poll-actions');
  });

  it('normalizes null lists on a poll', async () => {
    const poll = await schedulePollApi.get('c1', 'p1');
    expect(poll).toMatchObject({ etag: 'v2.r3', slots: [], myAnswers: [] });
  });
});
