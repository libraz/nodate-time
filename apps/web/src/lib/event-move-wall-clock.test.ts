import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { buildMovedEvent } from './event-move';
import { shiftStartKeepingDuration } from './event-times';

const NEW_YORK = 'America/New_York';

function makeEvent(startAt: string, endAt: string): CalendarEvent {
  return {
    id: 'event-1',
    calendarId: 'cal-1',
    title: 'Meeting',
    allDay: false,
    startAt,
    endAt,
    timezone: NEW_YORK,
    color: '#47B2F7',
    ownerId: null,
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participants: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  };
}

describe('moving an event preserves its wall-clock span on the destination date', () => {
  it.each([
    {
      label: 'spring-forward',
      oldStart: '2026-03-01T01:30',
      oldEnd: '2026-03-01T03:30',
      newStart: '2026-03-08T01:30',
      expectedEnd: '2026-03-08T03:30',
    },
    {
      label: 'fall-back',
      oldStart: '2026-10-25T00:30',
      oldEnd: '2026-10-25T03:30',
      newStart: '2026-11-01T00:30',
      expectedEnd: '2026-11-01T03:30',
    },
  ])(
    'keeps the editor wall-clock span on $label',
    ({ oldStart, oldEnd, newStart, expectedEnd }) => {
      const moved = shiftStartKeepingDuration(
        { allDay: false, startAt: oldStart, endAt: oldEnd },
        DateTime.fromISO(newStart, { zone: NEW_YORK }),
        NEW_YORK,
      );

      expect(moved.endAt).toBe(expectedEnd);
    },
  );

  it.each([
    {
      label: 'spring-forward',
      oldStart: '2026-03-01T01:30:00-05:00',
      oldEnd: '2026-03-01T03:30:00-05:00',
      newStart: '2026-03-08T01:30',
      expectedEnd: '2026-03-08T03:30',
    },
    {
      label: 'fall-back',
      oldStart: '2026-10-25T00:30:00-04:00',
      oldEnd: '2026-10-25T03:30:00-04:00',
      newStart: '2026-11-01T00:30',
      expectedEnd: '2026-11-01T03:30',
    },
  ])('keeps the drag wall-clock span on $label', ({ oldStart, oldEnd, newStart, expectedEnd }) => {
    const moved = buildMovedEvent(
      makeEvent(oldStart, oldEnd),
      DateTime.fromISO(newStart, { zone: NEW_YORK }),
    );

    expect(DateTime.fromISO(moved.endAt, { zone: NEW_YORK }).toFormat("yyyy-MM-dd'T'HH:mm")).toBe(
      expectedEnd,
    );
  });

  it.each([
    {
      label: 'first occurrence',
      newStart: '2026-11-01T01:00:00-04:00',
      expectedEnd: '2026-11-01T01:30:00.000-04:00',
    },
    {
      label: 'second occurrence',
      newStart: '2026-11-01T01:00:00-05:00',
      expectedEnd: '2026-11-01T01:30:00.000-05:00',
    },
  ])('retains the $label offset when the moved end is ambiguous', ({ newStart, expectedEnd }) => {
    const start = DateTime.fromISO(newStart, { zone: NEW_YORK });
    const moved = buildMovedEvent(
      makeEvent('2026-11-02T01:00:00-05:00', '2026-11-02T01:30:00-05:00'),
      start,
    );
    const end = DateTime.fromISO(moved.endAt);

    expect(moved.endAt).toBe(expectedEnd);
    expect(end.toMillis()).toBeGreaterThan(start.toMillis());
  });

  it('keeps the editor end after its parsed ambiguous start occurrence', () => {
    const start = DateTime.fromISO('2026-11-01T01:00', { zone: NEW_YORK });
    const moved = shiftStartKeepingDuration(
      { allDay: false, startAt: '2026-11-02T01:00', endAt: '2026-11-02T01:30' },
      start,
      NEW_YORK,
    );

    expect(moved.endAt).toBe('2026-11-01T01:30');
    expect(DateTime.fromISO(moved.endAt, { zone: NEW_YORK }).toMillis()).toBeGreaterThan(
      start.toMillis(),
    );
  });

  it('keeps overnight and multiday wall-clock spans', () => {
    const editorMove = shiftStartKeepingDuration(
      { allDay: false, startAt: '2026-02-01T22:30', endAt: '2026-02-03T00:30' },
      DateTime.fromISO('2026-03-01T22:30', { zone: NEW_YORK }),
      NEW_YORK,
    );
    const dragMove = buildMovedEvent(
      makeEvent('2026-02-01T22:30:00-05:00', '2026-02-03T00:30:00-05:00'),
      DateTime.fromISO('2026-03-01T22:30', { zone: NEW_YORK }),
    );

    expect(editorMove.endAt).toBe('2026-03-03T00:30');
    expect(
      DateTime.fromISO(dragMove.endAt, { zone: NEW_YORK }).toFormat("yyyy-MM-dd'T'HH:mm"),
    ).toBe('2026-03-03T00:30');
  });
});
