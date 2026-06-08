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

/** Returns true when an event spans more than one calendar day in the given zone. */
export function isMultiDay(evt: CalendarEvent, zone: string): boolean {
  const s = fromISOInZone(evt.startAt, zone).startOf('day');
  const e = fromISOInZone(evt.endAt, zone).startOf('day');
  return e > s;
}

/**
 * Lays out multi-day events for a single Sunday-aligned week into non-overlapping
 * horizontal tracks, returning each event's column span and track index.
 */
export function layoutWeek(
  weekStart: DateTime,
  events: CalendarEvent[],
  zone: string,
): PositionedEvent[] {
  const weekEnd = weekStart.plus({ days: 6 }).endOf('day');
  const tracks: { end: number }[] = [];
  const positioned: PositionedEvent[] = [];

  const sorted = [...events].sort((a, b) => {
    const aMulti = isMultiDay(a, zone);
    const bMulti = isMultiDay(b, zone);
    if (aMulti !== bMulti) return aMulti ? -1 : 1;
    return fromISOInZone(a.startAt, zone).toMillis() - fromISOInZone(b.startAt, zone).toMillis();
  });

  for (const evt of sorted) {
    const evtStart = fromISOInZone(evt.startAt, zone);
    const evtEnd = fromISOInZone(evt.endAt, zone);

    if (evtEnd < weekStart || evtStart > weekEnd) continue;

    const visStart = evtStart < weekStart ? weekStart : evtStart.startOf('day');
    const visEnd = evtEnd > weekEnd ? weekEnd : evtEnd.startOf('day');

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
      continuesLeft: evtStart < weekStart,
      continuesRight: evtEnd > weekEnd,
    });
  }

  return positioned;
}
