import { describe, expect, it } from 'vitest';
import { buildMovedEvent } from '@/lib/event-move';
import type { CalendarEvent } from '@/types/calendar';
import { monthDragLanding, monthDropStart } from './use-month-drag';

const VIEWER = 'America/New_York';

// A one-day Tokyo all-day event, read by a viewer whose zone leaves DST on
// 2026-11-01. Dragged eight cells it crosses that change.
const birthday = {
  id: 'e1',
  calendarId: 'cal-1',
  title: 'Birthday',
  allDay: true,
  startAt: '2026-10-30T00:00:00+09:00',
  endAt: '2026-10-31T00:00:00+09:00',
  timezone: 'Asia/Tokyo',
} as CalendarEvent;

describe('monthDropStart', () => {
  it('keeps an all-day event at midnight in its own zone across a DST change', () => {
    const start = monthDropStart(birthday, '2026-10-30', '2026-11-07', VIEWER);
    if (!start) throw new Error('no move');
    const payload = buildMovedEvent(birthday, start);

    // Days added on the viewer's clock land at 01:00 in Tokyo.
    expect(payload.startAt).toBe('2026-11-07T00:00:00.000+09:00');
    expect(payload.endAt).toBe('2026-11-08T00:00:00.000+09:00');
  });

  it('keeps a timed event at its wall-clock time on the viewer clock', () => {
    const timed = {
      ...birthday,
      allDay: false,
      startAt: '2026-10-30T09:00:00-04:00',
      endAt: '2026-10-30T10:00:00-04:00',
      timezone: VIEWER,
    } as CalendarEvent;

    const start = monthDropStart(timed, '2026-10-30', '2026-11-02', VIEWER);

    expect(start?.toISO()).toBe('2026-11-02T09:00:00.000-05:00');
  });

  it('does not move an event dropped on the cell it started from', () => {
    expect(monthDropStart(birthday, '2026-10-30', '2026-10-30', VIEWER)).toBeNull();
  });
});

describe('monthDragLanding', () => {
  it('lands the ghost on the cells the drop will write', () => {
    const landing = monthDragLanding(birthday, '2026-10-30', '2026-11-07', VIEWER);

    // Starting the ghost from the viewer-zone instant put it a day early and
    // two cells wide.
    expect(landing.start.toISODate()).toBe('2026-11-07');
    expect(landing.end.toISODate()).toBe('2026-11-07');
  });

  it('keeps a multi-day span its length', () => {
    const trip = { ...birthday, endAt: '2026-11-02T00:00:00+09:00' } as CalendarEvent;

    const landing = monthDragLanding(trip, '2026-10-30', '2026-10-31', VIEWER);

    expect(landing.start.toISODate()).toBe('2026-10-31');
    expect(landing.end.toISODate()).toBe('2026-11-02');
  });
});
