import { DateTime } from 'luxon';
import type { Locale } from '@/i18n';
import { MONTH_NAMES_EN } from '@/i18n';
import type { CalendarView } from '@/types/calendar';

const WEEKDAY_LABELS_JA = ['日', '月', '火', '水', '木', '金', '土'] as const;
const WEEKDAY_LABELS_EN = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;

/** Builds a day at midnight in `zone` (or the local zone when `zone` is empty). */
function dayInZone(year: number, month: number, day: number, zone?: string): DateTime {
  return zone && zone.length > 0
    ? DateTime.fromObject({ year, month, day }, { zone })
    : DateTime.local(year, month, day);
}

export function getMonthDays(year: number, month: number, zone?: string): DateTime[] {
  const first = dayInZone(year, month + 1, 1, zone);
  const last = first.endOf('month');
  const startDay = first.weekday % 7;
  const days: DateTime[] = [];

  for (let i = startDay - 1; i >= 0; i--) {
    days.push(first.minus({ days: i + 1 }));
  }

  for (let d = 1; d <= last.day; d++) {
    days.push(dayInZone(year, month + 1, d, zone));
  }

  // Use 5 weeks (35 cells) when possible, 6 weeks (42) only when needed
  const totalCells = startDay + last.day > 35 ? 42 : 35;
  const remaining = totalCells - days.length;
  for (let d = 1; d <= remaining; d++) {
    days.push(last.plus({ days: d }));
  }

  return days;
}

/**
 * The days a month grid actually draws, as the inclusive date range to ask the
 * API for.
 *
 * A month view is 35 or 42 cells, so it always shows some of the month before
 * and after. Fetching the month itself leaves those cells permanently empty,
 * which reads as "nothing planned" rather than "not asked for".
 */
export function gridRange(month: DateTime, zone?: string): { start: string; end: string } {
  const days = getMonthDays(month.year, month.month - 1, zone);
  const first = days[0];
  const last = days[days.length - 1];
  const fallback = month.toISODate() ?? '';
  return {
    start: first?.toISODate() ?? fallback,
    end: last?.toISODate() ?? fallback,
  };
}

/**
 * The date range a view needs fetched, as ISO dates.
 *
 * Views do not all show the same span, and one window sized for the month
 * grid does not serve them. The year grid draws twelve months of density
 * dots; given three months of events, nine of them are permanently blank and
 * read as a year with nothing in it.
 *
 * The end is exclusive, matching the API's own range handling.
 */
export function fetchWindow(view: CalendarView, month: DateTime): { start: string; end: string } {
  const [from, to] =
    view === 'year'
      ? [month.startOf('year'), month.startOf('year').plus({ years: 1 })]
      : // A month either side, so scrolling to the neighbouring month has
        // something to show before its own fetch lands.
        [month.minus({ months: 1 }).startOf('month'), month.plus({ months: 2 }).startOf('month')];
  const fallback = month.toISODate() ?? '';
  return { start: from.toISODate() ?? fallback, end: to.toISODate() ?? fallback };
}

export function getWeekDays(date: DateTime, zone?: string): DateTime[] {
  const anchored = zone && zone.length > 0 ? date.setZone(zone, { keepLocalTime: true }) : date;
  const dayOfWeek = anchored.weekday % 7;
  const start = anchored.minus({ days: dayOfWeek }).startOf('day');
  const days: DateTime[] = [];
  for (let i = 0; i < 7; i++) {
    days.push(start.plus({ days: i }));
  }
  return days;
}

/**
 * True when `date` falls on today's date in `zone` (the local zone when `zone`
 * is empty).
 *
 * Which day is "today" belongs to a zone, not to the machine: between midnight
 * and 09:00 in Tokyo a browser running on UTC is still on the previous date, so
 * reading the clock locally puts the today marker one cell off and sends the
 * Today button to the neighbouring week.
 */
export function isToday(date: DateTime, zone?: string): boolean {
  return date.hasSame(nowInZone(zone), 'day');
}

/** The current instant read in `zone` (the local zone when `zone` is empty). */
export function nowInZone(zone?: string): DateTime {
  const now = DateTime.now();
  return zone && zone.length > 0 ? now.setZone(zone) : now;
}

/** Formats an ISO timestamp as `HH:mm` in `zone` (local when `zone` is empty). */
export function formatTime(iso: string, zone?: string): string {
  return fromISOInZone(iso, zone).toFormat('HH:mm');
}

/** Formats an ISO timestamp as a short relative time string (e.g. "5m ago" / "5分前"). */
export function formatRelativeTime(iso: string, locale: Locale = 'ja'): string {
  const dt = DateTime.fromISO(iso);
  const diff = DateTime.now().diff(dt, ['days', 'hours', 'minutes']);
  if (locale === 'en') {
    if (diff.days >= 1) return `${Math.floor(diff.days)}d ago`;
    if (diff.hours >= 1) return `${Math.floor(diff.hours)}h ago`;
    if (diff.minutes >= 1) return `${Math.floor(diff.minutes)}m ago`;
    return 'just now';
  }
  if (diff.days >= 1) return `${Math.floor(diff.days)}日前`;
  if (diff.hours >= 1) return `${Math.floor(diff.hours)}時間前`;
  if (diff.minutes >= 1) return `${Math.floor(diff.minutes)}分前`;
  return 'たった今';
}

/**
 * Parse an ISO timestamp into the user's selected timezone.
 * Falls back to local time if `zone` is empty.
 */
export function fromISOInZone(iso: string, zone?: string): DateTime {
  if (zone && zone.length > 0) {
    return DateTime.fromISO(iso, { zone });
  }
  return DateTime.fromISO(iso);
}

export function getWeekdayLabel(dayIndex: number, locale: Locale = 'ja'): string {
  const labels = locale === 'en' ? WEEKDAY_LABELS_EN : WEEKDAY_LABELS_JA;
  return labels[dayIndex] ?? '';
}

export function formatMonthYear(date: DateTime, locale: Locale = 'ja'): string {
  if (locale === 'en') {
    return `${MONTH_NAMES_EN[date.month - 1]} ${date.year}`;
  }
  return `${date.year}年${date.month}月`;
}

export function jsDayOfWeek(dt: DateTime): number {
  return dt.weekday % 7;
}
