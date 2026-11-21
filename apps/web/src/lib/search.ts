import type { DateTime } from 'luxon';
import { useEffect, useState } from 'react';
import { getT, type Locale } from '@/i18n';
import { ApiError, errorMessage } from '@/lib/api';
import { client } from '@/lib/client';
import { formatTime } from '@/lib/date-utils';
import { eventStartDay } from '@/lib/week-layout';
import type { CalendarEvent } from '@/types/calendar';

/** Longest query the API accepts, in code points; the box stops short of a certain 422. */
export const SEARCH_QUERY_MAX = 100;

/** Pause after the last keystroke before a query is sent. */
const SEARCH_DEBOUNCE_MS = 250;

/** One page of matches: the earliest upcoming occurrence of each, soonest first. */
export interface SearchPage {
  items: CalendarEvent[];
  /** More matches exist than were returned. */
  hasMore: boolean;
}

/** Searches every calendar the caller belongs to; a refused query rejects with ApiError. */
export async function searchEvents(query: string, signal?: AbortSignal): Promise<SearchPage> {
  const { data } = await client.GET('/search', {
    params: { query: { q: query.trim() } },
    ...(signal ? { signal } : {}),
  });
  return { items: (data?.items ?? []) as CalendarEvent[], hasMore: data?.hasMore ?? false };
}

export type SearchState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'done'; items: CalendarEvent[]; hasMore: boolean }
  | { status: 'error'; message: string };

/** Runs the debounced server search for `query`, dropping answers to superseded queries. */
export function useEventSearch(query: string): SearchState {
  const q = query.trim();
  const [state, setState] = useState<SearchState>({ status: 'idle' });

  useEffect(() => {
    if (q === '') {
      setState({ status: 'idle' });
      return;
    }
    setState({ status: 'loading' });
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      searchEvents(q, ctrl.signal).then(
        (page) => {
          if (!ctrl.signal.aborted) setState({ status: 'done', ...page });
        },
        (e: unknown) => {
          if (ctrl.signal.aborted) return;
          const tooBroad = e instanceof ApiError && e.code === 'SEARCH.TOO_BROAD';
          setState({
            status: 'error',
            message: tooBroad ? getT()('search.tooBroad') : errorMessage(e),
          });
        },
      );
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [q]);

  return state;
}

/**
 * The day a search result names and jumps to: the cell the grid draws the
 * event's start in, so an all-day event keeps its own date in any viewer zone.
 */
export function searchResultDay(evt: CalendarEvent, zone: string): DateTime {
  return eventStartDay(evt, zone);
}

/** The date line under a search result: the day, plus the viewer-clock times when timed. */
export function searchResultLabel(evt: CalendarEvent, zone: string, locale: Locale): string {
  const day = searchResultDay(evt, zone).toFormat('yyyy/MM/dd (EEE)', { locale });
  if (evt.allDay) return day;
  return `${day} ${formatTime(evt.startAt, zone)} - ${formatTime(evt.endAt, zone)}`;
}
