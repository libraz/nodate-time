import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  errorMessage: (e: unknown) => String(e),
}));

// The typed client is replaced by an adapter that reports the request made as
// a path with its parameters filled in, so assertions read as URLs.
const raw = vi.hoisted(() => ({ get: vi.fn() }));

type Init = { params?: { path?: Record<string, string>; query?: Record<string, string> } };

vi.mock('@/lib/client', () => ({
  client: {
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    GET: async (path: string, init?: Init) => {
      const filled = path.replace(
        /\{(\w+)\}/g,
        (_, key: string) => init?.params?.path?.[key] ?? '',
      );
      const query = new URLSearchParams(init?.params?.query).toString();
      return { data: await raw.get(query ? `${filled}?${query}` : filled) };
    },
  },
}));

import { useCalendarPages } from './use-calendar-pages';

const mockGet = raw.get;

function deferred<T>() {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('useCalendarPages', () => {
  it('shows nothing of the previous calendar while the next one loads', async () => {
    const next = deferred<unknown>();
    mockGet
      .mockResolvedValueOnce({ items: ['a1'], nextCursor: 'c1' })
      .mockReturnValueOnce(next.promise as never);
    const { result, rerender } = renderHook(
      ({ id }) => useCalendarPages<string>(id, 'activity?limit=50', true),
      { initialProps: { id: 'cal-a' } },
    );
    await waitFor(() => expect(result.current.items).toEqual(['a1']));

    rerender({ id: 'cal-b' });

    expect(result.current.items).toEqual([]);
    expect(result.current.nextCursor).toBe('');
    await act(async () => next.resolve({ items: ['b1'] }));
    expect(result.current.items).toEqual(['b1']);
  });

  it('drops a page of the previous calendar that arrives after the switch', async () => {
    const lateA = deferred<unknown>();
    mockGet
      .mockResolvedValueOnce({ items: ['a1'], nextCursor: 'c1' })
      .mockReturnValueOnce(lateA.promise as never)
      .mockResolvedValueOnce({ items: ['b1'] });
    const { result, rerender } = renderHook(
      ({ id }) => useCalendarPages<string>(id, 'activity?limit=50', true),
      { initialProps: { id: 'cal-a' } },
    );
    await waitFor(() => expect(result.current.items).toEqual(['a1']));

    let more: Promise<void> = Promise.resolve();
    act(() => {
      more = result.current.loadMore();
    });
    expect(mockGet).toHaveBeenLastCalledWith('/calendars/cal-a/activity?limit=50&cursor=c1');
    rerender({ id: 'cal-b' });
    await waitFor(() => expect(result.current.items).toEqual(['b1']));

    await act(async () => {
      lateA.resolve({ items: ['a2'] });
      await more;
    });
    expect(result.current.items).toEqual(['b1']);
  });

  it('drops a first page superseded by a newer reload', async () => {
    const slowA = deferred<unknown>();
    mockGet.mockReturnValueOnce(slowA.promise as never).mockResolvedValueOnce({ items: ['b1'] });
    const { result, rerender } = renderHook(
      ({ id }) => useCalendarPages<string>(id, 'albums', true),
      { initialProps: { id: 'cal-a' } },
    );

    rerender({ id: 'cal-b' });
    await waitFor(() => expect(result.current.items).toEqual(['b1']));
    await act(async () => slowA.resolve({ items: ['a1'] }));

    expect(result.current.items).toEqual(['b1']);
  });
});
