import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const navigate = vi.fn();

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: Record<string, unknown>) => ({
    ...options,
    useParams: () => ({ token: 'tok-1' }),
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
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: { get: vi.fn(), post: vi.fn() },
    hasToken: () => true,
  };
});

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string; timezone: string }) => unknown) =>
    selector({ locale: 'ja', timezone: 'Asia/Tokyo' }),
}));

import { ApiError, api } from '@/lib/api';
import { SharedCalendarView } from './$token';

const apiGet = vi.mocked(api.get);
const apiPost = vi.mocked(api.post);

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
        ? Promise.resolve([])
        : Promise.resolve(shareResponse({ joinable: false, spent: false })),
    );

    render(<SharedCalendarView />);

    await waitFor(() => expect(askedFor('/events')).toHaveLength(1));
    expect(screen.getByText('日')).toBeTruthy();
    expect(screen.getByText('share.readOnly')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'share.joinCalendar' })).toBeNull();
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

    expect(await screen.findByText('share.joinFailed')).toBeTruthy();
  });
});
