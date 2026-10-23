import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const calendarListStub = vi.hoisted(() => () => null);
// biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
vi.mock('@/components/calendar-list', () => ({ CalendarList: calendarListStub }));

const memoStub = vi.hoisted(() => () => <p>memo body</p>);
// biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
vi.mock('@/components/right-panel', () => ({ MemoSection: memoStub }));

const uiState = { setShowActivity: vi.fn() };

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { LeftSidebar } from './left-sidebar';

afterEach(cleanup);

describe('LeftSidebar memo section', () => {
  it('names its collapse toggle and reports whether the section is open', () => {
    render(<LeftSidebar />);

    const toggle = screen.getByRole('button', { name: 'tabs.memo' });
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('memo body')).toBeInTheDocument();

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('memo body')).toBeNull();
  });
});
