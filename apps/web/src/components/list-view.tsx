import { DateTime } from 'luxon';
import { type CSSProperties, useLayoutEffect, useMemo, useRef } from 'react';
import { useT } from '@/i18n';
import { groupByDay } from '@/lib/agenda';
import { formatMonthYear, fromISOInZone, isToday, nowInZone } from '@/lib/date-utils';
import { getHoliday } from '@/lib/holidays';
import { useHolidayLoader } from '@/lib/use-holidays';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

export function ListView() {
  const t = useT();
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const timezone = useUiStore((s) => s.timezone);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const locale = useUiStore((s) => s.locale);
  const scrollRef = useRef<HTMLDivElement>(null);

  const grouped = useMemo(
    () => groupByDay(events, activeCalendarIds, timezone),
    [events, activeCalendarIds, timezone],
  );
  const holidayYears = useMemo(
    () => grouped.map(([date]) => Number(date.slice(0, 4))).filter(Number.isFinite),
    [grouped],
  );
  // The rows are drawn here rather than by a memoised child, so re-rendering on
  // the load is enough and the revision itself has no reader.
  useHolidayLoader(holidaysCountry, holidayYears);

  const todayKey = nowInZone(timezone).toFormat('yyyy-MM-dd');
  // The first day still ahead: an agenda is read from now on, so it opens there
  // with the days already past above it rather than at the top of the month.
  const firstUpcoming = grouped.find(([date]) => date >= todayKey)?.[0] ?? '';
  useLayoutEffect(() => {
    const container = scrollRef.current;
    if (!container || !firstUpcoming) return;
    const row = container.querySelector<HTMLElement>(`[data-agenda-day="${firstUpcoming}"]`);
    if (row) container.scrollTop = row.offsetTop - container.offsetTop;
  }, [firstUpcoming]);

  return (
    <div ref={scrollRef} className="relative flex h-full flex-col overflow-y-auto">
      {grouped.length === 0 && (
        <div className="flex flex-1 items-center justify-center py-20 text-default text-[var(--color-text-secondary)]">
          {t('calendar.noEvents')}
        </div>
      )}
      {grouped.map(([dateStr, evts], index) => {
        const dt = DateTime.fromISO(dateStr, { zone: timezone });
        const today = isToday(dt, timezone);
        const past = dateStr < todayKey;
        const weekday = dt.weekday % 7;
        const holiday = holidaysCountry ? getHoliday(holidaysCountry, dateStr) : null;
        const dayColor = today
          ? 'today-badge bg-[var(--color-accent)] text-white'
          : holiday || weekday === 0
            ? 'text-[var(--color-sunday)]'
            : weekday === 6
              ? 'text-[var(--color-saturday)]'
              : 'text-[var(--color-text-primary)]';
        const prev = grouped[index - 1]?.[0];
        const opensMonth = !prev || prev.slice(0, 7) !== dateStr.slice(0, 7);
        return (
          <section key={dateStr} data-agenda-day={dateStr}>
            {opensMonth && (
              <h3 className="px-5 pb-1 pt-5 text-footnote font-semibold tabular-nums tracking-wide text-[var(--color-text-secondary)]">
                {formatMonthYear(dt, locale)}
              </h3>
            )}
            <div
              className={`flex gap-4 px-5 py-2.5 transition-opacity ${past ? 'opacity-55' : ''}`}
            >
              <div className="sticky top-2 flex w-11 shrink-0 flex-col items-center self-start">
                <span className="text-caption font-medium text-[var(--color-text-secondary)]">
                  {dt.setLocale(locale === 'en' ? 'en' : 'ja').toFormat('ccc')}
                </span>
                <span
                  className={`mt-0.5 flex h-9 w-9 items-center justify-center rounded-full text-title font-semibold tabular-nums ${dayColor}`}
                >
                  {dt.day}
                </span>
              </div>
              <div className="min-w-0 flex-1">
                {holiday && (
                  <p className="mb-1.5 truncate pt-1 text-footnote font-medium text-[var(--color-sunday)]">
                    {holiday.name}
                  </p>
                )}
                <ul className="flex flex-col gap-1.5">
                  {evts.map((evt) => {
                    const start = fromISOInZone(evt.startAt, timezone);
                    const end = fromISOInZone(evt.endAt, timezone);
                    return (
                      <li key={evt.id}>
                        <button
                          type="button"
                          onClick={() => openEventModal(evt.id)}
                          className="event-block flex w-full items-center gap-3 rounded-[var(--radius-sm)] px-3.5 py-2.5 text-left"
                          style={{ '--evt': evt.color } as CSSProperties}
                        >
                          <span
                            aria-hidden
                            className="h-2 w-2 shrink-0 rounded-full"
                            style={{ backgroundColor: evt.color }}
                          />
                          <div className="min-w-0 flex-1">
                            <p className="event-block-title truncate text-default font-semibold">
                              {evt.title}
                            </p>
                            {evt.location && (
                              <p className="truncate text-footnote text-[var(--color-text-secondary)]">
                                {evt.location}
                              </p>
                            )}
                          </div>
                          <span className="shrink-0 text-footnote tabular-nums text-[var(--color-text-secondary)]">
                            {evt.allDay
                              ? t('calendar.allDay')
                              : `${start.toFormat('H:mm')} – ${end.toFormat('H:mm')}`}
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </div>
            </div>
          </section>
        );
      })}
      {/* Clears the phone FAB, which floats over the last row. */}
      <div className="h-24 shrink-0 sm:h-6" />
    </div>
  );
}
