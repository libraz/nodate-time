import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import { makeEvent } from '@/test/make-event';
import {
  buildMovedEvent,
  buildResizedEvent,
  isRecurringEvent,
  moveSeriesRule,
  restoreInput,
  scopeEtag,
} from './event-move';
import { rebuildPresetRule } from './recurrence-preset';

/** The wire value of a field the builder is known to send. */
function sent(value: string | undefined): string {
  if (value === undefined) throw new Error('the payload did not send this field');
  return value;
}

describe('buildMovedEvent', () => {
  it('preserves the duration when moving the start', () => {
    const evt = makeEvent();
    const newStart = DateTime.fromISO('2025-06-25T14:00:00+09:00');
    const moved = buildMovedEvent(evt, newStart);
    expect(DateTime.fromISO(sent(moved.startAt)).toMillis()).toBe(newStart.toMillis());
    // Original is 90 minutes long, so the new end is 15:30.
    expect(DateTime.fromISO(sent(moved.endAt)).toMillis()).toBe(
      newStart.plus({ minutes: 90 }).toMillis(),
    );
  });

  it('preserves wall-clock duration across DST transitions', () => {
    const evt = makeEvent({
      startAt: '2026-03-08T01:30:00-05:00',
      endAt: '2026-03-08T03:30:00-04:00',
      timezone: 'America/New_York',
    });
    const newStart = DateTime.fromISO('2026-03-09T01:30:00', { zone: 'America/New_York' });
    const moved = buildMovedEvent(evt, newStart);

    expect(DateTime.fromISO(sent(moved.endAt), { zone: 'America/New_York' }).toFormat('H:mm')).toBe(
      '3:30',
    );
  });

  it('moves an all-day event by whole dates and sends no instant', () => {
    const evt = makeEvent({
      allDay: true,
      startAt: '2026-03-10T00:00:00Z',
      endAt: '2026-03-13T00:00:00Z',
      startDate: '2026-03-10',
      endDate: '2026-03-13',
    });
    const moved = buildMovedEvent(
      evt,
      DateTime.fromISO('2026-03-12T00:00:00', { zone: 'Asia/Tokyo' }),
    );

    expect(moved).toEqual({ startDate: '2026-03-12', endDate: '2026-03-15' });
  });

  it('keeps a milestone on one date', () => {
    const evt = makeEvent({
      allDay: true,
      kind: 'milestone',
      startAt: '2026-03-10T00:00:00Z',
      endAt: '2026-03-10T00:00:00Z',
      startDate: '2026-03-10',
      endDate: '2026-03-10',
    });
    const moved = buildMovedEvent(evt, DateTime.fromISO('2026-03-11T00:00:00', { zone: 'UTC' }));

    expect(moved).toEqual({ startDate: '2026-03-11', endDate: '2026-03-11' });
  });
});

describe('buildResizedEvent', () => {
  it('sets a new end and leaves the start alone', () => {
    const evt = makeEvent();
    const newEnd = DateTime.fromISO('2025-06-24T12:00:00+09:00');
    const resized = buildResizedEvent(evt, newEnd);
    expect(resized).not.toHaveProperty('startAt');
    expect(DateTime.fromISO(sent(resized.endAt)).toMillis()).toBe(newEnd.toMillis());
  });
});

describe('isRecurringEvent', () => {
  it('flags events with a recurrence rule', () => {
    expect(isRecurringEvent(makeEvent({ recurrenceRule: { freq: 'daily', interval: 1 } }))).toBe(
      true,
    );
  });

  it('flags expanded occurrences by id and the isRecurrence marker', () => {
    expect(isRecurringEvent(makeEvent({ id: 'parent-uuid_20250624T090000' }))).toBe(true);
    expect(isRecurringEvent(makeEvent({ isRecurrence: true }))).toBe(true);
  });

  it('treats a plain single event as non-recurring', () => {
    expect(isRecurringEvent(makeEvent())).toBe(false);
  });
});

/**
 * What a drag and a resize put on the wire.
 *
 * A partial update leaves an omitted field as stored, so a move names the
 * time fields and nothing else. Anything more would send back what the
 * reader may not see, or overwrite a field the user did not touch.
 */
describe('drag and resize payloads', () => {
  it('sends only the instants when a timed event is moved', () => {
    const moved = buildMovedEvent(makeEvent(), DateTime.fromISO('2026-06-24T11:00:00+09:00'));

    expect(Object.keys(moved).sort()).toEqual(['endAt', 'startAt']);
  });

  it('sends only the end when a timed event is resized', () => {
    const resized = buildResizedEvent(makeEvent(), DateTime.fromISO('2026-06-24T12:00:00+09:00'));

    expect(Object.keys(resized)).toEqual(['endAt']);
  });

  it('puts an event back with the time fields of the copy it was', () => {
    const evt = makeEvent();

    expect(restoreInput(evt)).toEqual({ startAt: evt.startAt, endAt: evt.endAt });
  });
});

describe('scopeEtag', () => {
  const evt = makeEvent({ etag: '"row"', seriesEtag: '"master"' });

  it('checks the occurrence against its own row, the series against the master', () => {
    expect(scopeEtag(evt)).toBe('"row"');
    expect(scopeEtag(evt, 'this')).toBe('"row"');
    expect(scopeEtag(evt, 'all')).toBe('"master"');
    expect(scopeEtag(evt, 'following')).toBe('"master"');
  });

  it('falls back to the row when no master version came with it', () => {
    expect(scopeEtag(makeEvent({ etag: '"row"' }), 'all')).toBe('"row"');
  });
});

describe('moveSeriesRule', () => {
  const mondayWeekly = makeEvent({
    id: 'a1b2c3d4-0000-0000-0000-000000000000_20260803',
    isRecurrence: true,
    recurrenceDate: '20260803',
    startAt: '2026-08-03T10:00:00+09:00',
    endAt: '2026-08-03T11:00:00+09:00',
    recurrenceRule: { freq: 'weekly', interval: 1, byDay: ['MO'], count: 5 },
  });
  const tuesday = DateTime.fromISO('2026-08-04T10:00:00', { zone: 'Asia/Tokyo' });

  it('rebuilds a preset rule for the new start, as an editor date change does', () => {
    const payload = moveSeriesRule(mondayWeekly, buildMovedEvent(mondayWeekly, tuesday));

    expect(payload.recurrenceRule).toEqual({
      freq: 'weekly',
      interval: 1,
      byDay: ['TU'],
      count: 5,
    });
    expect(payload.recurrenceRule).toEqual(
      rebuildPresetRule('weekly', tuesday, mondayWeekly.recurrenceRule),
    );
  });

  it('reads the new start in the event zone, not the viewer’s', () => {
    // Tuesday 09:00 in Tokyo is Monday 20:00 in New York, so the series stays on Monday.
    const newYorkMonday = makeEvent({
      isRecurrence: true,
      recurrenceDate: '20260803',
      timezone: 'America/New_York',
      startAt: '2026-08-03T20:00:00-04:00',
      endAt: '2026-08-03T21:00:00-04:00',
      recurrenceRule: { freq: 'weekly', interval: 1, byDay: ['MO'] },
    });
    const nextMonday = DateTime.fromISO('2026-08-11T09:00:00', { zone: 'Asia/Tokyo' });

    const payload = moveSeriesRule(newYorkMonday, buildMovedEvent(newYorkMonday, nextMonday));

    expect(payload.recurrenceRule).toMatchObject({ byDay: ['MO'] });
  });

  it('leaves a custom rule as it is', () => {
    const custom = makeEvent({
      isRecurrence: true,
      recurrenceRule: { freq: 'weekly', interval: 2, byDay: ['MO', 'WE'] },
    });
    const moved = buildMovedEvent(custom, tuesday);

    // Not sent, so the stored rule stands.
    expect(moveSeriesRule(custom, moved).recurrenceRule).toBeUndefined();
  });

  it('leaves the payload alone when the start did not move', () => {
    const resized = buildResizedEvent(mondayWeekly, DateTime.fromISO('2026-08-03T12:00:00+09:00'));

    expect(moveSeriesRule(mondayWeekly, resized)).toBe(resized);
  });

  it('moves an all-day series by its dates', () => {
    const allDayMonday = makeEvent({
      id: 'a1b2c3d4-0000-0000-0000-000000000000_20260803',
      allDay: true,
      isRecurrence: true,
      recurrenceDate: '20260803',
      startAt: '2026-08-03T00:00:00Z',
      endAt: '2026-08-04T00:00:00Z',
      startDate: '2026-08-03',
      endDate: '2026-08-04',
      recurrenceRule: { freq: 'weekly', interval: 1, byDay: ['MO'] },
    });
    const payload = moveSeriesRule(
      allDayMonday,
      buildMovedEvent(allDayMonday, DateTime.fromISO('2026-08-04', { zone: 'UTC' })),
    );

    expect(payload).toMatchObject({ startDate: '2026-08-04', endDate: '2026-08-05' });
    expect(payload.recurrenceRule).toMatchObject({ byDay: ['TU'] });
  });

  // The occurrence of Monday the 3rd was edited onto Wednesday the 5th. Dragging
  // it a day later with "all events" shifts the series by that one day: Mondays
  // become Tuesdays. Reading the rule against where the occurrence is drawn
  // (a Wednesday) finds no match and sends the Monday rule back unchanged.
  it('shifts the series rule from an occurrence that was edited off its date', () => {
    const edited = makeEvent({
      ...mondayWeekly,
      startAt: '2026-08-05T10:00:00+09:00',
      endAt: '2026-08-05T11:00:00+09:00',
    });
    const toThursday = DateTime.fromISO('2026-08-06T10:00:00', { zone: 'Asia/Tokyo' });

    const payload = moveSeriesRule(edited, buildMovedEvent(edited, toThursday));

    expect(payload.recurrenceRule).toMatchObject({ freq: 'weekly', byDay: ['TU'] });
  });
});
