import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import { makeEvent } from '@/test/make-event';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  errorMessage: (e: unknown) => String((e as { code?: string }).code ?? e),
}));

const toastError = vi.hoisted(() => vi.fn());
const toastInfo = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('@/lib/toast', () => ({
  toast: { error: toastError, info: toastInfo, success: toastSuccess },
}));

const store = vi.hoisted(() => ({
  updateEvent: vi.fn(),
  fetchEvents: vi.fn(),
  visibleRange: () => ({ start: '2026-08-01', end: '2026-08-31' }),
  events: [] as CalendarEvent[],
}));
vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: Object.assign((selector: (s: typeof store) => unknown) => selector(store), {
    getState: () => store,
  }),
}));

import { useScopedUpdate } from './use-scoped-update';

function event(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return makeEvent({
    id: 'evt-1',
    startAt: '2026-08-03T10:00:00+09:00',
    endAt: '2026-08-03T11:00:00+09:00',
    etag: '"listed"',
    ...overrides,
  });
}

let request: ReturnType<typeof useScopedUpdate>['requestUpdate'] = () => {};

function Host() {
  const { requestUpdate, dialog } = useScopedUpdate();
  request = requestUpdate;
  return <>{dialog}</>;
}

const payload = { title: 'Standup', allDay: false, startAt: 'a', endAt: 'b' };

beforeEach(() => {
  store.updateEvent.mockReset().mockResolvedValue(undefined);
  store.fetchEvents.mockReset();
  toastError.mockReset();
  toastInfo.mockReset();
  toastSuccess.mockReset();
  store.events = [];
});

afterEach(cleanup);

describe('a drag or resize commit', () => {
  it('sends the etag the listing carried', async () => {
    render(<Host />);
    act(() => request(event(), payload));

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalled());
    expect(store.updateEvent).toHaveBeenCalledWith(
      'cal-1',
      'evt-1',
      payload,
      undefined,
      '"listed"',
    );
  });

  it('reports a stale refusal as a conflict and refetches, without retrying', async () => {
    store.updateEvent.mockRejectedValue(new ApiError(412, 'stale', 'EVENT.STALE'));
    render(<Host />);
    act(() => request(event(), payload));

    await waitFor(() => expect(store.fetchEvents).toHaveBeenCalledWith('2026-08-01', '2026-08-31'));
    expect(toastError).toHaveBeenCalledWith('conflict.stale');
    expect(store.updateEvent).toHaveBeenCalledTimes(1);
  });

  it('refetches without a conflict message when the event is gone', async () => {
    store.updateEvent.mockRejectedValue(new ApiError(404, 'gone', 'EVENT.NOT_FOUND'));
    render(<Host />);
    act(() => request(event(), payload));

    await waitFor(() => expect(store.fetchEvents).toHaveBeenCalled());
    expect(toastError).not.toHaveBeenCalledWith('conflict.stale');
  });
});

describe('undoing a drag', () => {
  it('puts the event back as it was, checked against the moved copy', async () => {
    const original = event();
    render(<Host />);
    act(() => request(original, payload));
    await waitFor(() => expect(toastInfo).toHaveBeenCalled());

    store.events = [event({ startAt: 'a', endAt: 'b', etag: '"moved"' })];
    const [, action] = toastInfo.mock.calls[0] ?? [];
    act(() => action.run());

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalledTimes(2));
    const [, , restored, scope, etag] = store.updateEvent.mock.calls[1] ?? [];
    expect(restored).toMatchObject({ startAt: original.startAt, endAt: original.endAt });
    expect(scope).toBeUndefined();
    expect(etag).toBe('"moved"');
    // The undo itself is not undoable: one offer, not an endless chain.
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('event.changeUndone'));
    expect(toastInfo).toHaveBeenCalledTimes(1);
  });

  it('is not offered for a write that failed', async () => {
    store.updateEvent.mockRejectedValue({ status: 500 });
    render(<Host />);
    act(() => request(event(), payload));

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastInfo).not.toHaveBeenCalled();
  });
});

describe('a drag of a recurring occurrence', () => {
  const mondayWeekly = event({
    id: 'evt-1_20260803',
    isRecurrence: true,
    etag: '"occurrence"',
    seriesEtag: '"master"',
    recurrenceRule: { freq: 'weekly', interval: 1, byDay: ['MO'] },
  });
  // The occurrence dropped on Tuesday the 4th.
  const toTuesday = { ...payload, startAt: '2026-08-04T10:00:00+09:00' };

  it('moves the whole series with a rule that agrees with the new start', async () => {
    render(<Host />);
    act(() => request(mondayWeekly, toTuesday));
    fireEvent.click(screen.getByRole('button', { name: 'event.scopeAll' }));

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalled());
    const [, , sent, scope, etag] = store.updateEvent.mock.calls[0] ?? [];
    expect(scope).toBe('all');
    // A scope over the series is checked against the master.
    expect(etag).toBe('"master"');
    expect(sent.recurrenceRule).toMatchObject({ freq: 'weekly', byDay: ['TU'] });
    // A series edit rewrites more than the copy an undo could restore.
    expect(toastInfo).not.toHaveBeenCalled();
  });

  it('leaves the rule alone when only this occurrence moves', async () => {
    render(<Host />);
    act(() => request(mondayWeekly, toTuesday));
    fireEvent.click(screen.getByRole('button', { name: 'event.scopeThis' }));

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalled());
    const [, , sent, scope, etag] = store.updateEvent.mock.calls[0] ?? [];
    expect(scope).toBe('this');
    expect(etag).toBe('"occurrence"');
    expect(sent.recurrenceRule).toBeUndefined();
  });

  it('asks before moving this and the following occurrences, then sends the master etag', async () => {
    render(<Host />);
    act(() => request(mondayWeekly, toTuesday));
    fireEvent.click(screen.getByRole('button', { name: 'scope.following' }));

    // Nothing is sent until the overwrite is confirmed.
    expect(store.updateEvent).not.toHaveBeenCalled();
    expect(screen.getByText('scope.followingDropsChanges')).toBeTruthy();
  });

  it('sends scope=following with the master etag and no rule once confirmed', async () => {
    render(<Host />);
    act(() => request(mondayWeekly, toTuesday));
    fireEvent.click(screen.getByRole('button', { name: 'scope.following' }));
    fireEvent.click(screen.getByRole('button', { name: 'scope.following' }));

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalled());
    const [, , sent, scope, etag] = store.updateEvent.mock.calls[0] ?? [];
    expect(scope).toBe('following');
    expect(etag).toBe('"master"');
    // The server derives the new rule from the moved start.
    expect(sent.recurrenceRule).toBeUndefined();
    expect(sent.startAt).toBe(toTuesday.startAt);
  });
});
