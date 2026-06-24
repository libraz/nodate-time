import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { isMultiDay, layoutWeek, MAX_VISIBLE_TRACKS } from './week-layout';

const ZONE = 'Asia/Tokyo';

function makeEvent(
  overrides: Partial<CalendarEvent> & { startAt: string; endAt: string },
): CalendarEvent {
  return {
    id: overrides.id ?? Math.random().toString(36).slice(2),
    calendarId: 'cal',
    title: overrides.title ?? 'Event',
    allDay: overrides.allDay ?? false,
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
    ...overrides,
  };
}

describe('isMultiDay', () => {
  it('is false for a same-day event', () => {
    const evt = makeEvent({
      startAt: '2026-04-20T09:00:00+09:00',
      endAt: '2026-04-20T18:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(false);
  });

  it('is true for an event spanning two days', () => {
    const evt = makeEvent({
      startAt: '2026-04-20T09:00:00+09:00',
      endAt: '2026-04-21T10:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(true);
  });

  it('is false for a timed event ending exactly at midnight', () => {
    // 23:00 -> next day 00:00 occupies only the first day.
    const evt = makeEvent({
      startAt: '2026-04-20T23:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(false);
  });

  it('is false for a single all-day event (exclusive end)', () => {
    const evt = makeEvent({
      allDay: true,
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(false);
  });

  it('is true for a two-day all-day event (exclusive end)', () => {
    const evt = makeEvent({
      allDay: true,
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-22T00:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(true);
  });
});

describe('layoutWeek', () => {
  const weekStart = DateTime.fromISO('2026-04-19T00:00:00', { zone: ZONE }); // Sunday

  it('positions a single multi-day event with correct columns', () => {
    const evt = makeEvent({
      title: 'Trip',
      startAt: '2026-04-20T00:00:00+09:00', // Monday (col 1)
      endAt: '2026-04-23T00:00:00+09:00', // exclusive end -> last day Wednesday (col 3)
    });
    const result = layoutWeek(weekStart, [evt], ZONE);
    expect(result).toHaveLength(1);
    const positioned = result[0];
    expect(positioned?.startCol).toBe(1);
    expect(positioned?.span).toBe(3);
    expect(positioned?.track).toBe(0);
    expect(positioned?.continuesLeft).toBe(false);
    expect(positioned?.continuesRight).toBe(false);
  });

  it('excludes single-day events (rendered as chips, not bars)', () => {
    const timed = makeEvent({
      startAt: '2026-04-20T10:00:00+09:00',
      endAt: '2026-04-20T11:00:00+09:00',
    });
    const nightToMidnight = makeEvent({
      startAt: '2026-04-20T23:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    const singleAllDay = makeEvent({
      allDay: true,
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    expect(layoutWeek(weekStart, [timed, nightToMidnight, singleAllDay], ZONE)).toHaveLength(0);
  });

  it('clamps events that overflow the week and flags continuation', () => {
    const evt = makeEvent({
      startAt: '2026-04-15T00:00:00+09:00', // before this week
      endAt: '2026-04-30T00:00:00+09:00', // after this week
    });
    const result = layoutWeek(weekStart, [evt], ZONE);
    const positioned = result[0];
    expect(positioned?.startCol).toBe(0);
    expect(positioned?.span).toBe(7);
    expect(positioned?.continuesLeft).toBe(true);
    expect(positioned?.continuesRight).toBe(true);
  });

  it('places overlapping events on separate tracks', () => {
    const a = makeEvent({
      id: 'a',
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-22T00:00:00+09:00',
    });
    const b = makeEvent({
      id: 'b',
      startAt: '2026-04-21T00:00:00+09:00',
      endAt: '2026-04-23T00:00:00+09:00',
    });
    const result = layoutWeek(weekStart, [a, b], ZONE);
    const tracks = result.map((p) => p.track).sort();
    expect(tracks).toEqual([0, 1]);
  });

  it('reuses a track when events do not overlap', () => {
    const a = makeEvent({
      id: 'a',
      startAt: '2026-04-19T00:00:00+09:00', // Sunday (col 0)
      endAt: '2026-04-21T00:00:00+09:00', // exclusive end -> through Monday (col 1)
    });
    const b = makeEvent({
      id: 'b',
      startAt: '2026-04-23T00:00:00+09:00', // Thursday (col 4)
      endAt: '2026-04-25T00:00:00+09:00', // exclusive end -> through Friday (col 5)
    });
    const result = layoutWeek(weekStart, [a, b], ZONE);
    expect(result).toHaveLength(2);
    expect(result.every((p) => p.track === 0)).toBe(true);
  });

  it('excludes events that fall entirely outside the week', () => {
    const evt = makeEvent({
      startAt: '2026-05-01T00:00:00+09:00',
      endAt: '2026-05-03T00:00:00+09:00',
    });
    expect(layoutWeek(weekStart, [evt], ZONE)).toHaveLength(0);
  });

  it('exposes a sensible visible-track limit', () => {
    expect(MAX_VISIBLE_TRACKS).toBeGreaterThan(0);
  });
});
