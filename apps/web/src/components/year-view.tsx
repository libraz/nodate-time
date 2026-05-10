import { useT } from '@/i18n';
import {
  fromISOInZone,
  getMonthDays,
  getWeekdayLabel,
  isToday,
  jsDayOfWeek,
} from '@/lib/date-utils';
import { getHoliday } from '@/lib/holidays';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import { DateTime } from 'luxon';
import { useMemo } from 'react';

export function YearView() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const timezone = useUiStore((s) => s.timezone);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const setCalendarView = useUiStore((s) => s.setCalendarView);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);

  // Build a count of events per ISO date so we can show density (1, 2, 3+ dots).
  const eventCount = useMemo(() => {
    const map = new Map<string, number>();
    for (const evt of events) {
      if (!activeCalendarIds.includes(evt.calendarId)) continue;
      const start = fromISOInZone(evt.startAt, timezone).startOf('day');
      const end = fromISOInZone(evt.endAt, timezone).startOf('day');
      let cur = start;
      // cap to avoid pathological multi-year events
      const maxDays = 366;
      let i = 0;
      while (cur <= end && i < maxDays) {
        const key = cur.toFormat('yyyy-MM-dd');
        map.set(key, (map.get(key) ?? 0) + 1);
        cur = cur.plus({ days: 1 });
        i += 1;
      }
    }
    return map;
  }, [events, activeCalendarIds, timezone]);

  const months = useMemo(() => Array.from({ length: 12 }, (_, i) => i), []);

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="flex items-center justify-between border-b border-[var(--color-separator)] px-4 py-3">
        <h2 className="text-[18px] font-semibold text-[var(--color-text-primary)]">
          {currentMonth.year}
        </h2>
        <span className="text-[12px] text-[var(--color-text-tertiary)]">{t('calendar.year')}</span>
      </div>
      <div className="grid grid-cols-2 gap-3 p-3 sm:grid-cols-3 md:grid-cols-3 lg:grid-cols-4 xl:gap-4">
        {months.map((m) => {
          const days = getMonthDays(currentMonth.year, m);
          const monthLabel = DateTime.local(currentMonth.year, m + 1, 1).toFormat(
            locale === 'ja' ? 'M月' : 'MMM',
          );
          const monthHasToday = days.some((d) => d.month === m + 1 && isToday(d));
          return (
            <button
              type="button"
              key={m}
              onClick={() => {
                setCurrentMonth(DateTime.local(currentMonth.year, m + 1, 1));
                setCalendarView('month');
              }}
              className={`flex flex-col gap-1.5 rounded-2xl border bg-[var(--color-surface)] p-3 text-left shadow-sm transition hover:-translate-y-0.5 hover:bg-[var(--color-hover)] hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] ${
                monthHasToday ? 'border-[var(--color-accent)]/50' : 'border-[var(--color-border)]'
              }`}
            >
              <div className="flex items-baseline justify-between">
                <span
                  className={`text-[15px] font-semibold ${
                    monthHasToday
                      ? 'text-[var(--color-accent)]'
                      : 'text-[var(--color-text-primary)]'
                  }`}
                >
                  {monthLabel}
                </span>
              </div>
              <div className="grid grid-cols-7 gap-px text-center text-[10px] font-medium uppercase tracking-wide text-[var(--color-text-tertiary)]">
                {[0, 1, 2, 3, 4, 5, 6].map((i) => (
                  <span
                    key={i}
                    style={
                      i === 0
                        ? { color: 'var(--color-sunday)' }
                        : i === 6
                          ? { color: 'var(--color-saturday)' }
                          : undefined
                    }
                  >
                    {getWeekdayLabel(i, locale).slice(0, 1)}
                  </span>
                ))}
              </div>
              <div className="grid grid-cols-7 gap-px">
                {days.map((dt) => {
                  const inMonth = dt.month === m + 1;
                  const dow = jsDayOfWeek(dt);
                  const isoDate = dt.toFormat('yyyy-MM-dd');
                  const count = eventCount.get(isoDate) ?? 0;
                  const holiday = getHoliday(holidaysCountry, isoDate);
                  const today = isToday(dt);
                  const color =
                    holiday || dow === 0
                      ? 'var(--color-sunday)'
                      : dow === 6
                        ? 'var(--color-saturday)'
                        : 'var(--color-text-primary)';
                  return (
                    <span
                      key={isoDate}
                      className="relative flex h-7 items-center justify-center text-[11px] font-medium"
                      style={{
                        color: today ? 'white' : color,
                        opacity: inMonth ? 1 : 0.28,
                      }}
                    >
                      {today && (
                        <span
                          aria-hidden
                          className="absolute inset-1 rounded-full"
                          style={{ backgroundColor: 'var(--color-accent)' }}
                        />
                      )}
                      <span className="relative">{dt.day}</span>
                      {!today && count > 0 && inMonth && (
                        <span
                          aria-hidden
                          className="absolute bottom-0.5 flex gap-0.5"
                          style={{ color: 'var(--color-accent)' }}
                        >
                          {Array.from(
                            { length: Math.min(count, 3) },
                            (_, i) => `${isoDate}-${i}`,
                          ).map((key) => (
                            <span key={key} className="h-[3px] w-[3px] rounded-full bg-current" />
                          ))}
                        </span>
                      )}
                    </span>
                  );
                })}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
