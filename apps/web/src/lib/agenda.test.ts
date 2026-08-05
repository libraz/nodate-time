import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { groupByDay } from './agenda';

function evt(id: string, startAt: string, endAt: string, allDay = false): CalendarEvent {
  return {
    id,
    calendarId: 'cal-1',
    title: id,
    allDay,
    startAt,
    endAt,
    color: '#000',
    ownerId: null,
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participants: [],
  } as unknown as CalendarEvent;
}

const zone = 'Asia/Tokyo';
const active = ['cal-1'];

describe('groupByDay', () => {
  it('lists a multi-day event on every day it covers', () => {
    // Filed under its start day alone, a trip in progress leaves today
    // looking free.
    const trip = evt('trip', '2026-08-03T10:00:00+09:00', '2026-08-05T18:00:00+09:00');
    const days = groupByDay([trip], active, zone).map(([d]) => d);
    expect(days).toEqual(['2026-08-03', '2026-08-04', '2026-08-05']);
  });

  it('does not claim the day a timed event ends at midnight on', () => {
    const evening = evt('evening', '2026-08-03T22:00:00+09:00', '2026-08-04T00:00:00+09:00');
    expect(groupByDay([evening], active, zone).map(([d]) => d)).toEqual(['2026-08-03']);
  });

  it('returns the days in order', () => {
    const later = evt('later', '2026-08-09T10:00:00+09:00', '2026-08-09T11:00:00+09:00');
    const earlier = evt('earlier', '2026-08-02T10:00:00+09:00', '2026-08-04T11:00:00+09:00');
    expect(groupByDay([later, earlier], active, zone).map(([d]) => d)).toEqual([
      '2026-08-02',
      '2026-08-03',
      '2026-08-04',
      '2026-08-09',
    ]);
  });

  it('leaves out calendars that are switched off', () => {
    const hidden = { ...evt('hidden', '2026-08-03T10:00:00+09:00', '2026-08-03T11:00:00+09:00') };
    (hidden as { calendarId: string }).calendarId = 'cal-2';
    expect(groupByDay([hidden], active, zone)).toEqual([]);
  });
});
