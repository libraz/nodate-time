import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  locale: 'ja',
  timezone: 'Asia/Tokyo',
  holidaysCountry: 'JP',
  openEventModal: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const newYear: CalendarEvent = {
  id: 'e1',
  calendarId: 'cal-1',
  title: 'Shrine visit',
  allDay: false,
  startAt: '2026-01-01T01:00:00+09:00',
  endAt: '2026-01-01T03:00:00+09:00',
  color: '#47B2F7',
  ownerId: 'u1',
  location: '',
  memo: '',
  url: '',
  showAs: 'busy',
  flexibility: 'fixed',
  visibility: 'default',
  notificationOffset: null,
  participantIds: [],
  recurrenceRule: null,
  isRecurrence: false,
  recurrenceDate: null,
  createdAt: '',
  updatedAt: '',
};

const calendarState = {
  events: [newYear] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
  calendars: [{ id: 'cal-1' }],
  eventsLoading: false,
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { ListView } from './list-view';

afterEach(cleanup);

describe('ListView holidays', () => {
  // The holiday data is an optional chunk that lands after the first paint, and
  // nothing on the row reports its arrival: the view has to redraw itself once
  // the load settles or the day is listed as an ordinary Thursday forever.
  it('names the holiday once its data has loaded', async () => {
    render(<ListView />);

    expect(screen.getByText('Shrine visit')).toBeInTheDocument();

    await waitFor(() => expect(screen.getByText('元日')).toBeInTheDocument());
  });
});

describe('ListView availability states', () => {
  it('marks the row and states showAs and flexibility as secondary text', () => {
    calendarState.events = [
      { ...newYear, showAs: 'free', flexibility: 'negotiable' } as CalendarEvent,
    ];

    render(<ListView />);

    const row = screen.getByRole('button', { name: /Shrine visit/ });
    expect(row).toHaveClass('event-block', 'evt-free');
    expect(
      within(row).getByText('event.showAsFree · event.flexibilityShort.negotiable'),
    ).toBeInTheDocument();
    calendarState.events = [newYear];
  });

  it('adds nothing for a busy, fixed event', () => {
    render(<ListView />);

    const row = screen.getByRole('button', { name: /Shrine visit/ });
    expect(row.className).not.toContain('evt-');
    expect(within(row).queryByText(/event\./)).toBeNull();
  });
});
