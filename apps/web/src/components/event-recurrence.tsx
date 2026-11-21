import { DateTime } from 'luxon';
import { CustomSelect, DateTimeField } from '@/components/pickers';
import { type Locale, type TranslationKey, useT } from '@/i18n';
import { getWeekdayLabel, jsDayOfWeek } from '@/lib/date-utils';
import {
  getNthWeekday,
  rebuildPresetRule,
  ruleToPreset,
  UI_WEEK_START,
  WEEKDAY_CODES,
} from '@/lib/recurrence-preset';
import { useUiStore } from '@/stores/ui-store';
import type { RecurrencePreset, RecurrenceRule } from '@/types/calendar';

const WEEKDAY_NAMES_EN = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
];

/**
 * The weekday as a recurrence sentence names it ("every Tuesday", "the 3rd
 * Tuesday"). English spells the day out where the calendar's own headings
 * abbreviate it; every other locale reads the same as those headings, so it
 * takes the shared label rather than a table of its own.
 */
export function weekdayName(dayIndex: number, locale: Locale): string {
  return locale === 'en' ? (WEEKDAY_NAMES_EN[dayIndex] ?? '') : getWeekdayLabel(dayIndex, locale);
}

export function recurrenceUntilDateValue(until: string, zone: string): DateTime {
  return DateTime.fromISO(until, { zone });
}

export function clampRecurrenceUntilDate(date: DateTime, startAt: string, zone: string): DateTime {
  const min = DateTime.fromISO(startAt, { zone }).startOf('day');
  return date < min ? min : date;
}

export function recurrenceLabel(
  rule: RecurrenceRule | null,
  dt: DateTime,
  t: (key: TranslationKey, params?: Record<string, string | number>) => string,
  locale: Locale,
): string {
  if (!rule) return t('event.recurrenceNone');
  const preset = ruleToPreset(rule, dt);
  const dayLabel = weekdayName(jsDayOfWeek(dt), locale);
  switch (preset) {
    case 'daily':
      return t('event.recurrenceDaily');
    case 'weekly':
      return t('event.recurrenceWeekly', { day: dayLabel });
    case 'weekdays':
      return t('event.recurrenceWeekdays');
    case 'monthly_nth':
      return t('event.recurrenceMonthlyNth', { n: getNthWeekday(dt), day: dayLabel });
    case 'monthly_date':
      return t('event.recurrenceMonthlyDate', { date: dt.day });
    case 'yearly':
      return t('event.recurrenceYearly');
    default:
      // Custom: build a descriptive label
      if (rule.interval > 1) {
        const freqMap: Record<string, TranslationKey> = {
          daily: 'event.unitDay',
          weekly: 'event.unitWeek',
          monthly: 'event.unitMonth',
          yearly: 'event.unitYear',
        };
        const unitKey = freqMap[rule.freq] ?? 'event.unitDay';
        return `${rule.interval} ${t(unitKey)}`;
      }
      return t('event.recurrenceCustom');
  }
}

/** The repeat settings as one value: the preset it reads as and the rule it saves. */
export interface RecurrenceValue {
  preset: RecurrencePreset;
  rule: RecurrenceRule | null;
}

interface RecurrenceEditorProps {
  preset: RecurrencePreset;
  rule: RecurrenceRule | null;
  /** The first occurrence, in the zone the rule is expanded in. */
  start: DateTime;
  /** The zone the rule is expanded in. */
  zone: string;
  /** Whether to restate the rule in words under the editor. */
  showSummary: boolean;
  onChange: (update: (current: RecurrenceValue) => RecurrenceValue) => void;
}

/**
 * Picks how an event repeats: a preset named from its start, a custom rule
 * when no preset fits, and when the series ends. Every change is an updater
 * on the current value, so edits made in one event loop compose.
 */
export function RecurrenceEditor({
  preset,
  rule,
  start,
  zone,
  showSummary,
  onChange,
}: RecurrenceEditorProps) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  return (
    <>
      {/* Recurrence */}
      <div className="flex items-center justify-between py-2.5">
        <span className="text-default text-[var(--color-text-primary)]">
          {t('event.recurrence')}
        </span>
        <CustomSelect
          value={preset}
          ariaLabel={t('event.recurrence')}
          options={(() => {
            const dayLabel = weekdayName(jsDayOfWeek(start), locale);
            const nth = getNthWeekday(start);
            return [
              { value: 'none', label: t('event.recurrenceNone') },
              { value: 'daily', label: t('event.recurrenceDaily') },
              { value: 'weekly', label: t('event.recurrenceWeekly', { day: dayLabel }) },
              { value: 'weekdays', label: t('event.recurrenceWeekdays') },
              {
                value: 'monthly_nth',
                label: t('event.recurrenceMonthlyNth', { n: nth, day: dayLabel }),
              },
              {
                value: 'monthly_date',
                label: t('event.recurrenceMonthlyDate', { date: start.day }),
              },
              { value: 'yearly', label: t('event.recurrenceYearly') },
              { value: 'custom', label: t('event.recurrenceCustom') },
            ];
          })()}
          onChange={(val) => {
            const next = val as RecurrencePreset;
            if (next === 'custom') {
              onChange((f) => ({
                ...f,
                preset: 'custom',
                rule: f.rule ?? { freq: 'daily', interval: 1 },
              }));
              return;
            }
            onChange((f) => ({
              ...f,
              preset: next,
              rule: rebuildPresetRule(next, start, f.rule),
            }));
          }}
        />
      </div>

      {/* Custom recurrence dialog */}
      {preset === 'custom' && rule && (
        <div className="card-section mt-2 space-y-3 bg-[var(--color-surface-inset)] p-4">
          <div className="space-y-1.5">
            <span className="block text-body text-[var(--color-text-secondary)]">
              {t('event.recurrenceInterval')}
            </span>
            <div className="flex items-center gap-2">
              <input
                data-1p-ignore
                type="number"
                min={1}
                max={99}
                value={rule.interval}
                onChange={(e) => {
                  const val = Math.max(1, Math.min(99, Number(e.target.value)));
                  onChange((f) => ({
                    ...f,
                    rule: f.rule ? { ...f.rule, interval: val } : null,
                  }));
                }}
                style={{ width: '4rem' }}
                className="input-modern shrink-0 text-center text-body"
              />
              <CustomSelect
                value={rule.freq}
                ariaLabel={t('event.recurrenceInterval')}
                options={[
                  { value: 'daily', label: t('event.unitDay') },
                  { value: 'weekly', label: t('event.unitWeek') },
                  { value: 'monthly', label: t('event.unitMonth') },
                  { value: 'yearly', label: t('event.unitYear') },
                ]}
                onChange={(val) => {
                  const freq = val as RecurrenceRule['freq'];
                  const dayCode = WEEKDAY_CODES[start.weekday % 7] ?? 'SU';
                  onChange((f) => {
                    if (!f.rule) return f;
                    // Seed defaults so the rule stays valid for its frequency.
                    const { byDay, byMonthDay, bySetPos, wkst, ...kept } = f.rule;
                    const base: RecurrenceRule = { ...kept, freq };
                    if (freq === 'weekly') {
                      base.byDay = [dayCode];
                      base.wkst = UI_WEEK_START;
                    }
                    if (freq === 'monthly') base.byMonthDay = [start.day];
                    return { ...f, rule: base };
                  });
                }}
              />
            </div>
          </div>

          {/* Weekly: pick one or more weekdays */}
          {rule.freq === 'weekly' && (
            <div className="space-y-1.5">
              <span className="block text-body text-[var(--color-text-secondary)]">
                {t('event.recurrenceWeekdaysLabel')}
              </span>
              <div className="flex flex-wrap gap-1.5">
                {WEEKDAY_CODES.map((code, idx) => {
                  const selected = rule?.byDay?.includes(code) ?? false;
                  return (
                    <button
                      key={code}
                      type="button"
                      aria-pressed={selected}
                      onClick={() =>
                        onChange((f) => {
                          if (!f.rule) return f;
                          const cur = f.rule.byDay ?? [];
                          const next = cur.includes(code)
                            ? cur.filter((d) => d !== code)
                            : [...cur, code];
                          // Keep at least one weekday selected.
                          return {
                            ...f,
                            rule: {
                              ...f.rule,
                              byDay: next.length > 0 ? next : cur,
                            },
                          };
                        })
                      }
                      className="flex h-8 w-8 items-center justify-center rounded-full text-caption font-semibold transition-colors"
                      style={{
                        backgroundColor: selected
                          ? 'var(--color-accent)'
                          : 'var(--color-surface-secondary)',
                        color: selected
                          ? 'var(--color-text-on-accent)'
                          : 'var(--color-text-secondary)',
                      }}
                    >
                      {getWeekdayLabel(idx, locale)}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Monthly: by day-of-month or by nth weekday */}
          {rule.freq === 'monthly' && (
            <div className="space-y-1.5">
              <span className="block text-body text-[var(--color-text-secondary)]">
                {t('event.recurrenceMonthlyMode')}
              </span>
              <CustomSelect
                value={rule.bySetPos?.length ? 'weekday' : 'date'}
                ariaLabel={t('event.recurrenceMonthlyMode')}
                options={[
                  { value: 'date', label: t('event.recurrenceMonthlyByDate') },
                  { value: 'weekday', label: t('event.recurrenceMonthlyByWeekday') },
                ]}
                onChange={(mode) => {
                  onChange((f) => {
                    if (!f.rule) return f;
                    if (mode === 'weekday') {
                      const { byMonthDay, ...kept } = f.rule;
                      return {
                        ...f,
                        rule: {
                          ...kept,
                          bySetPos: [getNthWeekday(start)],
                          byDay: [WEEKDAY_CODES[start.weekday % 7] ?? 'SU'],
                        },
                      };
                    }
                    const { bySetPos, byDay, ...kept } = f.rule;
                    return {
                      ...f,
                      rule: { ...kept, byMonthDay: [start.day] },
                    };
                  });
                }}
              />
              {rule.bySetPos?.length ? (
                <div className="flex items-center gap-2">
                  <CustomSelect
                    value={String(rule.bySetPos[0])}
                    ariaLabel={t('event.recurrenceMonthlyByWeekday')}
                    options={[
                      { value: '1', label: t('event.recurrenceNthFirst') },
                      { value: '2', label: t('event.recurrenceNthSecond') },
                      { value: '3', label: t('event.recurrenceNthThird') },
                      { value: '4', label: t('event.recurrenceNthFourth') },
                      // The fifth is what a start on the 29th-31st makes; the server
                      // counts 1-5 from the start of the month and has no "last".
                      { value: '5', label: t('event.recurrenceNthFifth') },
                    ]}
                    onChange={(pos) =>
                      onChange((f) => ({
                        ...f,
                        rule: f.rule ? { ...f.rule, bySetPos: [Number(pos)] } : null,
                      }))
                    }
                  />
                  <CustomSelect
                    value={rule.byDay?.[0] ?? 'SU'}
                    ariaLabel={t('event.recurrenceWeekdaysLabel')}
                    options={WEEKDAY_CODES.map((code, idx) => ({
                      value: code,
                      label: weekdayName(idx, locale) || code,
                    }))}
                    onChange={(day) =>
                      onChange((f) => ({
                        ...f,
                        rule: f.rule ? { ...f.rule, byDay: [day] } : null,
                      }))
                    }
                  />
                </div>
              ) : (
                <input
                  data-1p-ignore
                  type="number"
                  min={1}
                  max={31}
                  value={rule.byMonthDay?.[0] ?? start.day}
                  onChange={(e) => {
                    const val = Math.max(1, Math.min(31, Number(e.target.value)));
                    onChange((f) => ({
                      ...f,
                      rule: f.rule ? { ...f.rule, byMonthDay: [val] } : null,
                    }));
                  }}
                  style={{ width: '4rem' }}
                  className="input-modern shrink-0 text-center text-body"
                />
              )}
            </div>
          )}
        </div>
      )}

      {/* Any repeating rule may carry an end, whichever preset it reads as,
        so the end condition is never tucked behind the custom editor. */}
      {rule && (
        <div className="card-section mt-2 bg-[var(--color-surface-inset)] p-4">
          <div className="space-y-1.5">
            <span className="text-body text-[var(--color-text-secondary)]">
              {t('event.recurrenceEndLabel')}
            </span>
            <div className="flex flex-col gap-1.5">
              <label className="flex items-center gap-2">
                <input
                  type="radio"
                  name="recEnd"
                  checked={!rule.until && !rule.count}
                  onChange={() =>
                    onChange((f) => {
                      if (!f.rule) return { ...f, rule: null };
                      const { until, count, ...kept } = f.rule;
                      return { ...f, rule: kept };
                    })
                  }
                  className="accent-[var(--color-accent)]"
                />
                <span className="text-body text-[var(--color-text-primary)]">
                  {t('event.recurrenceEndNever')}
                </span>
              </label>
              <label className="flex flex-wrap items-center gap-2">
                <input
                  type="radio"
                  name="recEnd"
                  checked={!!rule.until}
                  onChange={() => {
                    const defaultEnd = start.plus({ months: 1 }).toFormat('yyyy-MM-dd');
                    onChange((f) => {
                      if (!f.rule) return { ...f, rule: null };
                      const { count, ...kept } = f.rule;
                      return { ...f, rule: { ...kept, until: defaultEnd } };
                    });
                  }}
                  className="accent-[var(--color-accent)]"
                />
                <span className="shrink-0 whitespace-nowrap text-body text-[var(--color-text-primary)]">
                  {t('event.recurrenceEndDate')}:
                </span>
                {rule.until && (
                  <DateTimeField
                    label=""
                    dateValue={recurrenceUntilDateValue(rule.until, zone)}
                    timeValue="00:00"
                    showTime={false}
                    onDateChange={(date) =>
                      onChange((f) => {
                        const clamped = clampRecurrenceUntilDate(date, start.toISO() ?? '', zone);
                        return {
                          ...f,
                          rule: f.rule
                            ? { ...f.rule, until: clamped.toFormat('yyyy-MM-dd') }
                            : null,
                        };
                      })
                    }
                    onTimeChange={() => {}}
                    minDate={start.startOf('day')}
                  />
                )}
              </label>
              <label className="flex flex-wrap items-center gap-2">
                <input
                  type="radio"
                  name="recEnd"
                  checked={!!rule.count}
                  onChange={() =>
                    onChange((f) => {
                      if (!f.rule) return { ...f, rule: null };
                      const { until, ...kept } = f.rule;
                      return { ...f, rule: { ...kept, count: 10 } };
                    })
                  }
                  className="accent-[var(--color-accent)]"
                />
                <span className="shrink-0 whitespace-nowrap text-body text-[var(--color-text-primary)]">
                  {t('event.recurrenceEndCount')}:
                </span>
                {rule.count && (
                  <input
                    data-1p-ignore
                    type="number"
                    min={1}
                    max={365}
                    value={rule.count}
                    onChange={(e) => {
                      const val = Math.max(1, Math.min(365, Number(e.target.value)));
                      onChange((f) => ({
                        ...f,
                        rule: f.rule ? { ...f.rule, count: val } : null,
                      }));
                    }}
                    style={{ width: '4rem' }}
                    className="input-modern shrink-0 text-center text-body"
                  />
                )}
              </label>
            </div>
          </div>
        </div>
      )}

      {/* Recurrence label for editing recurring events */}
      {showSummary && rule && (
        <p className="mt-1 text-footnote text-[var(--color-text-tertiary)]">
          {recurrenceLabel(rule, start, t, locale)}
        </p>
      )}
    </>
  );
}
