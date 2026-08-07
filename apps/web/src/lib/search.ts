import type { CalendarEvent } from '@/types/calendar';

/**
 * Events matching a free-text query, restricted to the calendars currently
 * shown.
 *
 * The store keeps every calendar's events regardless of which ones are shown,
 * so searching all of them surfaces entries from a calendar the reader has
 * switched off -- and following one lands on a day where it is not drawn. The
 * shown set is required rather than defaulted: a caller that forgot it and a
 * caller that meant every calendar must not be the same expression.
 */
export function filterEventsForSearch(
  events: CalendarEvent[],
  activeCalendarIds: string[],
  query: string,
): CalendarEvent[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  return events.filter(
    (evt) =>
      activeCalendarIds.includes(evt.calendarId) &&
      [evt.title, evt.location, evt.memo].some((field) => field.toLowerCase().includes(q)),
  );
}
