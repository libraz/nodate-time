import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { buildMovedEvent, buildResizedEvent, isRecurringEvent } from './event-move';

function makeEvent(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: 'a1b2c3d4-0000-0000-0000-000000000000',
    calendarId: 'cal-1',
    title: 'Meeting',
    allDay: false,
    startAt: '2025-06-24T09:00:00+09:00',
    endAt: '2025-06-24T10:30:00+09:00',
    timezone: 'Asia/Tokyo',
    color: '#47B2F7',
    assignedTo: null,
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participants: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '2025-06-01T00:00:00Z',
    updatedAt: '2025-06-01T00:00:00Z',
    ...overrides,
  };
}

describe('buildMovedEvent', () => {
  it('preserves the duration when moving the start', () => {
    const evt = makeEvent();
    const newStart = DateTime.fromISO('2025-06-25T14:00:00+09:00');
    const moved = buildMovedEvent(evt, newStart);
    expect(DateTime.fromISO(moved.startAt).toMillis()).toBe(newStart.toMillis());
    // Original is 90 minutes long, so the new end is 15:30.
    expect(DateTime.fromISO(moved.endAt).toMillis()).toBe(
      newStart.plus({ minutes: 90 }).toMillis(),
    );
  });

  it('carries over every other field', () => {
    const evt = makeEvent({ title: 'Lunch', location: 'Cafe', allDay: true });
    const moved = buildMovedEvent(evt, DateTime.fromISO('2025-06-26T00:00:00+09:00'));
    expect(moved.title).toBe('Lunch');
    expect(moved.location).toBe('Cafe');
    expect(moved.allDay).toBe(true);
  });
});

describe('buildResizedEvent', () => {
  it('keeps the start and sets a new end', () => {
    const evt = makeEvent();
    const newEnd = DateTime.fromISO('2025-06-24T12:00:00+09:00');
    const resized = buildResizedEvent(evt, newEnd);
    expect(resized.startAt).toBe(evt.startAt);
    expect(DateTime.fromISO(resized.endAt).toMillis()).toBe(newEnd.toMillis());
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
