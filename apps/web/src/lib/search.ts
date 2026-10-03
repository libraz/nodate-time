import { DateTime } from 'luxon';
import type { Locale } from '@/i18n';
import { fetchWindow, formatMonthYear, formatTime } from '@/lib/date-utils';
import { eventStartDay } from '@/lib/week-layout';
import type { CalendarEvent, CalendarView } from '@/types/calendar';

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

/**
 * The months a search actually covered: the window the view loaded, since the
 * search runs over loaded events only. An empty result is stated against this
 * rather than as if nothing existed anywhere.
 */
export function searchedMonths(
  view: CalendarView,
  month: DateTime,
  locale: Locale,
): { from: string; to: string } {
  const { start, end } = fetchWindow(view, month);
  const label = (iso: string) => formatMonthYear(DateTime.fromISO(iso), locale);
  return { from: label(start), to: label(end) };
}
