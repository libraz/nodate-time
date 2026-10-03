import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { weekDropStart } from './week-drag';

const ZONE = 'America/New_York';
const evt = {
  startAt: '2026-03-07T05:10:00-05:00',
  endAt: '2026-03-07T06:10:00-05:00',
} as CalendarEvent;

function drop(originKey: string, targetKey: string, travelPx: number) {
  return weekDropStart({
    evt,
    originKey,
    targetKey,
    travelPx,
    hourHeight: 48,
    snapMinutes: 30,
    zone: ZONE,
  });
}

describe('weekDropStart', () => {
  it('leaves an event dropped where it was grabbed alone, off-grid start included', () => {
    expect(drop('2026-03-07', '2026-03-07', 3)).toBeNull();
  });

  // New York springs forward overnight into the 8th: a day later is 05:10
  // on the wall clock, not 24 elapsed hours.
  it('moves across a DST change on the wall clock', () => {
    expect(drop('2026-03-07', '2026-03-08', 0)?.toISO()).toBe('2026-03-08T05:10:00.000-04:00');
  });

  it('carries vertical travel past midnight into the next day', () => {
    expect(drop('2026-03-07', '2026-03-07', -48 * 6)?.toISO()).toBe(
      '2026-03-06T23:10:00.000-05:00',
    );
  });
});
