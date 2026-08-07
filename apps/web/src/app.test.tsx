import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';

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

const calendarState = {
  calendars: [
    { id: 'cal-1', name: 'Family', color: '#47B2F7', role: 'owner', publicShared: false },
  ],
  activeCalendarIds: ['cal-1'],
  events: [],
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
import { useUiStore } from './stores/ui-store';

// Both layouts mount here regardless of width, and the mobile month scroll
// positions itself on mount; jsdom has no scrollTo.
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
