import type { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';
import { eventDayZone, toGridDay } from '@/lib/event-zone';
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
 * Returns the first calendar day an event occupies, expressed in the grid's
 * zone. All-day events are measured in their own zone (see `eventDayZone`).
 */
export function eventStartDay(evt: CalendarEvent, zone: string): DateTime {
  const own = eventDayZone(evt, zone);
  return toGridDay(fromISOInZone(evt.startAt, own), zone);
}

/**
 * Returns the inclusive last calendar day an event occupies, expressed in the
 * grid's zone.
 *
 * End times are stored exclusively (e.g. an all-day event ending on the 16th is
 * stored as the 17th at 00:00, and a timed event from 23:00 to 00:00 ends at the
 * next day's midnight). An end that lands exactly on midnight therefore belongs to
 * the previous day and must not bleed the event onto the next cell.
 */
export function eventEndDay(evt: CalendarEvent, zone: string): DateTime {
  const own = eventDayZone(evt, zone);
  const start = fromISOInZone(evt.startAt, own);
  const end = fromISOInZone(evt.endAt, own);
  const endDay = end.startOf('day');
  const inclusive = +end === +endDay && end > start ? endDay.minus({ days: 1 }) : endDay;
  return toGridDay(inclusive, zone);
}

/** Returns true when an event spans more than one calendar day in the given zone. */
export function isMultiDay(evt: CalendarEvent, zone: string): boolean {
  return eventEndDay(evt, zone) > eventStartDay(evt, zone);
}

/** Returns true when an event occupies the given calendar day. */
export function eventOccupiesDay(evt: CalendarEvent, day: DateTime, zone: string): boolean {
  const target = toGridDay(day, zone);
  return eventStartDay(evt, zone) <= target && eventEndDay(evt, zone) >= target;
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
  const gridStart = toGridDay(weekStart, zone);
  const weekEnd = gridStart.plus({ days: 6 }); // start of the week's Saturday
  const tracks: { end: number }[] = [];
  const positioned: PositionedEvent[] = [];

  // Earliest start first, and on a tie the longest bar first: only the last few
  // tracks of a crowded week are hidden, and the event that loses its track
  // should be the one that goes missing from the fewest cells.
  const multiDay = events
    .filter((evt) => isMultiDay(evt, zone))
    .sort(
      (a, b) =>
        +eventStartDay(a, zone) - +eventStartDay(b, zone) ||
        +eventEndDay(b, zone) - +eventEndDay(a, zone),
    );

  for (const evt of multiDay) {
    const startDay = eventStartDay(evt, zone);
    const endDay = eventEndDay(evt, zone);

    if (endDay < gridStart || startDay > weekEnd) continue;

    const visStart = startDay < gridStart ? gridStart : startDay;
    const visEnd = endDay > weekEnd ? weekEnd : endDay;

    // Rounded, not floored: a DST change inside the week makes one of these
    // days 23 hours long, and flooring would pull every later column back one.
    const startCol = Math.max(0, Math.round(visStart.diff(gridStart, 'days').days));
    const endCol = Math.min(6, Math.round(visEnd.diff(gridStart, 'days').days));
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
      continuesLeft: startDay < gridStart,
      continuesRight: endDay > weekEnd,
    });
  }

  return positioned;
}

/** What a single day cell renders, once its multi-day bars have claimed their tracks. */
export interface DayCellLayout {
  /** Tracks held by bars crossing this day, including tracks past the visible limit. */
  reserved: number[];
  /** Single-day chips placed in the visible tracks the bars left free. */
  singleSlots: { track: number; evt: CalendarEvent }[];
  /** How many of the day's events the cell cannot show. Never negative. */
  overflow: number;
}

/**
 * Places one day cell's events into the tracks it renders.
 *
 * Tracks are assigned across the whole week so that a bar keeps one row for
 * every day it spans, which means a bar can sit past the visible limit because
 * of congestion on days other than this one. The overflow count is therefore
 * taken from what this cell actually hides, not from the track index — on a day
 * whose crowd has already ended, that bar may be the only thing the cell holds.
 */
export function layoutDayCell(
  col: number,
  positioned: PositionedEvent[],
  singles: CalendarEvent[],
): DayCellLayout {
  const reserved: number[] = [];
  for (const p of positioned) {
    if (col >= p.startCol && col < p.startCol + p.span) reserved.push(p.track);
  }

  const used = new Set(reserved);
  const singleSlots: { track: number; evt: CalendarEvent }[] = [];
  let nextTrack = 0;
  for (const evt of singles) {
    while (used.has(nextTrack)) nextTrack++;
    if (nextTrack >= MAX_VISIBLE_TRACKS) break;
    singleSlots.push({ track: nextTrack, evt });
    used.add(nextTrack);
    nextTrack++;
  }

  const hiddenBars = reserved.filter((track) => track >= MAX_VISIBLE_TRACKS).length;
  return { reserved, singleSlots, overflow: hiddenBars + singles.length - singleSlots.length };
}
