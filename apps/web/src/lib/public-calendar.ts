import type { components } from '@/lib/api-schema';
import type { DayEvent } from '@/lib/week-layout';

/** What `GET /share/{token}` answers. */
export type PublicCalendar = Omit<components['schemas']['PublicCalendarOutputBody'], '$schema'>;

/**
 * An event as a published link shows it. A withheld event has `detailsHidden`
 * and no title; an all-day one carries `startDate`/`endDate` (end exclusive).
 */
export type PublicEvent = components['schemas']['PublicEventResponse'];

/**
 * The event as day placement reads it. An all-day event is placed by its
 * calendar dates, never by the instants, which would shift it with the zone.
 */
export function publicDayEvent(evt: PublicEvent): PublicEvent & DayEvent {
  if (!evt.allDay || !evt.startDate || !evt.endDate) return evt;
  return { ...evt, startAt: `${evt.startDate}T00:00:00`, endAt: `${evt.endDate}T00:00:00` };
}

/**
 * Whether this link publishes the calendar, and so has events to draw.
 *
 * A join link is an offer of access rather than access itself: the server
 * refuses it the events endpoint by design, and it stays an offer once it has
 * been used up. Drawing a grid for one produces six weeks of empty cells,
 * which reads as a calendar with nothing in it rather than as an invitation
 * waiting to be accepted.
 */
export function publishesEvents(cal: PublicCalendar): boolean {
  return !cal.joinable && !cal.spent;
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
