import type { DateTime } from 'luxon';

export interface ZonedEventLike {
  allDay: boolean;
  timezone?: string | null;
}

/**
 * The zone an event's calendar days are measured in.
 *
 * An all-day event is a span of dates, not of instants. It is stored as
 * midnight-to-midnight in the timezone it was created in, and those dates are
 * what it means: a birthday on 5 August is on 5 August wherever it is read.
 * Measuring it in the reader's zone turns a one-day event created in Tokyo
 * into a two-day bar starting on the 4th for a reader in New York.
 *
 * A timed event is the opposite. It names an instant, and the reader's own
 * zone is where they need to see it, so that is what they get.
 */
export function eventDayZone(evt: ZonedEventLike, viewerZone: string): string {
  if (evt.allDay && evt.timezone) return evt.timezone;
  return viewerZone;
}

/**
 * Re-expresses a calendar day in the grid's zone, keeping the date.
 *
 * Days measured in an event's own zone begin at a different instant from the
 * grid's columns, so comparing them directly puts an event a column off. The
 * date is what both sides agree on, so that is what is carried across.
 */
export function toGridDay(day: DateTime, gridZone: string): DateTime {
  return day
    .startOf('day')
    .setZone(gridZone && gridZone.length > 0 ? gridZone : 'local', { keepLocalTime: true })
    .startOf('day');
}
