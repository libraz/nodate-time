import type { DateTime } from 'luxon';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { client, isAbortError } from '@/lib/client';
import { getMonthDays, gridRange, nowInZone, type WeekStart } from '@/lib/date-utils';
import {
  type PublicEvent,
  publicDayEvent,
  type ShareFailure,
  shareFailureOf,
} from '@/lib/public-calendar';
import { bucketEventsByDay, dayKey } from '@/lib/week-layout';

export interface PublicEvents {
  /** The events read for the month and zone on screen; never another month's. */
  events: PublicEvent[];
  /** The server stopped the month's listing at its cap; the latest events are missing. */
  hasMore: boolean;
  failure: ShareFailure | null;
  retry: () => void;
}

/**
 * The events a published link shows for one month, read in the viewer's zone.
 *
 * A request the page has moved past is cancelled, and its answer could not be
 * drawn anyway: what is held is keyed by the range it was read for. A failure
 * is reported as one rather than drawn as a month with nothing in it.
 */
export function usePublicEvents(
  token: string,
  enabled: boolean,
  month: DateTime,
  timezone: string,
  weekStart: WeekStart = 'sun',
): PublicEvents {
  // The grid draws part of the neighbouring months, and the dates are days in
  // the zone this page renders -- both have to be said, or the edge cells stay
  // empty and an event near a month boundary lands in the wrong one.
  const { start, end } = gridRange(month, weekStart, timezone);
  const key = `${token}|${start}|${end}|${timezone}`;
  const [loaded, setLoaded] = useState<{
    key: string;
    events: PublicEvent[];
    hasMore: boolean;
  }>({ key: '', events: [], hasMore: false });
  const [failure, setFailure] = useState<ShareFailure | null>(null);
  const [attempt, setAttempt] = useState(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: `attempt` is how a retry asks again
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    setFailure(null);
    client
      .GET('/share/{token}/events', {
        params: { path: { token }, query: { start, end, tz: timezone } },
        signal: controller.signal,
      })
      .then(({ data }) => {
        if (controller.signal.aborted) return;
        setLoaded({ key, events: data?.items ?? [], hasMore: data?.hasMore ?? false });
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted && !isAbortError(e)) setFailure(shareFailureOf(e));
      });
    return () => controller.abort();
  }, [enabled, key, token, start, end, timezone, attempt]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  const current = loaded.key === key;
  return {
    events: current ? loaded.events : [],
    hasMore: current && loaded.hasMore,
    failure,
    retry,
  };
}

/**
 * The month a published page shows, opening on the current month in `timezone`.
 *
 * Held as a distance from today's month rather than a date, so a zone that
 * changes before the first navigation reopens on its own current month.
 */
export function usePublicMonth(timezone: string): {
  month: DateTime;
  shiftMonth: (delta: number) => void;
} {
  const [offset, setOffset] = useState(0);
  const month = useMemo(
    () => nowInZone(timezone).startOf('month').plus({ months: offset }),
    [timezone, offset],
  );
  const shiftMonth = useCallback((delta: number) => setOffset((n) => n + delta), []);
  return { month, shiftMonth };
}

/** The week rows of a month grid in `timezone`, with each day's events bucketed once. */
export function usePublicMonthGrid(
  events: PublicEvent[],
  month: DateTime,
  timezone: string,
  weekStart: WeekStart = 'sun',
): { weeks: DateTime[][]; eventsOn: (day: DateTime) => PublicEvent[] } {
  return useMemo(() => {
    const days = getMonthDays(month.year, month.month - 1, weekStart, timezone);
    const weeks: DateTime[][] = [];
    for (let i = 0; i < days.length; i += 7) weeks.push(days.slice(i, i + 7));
    const first = days[0];
    const last = days[days.length - 1];
    const buckets: Map<string, PublicEvent[]> =
      first && last
        ? bucketEventsByDay(events.map(publicDayEvent), timezone, first, last)
        : new Map();
    return { weeks, eventsOn: (day) => buckets.get(dayKey(day, timezone)) ?? [] };
  }, [events, month, timezone, weekStart]);
}
