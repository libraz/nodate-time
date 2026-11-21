import { renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/client';
import type { CalendarEvent } from '@/types/calendar';

const get = vi.fn();
vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  client: {
    // biome-ignore lint/style/useNamingConvention: mirrors the HTTP method name of the client
    GET: (...args: unknown[]) => get(...args),
  },
}));

import { useUiStore } from '@/stores/ui-store';
import { searchEvents, searchResultLabel, useEventSearch } from './search';

const item = { id: 'e1', title: 'Rehearsal' } as CalendarEvent;

afterEach(() => {
  get.mockReset();
});

describe('searchEvents', () => {
  it('asks the server with the trimmed query and returns the envelope', async () => {
    get.mockResolvedValue({ data: { items: [item], hasMore: true } });

    const page = await searchEvents('  rehearsal  ');

    expect(get).toHaveBeenCalledWith('/search', { params: { query: { q: 'rehearsal' } } });
    expect(page).toEqual({ items: [item], hasMore: true });
  });

  it('reads a null item list as no matches', async () => {
    get.mockResolvedValue({ data: { items: null, hasMore: false } });

    expect(await searchEvents('x')).toEqual({ items: [], hasMore: false });
  });
});

describe('useEventSearch', () => {
  it('stays idle and sends nothing for a blank query', () => {
    const { result } = renderHook(() => useEventSearch('   '));

    expect(result.current).toEqual({ status: 'idle' });
    expect(get).not.toHaveBeenCalled();
  });

  it('settles on the page the server returned', async () => {
    get.mockResolvedValue({ data: { items: [item], hasMore: true } });

    const { result } = renderHook(() => useEventSearch('reh'));

    await waitFor(() =>
      expect(result.current).toEqual({ status: 'done', items: [item], hasMore: true }),
    );
  });

  it('reports a query that matches too much with its own message', async () => {
    useUiStore.setState({ locale: 'en' });
    get.mockRejectedValue(new ApiError(422, 'too broad', 'SEARCH.TOO_BROAD'));

    const { result } = renderHook(() => useEventSearch('a'));

    await waitFor(() => expect(result.current.status).toBe('error'));
    expect(result.current).toMatchObject({
      message: 'Too many matches. Add a keyword to narrow it down',
    });
  });
});

describe('searchResultLabel', () => {
  it('names an all-day event by its own date only', () => {
    const evt = {
      ...item,
      allDay: true,
      startAt: '2026-08-05T00:00:00Z',
      endAt: '2026-08-06T00:00:00Z',
      startDate: '2026-08-05',
      endDate: '2026-08-06',
      timezone: 'Asia/Tokyo',
    } as CalendarEvent;

    expect(searchResultLabel(evt, 'America/New_York', 'en')).toBe('2026/08/05 (Wed)');
  });
});
