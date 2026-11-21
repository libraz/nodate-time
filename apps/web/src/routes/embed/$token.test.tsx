import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: Record<string, unknown>) => ({
    ...options,
    useParams: () => ({ token: 'tok-1' }),
  }),
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

// The typed client is replaced by an adapter over one stub that sees the
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
  return { resolve, get: vi.fn() };
});

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  client: {
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    GET: async (path: string, init?: Parameters<typeof raw.resolve>[1]) => ({
      data: await raw.get(raw.resolve(path, init), init?.signal),
    }),
  },
}));

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string; timezone: string }) => unknown) =>
    selector({ locale: 'ja', timezone: 'Asia/Tokyo' }),
}));

import { formatSheetDay } from '@/components/public-day-sheet';
import { ApiError } from '@/lib/client';
import { EmbeddedCalendarView } from './$token';

const apiGet = raw.get;

/** What `GET /share/{token}/events` answers. */
function page(items: unknown[] = [], hasMore = false) {
  return { items, hasMore };
}

function calendar(overrides: Record<string, unknown> = {}) {
  return {
    calendarId: 'cal-1',
    name: 'Family',
    color: '#42A5F5',
    joinable: false,
    spent: false,
    ...overrides,
  };
}

function eventsAsked(): number {
  return apiGet.mock.calls.filter((c) => String(c[0]).includes('/events')).length;
}

beforeEach(() => {
  apiGet.mockReset();
});

afterEach(cleanup);

describe('embedded calendar', () => {
  it('draws a published calendar', async () => {
    apiGet.mockImplementation((path) =>
      Promise.resolve(String(path).includes('/events') ? page() : calendar()),
    );

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('share.readOnly')).toBeTruthy();
    // The events load starts in an effect after the header has rendered.
    await waitFor(() => expect(eventsAsked()).toBe(1));
  });

  // A join link answers the calendar lookup but publishes nothing; the embed
  // drew its name over an empty grid as if the calendar had no plans.
  it('treats a join link as gone rather than drawing an empty calendar', async () => {
    apiGet.mockResolvedValue(calendar({ joinable: true }));

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('share.linkExpired')).toBeTruthy();
    expect(screen.queryByText('share.readOnly')).toBeNull();
    expect(eventsAsked()).toBe(0);
  });

  it('treats a used-up join link as gone', async () => {
    apiGet.mockResolvedValue(calendar({ spent: true }));

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('share.linkExpired')).toBeTruthy();
  });

  // A failed events request was swallowed into an empty month.
  it('says the server is busy when the events do not arrive', async () => {
    apiGet.mockImplementation((path) =>
      String(path).includes('/events')
        ? Promise.reject(new ApiError(503, 'down'))
        : Promise.resolve(calendar()),
    );

    render(<EmbeddedCalendarView />);

    await waitFor(() => expect(screen.getByText('share.tryAgainShortly')).toBeTruthy());
    expect(screen.queryByText('share.readOnly')).toBeNull();
  });

  it('says the link is gone when the events answer that it is', async () => {
    apiGet.mockImplementation((path) =>
      String(path).includes('/events')
        ? Promise.reject(new ApiError(404, 'gone'))
        : Promise.resolve(calendar()),
    );

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('share.linkExpired')).toBeTruthy();
  });

  // A month cut at the server's cap looked complete; its last days read as free.
  it('says when the page reports more events than it carried', async () => {
    apiGet.mockImplementation((path) =>
      Promise.resolve(String(path).includes('/events') ? page([], true) : calendar()),
    );

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('calendar.eventsTruncated')).toBeTruthy();
  });

  it('says nothing about a month that arrived whole', async () => {
    apiGet.mockImplementation((path) =>
      Promise.resolve(String(path).includes('/events') ? page() : calendar()),
    );

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('share.readOnly')).toBeTruthy();
    await waitFor(() => expect(eventsAsked()).toBe(1));
    expect(screen.queryByText('calendar.eventsTruncated')).toBeNull();
  });

  // A calendar lookup refused with a 429 used to leave a dead-end message.
  it('offers a retry that refetches the calendar after a busy lookup', async () => {
    let lookups = 0;
    apiGet.mockImplementation((path) => {
      if (String(path).includes('/events')) return Promise.resolve(page());
      lookups++;
      if (lookups === 1) return Promise.reject(new ApiError(429, 'busy'));
      return Promise.resolve(calendar());
    });

    render(<EmbeddedCalendarView />);

    fireEvent.click(await screen.findByRole('button', { name: 'share.retry' }));

    expect(await screen.findByText('share.readOnly')).toBeTruthy();
    expect(lookups).toBe(2);
  });

  it('offers a retry that refetches the events after a busy listing', async () => {
    let listings = 0;
    apiGet.mockImplementation((path) => {
      if (!String(path).includes('/events')) return Promise.resolve(calendar());
      listings++;
      return listings === 1 ? Promise.reject(new ApiError(503, 'down')) : Promise.resolve(page());
    });

    render(<EmbeddedCalendarView />);

    fireEvent.click(await screen.findByRole('button', { name: 'share.retry' }));

    expect(await screen.findByText('share.readOnly')).toBeTruthy();
    expect(listings).toBe(2);
  });

  // The device may be on any date; the page shows the month it is in the grid zone.
  it('opens on the current month in the grid zone', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-10-31T16:30:00Z')); // 2026-11-01 01:30 in Tokyo
    try {
      apiGet.mockImplementation((path) =>
        Promise.resolve(String(path).includes('/events') ? page() : calendar()),
      );

      render(<EmbeddedCalendarView />);

      await waitFor(() => expect(eventsAsked()).toBe(1));
      const asked = String(apiGet.mock.calls.find((c) => String(c[0]).includes('/events'))?.[0]);
      expect(asked).toContain('start=2026-11-01');
    } finally {
      vi.useRealTimers();
    }
  });

  // A withheld event is published without a title; the sheet lists it as busy.
  it('shows withheld events as busy, in the grid and in the day sheet', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-10T03:00:00Z'));
    try {
      apiGet.mockImplementation((path) =>
        Promise.resolve(
          String(path).includes('/events')
            ? page([
                {
                  id: 'e1',
                  title: null,
                  detailsHidden: true,
                  allDay: false,
                  startAt: '2026-11-10T01:00:00Z',
                  endAt: '2026-11-10T02:00:00Z',
                  timezone: 'Asia/Tokyo',
                  color: '#000',
                },
              ])
            : calendar(),
        ),
      );

      render(<EmbeddedCalendarView />);
      expect(await screen.findByText('share.busy')).toBeTruthy();

      fireEvent.click(
        screen.getByRole('button', { name: formatSheetDay(DateTime.fromISO('2026-11-10'), 'ja') }),
      );
      const sheet = screen.getByRole('region', { name: 'share.daySheetTitle' });
      expect(within(sheet).getByText('share.busy')).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });
});
