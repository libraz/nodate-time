import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn().mockResolvedValue({ items: [] }) },
  errorMessage: (e: unknown) => String(e),
}));

const calendarState = {
  calendars: [{ id: 'cal-1', role: 'reader' }],
  memos: [{ id: 'memo-1', calendarId: 'cal-1', title: 'Groceries', body: 'milk', done: false }],
  addMemo: vi.fn(),
  updateMemo: vi.fn(),
  deleteMemo: vi.fn(),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { MemoDialog } from './memo-dialog';

afterEach(() => {
  cleanup();
  calendarState.calendars = [{ id: 'cal-1', role: 'reader' }];
  document.body.style.overflow = '';
});

describe('MemoDialog', () => {
  // A reader could open a memo and was offered Save and Delete, which the
  // server then refused after they had typed their change.
  it('opens read-only for a reader', () => {
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={() => {}} />);

    expect(screen.queryByRole('button', { name: 'common.save' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'common.delete' })).toBeNull();
    // Both layouts render the form; CSS shows one.
    for (const field of [
      ...screen.getAllByDisplayValue('Groceries'),
      ...screen.getAllByDisplayValue('milk'),
    ]) {
      expect(field).toHaveAttribute('readonly');
    }
  });

  it('offers saving and deleting to an editor', () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={() => {}} />);

    expect(screen.getAllByRole('button', { name: 'common.save' }).length).toBeGreaterThan(0);
    expect(screen.getAllByRole('button', { name: 'common.delete' }).length).toBeGreaterThan(0);
    for (const field of screen.getAllByDisplayValue('Groceries')) {
      expect(field).not.toHaveAttribute('readonly');
    }
  });
});
