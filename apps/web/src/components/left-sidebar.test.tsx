import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const calendarListStub = vi.hoisted(() => () => null);
// biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
vi.mock('@/components/calendar-list', () => ({ CalendarList: calendarListStub }));

vi.mock('@/lib/brand', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_NAME: 'Acme',
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_LOGO: undefined,
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_ICON: undefined,
}));

const uiState = { setShowActivity: vi.fn() };

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { LeftSidebar } from './left-sidebar';

afterEach(cleanup);

describe('LeftSidebar memo entry', () => {
  it('leaves memos to the right sidebar', () => {
    render(<LeftSidebar />);
    expect(screen.queryByRole('button', { name: 'tabs.memo' })).toBeNull();
  });
});

describe('LeftSidebar brand row', () => {
  it('shows the deployment name above the calendar list', () => {
    render(<LeftSidebar />);
    expect(screen.getByText('Acme')).toBeInTheDocument();
  });
});
