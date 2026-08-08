import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Calendar, Member } from '@/types/calendar';
import type { InviteData } from '@/types/invite';

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => () => ({ useSearch: () => ({}) }),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ children }: { children?: unknown }) => children,
  useNavigate: () => vi.fn(),
}));

vi.mock('@/i18n', () => ({
  // Parameters are folded onto the key so a test can assert on the number a
  // message was given, not only on which message was chosen.
  useT: () => (key: string, params?: Record<string, string>) =>
    params ? `${key}:${Object.values(params).join(',')}` : key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => {
  class ApiError extends Error {
    constructor(
      public status: number,
      public detail: string,
    ) {
      super(detail);
    }
  }
  return {
    // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
    ApiError,
    api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn(), getBlob: vi.fn() },
    errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
    isAbortError: (e: unknown) => e instanceof DOMException && e.name === 'AbortError',
  };
});

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

function calendar(role: string): Calendar {
  return {
    id: 'cal-1',
    name: 'A',
    color: '#000',
    coverUrl: '',
    createdAt: '',
    publicShared: false,
    role,
    memberColor: '#000',
  };
}

const calendarState = {
  calendars: [calendar('owner')],
  membersMap: {} as Record<string, Member[]>,
  fetchMembers: vi.fn(),
  leaveCalendar: vi.fn(),
  fetchEvents: vi.fn(),
  visibleRange: vi.fn(() => ({ start: '', end: '' })),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

const authState = {
  user: { id: 'u1', name: 'Me', email: 'me@example.com', locale: 'ja', timezone: 'Asia/Tokyo' },
  isAuthenticated: true,
};

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

const uiState = {
  locale: 'ja',
  theme: 'light',
  colorMode: 'light',
  weekStart: 0,
  timezone: 'Asia/Tokyo',
  holidaysCountry: 'JP' as string | null,
  setTheme: vi.fn(),
  setColorMode: vi.fn(),
  setHolidaysCountry: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { api } from '@/lib/api';
import { fetchWindow } from '@/lib/date-utils';
import { toast } from '@/lib/toast';
import {
  AllowedEmailsSection,
  AppearanceSection,
  CalendarsSection,
  ExportSection,
} from './settings';

const apiGet = vi.mocked(api.get);
const apiPost = vi.mocked(api.post);
const apiGetBlob = vi.mocked(api.getBlob);
const toastError = vi.mocked(toast.error);
const toastSuccess = vi.mocked(toast.success);
const toastInfo = vi.mocked(toast.info);

function member(role: string, email = 'me@example.com', id = 'm1', name = 'Me'): Member {
  return { id, name, email, role, color: '#000' } as Member;
}

/** An import answer with every outcome at rest, so a test names only its own. */
function importResult(outcome: Record<string, number | boolean>) {
  return {
    imported: 0,
    skipped: 0,
    failed: 0,
    rejected: 0,
    duplicates: 0,
    truncated: 0,
    unknownTimezones: 0,
    unreadable: false,
    ...outcome,
  };
}

function invite(id: string, token: string): InviteData {
  return {
    id,
    token,
    role: 'editor',
    maxUses: 1,
    useCount: 0,
    isPublic: false,
    expiresAt: null,
    createdAt: '2026-08-01T00:00:00Z',
  };
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  toastError.mockReset();
  toastSuccess.mockReset();
  toastInfo.mockReset();
  apiGet.mockResolvedValue([]);
});

afterEach(cleanup);

/**
 * Listing a calendar's invites needs the right to manage it. The screen used
 * to ask on every calendar selection regardless, so an editor or viewer met a
 * permission error for opening a settings tab -- and again for every calendar
 * they picked.
 *
 * The role is read from the calendar itself, which is why the member list is
 * deliberately left empty here: the gate must hold before it arrives, and
 * must not depend on recognising the signed-in account in it.
 */
describe('CalendarsSection invites', () => {
  it('does not ask for invites a viewer is not allowed to read', async () => {
    calendarState.calendars = [calendar('viewer')];
    calendarState.membersMap = {};

    render(<CalendarsSection />);

    await waitFor(() => expect(calendarState.fetchMembers).toHaveBeenCalled());
    const asked = apiGet.mock.calls.map((c) => String(c[0]));
    expect(asked.filter((u) => u.includes('/invites'))).toEqual([]);
  });

  it('asks for invites when the caller may manage the calendar', async () => {
    calendarState.calendars = [calendar('owner')];
    calendarState.membersMap = {};

    render(<CalendarsSection />);

    await waitFor(() =>
      expect(apiGet).toHaveBeenCalledWith(expect.stringContaining('/calendars/cal-1/invites')),
    );
  });

  /**
   * The member list carries an address only for rows the caller is allowed to
   * see one on, and an address is not an identity in any case. A calendar that
   * says "owner" is administered even when nothing in the member list looks
   * like the signed-in account.
   */
  it('gates on the calendar role rather than on recognising an address', async () => {
    calendarState.calendars = [calendar('owner')];
    calendarState.membersMap = { 'cal-1': [member('owner', 'someone-else@example.com')] };

    render(<CalendarsSection />);

    await waitFor(() =>
      expect(apiGet).toHaveBeenCalledWith(expect.stringContaining('/calendars/cal-1/invites')),
    );
  });
});

/**
 * The settings tab and the share panel offer the same operations on a
 * calendar's links, and used to implement them apart. What they disagreed on
 * is what these cover: which requests are allowed to land, and what a failure
 * is allowed to say.
 */
describe('CalendarsSection invite listing', () => {
  it('does not let a superseded listing land under another calendar', async () => {
    calendarState.calendars = [calendar('owner'), { ...calendar('owner'), id: 'cal-2', name: 'B' }];
    calendarState.membersMap = {};

    // The first calendar answers slowly, so its listing comes back after the
    // selection has already moved on.
    let landFirst: (invites: InviteData[]) => void = () => {};
    apiGet.mockImplementation((path) => {
      if (String(path).startsWith('/calendars/cal-1/invites')) {
        return new Promise((resolve) => {
          landFirst = resolve;
        });
      }
      if (String(path).startsWith('/calendars/cal-2/invites')) {
        return Promise.resolve([invite('inv-b', 'token-b')]);
      }
      return Promise.resolve([]);
    });

    render(<CalendarsSection />);

    fireEvent.click(screen.getByRole('button', { name: 'A' }));
    fireEvent.click(await screen.findByRole('button', { name: 'B' }));
    expect(await screen.findByText('/share/token-b')).toBeTruthy();

    await act(async () => {
      landFirst([invite('inv-a', 'token-a')]);
    });

    expect(screen.queryByText('/share/token-a')).toBeNull();
    expect(screen.getByText('/share/token-b')).toBeTruthy();
  });

  it('says why the listing failed in the language the reader chose', async () => {
    calendarState.calendars = [calendar('owner')];
    calendarState.membersMap = {};
    apiGet.mockImplementation((path) =>
      String(path).includes('/invites')
        ? Promise.reject(new Error('error.serverUnavailable'))
        : Promise.resolve([]),
    );

    render(<CalendarsSection />);

    // The localised message, not a bare English word assembled at the call site.
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('error.serverUnavailable'));
  });

  /**
   * A link that could hand out management would let whoever holds it widen its
   * own reach, so the API accepts only editor and viewer. Offering any other
   * role produces a rejected request for having used the control on offer.
   */
  /**
   * The same picker, on the other screen that creates join links. The API
   * gives a link with no stated expiry seven days, so an option promising
   * "never" would produce one that dies in a week under that label.
   */
  it('offers no expiry the API cannot honour', async () => {
    calendarState.calendars = [calendar('owner')];
    calendarState.membersMap = {};

    render(<CalendarsSection />);

    fireEvent.click(await screen.findByRole('button', { name: 'invites.expiry7d' }));

    expect(await screen.findByRole('button', { name: 'invites.expiry30d' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'invites.expiryNever' })).toBeNull();
  });

  it('offers only the roles an invite link may grant', async () => {
    calendarState.calendars = [calendar('owner')];
    calendarState.membersMap = {};

    render(<CalendarsSection />);

    // The dropdown renders through a portal, so it is queried from the
    // document rather than from the render container.
    fireEvent.click(await screen.findByRole('button', { name: 'members.roleEditor' }));
    expect(await screen.findByRole('button', { name: 'members.roleViewer' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'members.roleOwner' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'members.roleManager' })).toBeNull();
  });
});

/**
 * You manage other members, not yourself. The server refuses a self role
 * change outright, so a picker on your own row could only ever produce an
 * error message for having used a control the screen offered.
 */
describe('CalendarsSection own membership', () => {
  function row(name: string) {
    return screen.getByText(name).closest('li') as HTMLElement;
  }

  beforeEach(() => {
    calendarState.calendars = [calendar('owner')];
    // Two owners, so the last-owner rule is not what hides the control.
    calendarState.membersMap = {
      'cal-1': [
        member('owner', 'me@example.com', 'u1', 'Me'),
        member('owner', 'other@example.com', 'm2', 'Other'),
      ],
    };
  });

  it('offers no role picker on the signed-in user own row', async () => {
    render(<CalendarsSection />);

    await waitFor(() => expect(screen.getByText('Me')).toBeTruthy());
    expect(within(row('Me')).queryByRole('button', { name: 'members.roleOwner' })).toBeNull();
  });

  it('still offers one on another member row', async () => {
    render(<CalendarsSection />);

    await waitFor(() => expect(screen.getByText('Other')).toBeTruthy());
    expect(within(row('Other')).getByRole('button', { name: 'members.roleOwner' })).toBeTruthy();
  });
});

/**
 * An administrator states why an address is excepted from the domain
 * restriction. The list is what makes that statement worth typing, so the
 * value has to be sent and read back under the same name the server uses.
 */
describe('AllowedEmailsSection', () => {
  it('shows the reason stored against an allowed address', async () => {
    apiGet.mockResolvedValue({
      allowedDomains: ['example.com'],
      restricted: true,
      emails: [
        {
          id: '0198f0c2-0000-7000-8000-000000000001',
          email: 'contractor@gmail.com',
          reason: 'contractor until March',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
    });

    render(<AllowedEmailsSection />);

    await waitFor(() => expect(screen.getByText('contractor@gmail.com')).toBeTruthy());
    expect(screen.getByText('contractor until March')).toBeTruthy();
  });

  it('sends the typed reason under the name the server stores it as', async () => {
    apiGet.mockResolvedValue({ allowedDomains: [], restricted: false, emails: [] });
    apiPost.mockResolvedValue({});

    render(<AllowedEmailsSection />);

    const add = await screen.findByRole('button', { name: 'settings.adminAllowedEmailsAdd' });
    fireEvent.change(screen.getByPlaceholderText('settings.adminAllowedEmailsEmailPlaceholder'), {
      target: { value: 'contractor@gmail.com' },
    });
    fireEvent.change(screen.getByPlaceholderText('settings.adminAllowedEmailsNotePlaceholder'), {
      target: { value: 'contractor until March' },
    });
    fireEvent.submit(add.closest('form') as HTMLElement);

    await waitFor(() =>
      expect(apiPost).toHaveBeenCalledWith('/admin/allowed-emails', {
        email: 'contractor@gmail.com',
        reason: 'contractor until March',
      }),
    );
  });
});

/**
 * An import is only visible once the grid has been asked for the events it
 * created, and the span to ask for is the one the app is already showing.
 * Computing it here from the browser clock gave a different answer to anyone
 * whose machine sits on the other side of midnight from the calendar, so the
 * events at the edge of the span stayed invisible until a reload.
 */
describe('ExportSection', () => {
  // A browser east of UTC: local midnight on the first of the month converts
  // back to the previous day, which is the day the old computation lost.
  const browserZone = 'Asia/Tokyo';
  const viewedMonth = DateTime.fromISO('2026-08-07T21:00', { zone: browserZone });

  beforeEach(() => {
    vi.stubEnv('TZ', browserZone);
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(viewedMonth.toJSDate());
    calendarState.calendars = [calendar('owner')];
    calendarState.visibleRange = vi.fn(() => fetchWindow('month', viewedMonth));
    calendarState.fetchEvents.mockReset();
    apiPost.mockResolvedValue(importResult({ imported: 1 }));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllEnvs();
  });

  it('refreshes the span the app is showing, not one recomputed from the clock', async () => {
    const { start, end } = fetchWindow('month', viewedMonth);
    // The boundary the old arithmetic moved: the first of the month, not the
    // last day of the one before it.
    expect(start).toBe('2026-07-01');

    render(<ExportSection />);

    fireEvent.change(screen.getByPlaceholderText('settings.importPlaceholder'), {
      target: { value: 'BEGIN:VCALENDAR\nEND:VCALENDAR' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'settings.importPasted' }));

    await waitFor(() => expect(calendarState.fetchEvents).toHaveBeenCalledWith(start, end));
  });

  /**
   * The server names the export after the calendar it came from. Naming it on
   * the client gave every calendar the same file, so exporting a second one
   * saved over the first with no sign that it had.
   */
  it('saves the export under the name the server gave it', async () => {
    const downloads: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      downloads.push(this.download);
    });
    const createObjectUrl = vi.fn(() => 'blob:export');
    vi.stubGlobal('URL', {
      ...URL,
      createObjectURL: createObjectUrl,
      revokeObjectURL: vi.fn(),
    });
    apiGetBlob.mockResolvedValue({
      blob: new Blob(['BEGIN:VCALENDAR']),
      filename: 'Family.ics',
    });

    render(<ExportSection />);
    fireEvent.click(screen.getByRole('button', { name: 'settings.exportIcal' }));

    await waitFor(() => expect(downloads).toEqual(['Family.ics']));

    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });
});

/**
 * The import answers with every outcome the file met, and the screen is the
 * only place any of them is ever read. An outcome the screen drops is one the
 * person who uploaded the file has no way of finding out about.
 */
describe('ExportSection import outcomes', () => {
  async function importFile(outcome: Record<string, number | boolean>) {
    apiPost.mockResolvedValue(importResult(outcome));
    render(<ExportSection />);
    fireEvent.change(screen.getByPlaceholderText('settings.importPlaceholder'), {
      target: { value: 'BEGIN:VCALENDAR\nEND:VCALENDAR' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'settings.importPasted' }));
    await waitFor(() => expect(apiPost).toHaveBeenCalled());
    await act(async () => {});
  }

  /**
   * Re-uploading a file to find out whether the first upload worked is the
   * situation the deduplication exists for, and "0 imported" on its own is the
   * one answer that does not settle it.
   */
  it('says how many events the calendar already held', async () => {
    await importFile({ duplicates: 40 });

    expect(toastSuccess).toHaveBeenCalledWith('settings.importDuplicates:40');
    // Already being here is not a failure and must not be coloured as one.
    expect(toastError).not.toHaveBeenCalled();
    // Nothing was added, and saying so as a success beside the real answer is
    // noise the reader has to discount.
    expect(toastSuccess).not.toHaveBeenCalledWith('settings.imported:0');
  });

  it('tells a file it could not read apart from one that held nothing', async () => {
    await importFile({ unreadable: true });

    expect(toastError).toHaveBeenCalledWith('settings.importUnreadable');
    expect(toastInfo).not.toHaveBeenCalledWith('settings.importEmpty');
  });

  it('says an empty file was empty rather than saying nothing', async () => {
    await importFile({});

    expect(toastInfo).toHaveBeenCalledWith('settings.importEmpty');
    expect(toastError).not.toHaveBeenCalled();
  });

  /**
   * A rejection is counted inside the failures, so reporting both numbers as
   * given states the same events twice. What the two are worth apart is the
   * advice: one is worth retrying and the other never will be.
   */
  it('counts a rejected event once, under the outcome that can be acted on', async () => {
    await importFile({ failed: 3, rejected: 2 });

    expect(toastError).toHaveBeenCalledWith('settings.importFailed:1');
    expect(toastError).toHaveBeenCalledWith('settings.importRejected:2');
    expect(toastError).not.toHaveBeenCalledWith('settings.importFailed:3');
  });

  it('leaves out the retry advice when every failure was a rejection', async () => {
    await importFile({ failed: 2, rejected: 2 });

    expect(toastError).toHaveBeenCalledWith('settings.importRejected:2');
    expect(toastError).not.toHaveBeenCalledWith('settings.importFailed:0');
  });

  /**
   * These events are on the calendar. Only their times are wrong, which is
   * exactly what nobody goes back and checks unless they are told to.
   */
  it('reports events placed at a time it could not resolve', async () => {
    await importFile({ imported: 5, unknownTimezones: 2 });

    expect(toastSuccess).toHaveBeenCalledWith('settings.imported:5');
    expect(toastError).toHaveBeenCalledWith('settings.importUnknownTimezones:2');
  });
});

/**
 * A screen that has gone is not owed an answer. Without a guard the reply to a
 * request nobody is waiting for still writes state, and cancelling it reads as
 * a failure -- an error message for having navigated away.
 */
describe('AllowedEmailsSection cancellation', () => {
  it('calls the request off when the section goes', async () => {
    let signal: AbortSignal | undefined;
    apiGet.mockImplementation((_path, _skipRedirect, s) => {
      signal = s;
      return new Promise(() => {});
    });

    const { unmount } = render(<AllowedEmailsSection />);
    await waitFor(() => expect(apiGet).toHaveBeenCalled());
    expect(signal).toBeInstanceOf(AbortSignal);
    expect(signal?.aborted).toBe(false);

    unmount();

    expect(signal?.aborted).toBe(true);
  });

  it('says nothing to the reader about a request it called off itself', async () => {
    apiGet.mockRejectedValue(new DOMException('The operation was aborted.', 'AbortError'));

    render(<AllowedEmailsSection />);

    await waitFor(() => expect(apiGet).toHaveBeenCalled());
    await act(async () => {});
    expect(toastError).not.toHaveBeenCalled();
  });
});

/**
 * The holiday country picker offered ten of the two hundred-odd countries the
 * bundled data covers, and switching holidays on gave everyone Japan.
 */
describe('AppearanceSection holidays', () => {
  /**
   * The picker offered ten of the countries the bundled data covers, and
   * switching holidays on gave everyone Japan.
   *
   * The dropdown renders through a portal, so it is queried from the document
   * rather than from the render container.
   */
  it('offers the countries the data covers, not the ten it used to', async () => {
    uiState.holidaysCountry = 'JP';
    render(<AppearanceSection />);

    const field = screen.getByText('settings.holidaysCountry').closest('div')
      ?.parentElement as HTMLElement;
    fireEvent.click(within(field).getByRole('button'));

    // The search box is the picker saying the list is long enough to need one.
    expect(await screen.findByLabelText('common.search')).toBeTruthy();
    // The full list arrives with a dynamically imported chunk.
    await waitFor(() => expect(screen.getAllByRole('button').length).toBeGreaterThan(100), {
      timeout: 5000,
    });
  });

  it('turns holidays on for the country the browser implies', () => {
    uiState.holidaysCountry = null;
    const { container } = render(<AppearanceSection />);

    const toggle = container.querySelector('input[type="checkbox"]') as HTMLInputElement;
    fireEvent.click(toggle);

    // jsdom reports en-US, so anything but the old hardcoded 'JP' is the point.
    expect(uiState.setHolidaysCountry).toHaveBeenCalledWith('US');
    uiState.holidaysCountry = 'JP';
  });
});
