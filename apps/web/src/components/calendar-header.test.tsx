import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';

const navigate = vi.fn();
const logout = vi.fn();

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => navigate,
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({
    to,
    children,
    'aria-label': ariaLabel,
  }: {
    to: string;
    children: React.ReactNode;
    'aria-label'?: string;
  }) => (
    <a href={to} aria-label={ariaLabel}>
      {children}
    </a>
  ),
}));

const uiState = {
  locale: 'en',
  currentMonth: DateTime.fromISO('2026-08-01'),
  selectedDate: DateTime.fromISO('2026-08-05'),
  calendarView: 'month' as const,
  navigateMonth: vi.fn(),
  setCalendarView: vi.fn(),
  setCurrentMonth: vi.fn(),
  setSelectedDate: vi.fn(),
  setShowMobileMenu: vi.fn(),
  triggerScrollToToday: vi.fn(),
  openEventModal: vi.fn(),
  toggleSearch: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const authState = {
  user: { id: 'u1', name: 'Taro', email: 'taro@example.com', locale: 'en', timezone: 'Asia/Tokyo' },
  logout,
};

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { useSchedulePollStore } from '@/stores/schedule-poll-store';
import { CalendarHeader } from './calendar-header';
import { SchedulePollActionNotice } from './schedule-poll-actions';

afterEach(() => {
  cleanup();
  navigate.mockClear();
  logout.mockClear();
  useSchedulePollStore.getState().reset();
});

describe('schedule poll entry and notice', () => {
  it('shows the same pending count in both headers and links to the response', () => {
    useSchedulePollStore.setState({
      accountId: 'u1',
      actions: [
        {
          calendarId: 'cal-1',
          calendarName: 'Family',
          pollId: 'poll-1',
          title: 'Studio day',
          kind: 'answer',
        },
      ],
    });

    render(
      <>
        <CalendarHeader />
        <SchedulePollActionNotice />
      </>,
    );

    expect(screen.getAllByRole('link', { name: /Scheduling/ })).toHaveLength(2);
    expect(screen.getByRole('link', { name: /Studio day/ })).toHaveAttribute(
      'href',
      '/schedule-polls/$calendarId/$pollId',
    );
  });

  it('keeps another account’s action out of the notice', () => {
    useSchedulePollStore.setState({
      accountId: 'u2',
      actions: [
        {
          calendarId: 'cal-2',
          calendarName: 'Work',
          pollId: 'poll-2',
          title: 'Private meeting',
          kind: 'answer',
        },
      ],
    });

    const { container } = render(<SchedulePollActionNotice />);
    expect(container).toBeEmptyDOMElement();
  });
});

/**
 * Both headers stay mounted and only their visibility differs, so a single
 * shared ref resolves to whichever mounted last. The outside-click handler
 * then treats a click in the other header as outside and closes the menu on
 * mousedown, before the click can reach the item under the pointer.
 */
describe('CalendarHeader profile menu', () => {
  it('reaches sign-out from the mobile header', async () => {
    const user = userEvent.setup();
    render(<CalendarHeader />);

    // Both headers render an avatar trigger; the first is the mobile one.
    const triggers = screen.getAllByRole('button', { name: 'Profile' });
    expect(triggers.length).toBe(2);
    await user.click(triggers[0] as HTMLElement);

    const logoutButtons = screen.getAllByRole('menuitem', { name: 'Log out' });
    await user.click(logoutButtons[0] as HTMLElement);
    expect(logout).toHaveBeenCalledTimes(1);
  });

  it('reaches sign-out from the desktop header', async () => {
    const user = userEvent.setup();
    render(<CalendarHeader />);

    const triggers = screen.getAllByRole('button', { name: 'Profile' });
    await user.click(triggers[1] as HTMLElement);

    const logoutButtons = screen.getAllByRole('menuitem', { name: 'Log out' });
    await user.click(logoutButtons[logoutButtons.length - 1] as HTMLElement);
    expect(logout).toHaveBeenCalledTimes(1);
  });

  it('closes when the click really is outside both headers', async () => {
    const user = userEvent.setup();
    render(
      <div>
        <CalendarHeader />
        <button type="button">elsewhere</button>
      </div>,
    );

    const triggers = screen.getAllByRole('button', { name: 'Profile' });
    await user.click(triggers[0] as HTMLElement);
    expect(screen.getAllByRole('menuitem', { name: 'Log out' }).length).toBeGreaterThan(0);

    await user.click(screen.getByRole('button', { name: 'elsewhere' }));
    expect(screen.queryByRole('menuitem', { name: 'Log out' })).toBeNull();
  });
});
