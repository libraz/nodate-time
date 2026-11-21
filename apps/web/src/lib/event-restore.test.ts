import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { jsonResponse, mockApi } from '@/test/mock-client';

vi.mock('@/i18n', () => ({
  getT: () => (key: string, params?: Record<string, string>) =>
    params ? `${key}:${Object.values(params).join(',')}` : key,
}));

const toastInfo = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
vi.mock('@/lib/toast', () => ({ toast: { info: toastInfo, error: toastError } }));

import { offerOccurrenceRestore, offerRestore } from './event-restore';

const RESTORE = 'POST /calendars/cal-1/events/evt-1/restore';
const UNDO_CANCEL = 'DELETE /calendars/cal-1/events/evt-1/exceptions/2026-04-03';

function errorBody(status: number, code: string) {
  return jsonResponse({ status, code, message: code }, status);
}

/** Runs the action the toast was shown with. */
function undo(): void {
  const [, action] = toastInfo.mock.calls[0] ?? [];
  action.run();
}

beforeEach(() => {
  localStorage.clear();
  toastInfo.mockReset();
  toastError.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the toast after a deletion', () => {
  const offer = () => ({
    calendarId: 'cal-1',
    eventId: 'evt-1',
    title: 'Lunch',
    onRestored: vi.fn(),
  });

  it('names the event and offers an undo', () => {
    offerRestore(offer());

    expect(toastInfo).toHaveBeenCalledWith('restore.toast:Lunch', {
      label: 'restore.undo',
      run: expect.any(Function),
    });
  });

  it('restores the event through the API and reloads', async () => {
    const fetchMock = mockApi({ [RESTORE]: jsonResponse({ id: 'evt-1' }) });
    const o = offer();
    offerRestore(o);

    undo();

    await vi.waitFor(() => expect(o.onRestored).toHaveBeenCalled());
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(toastError).not.toHaveBeenCalled();
  });

  it('says the grace period is over when the server refuses an old deletion', async () => {
    mockApi({ [RESTORE]: errorBody(410, 'EVENT.RESTORE_EXPIRED') });
    const o = offer();
    offerRestore(o);

    undo();

    await vi.waitFor(() => expect(toastError).toHaveBeenCalledWith('restore.expired'));
    expect(o.onRestored).not.toHaveBeenCalled();
  });

  it('reports any other refusal in its own words', async () => {
    mockApi({ [RESTORE]: errorBody(409, 'EVENT.NOT_DELETED') });
    offerRestore(offer());

    undo();

    await vi.waitFor(() => expect(toastError).toHaveBeenCalledWith('apiError.EVENT.NOT_DELETED'));
  });
});

describe('the toast after cancelling one occurrence', () => {
  const offer = () => ({
    calendarId: 'cal-1',
    seriesId: 'evt-1',
    occurrenceDate: '2026-04-03',
    seriesEtag: '"master"',
    title: 'Standup',
    onRestored: vi.fn(),
  });

  it('puts the occurrence back against the master version and reloads', async () => {
    const fetchMock = mockApi({ [UNDO_CANCEL]: new Response(null, { status: 204 }) });
    const o = offer();
    offerOccurrenceRestore(o);

    undo();

    await vi.waitFor(() => expect(o.onRestored).toHaveBeenCalled());
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(request.headers.get('If-Match')).toBe('"master"');
  });

  it('reports a master changed meanwhile as a conflict and reloads', async () => {
    mockApi({ [UNDO_CANCEL]: errorBody(412, 'EVENT.STALE') });
    const o = offer();
    offerOccurrenceRestore(o);

    undo();

    await vi.waitFor(() => expect(toastError).toHaveBeenCalledWith('conflict.stale'));
    expect(o.onRestored).toHaveBeenCalled();
  });
});
