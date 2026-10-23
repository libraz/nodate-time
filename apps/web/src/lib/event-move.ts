import { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';
import { rebuildPresetRule, ruleToPreset } from '@/lib/recurrence-preset';
import type { EventInput } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

/**
 * Builds the update payload for moving an event to a new start, preserving its
 * duration and every other field. `newStart` must carry the intended timezone.
 */
export function buildMovedEvent(evt: CalendarEvent, newStart: DateTime): EventInput {
  // Measured in the zone newStart carries, the one the span is re-applied in.
  const start = DateTime.fromISO(evt.startAt, { zone: newStart.zone });
  const end = DateTime.fromISO(evt.endAt, { zone: newStart.zone });
  const duration = end
    .setZone('UTC', { keepLocalTime: true })
    .diff(start.setZone('UTC', { keepLocalTime: true }), ['days', 'hours', 'minutes']);
  const wallClockStart = newStart.setZone('UTC', { keepLocalTime: true });
  const wallClockEnd = wallClockStart.plus(duration);
  // Setting fields on newStart retains its chosen offset when the end is ambiguous.
  const newEnd = newStart.set(wallClockEnd.toObject());
  return {
    title: evt.title,
    allDay: evt.allDay,
    startAt: newStart.toISO() ?? evt.startAt,
    endAt: newEnd.toISO() ?? evt.endAt,
    timezone: evt.timezone,
    location: evt.location,
    memo: evt.memo,
    url: evt.url,
    notificationOffset: evt.notificationOffset,
    participants: evt.participants,
    ownerId: evt.ownerId,
    recurrenceRule: evt.recurrenceRule,
    // A move is a move. Carrying these forward is what keeps a drag from
    // resetting an out-of-office block to busy, a negotiable meeting to
    // fixed, or a private event to whatever the calendar publishes.
    showAs: evt.showAs,
    flexibility: evt.flexibility,
    visibility: evt.visibility,
  };
}

/**
 * Re-targets a move of one occurrence at the whole series.
 *
 * The server shifts the series start by the same wall-clock amount the
 * occurrence moved, so the dates in `payload` stay as built. The rule does not
 * follow on its own: a weekly-Monday series whose start lands on a Tuesday would
 * be refused or keep its Monday. A preset rule is rebuilt for the new start in
 * the zone the server expands in, exactly as the editor does for a date change;
 * a custom rule is the user's own and is left alone.
 */
export function moveSeriesRule(evt: CalendarEvent, payload: EventInput): EventInput {
  const rule = evt.recurrenceRule;
  if (!rule) return payload;
  const oldStart = fromISOInZone(evt.startAt, evt.timezone);
  const newStart = fromISOInZone(payload.startAt, evt.timezone);
  if (+newStart === +oldStart) return payload;
  const preset = ruleToPreset(rule, oldStart);
  if (preset === 'custom') return payload;
  return { ...payload, recurrenceRule: rebuildPresetRule(preset, newStart, rule) };
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
    location: evt.location,
    memo: evt.memo,
    url: evt.url,
    notificationOffset: evt.notificationOffset,
    participants: evt.participants,
    ownerId: evt.ownerId,
    recurrenceRule: evt.recurrenceRule,
    // A move is a move. Carrying these forward is what keeps a drag from
    // resetting an out-of-office block to busy, a negotiable meeting to
    // fixed, or a private event to whatever the calendar publishes.
    showAs: evt.showAs,
    flexibility: evt.flexibility,
    visibility: evt.visibility,
  };
}

/**
 * The id of the series an event belongs to. An expanded occurrence carries
 * `{seriesId}_{date}`; anything stored against the event -- history, a photo
 * link -- is stored against the series.
 */
export function seriesId(eventId: string): string {
  return eventId.split('_')[0] ?? eventId;
}

/** Returns true when an event is recurring (rule master or expanded occurrence). */
export function isRecurringEvent(evt: CalendarEvent): boolean {
  return !!evt.recurrenceRule || evt.isRecurrence || evt.id.includes('_');
}
