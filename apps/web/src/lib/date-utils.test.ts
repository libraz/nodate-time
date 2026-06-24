import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import {
  formatDateKey,
  formatMonthYear,
  formatTime,
  fromISOInZone,
  getMonthDays,
  getWeekDays,
  getWeekdayLabel,
  isSameDay,
  jsDayOfWeek,
} from './date-utils';

describe('getMonthDays', () => {
  it('starts the grid on Sunday and covers the whole month', () => {
    // April 2026: 1st is a Wednesday
    const days = getMonthDays(2026, 3);
    const first = days[0];
    expect((first?.weekday ?? 0) % 7).toBe(0); // Sunday
    // First cell is the Sunday before/at April 1
    expect(first?.toISODate()).toBe('2026-03-29');
    expect(days.some((d) => d.toISODate() === '2026-04-01')).toBe(true);
    expect(days.some((d) => d.toISODate() === '2026-04-30')).toBe(true);
  });

  it('returns 35 cells when the month fits in 5 weeks', () => {
    // February 2026 starts on Sunday and has 28 days -> exactly 4 weeks, padded to 35
    const days = getMonthDays(2026, 1);
    expect(days.length).toBe(35);
  });

  it('returns 42 cells when the month needs 6 weeks', () => {
    // August 2026: 1st is a Saturday, 31 days -> needs 6 weeks
    const days = getMonthDays(2026, 7);
    expect(days.length).toBe(42);
  });
});

describe('getWeekDays', () => {
  it('returns 7 Sunday-aligned days', () => {
    const week = getWeekDays(DateTime.local(2026, 4, 22)); // Wednesday
    expect(week).toHaveLength(7);
    expect(week[0]?.toISODate()).toBe('2026-04-19'); // Sunday
    expect(week[6]?.toISODate()).toBe('2026-04-25'); // Saturday
  });
});

describe('isSameDay', () => {
  it('ignores the time component', () => {
    const a = DateTime.local(2026, 4, 20, 9, 0);
    const b = DateTime.local(2026, 4, 20, 23, 59);
    const c = DateTime.local(2026, 4, 21, 0, 0);
    expect(isSameDay(a, b)).toBe(true);
    expect(isSameDay(a, c)).toBe(false);
  });
});

describe('formatTime', () => {
  it('formats an ISO timestamp as HH:mm', () => {
    expect(formatTime('2026-04-20T09:05:00')).toBe('09:05');
    expect(formatTime('2026-04-20T18:30:00')).toBe('18:30');
  });
});

describe('fromISOInZone', () => {
  it('parses into the given zone', () => {
    const dt = fromISOInZone('2026-04-20T00:00:00+09:00', 'Asia/Tokyo');
    expect(dt.zoneName).toBe('Asia/Tokyo');
    expect(dt.hour).toBe(0);
  });

  it('keeps the same instant regardless of zone', () => {
    const tokyo = fromISOInZone('2026-04-20T00:00:00+09:00', 'Asia/Tokyo');
    const utc = fromISOInZone('2026-04-20T00:00:00+09:00', 'UTC');
    expect(tokyo.toMillis()).toBe(utc.toMillis());
    expect(utc.hour).toBe(15); // previous day 15:00 UTC
  });
});

describe('formatDateKey', () => {
  it('formats as yyyy-MM-dd', () => {
    expect(formatDateKey(DateTime.local(2026, 4, 5))).toBe('2026-04-05');
  });
});

describe('getWeekdayLabel', () => {
  it('returns Japanese labels by default', () => {
    expect(getWeekdayLabel(0)).toBe('日');
    expect(getWeekdayLabel(6)).toBe('土');
  });

  it('returns English labels when locale is en', () => {
    expect(getWeekdayLabel(0, 'en')).toBe('Sun');
    expect(getWeekdayLabel(1, 'en')).toBe('Mon');
  });
});

describe('formatMonthYear', () => {
  it('formats Japanese and English', () => {
    const d = DateTime.local(2026, 4, 1);
    expect(formatMonthYear(d, 'ja')).toBe('2026年4月');
    expect(formatMonthYear(d, 'en')).toBe('April 2026');
  });
});

describe('jsDayOfWeek', () => {
  it('maps Sunday to 0 and Saturday to 6', () => {
    expect(jsDayOfWeek(DateTime.local(2026, 4, 19))).toBe(0); // Sunday
    expect(jsDayOfWeek(DateTime.local(2026, 4, 25))).toBe(6); // Saturday
  });
});
