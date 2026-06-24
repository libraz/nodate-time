import type { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';
import type { CalendarEvent } from '@/types/calendar';

/** Maximum number of event tracks rendered per day cell before collapsing into a "+N" overflow. */
export const MAX_VISIBLE_TRACKS = 3;

export interface PositionedEvent {
  event: CalendarEvent;
  startCol: number;
  span: number;
  track: number;
  continuesLeft: boolean;
  continuesRight: boolean;
}

/**
 * Returns the inclusive last calendar day an event occupies in `zone`.
 *
 * End times are stored exclusively (e.g. an all-day event ending on the 16th is
 * stored as the 17th at 00:00, and a timed event from 23:00 to 00:00 ends at the
 * next day's midnight). An end that lands exactly on midnight therefore belongs to
 * the previous day and must not bleed the event onto the next cell.
 */
export function eventEndDay(evt: CalendarEvent, zone: string): DateTime {
  const start = fromISOInZone(evt.startAt, zone);
  const end = fromISOInZone(evt.endAt, zone);
  const endDay = end.startOf('day');
  if (+end === +endDay && end > start) return endDay.minus({ days: 1 });
  return endDay;
}

/** Returns true when an event spans more than one calendar day in the given zone. */
export function isMultiDay(evt: CalendarEvent, zone: string): boolean {
  const startDay = fromISOInZone(evt.startAt, zone).startOf('day');
  return eventEndDay(evt, zone) > startDay;
}

/**
 * Lays out multi-day events for a single Sunday-aligned week into non-overlapping
 * horizontal tracks, returning each event's column span and track index. Single-day
 * events are intentionally excluded — callers render those as per-day chips.
 */
export function layoutWeek(
  weekStart: DateTime,
  events: CalendarEvent[],
  zone: string,
): PositionedEvent[] {
  const weekEnd = weekStart.plus({ days: 6 }); // start of the week's Saturday
  const tracks: { end: number }[] = [];
  const positioned: PositionedEvent[] = [];

  const multiDay = events
    .filter((evt) => isMultiDay(evt, zone))
    .sort(
      (a, b) =>
        fromISOInZone(a.startAt, zone).toMillis() - fromISOInZone(b.startAt, zone).toMillis(),
    );

  for (const evt of multiDay) {
    const startDay = fromISOInZone(evt.startAt, zone).startOf('day');
    const endDay = eventEndDay(evt, zone);

    if (endDay < weekStart || startDay > weekEnd) continue;

    const visStart = startDay < weekStart ? weekStart : startDay;
    const visEnd = endDay > weekEnd ? weekEnd : endDay;

    const startCol = Math.max(0, Math.floor(visStart.diff(weekStart, 'days').days));
    const endCol = Math.min(6, Math.floor(visEnd.diff(weekStart, 'days').days));
    const span = Math.max(1, endCol - startCol + 1);

    let track = tracks.findIndex((tr) => tr.end < startCol);
    if (track < 0) {
      track = tracks.length;
      tracks.push({ end: endCol });
    } else {
      tracks[track] = { end: endCol };
    }

    positioned.push({
      event: evt,
      startCol,
      span,
      track,
      continuesLeft: startDay < weekStart,
      continuesRight: endDay > weekEnd,
    });
  }

  return positioned;
}
