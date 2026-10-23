import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

const getWithRevision = vi.hoisted(() => vi.fn());
vi.mock('@/lib/api', () => ({
  api: { getWithRevision },
  errorMessage: (e: unknown) => String((e as { code?: string }).code ?? e),
}));

const toastError = vi.hoisted(() => vi.fn());
vi.mock('@/lib/toast', () => ({ toast: { error: toastError } }));

const store = vi.hoisted(() => ({
  updateEvent: vi.fn(),
  fetchEvents: vi.fn(),
  visibleRange: () => ({ start: '2026-08-01', end: '2026-08-31' }),
}));
vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof store) => unknown) => selector(store),
}));

import { useScopedUpdate } from './use-scoped-update';

const UPDATED = '2026-08-01T00:00:00.000Z';

function makeEvent(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: 'evt-1',
    calendarId: 'cal-1',
    title: 'Standup',
    allDay: false,
    startAt: '2026-08-03T10:00:00+09:00',
    endAt: '2026-08-03T11:00:00+09:00',
    timezone: 'Asia/Tokyo',
    recurrenceRule: null,
    isRecurrence: false,
    updatedAt: UPDATED,
    ...overrides,
  } as CalendarEvent;
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
  getWithRevision.mockReset().mockResolvedValue({ data: { updatedAt: UPDATED }, revision: '"r1"' });
});

afterEach(cleanup);

describe('a drag or resize commit', () => {
  it('names the revision it checked its copy against', async () => {
    render(<Host />);
    act(() => request(makeEvent(), payload));

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalled());
    expect(store.updateEvent).toHaveBeenCalledWith('cal-1', 'evt-1', payload, undefined, '"r1"');
  });

  it('is refused, and the view refetched, when the copy it was built from is stale', async () => {
    getWithRevision.mockResolvedValue({
      data: { updatedAt: '2026-08-02T00:00:00.000Z' },
      revision: '"r2"',
    });
    render(<Host />);
    act(() => request(makeEvent(), payload));

    await waitFor(() => expect(store.fetchEvents).toHaveBeenCalledWith('2026-08-01', '2026-08-31'));
    expect(store.updateEvent).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledWith('apiError.EVENT.STALE');
  });

  it('refetches when the server refuses the revision', async () => {
    store.updateEvent.mockRejectedValue({ status: 409, code: 'EVENT.STALE' });
    render(<Host />);
    act(() => request(makeEvent(), payload));

    await waitFor(() => expect(store.fetchEvents).toHaveBeenCalled());
    expect(toastError).toHaveBeenCalled();
  });
});

describe('a drag of a recurring occurrence', () => {
  const mondayWeekly = makeEvent({
    id: 'evt-1_20260803',
    isRecurrence: true,
    recurrenceRule: { freq: 'weekly', interval: 1, byDay: ['MO'] },
  });
  // The occurrence dropped on Tuesday the 4th.
  const toTuesday = { ...payload, startAt: '2026-08-04T10:00:00+09:00' };

  it('moves the whole series with a rule that agrees with the new start', async () => {
    render(<Host />);
    act(() => request(mondayWeekly, toTuesday));
    fireEvent.click(screen.getByRole('button', { name: 'event.scopeAll' }));

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalled());
    const [, , sent, scope] = store.updateEvent.mock.calls[0] ?? [];
    expect(scope).toBe('all');
    expect(sent.recurrenceRule).toMatchObject({ freq: 'weekly', byDay: ['TU'] });
  });

  it('leaves the rule alone when only this occurrence moves', async () => {
    render(<Host />);
    act(() => request(mondayWeekly, toTuesday));
    fireEvent.click(screen.getByRole('button', { name: 'event.scopeThis' }));

    await waitFor(() => expect(store.updateEvent).toHaveBeenCalled());
    const [, , sent, scope] = store.updateEvent.mock.calls[0] ?? [];
    expect(scope).toBe('this');
    expect(sent.recurrenceRule).toBeUndefined();
  });
});
