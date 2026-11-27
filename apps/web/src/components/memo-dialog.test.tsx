import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/client', async (importOriginal) => {
  class ApiError extends Error {
    constructor(
      public status: number,
      detail: string,
      public code = '',
    ) {
      super(detail);
    }
  }
  return {
    ...(await importOriginal<typeof import('@/lib/client')>()),
    // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
    ApiError,
    isStaleError: (e: unknown) => e instanceof ApiError && e.status === 412,
    errorMessage: (e: unknown) => String(e),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    client: { GET: vi.fn().mockResolvedValue({ data: [] }) },
  };
});

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

const calendarState = {
  calendars: [{ id: 'cal-1', role: 'reader' }],
  memos: [{ id: 'memo-1', calendarId: 'cal-1', title: 'Groceries', body: 'milk', done: false }],
  addMemo: vi.fn(),
  updateMemo: vi.fn(),
  deleteMemo: vi.fn(),
  fetchMemos: vi.fn(),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { ApiError, client } from '@/lib/client';
import { toast } from '@/lib/toast';
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

  it('closes at once when nothing was edited', () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    const onClose = vi.fn();
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={onClose} />);

    fireEvent.click(screen.getByRole('button', { name: 'common.cancel' }));

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('asks before discarding edits on any dismiss', () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    const onClose = vi.fn();
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={onClose} />);
    fireEvent.change(screen.getByDisplayValue('Groceries'), { target: { value: 'Errands' } });

    fireEvent.click(screen.getByRole('button', { name: 'common.cancel' }));
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'common.keepEditing' }));
    expect(screen.queryByRole('alertdialog')).toBeNull();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'common.discard' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('saves on Ctrl+Enter from the body field, but not a plain Enter', async () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    calendarState.updateMemo.mockResolvedValueOnce(undefined);
    const onClose = vi.fn();
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={onClose} />);
    const body = screen.getByDisplayValue('milk');

    fireEvent.keyDown(body, { key: 'Enter' });
    expect(calendarState.updateMemo).not.toHaveBeenCalled();

    fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(calendarState.updateMemo).toHaveBeenCalledTimes(1);
  });

  it('asks before deleting the memo', () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={() => {}} />);

    fireEvent.click(screen.getByRole('button', { name: 'common.delete' }));

    expect(calendarState.deleteMemo).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  });

  // The form used to render once per layout, so the history loaded twice and
  // the title focus could land on the copy CSS was hiding.
  it('renders the form once and loads its history once', async () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    vi.mocked(client.GET).mockClear();
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={() => {}} />);

    expect(screen.getAllByDisplayValue('Groceries')).toHaveLength(1);
    await waitFor(() => expect(client.GET).toHaveBeenCalledTimes(1));
  });

  // A save refused as stale is not retried with a newer version: the reader is
  // told, and the memo list is read again.
  it('reports a stale save and refetches instead of retrying', async () => {
    calendarState.calendars = [{ id: 'cal-1', role: 'editor' }];
    calendarState.updateMemo.mockRejectedValueOnce(new ApiError(412, 'stale', 'MEMO.STALE'));
    render(<MemoDialog calendarId="cal-1" memoId="memo-1" onClose={() => {}} />);

    fireEvent.change(screen.getByDisplayValue('milk'), { target: { value: 'eggs' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('conflict.stale'));
    expect(calendarState.updateMemo).toHaveBeenCalledTimes(1);
    expect(calendarState.fetchMemos).toHaveBeenCalledTimes(1);
  });
});
