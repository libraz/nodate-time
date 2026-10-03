import { cleanup, render, screen, waitFor } from '@testing-library/react';
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

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return { ...actual, api: { get: vi.fn(), getListing: vi.fn() } };
});

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string; timezone: string }) => unknown) =>
    selector({ locale: 'ja', timezone: 'Asia/Tokyo' }),
}));

import { ApiError, api } from '@/lib/api';
import { EmbeddedCalendarView } from './$token';

const apiGet = vi.mocked(api.get);
const apiGetListing = vi.mocked(api.getListing);

/** A listing answers through the same stub as a plain GET, uncut. */
function listingFromGet() {
  apiGetListing.mockImplementation(async (path, skip, signal) => ({
    data: (await apiGet(path, skip, signal)) as never,
    truncated: false,
  }));
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
  apiGetListing.mockReset();
  listingFromGet();
});

afterEach(cleanup);

describe('embedded calendar', () => {
  it('draws a published calendar', async () => {
    apiGet.mockImplementation((path) =>
      Promise.resolve(String(path).includes('/events') ? [] : calendar()),
    );

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('share.readOnly')).toBeTruthy();
    expect(eventsAsked()).toBe(1);
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
  it('says when the month was cut short', async () => {
    apiGet.mockResolvedValue(calendar());
    apiGetListing.mockResolvedValue({ data: [] as never, truncated: true });

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('calendar.eventsTruncated')).toBeTruthy();
  });

  it('says nothing about a month that arrived whole', async () => {
    apiGet.mockImplementation((path) =>
      Promise.resolve(String(path).includes('/events') ? [] : calendar()),
    );

    render(<EmbeddedCalendarView />);

    expect(await screen.findByText('share.readOnly')).toBeTruthy();
    await waitFor(() => expect(apiGetListing).toHaveBeenCalled());
    expect(screen.queryByText('calendar.eventsTruncated')).toBeNull();
  });
});
