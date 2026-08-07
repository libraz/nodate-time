import { act, cleanup, render, screen } from '@testing-library/react';
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

// Five events on one day against three visible tracks: two are out of sight.
const calendarState = {
  events: [event('a', 1), event('b', 2), event('c', 3), event('d', 4), event('e', 5)],
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
});

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
