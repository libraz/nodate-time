import type { DateTime } from 'luxon';
import type { WeekStart } from '@/lib/date-utils';
import type { RecurrencePreset, RecurrenceRule } from '@/types/calendar';

export const WEEKDAY_CODES = ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'] as const;

/** The rule week start matching the grid's; the API takes no `SA`, so Saturday maps to `SU`. */
export function wkstFor(ws: WeekStart): 'MO' | 'SU' {
  return ws === 'mon' ? 'MO' : 'SU';
}

/** The 1-based position of `dt`'s weekday within its month (1-5). */
export function getNthWeekday(dt: DateTime): number {
  return Math.ceil(dt.day / 7);
}

export function presetToRule(
  preset: RecurrencePreset,
  dt: DateTime,
  ws: WeekStart,
): RecurrenceRule | null {
  const dayIdx = dt.weekday % 7;
  const dayCode = WEEKDAY_CODES[dayIdx] ?? 'SU';
  switch (preset) {
    case 'none':
      return null;
    case 'daily':
      return { freq: 'daily', interval: 1 };
    case 'weekly':
      return { freq: 'weekly', interval: 1, byDay: [dayCode], wkst: wkstFor(ws) };
    case 'weekdays':
      return {
        freq: 'weekly',
        interval: 1,
        byDay: ['MO', 'TU', 'WE', 'TH', 'FR'],
        wkst: wkstFor(ws),
      };
    case 'monthly_nth':
      return { freq: 'monthly', interval: 1, bySetPos: [getNthWeekday(dt)], byDay: [dayCode] };
    case 'monthly_date':
      return { freq: 'monthly', interval: 1, byMonthDay: [dt.day] };
    case 'yearly':
      return { freq: 'yearly', interval: 1 };
    case 'custom':
      return null;
  }
}

/**
 * The rule a preset makes for `dt`, keeping the end condition the current rule
 * carries. A preset says how a series repeats, not when it stops, so rebuilding
 * one on a date or preset change must not quietly make the series endless.
 */
export function rebuildPresetRule(
  preset: RecurrencePreset,
  dt: DateTime,
  current: RecurrenceRule,
): RecurrenceRule | null {
  // A stored rule keeps its own week start, so the one passed here is dropped below.
  const next = presetToRule(preset, dt, 'sun');
  if (!next) return next;
  const { wkst: _newWkst, ...rest } = next;
  return {
    ...rest,
    ...(current.wkst ? { wkst: current.wkst } : {}),
    ...(current.until ? { until: current.until } : {}),
    ...(current.count ? { count: current.count } : {}),
  };
}

export function ruleToPreset(rule: RecurrenceRule | null, dt: DateTime): RecurrencePreset {
  if (!rule) return 'none';
  const dayIdx = dt.weekday % 7;
  const dayCode = WEEKDAY_CODES[dayIdx] ?? 'SU';

  if (rule.freq === 'daily' && rule.interval === 1) return 'daily';
  if (rule.freq === 'yearly' && rule.interval === 1) return 'yearly';
  if (rule.freq === 'weekly' && rule.interval === 1) {
    if (
      rule.byDay?.length === 5 &&
      ['MO', 'TU', 'WE', 'TH', 'FR'].every((d) => rule.byDay?.includes(d))
    )
      return 'weekdays';
    if (rule.byDay?.length === 1 && rule.byDay[0] === dayCode) return 'weekly';
  }
  if (rule.freq === 'monthly' && rule.interval === 1) {
    if (rule.bySetPos?.length && rule.byDay?.length === 1) return 'monthly_nth';
    if (rule.byMonthDay?.length === 1) return 'monthly_date';
  }
  return 'custom';
}
