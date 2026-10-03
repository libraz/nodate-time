import { useCallback, useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '@/lib/api';

interface Page<T> {
  items: T[];
  nextCursor?: string;
}

interface Loaded<T> {
  calendarId: string;
  items: T[];
  nextCursor: string;
  /** The cursor each page on screen was read from, in order; '' is the first. */
  cursors: string[];
}

const NOTHING: Loaded<never> = { calendarId: '', items: [], nextCursor: '', cursors: [] };

export interface CalendarPages<T> {
  /** What has been read for the current calendar; never another calendar's. */
  items: T[];
  nextCursor: string;
  cursors: string[];
  loading: boolean;
  loadingMore: boolean;
  error: string | null;
  setError: (error: string | null) => void;
  reload: () => Promise<void>;
  loadMore: () => Promise<void>;
  /** Changes the items read for `calendarId`, and does nothing once it is no longer current. */
  updateItems: (calendarId: string, update: (items: T[]) => T[]) => void;
}

/**
 * A cursor-paged list under `/calendars/{id}/{path}`, read for one calendar at
 * a time.
 *
 * Everything held is keyed by the calendar it was read for, and a response is
 * applied only while that calendar is still the one asked about: switching
 * calendars while a page is in flight must not leave one calendar's entries
 * under another's name, nor append them to its list.
 */
export function useCalendarPages<T>(
  calendarId: string,
  path: string,
  enabled: boolean,
): CalendarPages<T> {
  const [loaded, setLoaded] = useState<Loaded<T>>(NOTHING);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Bumped by every reload and on unmount; a response from an older one is dropped.
  const generation = useRef(0);

  const url = useCallback(
    (cursor: string) => {
      const query = cursor
        ? `${path.includes('?') ? '&' : '?'}cursor=${encodeURIComponent(cursor)}`
        : '';
      return `/calendars/${calendarId}/${path}${query}`;
    },
    [calendarId, path],
  );

  const reload = useCallback(async () => {
    const run = ++generation.current;
    setError(null);
    if (!calendarId) {
      setLoaded(NOTHING);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const data = await api.get<Page<T>>(url(''));
      if (run !== generation.current) return;
      setLoaded({
        calendarId,
        items: data.items ?? [],
        nextCursor: data.nextCursor ?? '',
        cursors: [''],
      });
    } catch (e) {
      if (run === generation.current) setError(errorMessage(e));
    } finally {
      if (run === generation.current) setLoading(false);
    }
  }, [calendarId, url]);

  const current = loaded.calendarId === calendarId;
  const nextCursor = current ? loaded.nextCursor : '';

  const loadMore = useCallback(async () => {
    if (!nextCursor || loadingMore) return;
    const run = generation.current;
    setLoadingMore(true);
    setError(null);
    try {
      const data = await api.get<Page<T>>(url(nextCursor));
      if (run !== generation.current) return;
      setLoaded((prev) =>
        prev.calendarId === calendarId
          ? {
              ...prev,
              items: [...prev.items, ...(data.items ?? [])],
              nextCursor: data.nextCursor ?? '',
              cursors: [...prev.cursors, nextCursor],
            }
          : prev,
      );
    } catch (e) {
      if (run === generation.current) setError(errorMessage(e));
    } finally {
      setLoadingMore(false);
    }
  }, [calendarId, nextCursor, loadingMore, url]);

  const updateItems = useCallback((forCalendarId: string, update: (items: T[]) => T[]) => {
    setLoaded((prev) =>
      prev.calendarId === forCalendarId ? { ...prev, items: update(prev.items) } : prev,
    );
  }, []);

  useEffect(() => {
    if (enabled) reload();
  }, [enabled, reload]);

  useEffect(
    () => () => {
      generation.current++;
    },
    [],
  );

  return {
    items: current ? loaded.items : [],
    nextCursor,
    cursors: current ? loaded.cursors : [],
    loading,
    loadingMore,
    error,
    setError,
    reload,
    loadMore,
    updateItems,
  };
}
