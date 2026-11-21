import { DateTime } from 'luxon';
import { addDays, allDayWireDates, daysBetween } from '@/lib/all-day';
import { fromISOInZone } from '@/lib/date-utils';
import { rebuildPresetRule, ruleToPreset } from '@/lib/recurrence-preset';
import type { EventPatch } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

/** The scope an edit or delete is applied at. */
export type EventScope = 'this' | 'all' | 'following';

/**
 * The version a write at `scope` is checked against: the listing's `etag` for
 * the occurrence itself, the master's `seriesEtag` for anything wider.
 */
export function scopeEtag(evt: Pick<CalendarEvent, 'etag' | 'seriesEtag'>, scope?: EventScope) {
  return scope === 'all' || scope === 'following' ? (evt.seriesEtag ?? evt.etag) : evt.etag;
}

/** The time fields of an event as the API takes them: dates for all-day, instants otherwise. */
type TimeFields =
  | { startAt: string; endAt: string; startDate?: undefined; endDate?: undefined }
  | { startDate: string; endDate: string; startAt?: undefined; endAt?: undefined };

function timeFields(evt: CalendarEvent): TimeFields {
  if (evt.allDay) {
    const { startDate, endDate } = allDayWireDates(evt);
    return { startDate, endDate };
  }
  return { startAt: evt.startAt, endAt: evt.endAt };
}

/**
 * Builds the update that moves an event to a new start, preserving its
 * duration. `newStart` must carry the intended timezone. Only the time fields
 * are sent: an omitted field is left as stored, which keeps a move from
 * touching anything the reader may not see or the user did not change.
 */
export function buildMovedEvent(evt: CalendarEvent, newStart: DateTime): EventPatch {
  if (evt.allDay) {
    // Dates carry no zone; the move is a whole number of days.
    const { startDate, endDate } = allDayWireDates(evt);
    const shift = daysBetween(startDate, newStart.toFormat('yyyy-MM-dd'));
    return { startDate: addDays(startDate, shift), endDate: addDays(endDate, shift) };
  }
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
    startAt: newStart.toISO() ?? evt.startAt,
    endAt: newEnd.toISO() ?? evt.endAt,
  };
}

/**
 * Re-targets a move of one occurrence at the whole series.
 *
 * The server shifts the series start by the same wall-clock amount the
 * occurrence moved, so the dates in `payload` stay as built. The rule does not
 * follow on its own: a weekly-Monday series whose start lands on a Tuesday would
 * be refused or keep its Monday. A preset rule is rebuilt for the shifted
 * series start in the zone the server expands in, exactly as the editor does
 * for a date change; a custom rule is the user's own and is left alone.
 *
 * The shift is applied to the date the rule generated the occurrence on
 * (`recurrenceDate`), not to where the occurrence is drawn: an occurrence
 * edited earlier sits off the rule, and reading the rule against it would
 * leave the series where it was.
 */
export function moveSeriesRule(evt: CalendarEvent, payload: EventPatch): EventPatch {
  const rule = evt.recurrenceRule;
  if (!rule) return payload;
  const zone = evt.allDay ? 'UTC' : evt.timezone;
  const shown = evt.allDay
    ? DateTime.fromISO(allDayWireDates(evt).startDate, { zone })
    : fromISOInZone(evt.startAt, zone);
  const moved = evt.allDay
    ? payload.startDate && DateTime.fromISO(payload.startDate, { zone })
    : payload.startAt && fromISOInZone(payload.startAt, zone);
  if (!moved || +moved === +shown) return payload;
  const generated = generatedStart(evt, shown);
  const newStart =
    +generated === +shown ? moved : generated.plus(moved.diff(shown, ['days', 'hours', 'minutes']));
  const preset = ruleToPreset(rule, generated);
  if (preset === 'custom') return payload;
  return { ...payload, recurrenceRule: rebuildPresetRule(preset, newStart, rule) };
}

/** The start the rule generated this occurrence at, which differs from `shown` once it was edited. */
function generatedStart(evt: CalendarEvent, shown: DateTime): DateTime {
  const key = evt.recurrenceDate;
  if (!key || !/^\d{8}$/.test(key)) return shown;
  return shown.set({
    year: Number(key.slice(0, 4)),
    month: Number(key.slice(4, 6)),
    day: Number(key.slice(6, 8)),
  });
}

/** Builds the update that resizes a timed event to a new end. `newEnd` must carry the intended timezone. */
export function buildResizedEvent(evt: CalendarEvent, newEnd: DateTime): EventPatch {
  return { endAt: newEnd.toISO() ?? evt.endAt };
}

/** The update that puts an event back exactly as this copy of it reads. */
export function restoreInput(evt: CalendarEvent): EventPatch {
  return timeFields(evt);
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
