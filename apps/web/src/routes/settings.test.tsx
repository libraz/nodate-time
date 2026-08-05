import { cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Member } from '@/types/calendar';

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

const calendarState = {
  calendars: [
    { id: 'cal-1', name: 'A', color: '#000', coverUrl: '', createdAt: '', publicShared: false },
  ],
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

const uiState = { locale: 'ja', theme: 'light', weekStart: 0, timezone: 'Asia/Tokyo' };

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { api } from '@/lib/api';
import { CalendarsSection } from './settings';

const apiGet = vi.mocked(api.get);

function member(role: string): Member {
  return { id: 'm1', name: 'Me', email: 'me@example.com', role, color: '#000' } as Member;
}

beforeEach(() => {
  apiGet.mockReset();
  apiGet.mockResolvedValue([]);
});

afterEach(cleanup);

/**
 * Listing a calendar's invites needs the right to manage it. The screen used
 * to ask on every calendar selection regardless, so an editor or viewer met a
 * permission error for opening a settings tab -- and again for every calendar
 * they picked.
 */
describe('CalendarsSection invites', () => {
  it('does not ask for invites a viewer is not allowed to read', async () => {
    calendarState.membersMap = { 'cal-1': [member('viewer')] };

    render(<CalendarsSection />);

    await waitFor(() => expect(calendarState.fetchMembers).toHaveBeenCalled());
    const asked = apiGet.mock.calls.map((c) => String(c[0]));
    expect(asked.filter((u) => u.includes('/invites'))).toEqual([]);
  });

  it('asks for invites when the caller may manage the calendar', async () => {
    calendarState.membersMap = { 'cal-1': [member('owner')] };

    render(<CalendarsSection />);

    await waitFor(() =>
      expect(apiGet).toHaveBeenCalledWith(expect.stringContaining('/calendars/cal-1/invites')),
    );
  });
});
