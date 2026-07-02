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
