import { cleanup, render, screen } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  locale: 'en',
  selectedDate: DateTime.fromISO('2026-08-05', { zone: 'Asia/Tokyo' }),
  showDayDetail: true,
  timezone: 'Asia/Tokyo',
  closeDayDetail: vi.fn(),
  openEventModal: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const rehearsal: CalendarEvent = {
  id: 'e1',
  calendarId: 'cal-1',
  title: 'Rehearsal',
  allDay: false,
  // Stored in UTC, shown on the account's Tokyo clock: 09:00 to 10:00 there.
  startAt: '2026-08-05T00:00:00Z',
  endAt: '2026-08-05T01:00:00Z',
  timezone: 'Asia/Tokyo',
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
  createdAt: '2026-08-01T00:00:00Z',
  updatedAt: '2026-08-01T00:00:00Z',
};

const calendarState = {
  events: [] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
  calendars: [{ id: 'cal-1', name: 'Family', color: '#47B2F7', role: 'owner' }],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { DayDetail } from './day-detail';

beforeEach(() => {
  calendarState.events = [];
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('DayDetail', () => {
  // The desktop month view has no other way to reach an overflowed day, so a
  // sheet that only exists below the breakpoint leaves those events unread.
  // The stylesheet is not loaded here, so the class is the evidence available.
  it('is not hidden above the breakpoint', () => {
    render(<DayDetail />);

    const heading = screen.getByRole('heading');
    const hiddenAncestors: string[] = [];
    for (let node: HTMLElement | null = heading; node; node = node.parentElement) {
      if (node.classList.contains('sm:hidden')) hiddenAncestors.push(node.className);
    }

    expect(hiddenAncestors).toEqual([]);
  });

  // Centring the sheet on desktop puts a full-screen wrapper over the
  // backdrop; it has to pass its clicks through or the day cannot be closed.
  it('lets clicks through the wrapper around the sheet', () => {
    render(<DayDetail />);

    const sheet = screen.getByRole('heading').closest('.bottom-sheet');
    expect(sheet?.className).toContain('pointer-events-auto');
    expect(sheet?.parentElement?.className).toContain('pointer-events-none');
  });

  // The times are read on the account's clock, not the machine's. This held
  // before the two inline formats here were routed through the shared helper
  // and has to go on holding after it.
  it('reads an event span in the configured zone', () => {
    calendarState.events = [rehearsal];

    render(<DayDetail />);

    expect(screen.getByText('09:00 - 10:00')).toBeInTheDocument();
  });

  /**
   * An event whose end equals its start is a marker at a moment, which the API
   * returns and this list has always been able to show. A moment has one time:
   * printed as a range it says "09:00 - 09:00", which reads as broken data
   * rather than as a thing with no duration.
   */
  it('gives a marker one time rather than a range from itself to itself', () => {
    calendarState.events = [
      { ...rehearsal, id: 'marker', title: 'Standup', endAt: rehearsal.startAt },
    ];

    render(<DayDetail />);

    expect(screen.getByText('Standup')).toBeInTheDocument();
    expect(screen.getByText('09:00')).toBeInTheDocument();
    expect(screen.queryByText('09:00 - 09:00')).toBeNull();
  });

  // The marker the server had to be fixed twice for: midnight belongs to the
  // day that opens there, and this list is one of the places it has to appear.
  it('lists a marker sitting on the day boundary', () => {
    calendarState.events = [
      {
        ...rehearsal,
        id: 'midnight',
        title: 'Day one',
        startAt: '2026-08-04T15:00:00Z',
        endAt: '2026-08-04T15:00:00Z',
      },
    ];

    render(<DayDetail />);

    expect(screen.getByText('Day one')).toBeInTheDocument();
    expect(screen.getByText('00:00')).toBeInTheDocument();
  });
});
