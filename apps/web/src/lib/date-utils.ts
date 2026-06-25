import { DateTime } from 'luxon';
import type { Locale } from '@/i18n';
import { MONTH_NAMES_EN } from '@/i18n';

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

export function isSameDay(a: DateTime, b: DateTime): boolean {
  return a.hasSame(b, 'day');
}

export function isToday(date: DateTime): boolean {
  return date.hasSame(DateTime.now(), 'day');
}

export function formatTime(iso: string): string {
  return DateTime.fromISO(iso).toFormat('HH:mm');
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

export function formatDateKey(date: DateTime): string {
  return date.toFormat('yyyy-MM-dd');
}

export function getDayStart(date: DateTime): number {
  return date.startOf('day').toMillis();
}

export function getDayEnd(date: DateTime): number {
  return date.endOf('day').toMillis() + 1;
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

export function toDateTime(date: Date): DateTime {
  return DateTime.fromJSDate(date);
}

export function toJsDate(dt: DateTime): Date {
  return dt.toJSDate();
}

export function jsDayOfWeek(dt: DateTime): number {
  return dt.weekday % 7;
}
