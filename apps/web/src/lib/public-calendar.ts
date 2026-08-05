import type { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';

export interface PublicCalendarEventLike {
  startAt: string;
  endAt: string;
  timezone?: string;
}

export function publicEventOccursOnDay(event: PublicCalendarEventLike, day: DateTime): boolean {
  const zone = event.timezone || undefined;
  const zonedDay = zone ? day.setZone(zone, { keepLocalTime: true }) : day;
  const dayStart = zonedDay.startOf('day');
  const dayEnd = dayStart.plus({ days: 1 });
  const eventStart = fromISOInZone(event.startAt, zone);
  const eventEnd = fromISOInZone(event.endAt, zone);
  return eventStart < dayEnd && eventEnd > dayStart;
}

/**
 * Why a share link did not load.
 *
 * `gone` is the link's own state -- revoked, expired, never existed. `busy` is
 * everything the link cannot be blamed for: a throttled or failing server.
 * Collapsing the two tells the calendar's owner their link has died when it
 * has not, and the fix they reach for is to issue a new one -- which does kill
 * every embed already published against the old.
 */
export type ShareFailure = 'gone' | 'busy';

export function shareFailureOf(error: unknown): ShareFailure {
  const status =
    typeof error === 'object' && error !== null ? (error as { status?: number }).status : undefined;
  if (status === 404 || status === 410) return 'gone';
  return 'busy';
}
