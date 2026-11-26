import { DateTime, Settings } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  columnOf,
  fetchWindow,
  formatMonthYear,
  formatRelativeTime,
  formatTime,
  fromISOInZone,
  getMonthDays,
  getWeekDays,
  getWeekdayLabel,
  gridRange,
  isToday,
  isWeekStart,
  jsDayOfWeek,
  nowInZone,
  startOfWeekFor,
  type WeekStart,
  weekdayOrder,
  weekStartWeekday,
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

describe('formatTime', () => {
  it('formats an ISO timestamp as HH:mm', () => {
    expect(formatTime('2026-04-20T09:05:00')).toBe('09:05');
    expect(formatTime('2026-04-20T18:30:00')).toBe('18:30');
  });

  it('reads the timestamp in the given zone', () => {
    expect(formatTime('2026-04-20T09:05:00+09:00', 'UTC')).toBe('00:05');
  });
});

// The machine is on UTC and the account is set to Tokyo, at an instant where
// the two disagree about the date: 16:00 UTC on the 10th is already 01:00 on
// the 11th in Tokyo. Reading the clock locally puts the today marker on the
// 10th, a day the account has already finished.
describe('isToday', () => {
  const originalZone = Settings.defaultZone;

  beforeEach(() => {
    Settings.defaultZone = 'UTC';
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-03-10T16:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
    Settings.defaultZone = originalZone;
  });

  it('marks the day the configured zone is on, not the machine', () => {
    const tokyo = (dayOfMonth: number) =>
      DateTime.fromObject({ year: 2026, month: 3, day: dayOfMonth }, { zone: 'Asia/Tokyo' });
    expect(isToday(tokyo(11), 'Asia/Tokyo')).toBe(true);
    expect(isToday(tokyo(10), 'Asia/Tokyo')).toBe(false);
  });

  it('falls back to the machine zone when no zone is configured', () => {
    expect(isToday(DateTime.local(2026, 3, 10))).toBe(true);
    expect(isToday(DateTime.local(2026, 3, 11))).toBe(false);
  });
});

describe('nowInZone', () => {
  const originalZone = Settings.defaultZone;

  beforeEach(() => {
    Settings.defaultZone = 'UTC';
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-03-10T16:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
    Settings.defaultZone = originalZone;
  });

  it('reads the current instant on the configured zone’s calendar', () => {
    expect(nowInZone('Asia/Tokyo').toISODate()).toBe('2026-03-11');
    expect(nowInZone('').toISODate()).toBe('2026-03-10');
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

describe('formatRelativeTime', () => {
  const originalZone = Settings.defaultZone;

  beforeEach(() => {
    Settings.defaultZone = 'UTC';
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-03-10T16:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
    Settings.defaultZone = originalZone;
  });

  it('falls back to Japanese when no locale is given', () => {
    expect(formatRelativeTime('2026-03-10T15:30:00Z')).toBe('30分前');
    expect(formatRelativeTime('2026-03-08T16:00:00Z')).toBe('2日前');
    expect(formatRelativeTime('2026-03-10T15:59:30Z')).toBe('たった今');
  });

  it('reads in English when the locale is en', () => {
    expect(formatRelativeTime('2026-03-10T15:30:00Z', 'en')).toBe('30m ago');
    expect(formatRelativeTime('2026-03-10T13:00:00Z', 'en')).toBe('3h ago');
    expect(formatRelativeTime('2026-03-10T15:59:30Z', 'en')).toBe('just now');
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

describe('gridRange', () => {
  it('covers every cell the month grid draws, not just the month', () => {
    // April 2026 starts on a Wednesday, so the grid opens with three days of
    // March. Asking for the month alone leaves them permanently blank, which
    // reads as an empty day rather than one nobody asked about.
    const month = DateTime.fromISO('2026-04-01T00:00:00', { zone: 'Asia/Tokyo' });
    const { start, end } = gridRange(month, 'Asia/Tokyo');
    const days = getMonthDays(2026, 3, 'Asia/Tokyo');

    expect(start).toBe(days[0]?.toISODate());
    expect(end).toBe(days[days.length - 1]?.toISODate());
    expect(start < '2026-04-01').toBe(true);
    expect(end > '2026-04-30').toBe(true);
  });

  it('spans a whole grid for a month that starts on a Sunday', () => {
    const month = DateTime.fromISO('2026-03-01T00:00:00', { zone: 'UTC' });
    const { start, end } = gridRange(month, 'UTC');
    expect(start).toBe('2026-03-01');
    expect(DateTime.fromISO(end).diff(DateTime.fromISO(start), 'days').days).toBeGreaterThan(27);
  });
});

describe('fetchWindow', () => {
  const august = DateTime.fromISO('2026-08-01T00:00:00', { zone: 'Asia/Tokyo' });

  it('asks for the whole year when the year grid is showing', () => {
    // The year grid draws twelve months of density dots. Given the month
    // window, nine of them are permanently blank and read as a year with
    // nothing planned in it.
    expect(fetchWindow('year', august)).toEqual({ start: '2026-01-01', end: '2026-12-31' });
  });

  it('asks for a month either side for the other views', () => {
    for (const view of ['month', 'week', 'list'] as const) {
      expect(fetchWindow(view, august)).toEqual({ start: '2026-07-01', end: '2026-09-30' });
    }
  });

  it('crosses the year boundary rather than clipping at it', () => {
    const december = DateTime.fromISO('2026-12-01T00:00:00', { zone: 'Asia/Tokyo' });
    expect(fetchWindow('month', december)).toEqual({ start: '2026-11-01', end: '2027-01-31' });
  });

  // The API reads the end as the last day included and refuses more than 366
  // days, so a leap year's window has to name 31 December, not 1 January.
  it('keeps a leap year within the widest window the API accepts', () => {
    const leap = DateTime.fromISO('2028-02-29T00:00:00', { zone: 'Asia/Tokyo' });
    const { start, end } = fetchWindow('year', leap);
    expect({ start, end }).toEqual({ start: '2028-01-01', end: '2028-12-31' });
    const days = DateTime.fromISO(end, { zone: 'UTC' })
      .plus({ days: 1 })
      .diff(DateTime.fromISO(start, { zone: 'UTC' }), 'days').days;
    expect(days).toBe(366);
  });
});

describe('isWeekStart', () => {
  it('accepts exactly the three starts', () => {
    expect(['mon', 'sun', 'sat'].every(isWeekStart)).toBe(true);
  });

  it.each([['MON'], ['tue'], [''], [0], [1], [null], [undefined]])('rejects %s', (v) => {
    expect(isWeekStart(v)).toBe(false);
  });
});

describe('weekStartWeekday', () => {
  it('maps each start to its Luxon weekday', () => {
    expect(weekStartWeekday('mon')).toBe(1);
    expect(weekStartWeekday('sat')).toBe(6);
    expect(weekStartWeekday('sun')).toBe(7);
  });
});

describe('weekdayOrder', () => {
  it('lists Sunday-first label indexes from the week start', () => {
    expect(weekdayOrder('sun')).toEqual([0, 1, 2, 3, 4, 5, 6]);
    expect(weekdayOrder('mon')).toEqual([1, 2, 3, 4, 5, 6, 0]);
    expect(weekdayOrder('sat')).toEqual([6, 0, 1, 2, 3, 4, 5]);
  });
});

/*
 * Pairwise model (coverwise, strength 2, 23 rows, seed 1):
 *   weekStart:    mon | sun | sat
 *   firstWeekday: mon | tue | wed | thu | fri | sat | sun   (weekday the month starts on)
 *   monthLength:  28 | 30 | 31
 * A month is anchored on a Monday (2026-06-01) shifted to the row's weekday; the
 * functions are pure on the date, so only the weekday and the length matter.
 */
describe('columnOf / startOfWeekFor over week start x first weekday x month length', () => {
  const MONDAY = DateTime.fromISO('2026-06-01', { zone: 'utc' });
  const OFFSET: Record<string, number> = { mon: 0, tue: 1, wed: 2, thu: 3, fri: 4, sat: 5, sun: 6 };
  const JS_INDEX: Record<string, number> = {
    mon: 1,
    tue: 2,
    wed: 3,
    thu: 4,
    fri: 5,
    sat: 6,
    sun: 0,
  };
  const ROWS: [WeekStart, string, number][] = [
    ['mon', 'wed', 28],
    ['sun', 'wed', 31],
    ['sat', 'fri', 28],
    ['sat', 'sun', 31],
    ['sun', 'sun', 30],
    ['sun', 'fri', 28],
    ['mon', 'sat', 31],
    ['sun', 'tue', 31],
    ['mon', 'mon', 30],
    ['sun', 'mon', 31],
    ['sun', 'thu', 30],
    ['mon', 'fri', 30],
    ['mon', 'thu', 31],
    ['sun', 'sat', 28],
    ['sun', 'fri', 31],
    ['mon', 'sun', 28],
    ['sat', 'tue', 30],
    ['sat', 'mon', 28],
    ['mon', 'tue', 28],
    ['sat', 'thu', 28],
    ['sat', 'wed', 30],
    ['sat', 'sat', 31],
    ['sat', 'sat', 30],
  ];

  it.each(ROWS)('%s start, month starting %s, %i days', (ws, firstWeekday, length) => {
    const first = MONDAY.plus({ days: OFFSET[firstWeekday] });
    const last = first.plus({ days: length - 1 });

    // The column is where the weekday sits in the header order.
    const col = columnOf(first, ws);
    expect(col).toBe(weekdayOrder(ws).indexOf(JS_INDEX[firstWeekday] as number));

    // The week's first day is the start weekday, no more than six days back.
    const weekStart = startOfWeekFor(first, ws);
    expect(weekStart.weekday).toBe(weekStartWeekday(ws));
    expect(first.diff(weekStart, 'days').days).toBe(col);
    expect(col).toBeGreaterThanOrEqual(0);
    expect(col).toBeLessThanOrEqual(6);

    // Leading blanks plus the month fill whole rows.
    const rows = Math.ceil((col + length) / 7);
    const gridStart = startOfWeekFor(first, ws);
    const lastRowStart = startOfWeekFor(last, ws);
    expect(Math.round(lastRowStart.diff(gridStart, 'days').days / 7) + 1).toBe(rows);
  });

  it('is its own start for a date that already begins the week', () => {
    const sunday = DateTime.fromISO('2026-06-07', { zone: 'utc' });
    expect(columnOf(sunday, 'sun')).toBe(0);
    expect(columnOf(sunday, 'mon')).toBe(6);
    expect(columnOf(sunday, 'sat')).toBe(1);
    expect(startOfWeekFor(sunday, 'sun').toISODate()).toBe('2026-06-07');
    expect(startOfWeekFor(sunday, 'mon').toISODate()).toBe('2026-06-01');
    expect(startOfWeekFor(sunday, 'sat').toISODate()).toBe('2026-06-06');
  });

  it('drops the time of day', () => {
    const noon = DateTime.fromISO('2026-06-10T15:30:00', { zone: 'utc' });
    expect(startOfWeekFor(noon, 'mon').toISO()).toBe('2026-06-08T00:00:00.000Z');
  });
});
