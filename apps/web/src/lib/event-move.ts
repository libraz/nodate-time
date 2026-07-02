import type { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';
import type { EventInput } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

/**
 * Builds the update payload for moving an event to a new start, preserving its
 * duration and every other field. `newStart` must carry the intended timezone.
 */
export function buildMovedEvent(evt: CalendarEvent, newStart: DateTime): EventInput {
  const start = fromISOInZone(evt.startAt, evt.timezone);
  const end = fromISOInZone(evt.endAt, evt.timezone);
  const duration = end
    .setZone('UTC', { keepLocalTime: true })
    .diff(start.setZone('UTC', { keepLocalTime: true }), ['days', 'hours', 'minutes']);
  const newEnd = newStart.plus(duration);
  return {
    title: evt.title,
    allDay: evt.allDay,
    startAt: newStart.toISO() ?? evt.startAt,
    endAt: newEnd.toISO() ?? evt.endAt,
    timezone: evt.timezone,
    color: evt.color,
    location: evt.location,
    memo: evt.memo,
    url: evt.url,
    notificationOffset: evt.notificationOffset,
    participants: evt.participants,
    assignedTo: evt.assignedTo,
    recurrenceRule: evt.recurrenceRule,
  };
}

/**
 * Builds the update payload for resizing an event to a new end, keeping its start
 * and every other field. `newEnd` must carry the intended timezone.
 */
export function buildResizedEvent(evt: CalendarEvent, newEnd: DateTime): EventInput {
  return {
    title: evt.title,
    allDay: evt.allDay,
    startAt: evt.startAt,
    endAt: newEnd.toISO() ?? evt.endAt,
    timezone: evt.timezone,
    color: evt.color,
    location: evt.location,
    memo: evt.memo,
    url: evt.url,
    notificationOffset: evt.notificationOffset,
    participants: evt.participants,
    assignedTo: evt.assignedTo,
    recurrenceRule: evt.recurrenceRule,
  };
}

/** Returns true when an event is recurring (rule master or expanded occurrence). */
export function isRecurringEvent(evt: CalendarEvent): boolean {
  return !!evt.recurrenceRule || evt.isRecurrence || evt.id.includes('_');
}
