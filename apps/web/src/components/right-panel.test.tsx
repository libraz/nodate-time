import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

const uiState = {
  theme: 'glass',
  colorMode: 'light',
  locale: 'ja',
  setTheme: vi.fn(),
  setColorMode: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState: {
  memos: unknown[];
  toggleMemo: ReturnType<typeof vi.fn>;
  deleteMemo: ReturnType<typeof vi.fn>;
  calendars: unknown[];
  activeCalendarIds: string[];
} = {
  memos: [],
  toggleMemo: vi.fn(),
  deleteMemo: vi.fn(),
  calendars: [],
  activeCalendarIds: [],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

const authState = { saveAccountPreference: vi.fn().mockResolvedValue(undefined) };

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { MemoSection } from './right-panel';

afterEach(() => {
  cleanup();
  calendarState.memos = [];
  calendarState.calendars = [];
  calendarState.activeCalendarIds = [];
  vi.clearAllMocks();
  vi.restoreAllMocks();
  document.body.style.overflow = '';
});

// With a single calendar on screen nothing named the calendar a memo lived in,
// so a shared memo read like a private note on the reader's account.
describe('MemoSection calendar scope', () => {
  beforeEach(() => {
    calendarState.calendars = [
      { id: 'cal-1', name: 'Family', color: '#f00', role: 'editor', memberCount: 2 },
    ];
    calendarState.activeCalendarIds = ['cal-1'];
  });

  it('names the calendar memos are shared in when only one is shown', () => {
    calendarState.memos = [{ id: 'm-1', calendarId: 'cal-1', title: 'Milk', done: false }];
    render(<MemoSection />);

    const group = screen.getByRole('region', { name: 'Family' });
    expect(within(group).getByText('Milk')).toBeTruthy();
  });

  it('names the calendar a new memo goes to when there is only one choice', () => {
    render(<MemoSection />);

    expect(screen.getByText('panel.postTo')).toBeTruthy();
    expect(screen.getByText('Family')).toBeTruthy();
  });
});
