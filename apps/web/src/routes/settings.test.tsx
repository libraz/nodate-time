import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Calendar, Member } from '@/types/calendar';

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => () => ({ useSearch: () => ({}) }),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ children }: { children?: unknown }) => children,
  useNavigate: () => vi.fn(),
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
    ) {
      super(detail);
    }
  }
  return {
    // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
    ApiError,
    api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn(), getBlob: vi.fn() },
    errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
  };
});

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
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
import { AllowedEmailsSection, AppearanceSection, CalendarsSection } from './settings';

const apiGet = vi.mocked(api.get);
const apiPost = vi.mocked(api.post);

function member(role: string, email = 'me@example.com', id = 'm1', name = 'Me'): Member {
  return { id, name, email, role, color: '#000' } as Member;
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
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
