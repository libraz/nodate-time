import { cleanup, render, screen, waitFor } from '@testing-library/react';
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

import { api } from '@/lib/api';
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
    expect(screen.getByDisplayValue('Groceries')).toHaveAttribute('readonly');
    expect(screen.getByDisplayValue('milk')).toHaveAttribute('readonly');
  });

  it('offers saving and deleting to an editor', () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={() => {}} />);

    expect(screen.getByRole('button', { name: 'common.save' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'common.delete' })).toBeInTheDocument();
    expect(screen.getByDisplayValue('Groceries')).not.toHaveAttribute('readonly');
  });

  // The form used to render once per layout, so the history loaded twice and
  // the title focus could land on the copy CSS was hiding.
  it('renders the form once and loads its history once', async () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    vi.mocked(api.get).mockClear();
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={() => {}} />);

    expect(screen.getAllByDisplayValue('Groceries')).toHaveLength(1);
    await waitFor(() => expect(api.get).toHaveBeenCalledTimes(1));
  });
});
