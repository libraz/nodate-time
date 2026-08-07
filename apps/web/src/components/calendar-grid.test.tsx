import { act, cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => String(e),
}));

const openDayDetail = vi.fn();
const openEventModal = vi.fn();

const uiState = {
  locale: 'en',
  currentMonth: DateTime.fromISO('2026-08-01', { zone: 'Asia/Tokyo' }),
  selectedDate: DateTime.fromISO('2026-08-01', { zone: 'Asia/Tokyo' }),
  calendarView: 'month' as const,
  // No holiday country: the grid then needs no holiday data to render.
  holidaysCountry: null,
  timezone: 'Asia/Tokyo',
  openEventModal,
  openDayDetail,
  setSelectedDate: vi.fn(),
  setCalendarView: vi.fn(),
  navigateMonth: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

function event(id: string, hour: number): CalendarEvent {
  return {
    id,
    calendarId: 'cal-1',
    title: `Event ${id}`,
    allDay: false,
    startAt: `2026-08-05T0${hour}:00:00+09:00`,
    endAt: `2026-08-05T0${hour}:30:00+09:00`,
    color: '#47B2F7',
    ownerId: 'u1',
    location: '',
    memo: '',
    url: '',
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    notificationOffset: null,
    participants: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '2026-08-01T00:00:00+09:00',
    updatedAt: '2026-08-01T00:00:00+09:00',
  };
}

/** A bar running from the start of one day through the end of another (exclusive end). */
function spanEvent(
  id: string,
  title: string,
  from: string,
  throughExclusive: string,
): CalendarEvent {
  return {
    ...event(id, 1),
    title,
    allDay: true,
    startAt: `${from}T00:00:00+09:00`,
    endAt: `${throughExclusive}T00:00:00+09:00`,
  };
}

// Five events on one day against three visible tracks: two are out of sight.
const defaultEvents = [event('a', 1), event('b', 2), event('c', 3), event('d', 4), event('e', 5)];

const calendarState = {
  events: defaultEvents as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
  calendars: [{ id: 'cal-1', name: 'Family', color: '#47B2F7', role: 'owner' }],
  updateEvent: vi.fn().mockResolvedValue(undefined),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

const authState = { user: { id: 'u1', name: 'Taro', email: 'taro@example.com' } };

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { CalendarGrid } from './calendar-grid';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  calendarState.events = defaultEvents;
});

/** The grid re-renders once the (empty) holiday load settles. */
async function renderGrid() {
  await act(async () => {
    render(<CalendarGrid />);
  });
}

describe('CalendarGrid overflow indicator', () => {
  it('opens the day detail on the day it counts', async () => {
    const user = userEvent.setup();
    // The grid re-renders once the (empty) holiday load settles.
    await act(async () => {
      render(<CalendarGrid />);
    });

    const more = screen.getByRole('button', { name: 'calendar.moreEvents' });
    expect(more).toHaveTextContent('+2');

    await user.click(more);

    expect(openDayDetail).toHaveBeenCalledTimes(1);
    const opened = openDayDetail.mock.calls[0]?.[0] as DateTime;
    expect(opened.toFormat('yyyy-MM-dd')).toBe('2026-08-05');
  });

  // The chip sits in a container that hands its clicks to the day button
  // behind it, so a handler alone is not enough -- it has to take pointer
  // events back. The stylesheet is not loaded here, so the class is the
  // evidence available.
  it('takes pointer events back from its container', async () => {
    // The grid re-renders once the (empty) holiday load settles.
    await act(async () => {
      render(<CalendarGrid />);
    });

    const more = screen.getByRole('button', { name: 'calendar.moreEvents' });
    expect(more.className).toContain('pointer-events-auto');

    const blocked = more.closest('.pointer-events-none');
    expect(blocked).not.toBeNull();
    expect(blocked?.querySelector('.pointer-events-auto')).not.toBeNull();
  });
});

// Tracks are held for a bar's whole span, so a bar can end up on a high track
// because of crowding on days it shares with nothing else. Those quiet days
// must still account for it.
describe('CalendarGrid multi-day bars past the visible tracks', () => {
  // Sunday 2026-08-02 through Saturday 2026-08-08 is one grid row.
  const shortBars = ['s1', 's2', 's3'].map((id) =>
    spanEvent(id, `Short ${id}`, '2026-08-02', '2026-08-06'),
  );

  it('keeps a week-long bar visible when shorter ones crowd its first days', async () => {
    calendarState.events = [
      ...shortBars,
      spanEvent('long', 'Long trip', '2026-08-02', '2026-08-09'),
    ];
    await renderGrid();

    const bar = screen.getByTitle('Long trip');
    expect(bar.style.top).toBe('0px');
  });

  it('offers a +N on a day whose only event sits past the visible tracks', async () => {
    // Three bars run Sunday to Thursday; a fourth starts on that Thursday, so it
    // can only take the fourth track -- and Friday holds nothing else.
    calendarState.events = [
      ...['a', 'b', 'c'].map((id) => spanEvent(id, `Crowd ${id}`, '2026-08-02', '2026-08-07')),
      spanEvent('late', 'Late trip', '2026-08-06', '2026-08-09'),
    ];
    await renderGrid();

    expect(screen.queryByTitle('Late trip')).toBeNull();

    const friday = document.querySelector<HTMLElement>('[data-day="2026-08-07"]');
    expect(friday).not.toBeNull();
    // biome-ignore lint/style/noNonNullAssertion: asserted non-null above
    const more = within(friday!).getByRole('button', { name: 'calendar.moreEvents' });
    expect(more).toHaveTextContent('+1');
  });

  it('never counts an overflow below zero', async () => {
    calendarState.events = [
      ...['a', 'b', 'c', 'd'].map((id) => spanEvent(id, `Crowd ${id}`, '2026-08-02', '2026-08-07')),
      spanEvent('late', 'Late trip', '2026-08-06', '2026-08-09'),
    ];
    await renderGrid();

    for (const more of screen.getAllByRole('button', { name: 'calendar.moreEvents' })) {
      expect(more.textContent).toMatch(/^\+[1-9]\d*$/);
    }
  });
});
