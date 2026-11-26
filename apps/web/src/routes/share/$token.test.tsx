import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const navigate = vi.fn();
const searchState: { join?: string | undefined } = {};

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: Record<string, unknown>) => ({
    ...options,
    useParams: () => ({ token: 'tok-1' }),
    useSearch: () => searchState,
  }),
  useNavigate: () => navigate,
}));

// A fresh closure per call, as the real `useT` returns: it builds its
// translator from the current locale every render. A mock that handed back one
// stable function would make a component that depends on the translator look
// settled here while it re-ran on every render in a browser.
vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// Only the transport is replaced. The error localisation is the thing under
// test on the join path, so it runs for real.
vi.mock('@/lib/session', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/session')>();
  return { ...actual, hasToken: vi.fn(() => true) };
});

// The typed client is replaced by an adapter over two stubs that see the
// resolved path (query included), so the assertions read as the requests made.
const raw = vi.hoisted(() => {
  type Init = {
    params?: { path?: Record<string, string>; query?: Record<string, string> };
    signal?: AbortSignal;
  };
  const resolve = (path: string, init?: Init) => {
    const filled = path.replace(/\{(\w+)\}/g, (_, key: string) => init?.params?.path?.[key] ?? '');
    const query = new URLSearchParams(init?.params?.query).toString();
    return query ? `${filled}?${query}` : filled;
  };
  return { resolve, get: vi.fn(), post: vi.fn() };
});

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  client: {
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    GET: async (path: string, init?: Parameters<typeof raw.resolve>[1]) => ({
      data: await raw.get(raw.resolve(path, init), init?.signal),
    }),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    POST: async (path: string, init?: Parameters<typeof raw.resolve>[1]) => ({
      data: await raw.post(raw.resolve(path, init)),
    }),
  },
}));

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string; timezone: string }) => unknown) =>
    selector({ locale: 'ja', timezone: 'Asia/Tokyo' }),
}));

import { formatSheetDay } from '@/components/public-day-sheet';
import { ApiError } from '@/lib/client';
import { hasToken } from '@/lib/session';
import { toast } from '@/lib/toast';
import { SharedCalendarView } from './$token';

const apiGet = raw.get;
const apiPost = raw.post;

/** What `GET /share/{token}/events` answers. */
function page(items: unknown[] = [], hasMore = false) {
  return { items, hasMore };
}

/** What `GET /share/{token}` answers, as `PublicCalendarOutput` serialises it. */
function shareResponse(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    calendarId: '0198f0c2-0000-7000-8000-000000000001',
    name: 'Family',
    color: '#42A5F5',
    joinable: true,
    spent: false,
    ...overrides,
  };
}

function askedFor(fragment: string): string[] {
  return apiGet.mock.calls.map((c) => String(c[0])).filter((u) => u.includes(fragment));
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  navigate.mockReset();
  vi.mocked(hasToken).mockReturnValue(true);
  searchState.join = undefined;
  vi.mocked(toast.success).mockReset();
});

afterEach(cleanup);

/**
 * A join link is an offer of access, not access. The server refuses it the
 * events endpoint deliberately -- `PublicEvents` looks the token up as a
 * public invite and answers `INVITE.NOT_FOUND` otherwise -- so a page that
 * asks anyway draws six weeks of empty cells, and somebody following an
 * invitation is shown a calendar with nothing in it instead of a question.
 */
describe('share landing for a join link', () => {
  it('draws no calendar and asks for no events', async () => {
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));

    render(<SharedCalendarView />);

    expect(await screen.findByText('Family')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'share.joinCalendar' })).toBeTruthy();
    // The weekday header belongs to the grid and to nothing else on this page.
    expect(screen.queryByText('日')).toBeNull();
    await waitFor(() => expect(apiGet).toHaveBeenCalled());
    expect(askedFor('/events')).toEqual([]);
  });

  /**
   * The load is keyed to the token. Holding the translator in it instead made
   * it a new function on every render, and since every response is a new
   * object, each answer caused the next request -- a public page asking the
   * server for the same thing for as long as it was left open.
   */
  it('asks for the calendar once, not once per render', async () => {
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));

    render(<SharedCalendarView />);

    await screen.findByText('Family');
    await act(async () => {});
    expect(apiGet.mock.calls.filter((c) => !String(c[0]).includes('/events'))).toHaveLength(1);
  });

  it('says nothing about reading a calendar it does not publish', async () => {
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));

    render(<SharedCalendarView />);

    await screen.findByText('Family');
    expect(screen.queryByText('share.readOnly')).toBeNull();
  });

  /** A used-up link is still an offer that was made, and still not a calendar. */
  it('draws no calendar for a link that has been used up', async () => {
    apiGet.mockResolvedValue(shareResponse({ joinable: false, spent: true }));

    render(<SharedCalendarView />);

    expect(await screen.findByText('share.linkUsedUp')).toBeTruthy();
    expect(screen.queryByText('日')).toBeNull();
    expect(askedFor('/events')).toEqual([]);
  });
});

/** A public link publishes the calendar, which is the case the grid is for. */
describe('share landing for a public link', () => {
  it('asks for the events and draws them', async () => {
    apiGet.mockImplementation((path) =>
      String(path).includes('/events')
        ? Promise.resolve(page())
        : Promise.resolve(shareResponse({ joinable: false, spent: false })),
    );

    render(<SharedCalendarView />);

    await waitFor(() => expect(askedFor('/events')).toHaveLength(1));
    expect(screen.getByText('日')).toBeTruthy();
    expect(screen.getByText('share.readOnly')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'share.joinCalendar' })).toBeNull();
  });

  it('draws every event of a crowded day and no "+N"', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-10T03:00:00Z'));
    try {
      const titles = ['One', 'Two', 'Three', 'Four', 'Five'];
      apiGet.mockImplementation((path) =>
        String(path).includes('/events')
          ? Promise.resolve(
              page(
                titles.map((title, i) => ({
                  id: `e${i}`,
                  title,
                  detailsHidden: false,
                  allDay: false,
                  startAt: `2026-11-10T0${i}:00:00Z`,
                  endAt: `2026-11-10T0${i}:30:00Z`,
                  timezone: 'Asia/Tokyo',
                  color: '#000',
                })),
              ),
            )
          : Promise.resolve(shareResponse({ joinable: false, spent: false })),
      );

      render(<SharedCalendarView />);
      for (const title of titles) {
        expect(await screen.findByText(title)).toBeTruthy();
      }
      expect(screen.queryByText(/^\+\d/)).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('share landing near midnight in the grid zone', () => {
  // The device may be on any date; the page opens on, and marks, the day it is in the grid zone.
  it('opens on the grid zone month and marks its today', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-10-31T16:30:00Z')); // 2026-11-01 01:30 in Tokyo
    try {
      apiGet.mockImplementation((path) =>
        String(path).includes('/events')
          ? Promise.resolve(page())
          : Promise.resolve(shareResponse({ joinable: false, spent: false })),
      );

      render(<SharedCalendarView />);

      await waitFor(() => expect(askedFor('/events')).toHaveLength(1));
      expect(askedFor('/events')[0]).toContain('start=2026-11-01');
      const marked = screen
        .getAllByText('1')
        .filter((el) => (el as HTMLElement).style.fontWeight === '700');
      expect(marked).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('share landing when the month does not arrive', () => {
  const published = () => shareResponse({ joinable: false, spent: false });
  const publicEvent = (id: string, title: string, startAt: string) => ({
    id,
    title,
    detailsHidden: false,
    allDay: false,
    startAt,
    endAt: startAt,
    timezone: 'Asia/Tokyo',
    color: '#000',
  });

  // A failed events request used to draw an empty month, which says the
  // calendar has nothing on when the page simply did not hear back.
  it('says so, and asks again on retry', async () => {
    let eventsCalls = 0;
    apiGet.mockImplementation((path) => {
      if (!String(path).includes('/events')) return Promise.resolve(published());
      eventsCalls += 1;
      return eventsCalls === 1
        ? Promise.reject(new ApiError(503, 'down'))
        : Promise.resolve(page());
    });

    render(<SharedCalendarView />);

    expect(await screen.findByText('share.temporarilyUnavailable')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'share.retry' }));
    await waitFor(() => expect(askedFor('/events')).toHaveLength(2));
    expect(await screen.findByText('share.readOnly')).toBeTruthy();
  });

  // Paging quickly left the slower answer last, and the last answer won even
  // when it was for the month the reader had already left.
  it('does not let an earlier month’s late answer replace the month on screen', async () => {
    let answerFirst: (events: unknown) => void = () => {};
    apiGet.mockImplementation((path) => {
      if (!String(path).includes('/events')) return Promise.resolve(published());
      if (askedFor('/events').length === 1) {
        return new Promise((resolve) => {
          answerFirst = resolve;
        });
      }
      return Promise.resolve(
        page([
          publicEvent('next', 'Next month plan', DateTime.now().plus({ months: 1 }).toISO() ?? ''),
        ]),
      );
    });

    render(<SharedCalendarView />);
    await waitFor(() => expect(askedFor('/events')).toHaveLength(1));
    fireEvent.click(screen.getByRole('button', { name: 'calendar.nextMonth' }));
    expect(await screen.findByText('Next month plan')).toBeTruthy();

    await act(async () => {
      answerFirst([publicEvent('stale', 'Stale plan', DateTime.now().toISO() ?? '')]);
    });

    expect(screen.queryByText('Stale plan')).toBeNull();
    expect(screen.getByText('Next month plan')).toBeTruthy();
  });
});

describe('share landing for a month with more events than the page', () => {
  it('says the latest events are missing when the page reports hasMore', async () => {
    apiGet.mockImplementation((path) =>
      Promise.resolve(
        String(path).includes('/events')
          ? page([], true)
          : shareResponse({ joinable: false, spent: false }),
      ),
    );

    render(<SharedCalendarView />);

    expect(await screen.findByText('calendar.eventsTruncated')).toBeTruthy();
  });
});

/**
 * The refusals this endpoint can give, taken from the server's own error
 * table: 404 INVITE.NOT_FOUND, 410 INVITE.EXPIRED, 403 INVITE.PUBLIC_VIEW_ONLY.
 * There is no 409 -- an existing member is readmitted silently rather than
 * refused -- so the branch that handled one could never run, while the 403 it
 * had no branch for arrived as the server's own English.
 */
describe('joining a calendar that refuses', () => {
  async function join() {
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));
    render(<SharedCalendarView />);
    fireEvent.click(await screen.findByRole('button', { name: 'share.joinCalendar' }));
  }

  it('says in the reader language that a public link cannot be joined', async () => {
    apiPost.mockRejectedValue(
      new ApiError(
        403,
        'This is a public view-only link and cannot be joined',
        'INVITE.PUBLIC_VIEW_ONLY',
      ),
    );

    await join();

    expect(await screen.findByText('apiError.INVITE.PUBLIC_VIEW_ONLY')).toBeTruthy();
    // The server's sentence is written in one language; it must not be what a
    // reader in another one is shown.
    expect(screen.queryByText('This is a public view-only link and cannot be joined')).toBeNull();
  });

  it('says in the reader language that a link has run out', async () => {
    apiPost.mockRejectedValue(
      new ApiError(410, 'Invite has expired or reached max uses', 'INVITE.EXPIRED'),
    );

    await join();

    expect(await screen.findByText('apiError.INVITE.EXPIRED')).toBeTruthy();
  });

  it('falls back to its own words when the failure carries none', async () => {
    apiPost.mockRejectedValue(new Error('network down'));

    await join();

    expect(await screen.findByText('invite.joinFailed')).toBeTruthy();
  });
});

/**
 * An event whose details are withheld is published as a time with no title.
 * The page names it for what it is, rather than drawing an empty chip.
 */
describe('share landing for withheld events', () => {
  const published = () => shareResponse({ joinable: false, spent: false });
  const eventsOf = (items: unknown[], hasMore = false) =>
    apiGet.mockImplementation((path) =>
      Promise.resolve(String(path).includes('/events') ? page(items, hasMore) : published()),
    );
  const timed = (id: string, title: string | null, hidden: boolean, startAt: string) => ({
    id,
    title,
    detailsHidden: hidden,
    allDay: false,
    startAt,
    endAt: startAt,
    timezone: 'Asia/Tokyo',
    color: '#000',
  });

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-10T03:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('shows share.busy instead of a title', async () => {
    eventsOf([
      timed('a', null, true, '2026-11-10T01:00:00Z'),
      timed('b', 'Team lunch', false, '2026-11-11T01:00:00Z'),
    ]);

    render(<SharedCalendarView />);

    expect(await screen.findByText('share.busy')).toBeTruthy();
    expect(screen.getByText('Team lunch')).toBeTruthy();
  });

  it('opens a sheet for the tapped day listing its events, busy ones as busy', async () => {
    eventsOf([
      timed('a', null, true, '2026-11-10T01:00:00Z'),
      timed('b', 'Standup', false, '2026-11-10T02:00:00Z'),
      timed('c', 'Another day', false, '2026-11-12T02:00:00Z'),
    ]);

    render(<SharedCalendarView />);
    await screen.findByText('Standup');
    expect(screen.queryByRole('region', { name: 'share.daySheetTitle' })).toBeNull();

    fireEvent.click(
      screen.getByRole('button', { name: formatSheetDay(DateTime.fromISO('2026-11-10'), 'ja') }),
    );

    const sheet = screen.getByRole('region', { name: 'share.daySheetTitle' });
    expect(within(sheet).getByText('share.busy')).toBeTruthy();
    expect(within(sheet).getByText('Standup')).toBeTruthy();
    expect(within(sheet).queryByText('Another day')).toBeNull();

    fireEvent.click(within(sheet).getByRole('button', { name: 'common.close' }));
    expect(screen.queryByRole('region', { name: 'share.daySheetTitle' })).toBeNull();
  });

  it('places an all-day event on its dates, whatever the instants say', async () => {
    // startAt/endAt are the UTC instants of Tokyo midnight, a day off in New York.
    eventsOf([
      {
        id: 'ad',
        title: 'Holiday',
        detailsHidden: false,
        allDay: true,
        startAt: '2026-11-04T15:00:00Z',
        endAt: '2026-11-05T15:00:00Z',
        startDate: '2026-11-05',
        endDate: '2026-11-06',
        timezone: 'America/New_York',
        color: '#000',
      },
    ]);

    render(<SharedCalendarView />);
    await screen.findByText('Holiday');

    const open = (iso: string) =>
      fireEvent.click(
        screen.getByRole('button', { name: formatSheetDay(DateTime.fromISO(iso), 'ja') }),
      );
    open('2026-11-05');
    expect(
      within(screen.getByRole('region', { name: 'share.daySheetTitle' })).getByText('Holiday'),
    ).toBeTruthy();
    open('2026-11-06');
    expect(
      within(screen.getByRole('region', { name: 'share.daySheetTitle' })).queryByText('Holiday'),
    ).toBeNull();
  });
});

/** Returning from sign-in with join=1 accepts the invite without another tap. */
describe('share landing with join=1', () => {
  it('accepts once, says so, and moves on to the app', async () => {
    searchState.join = '1';
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));
    apiPost.mockResolvedValue({ calendarId: 'cal', role: 'editor' });

    render(<SharedCalendarView />);

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/' }));
    expect(apiPost).toHaveBeenCalledTimes(1);
    expect(apiPost).toHaveBeenCalledWith('/invites/tok-1/accept');
    expect(toast.success).toHaveBeenCalledWith('invite.joined');
  });

  it('does not accept on a plain visit', async () => {
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));

    render(<SharedCalendarView />);

    await screen.findByRole('button', { name: 'share.joinCalendar' });
    await act(async () => {});
    expect(apiPost).not.toHaveBeenCalled();
  });

  it('sends a signed-out visitor to register, and back here with join=1', async () => {
    vi.mocked(hasToken).mockReturnValue(false);
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));

    render(<SharedCalendarView />);
    fireEvent.click(await screen.findByRole('button', { name: 'share.joinCalendar' }));

    expect(navigate).toHaveBeenCalledWith({
      to: '/login',
      search: { mode: 'register', redirect: '/share/tok-1?join=1' },
    });
    expect(apiPost).not.toHaveBeenCalled();
  });

  it('shows the failure and stays when the accept is refused', async () => {
    searchState.join = '1';
    apiGet.mockResolvedValue(shareResponse({ joinable: true }));
    apiPost.mockRejectedValue(new ApiError(410, 'expired', 'INVITE.EXPIRED'));

    render(<SharedCalendarView />);

    expect(await screen.findByText('apiError.INVITE.EXPIRED')).toBeTruthy();
    expect(navigate).not.toHaveBeenCalled();
  });
});
