import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Calendar, Member } from '@/types/calendar';
import type { InviteData } from '@/types/invite';

const page = vi.hoisted(() => ({ component: undefined as (() => unknown) | undefined }));

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: { component: () => unknown }) => {
    page.component = options.component;
    return { useSearch: () => ({}) };
  },
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

vi.mock('@/lib/client', async (importOriginal) => {
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
    ...(await importOriginal<typeof import('@/lib/client')>()),
    // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
    ApiError,
    filenameFromDisposition: (header: string | null) =>
      header?.match(/filename="([^"]+)"/)?.[1] ?? '',
    errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
    isAbortError: (e: unknown) => e instanceof DOMException && e.name === 'AbortError',
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    client: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
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
    createdAt: '',
    publicShared: false,
    memberCount: 1,
    role: role as Calendar['role'],
    memberColor: '#000',
    coverUploadAllowed: false,
    etag: '',
  };
}

const calendarState = {
  calendars: [calendar('owner')],
  membersMap: {} as Record<string, Member[]>,
  fetchMembers: vi.fn(),
  updateCalendar: vi.fn(),
  uploadCalendarCover: vi.fn(),
  deleteCalendarCover: vi.fn(),
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

import { resetAuthConfig } from '@/lib/auth-config';
import { APP_NAME } from '@/lib/brand';
import { ApiError, client } from '@/lib/client';
import { fetchWindow } from '@/lib/date-utils';
import { toast } from '@/lib/toast';
import {
  AllowedEmailsSection,
  AppearanceSection,
  CalendarsSection,
  ExportSection,
  InstanceAdminsSection,
  ProfileSection,
  ProviderCard,
} from './settings';

const apiGet = vi.mocked(client.GET);
const apiPost = vi.mocked(client.POST);

/** The concrete URL a typed call addresses, so a test can name it as a reader would. */
function urlOf(path: unknown, init?: unknown): string {
  const params = (init as { params?: { path?: Record<string, string> } } | undefined)?.params;
  return String(path).replace(/\{(\w+)\}/g, (_, key: string) => params?.path?.[key] ?? '');
}

const askedUrls = () =>
  (apiGet.mock.calls as unknown as [string, unknown][]).map(([path, init]) => urlOf(path, init));

/** What an export answers: the file, and the headers that name and bound it. */
function exported(filename: string, window = 'full') {
  return {
    data: new Blob(['x']),
    response: new Response(null, {
      headers: {
        'Content-Disposition': `attachment; filename="${filename}"`,
        'X-Export-Window': window,
      },
    }),
  } as never;
}
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
  apiGet.mockResolvedValue({ data: [] } as never);
});

afterEach(() => {
  cleanup();
  calendarState.calendars = [calendar('owner')];
  calendarState.membersMap = {};
  calendarState.uploadCalendarCover.mockReset();
  calendarState.deleteCalendarCover.mockReset();
});

describe('SettingsPage header', () => {
  it('shows the brand row above the title', () => {
    const Page = page.component as () => React.ReactElement;
    render(<Page />);
    expect(screen.getByText(APP_NAME)).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('settings.title');
  });
});

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
    expect(askedUrls().filter((u) => u.includes('/invites'))).toEqual([]);
  });

  it('asks for invites when the caller may manage the calendar', async () => {
    calendarState.calendars = [calendar('owner')];
    calendarState.membersMap = {};

    render(<CalendarsSection />);

    await waitFor(() => expect(askedUrls()).toContain('/calendars/cal-1/invites'));
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

    await waitFor(() => expect(askedUrls()).toContain('/calendars/cal-1/invites'));
  });
});

describe('Calendar details cover image', () => {
  const objectUrlApi = {
    createObjectURL: Object.getOwnPropertyDescriptor(URL, 'createObjectURL'),
    revokeObjectURL: Object.getOwnPropertyDescriptor(URL, 'revokeObjectURL'),
  };
  beforeEach(() => {
    Object.defineProperty(URL, 'createObjectURL', {
      value: () => 'blob:picked-cover',
      configurable: true,
    });
    Object.defineProperty(URL, 'revokeObjectURL', { value: () => {}, configurable: true });
  });
  afterEach(() => {
    for (const [key, descriptor] of Object.entries(objectUrlApi)) {
      if (descriptor) Object.defineProperty(URL, key, descriptor);
      else Reflect.deleteProperty(URL, key);
    }
  });

  it('previews the uploaded cover, uploads a selected file, and removes it', async () => {
    const file = new File(['image bytes'], 'cover.png', { type: 'image/png' });
    calendarState.calendars = [
      {
        ...calendar('owner'),
        coverImageUrl: 'https://storage.example/signed-cover',
        coverUploadAllowed: true,
      },
    ];
    calendarState.uploadCalendarCover.mockResolvedValue(undefined);
    calendarState.deleteCalendarCover.mockResolvedValue(undefined);

    const { container, rerender } = render(<CalendarsSection />);
    const preview = screen.getByAltText('A');
    expect(preview).toHaveAttribute('src', 'https://storage.example/signed-cover');

    const nameInput = container.querySelector<HTMLInputElement>('input[type="text"]');
    const colorInput = container.querySelector<HTMLInputElement>('input[type="color"]');
    if (!nameInput || !colorInput) {
      throw new Error('Calendar detail inputs were not rendered');
    }
    fireEvent.change(nameInput, { target: { value: 'Draft name' } });
    fireEvent.change(colorInput, { target: { value: '#ff0000' } });
    expect(screen.getByAltText('A')).toHaveAttribute('src', 'https://storage.example/signed-cover');

    const input = container.querySelector('input[type="file"]');
    expect(input).toHaveAttribute('accept', 'image/jpeg,image/png,image/webp');
    if (!input) throw new Error('Cover upload input was not rendered');
    fireEvent.change(input, { target: { files: [file] } });
    await waitFor(() =>
      expect(calendarState.uploadCalendarCover).toHaveBeenCalledWith('cal-1', file),
    );
    const uploadedCalendar = calendarState.calendars[0];
    if (!uploadedCalendar) throw new Error('Calendar fixture missing after upload');
    calendarState.calendars[0] = {
      ...uploadedCalendar,
      coverImageUrl: 'https://storage.example/replacement-cover',
    };
    await act(async () => rerender(<CalendarsSection />));
    expect(nameInput).toHaveValue('Draft name');
    expect(colorInput).toHaveValue('#ff0000');
    expect(screen.getByAltText('A')).toHaveAttribute(
      'src',
      'https://storage.example/replacement-cover',
    );

    fireEvent.click(screen.getByRole('button', { name: 'settings.calendarCoverRemove' }));
    expect(calendarState.deleteCalendarCover).not.toHaveBeenCalled();
    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', { name: 'common.delete' }),
    );
    await waitFor(() => expect(calendarState.deleteCalendarCover).toHaveBeenCalledWith('cal-1'));
    const removedCalendar = calendarState.calendars[0];
    if (!removedCalendar) throw new Error('Calendar fixture missing after removal');
    const withoutUploadedCover = { ...removedCalendar };
    delete withoutUploadedCover.coverImageUrl;
    calendarState.calendars[0] = withoutUploadedCover;
    await act(async () => rerender(<CalendarsSection />));
    expect(nameInput).toHaveValue('Draft name');
    expect(colorInput).toHaveValue('#ff0000');
    expect(screen.queryByAltText('A')).toBeNull();
  });

  it('shows a dropped file at once and uploads it', async () => {
    const file = new File(['image bytes'], 'cover.webp', { type: 'image/webp' });
    let finishUpload: () => void = () => {};
    calendarState.calendars = [{ ...calendar('owner'), coverUploadAllowed: true }];
    calendarState.uploadCalendarCover.mockImplementation(
      () => new Promise<void>((resolve) => (finishUpload = resolve)),
    );

    const { container } = render(<CalendarsSection />);
    const tile = container.querySelector('.image-upload-tile');
    if (!tile) throw new Error('Cover upload tile was not rendered');
    expect(tile).toHaveAttribute('data-empty');
    fireEvent.drop(tile, { dataTransfer: { files: [file], types: ['Files'] } });

    await waitFor(() =>
      expect(calendarState.uploadCalendarCover).toHaveBeenCalledWith('cal-1', file),
    );
    expect(tile).toHaveAttribute('aria-busy', 'true');
    expect(container.querySelector('img[src="blob:picked-cover"]')).not.toBeNull();

    await act(async () => finishUpload());
    expect(tile).toHaveAttribute('aria-busy', 'false');
    expect(container.querySelector('img[src="blob:picked-cover"]')).toBeNull();
  });

  it('rejects a file of the wrong type before uploading', async () => {
    calendarState.calendars = [{ ...calendar('owner'), coverUploadAllowed: true }];

    const { container } = render(<CalendarsSection />);
    const input = container.querySelector('input[type="file"]');
    if (!input) throw new Error('Cover upload input was not rendered');
    fireEvent.change(input, {
      target: { files: [new File(['gif'], 'cover.gif', { type: 'image/gif' })] },
    });

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith('settings.calendarCoverInvalidType'),
    );
    expect(calendarState.uploadCalendarCover).not.toHaveBeenCalled();
  });

  it('previews the unsaved colour in the calendar-list row', () => {
    calendarState.calendars = [{ ...calendar('owner'), coverUploadAllowed: true }];

    const { container } = render(<CalendarsSection />);
    const colorInput = container.querySelector<HTMLInputElement>('input[type="color"]');
    if (!colorInput) throw new Error('Colour input was not rendered');
    fireEvent.change(colorInput, { target: { value: '#ff0000' } });

    const dot = container.querySelector<HTMLElement>('[data-calendar-cover-fallback]');
    expect(dot?.style.backgroundColor).toBe('rgb(255, 0, 0)');
  });

  it('hides the upload control when storage uploads are unavailable', () => {
    calendarState.calendars = [{ ...calendar('owner'), coverUploadAllowed: false }];

    const { container } = render(<CalendarsSection />);

    expect(container.querySelector('input[type="file"]')).toBeNull();
    expect(screen.queryByRole('button', { name: 'settings.calendarCoverUpload' })).toBeNull();
  });

  it('does not show cover controls to a calendar editor', () => {
    calendarState.calendars = [
      {
        ...calendar('editor'),
        coverImageUrl: 'https://storage.example/signed-cover',
        coverUploadAllowed: true,
      },
    ];

    const { container } = render(<CalendarsSection />);

    expect(container.querySelector('input[type="file"]')).toBeNull();
    expect(screen.queryByRole('button', { name: 'settings.calendarCoverRemove' })).toBeNull();
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
    apiGet.mockImplementation(((path: string, init: unknown) => {
      const url = urlOf(path, init);
      if (url.startsWith('/calendars/cal-1/invites')) {
        return new Promise((resolve) => {
          landFirst = (invites) => resolve({ data: invites });
        });
      }
      if (url.startsWith('/calendars/cal-2/invites')) {
        return Promise.resolve({ data: [invite('inv-b', 'token-b')] });
      }
      return Promise.resolve({ data: [] });
    }) as never);

    render(<CalendarsSection />);

    fireEvent.click(screen.getByRole('button', { name: 'calendar.calendarName' }));
    fireEvent.click(await screen.findByRole('option', { name: 'B' }));
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
    apiGet.mockImplementation(((path: string, init: unknown) =>
      urlOf(path, init).includes('/invites')
        ? Promise.reject(new Error('error.serverUnavailable'))
        : Promise.resolve({ data: [] })) as never);

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

    fireEvent.click(await screen.findByRole('button', { name: 'invites.expiry' }));

    expect(await screen.findByRole('option', { name: 'invites.expiry30d' })).toBeTruthy();
    expect(screen.queryByRole('option', { name: 'invites.expiryNever' })).toBeNull();
  });

  it('offers only the roles an invite link may grant', async () => {
    calendarState.calendars = [calendar('owner')];
    calendarState.membersMap = {};

    render(<CalendarsSection />);

    // The dropdown renders through a portal, so it is queried from the
    // document rather than from the render container.
    fireEvent.click(await screen.findByRole('button', { name: 'members.role' }));
    expect(await screen.findByRole('option', { name: 'members.roleViewer' })).toBeTruthy();
    expect(screen.queryByRole('option', { name: 'members.roleOwner' })).toBeNull();
    expect(screen.queryByRole('option', { name: 'members.roleManager' })).toBeNull();
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
    expect(within(row('Me')).queryByRole('button', { name: 'members.roleFor:Me' })).toBeNull();
  });

  it('still offers one on another member row', async () => {
    render(<CalendarsSection />);

    await waitFor(() => expect(screen.getByText('Other')).toBeTruthy());
    expect(
      within(row('Other')).getByRole('button', { name: 'members.roleFor:Other' }),
    ).toBeTruthy();
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
      data: {
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
      },
    } as never);

    render(<AllowedEmailsSection />);

    await waitFor(() => expect(screen.getByText('contractor@gmail.com')).toBeTruthy());
    expect(screen.getByText('contractor until March')).toBeTruthy();
  });

  it('sends the typed reason under the name the server stores it as', async () => {
    apiGet.mockResolvedValue({
      data: { allowedDomains: [], restricted: false, emails: [] },
    } as never);
    apiPost.mockResolvedValue({ data: {} } as never);

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
        body: { email: 'contractor@gmail.com', reason: 'contractor until March' },
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
    apiPost.mockResolvedValue({ data: importResult({ imported: 1 }) } as never);
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
    apiGet.mockResolvedValue(exported('Family.ics'));

    render(<ExportSection />);
    fireEvent.click(screen.getByRole('button', { name: 'settings.exportIcal' }));

    await waitFor(() => expect(downloads).toEqual(['Family.ics']));

    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });
  /** The grid reads days in the app's zone; an export read in another covers different hours. */
  it('exports in the zone the app shows the calendar in', async () => {
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    vi.stubGlobal('URL', {
      ...URL,
      createObjectURL: vi.fn(() => 'blob:export'),
      revokeObjectURL: vi.fn(),
    });
    uiState.timezone = 'America/New_York';
    apiGet.mockResolvedValue(exported('Family.csv'));

    render(<ExportSection />);
    fireEvent.click(screen.getByRole('button', { name: 'settings.exportCsv' }));

    await waitFor(() =>
      expect(apiGet).toHaveBeenCalledWith(
        '/calendars/{calendarId}/export',
        expect.objectContaining({
          params: {
            path: { calendarId: 'cal-1' },
            query: { format: 'csv', tz: 'America/New_York' },
          },
        }),
      ),
    );

    uiState.timezone = 'Asia/Tokyo';
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });
});

describe('ExportSection coverage', () => {
  beforeEach(() => {
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    vi.stubGlobal('URL', {
      ...URL,
      createObjectURL: vi.fn(() => 'blob:export'),
      revokeObjectURL: vi.fn(),
    });
    calendarState.calendars = [calendar('owner')];
    toastError.mockReset();
    toastInfo.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  async function exportIcs(window: string) {
    apiGet.mockResolvedValue(exported('F.ics', window));
    render(<ExportSection />);
    fireEvent.click(screen.getByRole('button', { name: 'settings.exportIcal' }));
    await waitFor(() => expect(apiGet).toHaveBeenCalled());
    await act(async () => {});
  }

  // A range holding too many events is refused outright rather than cut short.
  it('reports an export the server refused for being too large', async () => {
    apiGet.mockRejectedValue(new Error('apiError.EXPORT.RANGE_TOO_LARGE'));
    render(<ExportSection />);
    fireEvent.click(screen.getByRole('button', { name: 'settings.exportIcal' }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('apiError.EXPORT.RANGE_TOO_LARGE'));
  });

  it('says which window a bounded export covers', async () => {
    await exportIcs('2026-01-01/2026-03-31');
    expect(toastInfo).toHaveBeenCalledWith('settings.exportWindow:2026-01-01 – 2026-03-31');
    expect(toastError).not.toHaveBeenCalled();
  });

  it('says nothing extra for a whole export', async () => {
    await exportIcs('full');
    expect(toastInfo).not.toHaveBeenCalled();
    expect(toastError).not.toHaveBeenCalled();
  });
});

describe('ExportSection without a calendar', () => {
  afterEach(() => {
    calendarState.calendars = [calendar('owner')];
  });

  it('sends no import request when no calendar is selected', async () => {
    calendarState.calendars = [];
    apiPost.mockReset();
    render(<ExportSection />);

    fireEvent.change(screen.getByPlaceholderText('settings.importPlaceholder'), {
      target: { value: 'BEGIN:VCALENDAR\nEND:VCALENDAR' },
    });

    const button = screen.getByRole('button', {
      name: 'settings.importPasted',
    }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    fireEvent.click(button);
    expect(apiPost).not.toHaveBeenCalled();
  });
});

/**
 * The import answers with every outcome the file met, and the screen is the
 * only place any of them is ever read. An outcome the screen drops is one the
 * person who uploaded the file has no way of finding out about.
 */
describe('ExportSection import outcomes', () => {
  async function importFile(outcome: Record<string, number | boolean>) {
    apiPost.mockResolvedValue({ data: importResult(outcome) } as never);
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
    apiGet.mockImplementation(((_path: string, init: { signal?: AbortSignal }) => {
      signal = init.signal;
      return new Promise(() => {});
    }) as never);

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
    await waitFor(() => expect(screen.getAllByRole('option').length).toBeGreaterThan(100), {
      timeout: 5000,
    });
  });

  it('turns holidays on for the country the browser implies', () => {
    uiState.holidaysCountry = null;
    render(<AppearanceSection />);

    const toggle = screen.getByRole('button', { name: 'settings.holidays' });
    fireEvent.click(toggle);

    // jsdom reports en-US, so anything but the old hardcoded 'JP' is the point.
    expect(uiState.setHolidaysCountry).toHaveBeenCalledWith('US');
    uiState.holidaysCountry = 'JP';
  });
});

describe('ProfileSection auth-config dependent parts', () => {
  const unverified = { ...authState.user, emailVerified: false, hasPassword: true };

  beforeEach(() => {
    (authState as { user: unknown }).user = unverified;
    resetAuthConfig();
  });

  afterEach(() => {
    (authState as { user: unknown }).user = { ...unverified, emailVerified: undefined };
  });

  /** Answers the providers request with `answer` and every other read with an empty list. */
  function answerProvidersWith(answer: Record<string, unknown> | Error) {
    apiGet.mockImplementation((async (path: string) => {
      if (path !== '/auth/oauth/providers') return { data: [] };
      if (answer instanceof Error) throw answer;
      return { data: answer };
    }) as never);
  }
  const config = (over: Record<string, unknown>) => ({
    providers: [],
    passwordEnabled: true,
    mailEnabled: true,
    ...over,
  });

  it('shows neither the resend button nor the password form while loading', () => {
    apiGet.mockReturnValue(new Promise(() => {}) as never);
    render(<ProfileSection />);
    expect(screen.queryByText('profile.resendVerification')).toBeNull();
    expect(screen.queryByText('profile.changePassword')).toBeNull();
  });

  it('shows both when mail and password sign-in are on', async () => {
    answerProvidersWith(config({}));
    render(<ProfileSection />);
    await waitFor(() => expect(screen.getByText('profile.resendVerification')).toBeTruthy());
    expect(screen.getByText('profile.changePassword')).toBeTruthy();
  });

  it('withdraws the resend button when mail is off', async () => {
    answerProvidersWith(config({ mailEnabled: false }));
    render(<ProfileSection />);
    await waitFor(() => expect(screen.getByText('profile.changePassword')).toBeTruthy());
    expect(screen.queryByText('profile.resendVerification')).toBeNull();
    expect(screen.getByText('profile.emailUnverified')).toBeTruthy();
  });

  it('withdraws the password change when password sign-in is off', async () => {
    answerProvidersWith(config({ passwordEnabled: false }));
    render(<ProfileSection />);
    await waitFor(() => expect(screen.getByText('profile.resendVerification')).toBeTruthy());
    expect(screen.queryByText('profile.changePassword')).toBeNull();
  });

  it('withdraws the password change for an account with no local password', async () => {
    (authState as { user: unknown }).user = { ...unverified, hasPassword: false };
    answerProvidersWith(config({}));
    render(<ProfileSection />);
    await waitFor(() => expect(screen.getByText('profile.resendVerification')).toBeTruthy());
    expect(screen.queryByText('profile.changePassword')).toBeNull();
  });

  it('offers a retry when the config cannot be read, and recovers', async () => {
    answerProvidersWith(new Error('network'));
    render(<ProfileSection />);
    await waitFor(() => expect(screen.getByText('error.authConfigUnavailable')).toBeTruthy());
    expect(screen.queryByText('profile.resendVerification')).toBeNull();

    answerProvidersWith(config({}));
    fireEvent.click(screen.getByText('error.retry'));
    await waitFor(() => expect(screen.getByText('profile.resendVerification')).toBeTruthy());
    expect(screen.queryByText('error.authConfigUnavailable')).toBeNull();
  });
});

describe('InstanceAdminsSection', () => {
  const alice = {
    userId: 'u-alice',
    name: 'Alice',
    email: 'alice@example.com',
    grantedAt: '2026-01-01T00:00:00Z',
  };
  const typeEmail = (value: string) => {
    fireEvent.change(screen.getByPlaceholderText('settings.adminAllowedEmailsEmailPlaceholder'), {
      target: { value },
    });
    fireEvent.submit(
      screen.getByRole('button', { name: 'admin.grant' }).closest('form') as HTMLElement,
    );
  };

  beforeEach(() => {
    apiGet.mockResolvedValue({ data: [alice] } as never);
  });

  it('lists the administrators', async () => {
    render(<InstanceAdminsSection />);

    expect(await screen.findByText('Alice')).toBeTruthy();
    expect(apiGet).toHaveBeenCalledWith('/admin/instance-admins', expect.anything());
  });

  it('grants the right by email and lists the new administrator', async () => {
    const bob = {
      userId: 'u-bob',
      name: 'Bob',
      email: 'bob@example.com',
      grantedAt: alice.grantedAt,
    };
    apiPost.mockResolvedValue({ data: bob } as never);
    render(<InstanceAdminsSection />);
    await screen.findByText('Alice');
    apiGet.mockResolvedValue({ data: [alice, bob] } as never);

    typeEmail('bob@example.com');

    await waitFor(() =>
      expect(apiPost).toHaveBeenCalledWith('/admin/instance-admins', {
        body: { email: 'bob@example.com' },
      }),
    );
    expect(await screen.findByText('Bob')).toBeTruthy();
    expect(toastError).not.toHaveBeenCalled();
  });

  it('says no user matched when the address is unknown', async () => {
    apiPost.mockRejectedValue(new ApiError(404, 'not found', 'ADMIN.USER_NOT_FOUND'));
    render(<InstanceAdminsSection />);
    await screen.findByText('Alice');

    typeEmail('nobody@example.com');

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('admin.userNotFound'));
  });

  it('revokes the right after confirmation', async () => {
    vi.mocked(client.DELETE).mockResolvedValue({} as never);
    render(<InstanceAdminsSection />);
    await screen.findByText('Alice');

    fireEvent.click(screen.getByRole('button', { name: 'admin.revoke' }));
    fireEvent.click(await screen.findByRole('button', { name: 'confirm.remove' }));

    await waitFor(() =>
      expect(client.DELETE).toHaveBeenCalledWith('/admin/instance-admins/{userId}', {
        params: { path: { userId: 'u-alice' } },
      }),
    );
  });
});

/**
 * The provider update is a merge patch: leaving the secret out keeps the
 * stored one, so saving other fields must not mention it.
 */
describe('ProviderCard', () => {
  const info = {
    provider: 'google' as const,
    clientId: 'client-1',
    hasSecret: true,
    enabled: true,
    source: 'db' as const,
  };

  it('leaves the stored secret out of a save that did not touch it', async () => {
    vi.mocked(client.PATCH).mockResolvedValue({ data: info } as never);
    render(<ProviderCard info={info} onChange={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText('xxxxxxxx.apps.googleusercontent.com'), {
      target: { value: 'client-2' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() =>
      expect(client.PATCH).toHaveBeenCalledWith('/admin/oauth-providers/{provider}', {
        params: { path: { provider: 'google' } },
        body: { clientId: 'client-2', enabled: true },
      }),
    );
  });

  it('sends a replacement secret when one is typed', async () => {
    vi.mocked(client.PATCH).mockResolvedValue({ data: info } as never);
    render(<ProviderCard info={info} onChange={vi.fn()} />);

    fireEvent.click(screen.getByText('settings.adminProviderReplaceSecret'));
    const secret = document.querySelector('input[type="password"]') as HTMLInputElement;
    fireEvent.change(secret, { target: { value: 'new-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() =>
      expect(client.PATCH).toHaveBeenCalledWith('/admin/oauth-providers/{provider}', {
        params: { path: { provider: 'google' } },
        body: { clientId: 'client-1', enabled: true, clientSecret: 'new-secret' },
      }),
    );
  });

  it('sends null to remove the stored secret', async () => {
    vi.mocked(client.PATCH).mockResolvedValue({ data: info } as never);
    render(<ProviderCard info={info} onChange={vi.fn()} />);

    fireEvent.click(screen.getByText('settings.adminProviderRemoveSecret'));
    expect(screen.getByText('settings.adminProviderSecretWillBeRemoved')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() =>
      expect(client.PATCH).toHaveBeenLastCalledWith('/admin/oauth-providers/{provider}', {
        params: { path: { provider: 'google' } },
        body: { clientId: 'client-1', enabled: true, clientSecret: null },
      }),
    );
  });

  it('keeps the stored secret when its removal is undone', async () => {
    vi.mocked(client.PATCH).mockResolvedValue({ data: info } as never);
    render(<ProviderCard info={info} onChange={vi.fn()} />);

    fireEvent.click(screen.getByText('settings.adminProviderRemoveSecret'));
    fireEvent.click(screen.getByRole('button', { name: 'common.cancel' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() =>
      expect(client.PATCH).toHaveBeenLastCalledWith('/admin/oauth-providers/{provider}', {
        params: { path: { provider: 'google' } },
        body: { clientId: 'client-1', enabled: true },
      }),
    );
  });
});
