import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { layoutTimedEventsForDay, resizedEndForDaySegment } from './timed-layout';

const ZONE = 'Asia/Tokyo';

function makeEvent(id: string, startAt: string, endAt: string): CalendarEvent {
  return {
    id,
    calendarId: 'cal',
    title: id,
    allDay: false,
    startAt,
    endAt,
    timezone: ZONE,
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
    createdAt: '',
    updatedAt: '',
  };
}

describe('layoutTimedEventsForDay', () => {
  it('splits overlapping timed events into separate lanes', () => {
    const day = DateTime.fromISO('2026-04-20T00:00:00', { zone: ZONE });
    const result = layoutTimedEventsForDay(
      [
        makeEvent('a', '2026-04-20T10:00:00+09:00', '2026-04-20T11:00:00+09:00'),
        makeEvent('b', '2026-04-20T10:30:00+09:00', '2026-04-20T11:30:00+09:00'),
      ],
      day,
      ZONE,
    );

    expect(result).toHaveLength(2);
    expect(result.map((r) => r.laneCount)).toEqual([2, 2]);
    expect(result.map((r) => r.widthPct)).toEqual([50, 50]);
    expect(result.map((r) => r.leftPct).sort((a, b) => a - b)).toEqual([0, 50]);
  });

  it('keeps non-overlapping events full width', () => {
    const day = DateTime.fromISO('2026-04-20T00:00:00', { zone: ZONE });
    const result = layoutTimedEventsForDay(
      [
        makeEvent('a', '2026-04-20T10:00:00+09:00', '2026-04-20T11:00:00+09:00'),
        makeEvent('b', '2026-04-20T11:00:00+09:00', '2026-04-20T12:00:00+09:00'),
      ],
      day,
      ZONE,
    );

    expect(result.map((r) => r.laneCount)).toEqual([1, 1]);
    expect(result.map((r) => r.widthPct)).toEqual([100, 100]);
  });
});

describe('resizedEndForDaySegment', () => {
  it('uses the displayed day for overnight continuation resize', () => {
    const end = resizedEndForDaySegment({
      eventStartISO: '2026-04-20T22:00:00+09:00',
      dayStart: DateTime.fromISO('2026-04-21T00:00:00', { zone: ZONE }),
      clientY: 3 * 48,
      colTop: 0,
      hourHeight: 48,
      snapMinutes: 30,
      minDurationMinutes: 30,
      zone: ZONE,
    });

    expect(end.toISO()).toContain('2026-04-21T03:00:00.000+09:00');
  });

  it('guards same-day resize before the event start', () => {
    const end = resizedEndForDaySegment({
      eventStartISO: '2026-04-20T22:00:00+09:00',
      dayStart: DateTime.fromISO('2026-04-20T00:00:00', { zone: ZONE }),
      clientY: 3 * 48,
      colTop: 0,
      hourHeight: 48,
      snapMinutes: 30,
      minDurationMinutes: 30,
      zone: ZONE,
    });

    expect(end.toISO()).toContain('2026-04-20T22:30:00.000+09:00');
  });
});
