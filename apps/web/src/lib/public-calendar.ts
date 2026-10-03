/** What `GET /share/{token}` answers. */
export interface PublicCalendar {
  calendarId: string;
  name: string;
  color: string;
  joinable: boolean;
  /** The link was a join link, but it has been followed as often as it allowed. */
  spent: boolean;
}

/** An event as a published link shows it. */
export interface PublicEvent {
  id: string;
  title: string;
  allDay: boolean;
  startAt: string;
  endAt: string;
  timezone?: string;
  color: string;
  location?: string;
  /** The event's details are withheld; only the time is published. */
  private?: boolean;
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
