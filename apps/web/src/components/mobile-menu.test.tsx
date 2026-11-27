import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/use-modal-a11y', () => ({
  useModalA11y: () => ({ current: null }),
}));

vi.mock('@/lib/brand', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_NAME: 'Acme',
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_LOGO: undefined,
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_ICON: undefined,
}));

const calendarListStub = vi.hoisted(() => () => null);
// biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
vi.mock('@/components/calendar-list', () => ({ CalendarList: calendarListStub }));

const authState = { logout: vi.fn(), user: { id: 'u1' } };
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

const uiState = {
  showMobileMenu: true,
  setShowMobileMenu: vi.fn(),
  toggleRightPanel: vi.fn(),
  setShowActivity: vi.fn(),
};
vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { useSchedulePollStore } from '@/stores/schedule-poll-store';
import { MobileMenu } from './mobile-menu';

afterEach(() => {
  cleanup();
  uiState.showMobileMenu = true;
});

describe('MobileMenu header', () => {
  it('shows the brand name and keeps the dialog labelled', () => {
    render(<MobileMenu />);
    expect(screen.getByText('Acme')).toBeInTheDocument();
    expect(screen.getByRole('dialog')).toHaveAttribute('aria-label', 'tabs.calendar');
  });

  it('renders nothing while closed', () => {
    uiState.showMobileMenu = false;
    render(<MobileMenu />);
    expect(screen.queryByText('Acme')).toBeNull();
  });
});

describe('MobileMenu scheduling entry', () => {
  it('offers scheduling, with the waiting count when there is one', () => {
    useSchedulePollStore.setState({
      accountId: 'u1',
      actions: [
        { calendarId: 'c', calendarName: 'C', pollId: 'p', title: 'Picnic', kind: 'answer' },
      ],
    });
    render(<MobileMenu />);

    const entry = screen.getByRole('button', { name: /schedulePoll.nav/ });
    expect(entry).toHaveTextContent('1');
    useSchedulePollStore.getState().reset();
  });
});
