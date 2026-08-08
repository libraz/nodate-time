import type { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';
import { atMinutesIntoDay, minutesIntoDay } from '@/lib/event-times';
import type { CalendarEvent } from '@/types/calendar';

export interface TimedLayout {
  event: CalendarEvent;
  lane: number;
  laneCount: number;
  leftPct: number;
  widthPct: number;
}

interface Segment {
  event: CalendarEvent;
  start: number;
  end: number;
}

/**
 * The least time a block is drawn across.
 *
 * A block has a minimum height, so anything shorter than this -- a marker at a
 * single moment above all -- covers more of the day on screen than it holds in
 * time. Lanes exist to stop one block being drawn over another, so they are
 * packed by the room a block takes rather than by the time it occupies. The
 * view derives its own height floor from this figure, so the two cannot drift.
 */
export const MIN_RENDERED_MINUTES = 25;

/**
 * Packs same-day timed events into horizontal lanes so overlapping blocks remain
 * visible and clickable.
 *
 * An event whose end equals its start is a marker at a moment rather than a
 * span, and belongs to the day it opens: it is not late enough for the day that
 * closes on it, which is how the API attributes one too.
 */
export function layoutTimedEventsForDay(
  events: CalendarEvent[],
  dayStart: DateTime,
  zone: string,
): TimedLayout[] {
  const dayStartMs = dayStart.startOf('day').toMillis();
  const dayEndMs = dayStart.startOf('day').plus({ days: 1 }).toMillis();
  const minExtentMs = MIN_RENDERED_MINUTES * 60_000;
  const segments = events
    .map((event) => ({
      event,
      start: fromISOInZone(event.startAt, zone).toMillis(),
      end: fromISOInZone(event.endAt, zone).toMillis(),
    }))
    // Which events this column draws. A span has to reach into the day; a
    // marker has only an instant to place, and the day it opens is the one.
    .filter(({ start, end }) =>
      end === start
        ? start >= dayStartMs && start < dayEndMs
        : start < dayEndMs && end > dayStartMs,
    )
    .map(({ event, start, end }) => {
      const from = Math.max(start, dayStartMs);
      return {
        event,
        start: from,
        end: Math.max(Math.min(end, dayEndMs), from + minExtentMs),
      };
    })
    .sort((a, b) => a.start - b.start || b.end - a.end);

  const out: TimedLayout[] = [];
  let i = 0;
  while (i < segments.length) {
    const cluster: Segment[] = [];
    let clusterEnd = segments[i]?.end ?? 0;
    while (i < segments.length) {
      const cur = segments[i];
      if (!cur) break;
      if (cluster.length > 0 && cur.start >= clusterEnd) break;
      cluster.push(cur);
      clusterEnd = Math.max(clusterEnd, cur.end);
      i++;
    }

    const laneEnds: number[] = [];
    const assigned = cluster.map((seg) => {
      let lane = laneEnds.findIndex((end) => end <= seg.start);
      if (lane < 0) {
        lane = laneEnds.length;
        laneEnds.push(seg.end);
      } else {
        laneEnds[lane] = seg.end;
      }
      return { seg, lane };
    });
    const laneCount = Math.max(1, laneEnds.length);
    for (const item of assigned) {
      out.push({
        event: item.seg.event,
        lane: item.lane,
        laneCount,
        leftPct: (item.lane / laneCount) * 100,
        widthPct: 100 / laneCount,
      });
    }
  }

  return out;
}

export function resizedEndForDaySegment(params: {
  eventStartISO: string;
  dayStart: DateTime;
  clientY: number;
  colTop: number;
  hourHeight: number;
  snapMinutes: number;
  minDurationMinutes: number;
  zone: string;
}): DateTime {
  const rawMin = ((params.clientY - params.colTop) / params.hourHeight) * 60;
  const snapped = Math.round(rawMin / params.snapMinutes) * params.snapMinutes;
  const eventStart = fromISOInZone(params.eventStartISO, params.zone);
  // Wall-clock minutes, matching the rules the cursor is being dragged to. The
  // elapsed-time form lands an hour away on a day that gains or loses one, and
  // this is a write path: the wrong end time is what gets stored.
  const startOffset = minutesIntoDay(eventStart, params.dayStart);
  const minEnd = Math.max(startOffset + params.minDurationMinutes, params.minDurationMinutes);
  const endMin = Math.min(Math.max(snapped, minEnd), 24 * 60);
  return atMinutesIntoDay(params.dayStart, endMin);
}
