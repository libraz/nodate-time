import { fromISOInZone } from '@/lib/date-utils';
import { eventEndDay, eventStartDay } from '@/lib/week-layout';
import type { CalendarEvent } from '@/types/calendar';

/** Guards against a malformed range turning one event into an endless list. */
const MAX_SPAN_DAYS = 366;

/**
 * Groups events into the days an agenda lists them under, earliest day first.
 *
 * An event appears under every day it covers rather than only the one it
 * began on. A trip that started last Tuesday is still on today, and a list
 * that files it under Tuesday alone says today is free.
 */
export function groupByDay(
  events: CalendarEvent[],
  activeCalendarIds: string[],
  timezone: string,
): [string, CalendarEvent[]][] {
  const map = new Map<string, CalendarEvent[]>();
  const filtered = events
    .filter((e) => activeCalendarIds.includes(e.calendarId))
    .sort(
      (a, b) =>
        fromISOInZone(a.startAt, timezone).toMillis() -
        fromISOInZone(b.startAt, timezone).toMillis(),
    );
  for (const evt of filtered) {
    const end = eventEndDay(evt, timezone);
    let day = eventStartDay(evt, timezone);
    for (let i = 0; day <= end && i < MAX_SPAN_DAYS; i++) {
      const key = day.toFormat('yyyy-MM-dd');
      const arr = map.get(key) ?? [];
      arr.push(evt);
      map.set(key, arr);
      day = day.plus({ days: 1 });
    }
  }
  return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
}
