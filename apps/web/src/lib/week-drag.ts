import { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';
import type { CalendarEvent } from '@/types/calendar';

const MINUTES_PER_DAY = 24 * 60;

/**
 * The start a timed event moves to when its block is dragged on the week grid.
 *
 * The event moves by exactly what the pointer did: whole columns between the
 * grabbed day and the drop day, plus the vertical travel snapped to
 * `snapMinutes`, both on the wall clock. Measuring from the pointer rather
 * than from the block's position keeps the result the same whichever child of
 * the block was pressed and whichever day's segment of an overnight event was
 * grabbed. Null when the drop is where the drag began.
 */
export function weekDropStart(params: {
  evt: CalendarEvent;
  originKey: string;
  targetKey: string;
  travelPx: number;
  hourHeight: number;
  snapMinutes: number;
  zone: string;
}): DateTime | null {
  const from = DateTime.fromISO(params.originKey, { zone: 'UTC' });
  const to = DateTime.fromISO(params.targetKey, { zone: 'UTC' });
  const days = Math.round(to.diff(from, 'days').days);
  const raw = (params.travelPx / params.hourHeight) * 60;
  const minutes = Math.round(raw / params.snapMinutes) * params.snapMinutes;
  if (days === 0 && minutes === 0) return null;

  const start = fromISOInZone(params.evt.startAt, params.zone);
  const wall = start.hour * 60 + start.minute + minutes;
  const carry = Math.floor(wall / MINUTES_PER_DAY);
  const inDay = wall - carry * MINUTES_PER_DAY;
  return start
    .plus({ days: days + carry })
    .set({ hour: Math.floor(inDay / 60), minute: inDay % 60 });
}
