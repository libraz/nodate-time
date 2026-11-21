import type { DateTime } from 'luxon';

export interface ZonedEventLike {
  allDay: boolean;
}

/**
 * The zone an event's calendar days are measured in.
 *
 * An all-day event is a span of dates, not of instants: its instants are UTC
 * midnights and its dates mean the same thing wherever it is read. Measuring
 * it in the reader's zone turns a one-day event into a two-day bar for a
 * reader west of UTC.
 *
 * A timed event is the opposite. It names an instant, and the reader's own
 * zone is where they need to see it, so that is what they get.
 */
export function eventDayZone(evt: ZonedEventLike, viewerZone: string): string {
  return evt.allDay ? 'UTC' : viewerZone;
}

/**
 * Re-expresses a calendar day in the grid's zone, keeping the date.
 *
 * Days measured in another zone begin at a different instant from the grid's
 * columns, so comparing them directly puts an event a column off. The date is
 * what both sides agree on, so that is what is carried across.
 */
export function toGridDay(day: DateTime, gridZone: string): DateTime {
  return day
    .startOf('day')
    .setZone(gridZone && gridZone.length > 0 ? gridZone : 'local', { keepLocalTime: true })
    .startOf('day');
}
