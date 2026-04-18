import type { Locale } from '@/i18n';
import { MONTH_NAMES_EN } from '@/i18n';
import { DateTime } from 'luxon';

const WEEKDAY_LABELS_JA = ['日', '月', '火', '水', '木', '金', '土'] as const;
const WEEKDAY_LABELS_EN = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;

export function getMonthDays(year: number, month: number): DateTime[] {
  const first = DateTime.local(year, month + 1, 1);
  const last = first.endOf('month');
  const startDay = first.weekday % 7;
  const days: DateTime[] = [];

  for (let i = startDay - 1; i >= 0; i--) {
    days.push(first.minus({ days: i + 1 }));
  }

  for (let d = 1; d <= last.day; d++) {
    days.push(DateTime.local(year, month + 1, d));
  }

  const remaining = 42 - days.length;
  for (let d = 1; d <= remaining; d++) {
    days.push(last.plus({ days: d }));
  }

  return days;
}

export function getWeekDays(date: DateTime): DateTime[] {
  const dayOfWeek = date.weekday % 7;
  const start = date.minus({ days: dayOfWeek });
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
