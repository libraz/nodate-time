import type { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';
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
 * Packs same-day timed events into horizontal lanes so overlapping blocks remain
 * visible and clickable.
 */
export function layoutTimedEventsForDay(
  events: CalendarEvent[],
  dayStart: DateTime,
  zone: string,
): TimedLayout[] {
  const dayStartMs = dayStart.startOf('day').toMillis();
  const dayEndMs = dayStart.startOf('day').plus({ days: 1 }).toMillis();
  const segments = events
    .map((event) => ({
      event,
      start: Math.max(fromISOInZone(event.startAt, zone).toMillis(), dayStartMs),
      end: Math.min(fromISOInZone(event.endAt, zone).toMillis(), dayEndMs),
    }))
    .filter((s) => s.end > s.start)
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
  const startOffset = eventStart.diff(params.dayStart, 'minutes').minutes;
  const minEnd = Math.max(startOffset + params.minDurationMinutes, params.minDurationMinutes);
  const endMin = Math.min(Math.max(snapped, minEnd), 24 * 60);
  return params.dayStart.plus({ minutes: endMin });
}
