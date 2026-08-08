import { act, cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => String(e),
}));

// The month views stand in for themselves here. Nothing in this file asserts
// anything about a day cell, and jsdom applies no CSS, so both layouts mount at
// once, putting several thousand elements in front of every accessible-name
// query. The scroller holds far less than it used to and the real pair passes
// these tests, but it still takes this file from under a second to nearly five.
vi.mock('@/components/calendar-grid', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  CalendarGrid: () => <div data-testid="calendar-grid" />,
}));

vi.mock('@/components/month-scroll', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  MonthScroll: () => <div data-testid="month-scroll" />,
}));

const calendarState = {
  calendars: [
    { id: 'cal-1', name: 'Family', color: '#47B2F7', role: 'owner', publicShared: false },
  ],
  activeCalendarIds: ['cal-1'],
  events: [] as CalendarEvent[],
  memos: [],
  membersMap: {},
  memberErrors: {},
  loadError: null,
  fetchCalendars: vi.fn().mockResolvedValue(undefined),
  fetchEvents: vi.fn().mockResolvedValue(undefined),
  fetchMemos: vi.fn().mockResolvedValue(undefined),
  retryFailedLoads: vi.fn(),
  toggleCalendarFilter: vi.fn(),
  setActiveCalendarIds: vi.fn(),
  addCalendar: vi.fn(),
  deleteCalendar: vi.fn().mockResolvedValue(undefined),
  toggleMemo: vi.fn().mockResolvedValue(undefined),
  deleteMemo: vi.fn().mockResolvedValue(undefined),
  updateEvent: vi.fn().mockResolvedValue(undefined),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: Object.assign(
    (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
    { getState: () => calendarState },
  ),
}));

const authState = {
  user: { id: 'u1', name: 'Taro', email: 'taro@example.com' },
  logout: vi.fn(),
  saveAccountPreference: vi.fn().mockResolvedValue(undefined),
};

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: Object.assign((selector: (s: typeof authState) => unknown) => selector(authState), {
    getState: () => authState,
  }),
}));

import { App } from './app';
import { fetchWindow } from './lib/date-utils';
import { useUiStore } from './stores/ui-store';

function event(id: string, calendarId: string, title: string): CalendarEvent {
  return {
    id,
    calendarId,
    title,
    allDay: false,
    startAt: '2026-08-05T10:00:00+09:00',
    endAt: '2026-08-05T11:00:00+09:00',
    color: '#47B2F7',
    ownerId: 'u1',
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participants: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '2026-08-01T00:00:00+09:00',
    updatedAt: '2026-08-01T00:00:00+09:00',
  };
}

// jsdom has no scrollTo, and the panels position themselves on open.
Element.prototype.scrollTo = () => {};

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  useUiStore.setState({ rightPanel: null });
});

// The real UI store drives this: the point is that pressing the sidebar
// button reaches a panel, not that a handler fires.
useUiStore.setState({
  holidaysCountry: null,
  currentMonth: DateTime.fromISO('2026-08-01', { zone: 'Asia/Tokyo' }),
  selectedDate: DateTime.fromISO('2026-08-05', { zone: 'Asia/Tokyo' }),
  timezone: 'Asia/Tokyo',
});

describe('App right sidebar', () => {
  it('opens the memo panel from the sidebar and closes it again', async () => {
    const user = userEvent.setup();
    render(<App />);

    expect(screen.queryByRole('heading', { name: 'panel.memo' })).toBeNull();

    await user.click(screen.getByRole('button', { name: 'panel.memo' }));

    const heading = screen.getByRole('heading', { name: 'panel.memo' });
    const panel = heading.closest('.side-panel');
    expect(panel).not.toBeNull();
    // The panel carries the memo list itself, not just a title.
    expect(within(panel as HTMLElement).getByText('panel.noMemos')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'panel.memo' }));
    expect(screen.queryByRole('heading', { name: 'panel.memo' })).toBeNull();
  });
});

describe('App mobile search', () => {
  afterEach(() => {
    useUiStore.setState({ mobileTab: 'calendar' });
    calendarState.calendars = calendarState.calendars.slice(0, 1);
    calendarState.activeCalendarIds = ['cal-1'];
    calendarState.events = [];
  });

  it('leaves out events from a calendar the reader has switched off', async () => {
    // The store keeps every calendar's events. Offering one from a hidden
    // calendar leads to a day that does not draw it.
    calendarState.calendars = [
      ...calendarState.calendars,
      { id: 'cal-2', name: 'Work', color: '#F76B47', role: 'owner', publicShared: false },
    ];
    calendarState.activeCalendarIds = ['cal-1'];
    calendarState.events = [
      event('e-shown', 'cal-1', 'Rehearsal at home'),
      event('e-hidden', 'cal-2', 'Rehearsal at work'),
    ];
    useUiStore.setState({ mobileTab: 'search' });

    const user = userEvent.setup();
    render(<App />);
    await user.type(screen.getByPlaceholderText('search.placeholder'), 'rehearsal');

    expect(screen.getByText('Rehearsal at home')).toBeInTheDocument();
    expect(screen.queryByText('Rehearsal at work')).toBeNull();
  });
});

describe('App month fetching', () => {
  const month = (iso: string) => DateTime.fromISO(iso, { zone: 'Asia/Tokyo' });

  afterEach(() => {
    vi.useRealTimers();
    useUiStore.setState({ currentMonth: month('2026-08-01') });
  });

  it('fetches once for a burst of month changes, for the month it settles on', () => {
    vi.useFakeTimers();
    render(<App />);
    expect(calendarState.fetchEvents).toHaveBeenCalledTimes(1);

    // A flick through the mobile month scroll: every month it passes through
    // arrives as its own change.
    for (const iso of ['2026-09-01', '2026-10-01', '2026-11-01', '2026-12-01']) {
      act(() => {
        useUiStore.setState({ currentMonth: month(iso) });
      });
    }

    expect(calendarState.fetchEvents).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(300);
    });

    expect(calendarState.fetchEvents).toHaveBeenCalledTimes(2);
    const settled = fetchWindow('month', month('2026-12-01'));
    const [start, end] = calendarState.fetchEvents.mock.lastCall as [string, string, AbortSignal];
    expect([start, end]).toEqual([settled.start, settled.end]);
  });

  it('cancels a request the month has already moved past', () => {
    vi.useFakeTimers();
    render(<App />);

    const [, , inFlight] = calendarState.fetchEvents.mock.calls[0] as [string, string, AbortSignal];
    expect(inFlight.aborted).toBe(false);

    act(() => {
      useUiStore.setState({ currentMonth: month('2026-09-01') });
    });

    // Nothing waits for the response to a month nobody is on any more.
    expect(inFlight.aborted).toBe(true);
  });
});
