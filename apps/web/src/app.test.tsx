import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => {
  class ApiError extends Error {
    constructor(
      public status: number,
      public detail: string,
      public code = '',
    ) {
      super(detail);
    }
  }
  return {
    // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
    ApiError,
    errorMessage: (e: unknown) => String(e),
  };
});

// biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
vi.mock('@/lib/client', () => ({ client: { GET: vi.fn() } }));

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
  truncatedCalendarIds: [] as string[],
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

import { ApiError } from '@/lib/api';
import { client } from '@/lib/client';
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
    participantIds: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '2026-08-01T00:00:00+09:00',
    updatedAt: '2026-08-01T00:00:00+09:00',
  } as unknown as CalendarEvent;
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
    expect(within(panel as HTMLElement).getByText('empty.memosTitle')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'panel.memo' }));
    // Closing plays an exit first: the sheet is marked as leaving, then goes.
    expect(panel).toHaveClass('side-panel-exit');
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'panel.memo' })).toBeNull());
  });
});

describe('App mobile search', () => {
  const searchGet = vi.mocked(client.GET);
  const line = (text: string) => (_: string, el: Element | null) =>
    el?.tagName === 'P' && el.textContent === text;

  afterEach(() => {
    useUiStore.setState({ mobileTab: 'calendar' });
    searchGet.mockReset();
  });

  async function typeQuery(text: string) {
    useUiStore.setState({ mobileTab: 'search' });
    const user = userEvent.setup();
    render(<App />);
    await user.type(screen.getByPlaceholderText('search.placeholder'), text);
  }

  it('lists what the server found and says when more matched than were returned', async () => {
    searchGet.mockResolvedValue({
      data: { items: [event('e-1', 'cal-1', 'Rehearsal at home')], hasMore: true },
    } as never);

    await typeQuery('rehearsal');

    expect(await screen.findByText(line('Rehearsal at home'))).toBeInTheDocument();
    expect(searchGet).toHaveBeenCalledWith('/search', {
      params: { query: { q: 'rehearsal' } },
      signal: expect.any(AbortSignal),
    });
    expect(screen.getByText('search.hasMore')).toBeInTheDocument();
  });

  it('says nothing matched when the server returns no events', async () => {
    searchGet.mockResolvedValue({ data: { items: [], hasMore: false } } as never);

    await typeQuery('nothing');

    expect(await screen.findByText('search.noResults')).toBeInTheDocument();
    expect(screen.queryByText('search.hasMore')).toBeNull();
  });

  it('shows a refused query as an alert', async () => {
    searchGet.mockRejectedValue(new ApiError(422, 'too broad', 'SEARCH.TOO_BROAD'));

    await typeQuery('a');

    expect(await screen.findByRole('alert')).toHaveTextContent('search.tooBroad');
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

  // Memos belong to the calendars, not to the month on screen.
  it('does not reload memos when the month changes', () => {
    vi.useFakeTimers();
    render(<App />);
    expect(calendarState.fetchMemos).toHaveBeenCalledTimes(1);

    for (const iso of ['2026-09-01', '2026-10-01', '2026-11-01', '2026-12-01', '2027-01-01']) {
      act(() => {
        useUiStore.setState({ currentMonth: month(iso) });
      });
    }
    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(calendarState.fetchMemos).toHaveBeenCalledTimes(1);
  });

  it('hands the memo load a signal that is cancelled when it is superseded', () => {
    render(<App />);
    const [signal] = calendarState.fetchMemos.mock.calls[0] as [AbortSignal];
    expect(signal.aborted).toBe(false);

    cleanup();

    expect(signal.aborted).toBe(true);
  });
});

// CSS hides one layout, but a hidden calendar still re-renders on every change
// and measures itself as zero-sized: the phone scroller consumed its one
// scroll-to-today while undisplayed and stayed months in the past.
describe('App layout mounting', () => {
  function stubViewport(desktop: boolean) {
    let wide = desktop;
    const listeners = new Set<(e: MediaQueryListEvent) => void>();
    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        get matches() {
          return query === '(min-width: 640px)' ? wide : false;
        },
        media: query,
        addEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) => listeners.add(cb),
        removeEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) =>
          listeners.delete(cb),
      })),
    );
    return (value: boolean) => {
      wide = value;
      for (const cb of listeners) cb({ matches: value } as MediaQueryListEvent);
    };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('mounts only the desktop month grid on a wide viewport', () => {
    stubViewport(true);
    render(<App />);

    expect(screen.getByTestId('calendar-grid')).toBeInTheDocument();
    expect(screen.queryByTestId('month-scroll')).toBeNull();
  });

  it('mounts the phone scroller, freshly, once the viewport narrows', () => {
    const resize = stubViewport(true);
    render(<App />);

    act(() => {
      resize(false);
    });

    expect(screen.getByTestId('month-scroll')).toBeInTheDocument();
    expect(screen.queryByTestId('calendar-grid')).toBeNull();
  });
});

describe('App capped listings', () => {
  afterEach(() => {
    calendarState.truncatedCalendarIds = [];
  });

  // A calendar whose listing stopped at the cap looked complete, so the days
  // past the cut read as free time.
  it('says when a shown calendar came back cut short', () => {
    calendarState.truncatedCalendarIds = ['cal-1'];
    render(<App />);

    expect(screen.getByText('calendar.eventsTruncated')).toBeInTheDocument();
  });

  it('says nothing for a calendar that is switched off', () => {
    calendarState.truncatedCalendarIds = ['cal-hidden'];
    render(<App />);

    expect(screen.queryByText('calendar.eventsTruncated')).toBeNull();
  });
});
