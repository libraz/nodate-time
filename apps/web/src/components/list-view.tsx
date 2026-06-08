import { DateTime } from 'luxon';
import { useMemo } from 'react';
import { useT } from '@/i18n';
import { fromISOInZone } from '@/lib/date-utils';
import { getHoliday } from '@/lib/holidays';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

export function ListView() {
  const t = useT();
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const timezone = useUiStore((s) => s.timezone);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const locale = useUiStore((s) => s.locale);

  const grouped = useMemo(() => {
    const map = new Map<string, CalendarEvent[]>();
    const filtered = events
      .filter((e) => activeCalendarIds.includes(e.calendarId))
      .sort(
        (a, b) =>
          fromISOInZone(a.startAt, timezone).toMillis() -
          fromISOInZone(b.startAt, timezone).toMillis(),
      );
    for (const evt of filtered) {
      const key = fromISOInZone(evt.startAt, timezone).toFormat('yyyy-MM-dd');
      const arr = map.get(key) ?? [];
      arr.push(evt);
      map.set(key, arr);
    }
    return Array.from(map.entries());
  }, [events, activeCalendarIds, timezone]);

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      {grouped.length === 0 && (
        <div className="flex flex-1 items-center justify-center py-20 text-[14px] text-[var(--color-text-secondary)]">
          {t('calendar.noEvents')}
        </div>
      )}
      {grouped.map(([dateStr, evts]) => {
        const dt = DateTime.fromISO(dateStr);
        const today = dt.hasSame(DateTime.now(), 'day');
        const weekday = dt.weekday % 7;
        const holiday = holidaysCountry ? getHoliday(holidaysCountry, dateStr) : null;
        const isHoliday = !!holiday;
        const dayColor = today
          ? 'bg-[var(--color-accent)] text-white shadow-md'
          : isHoliday || weekday === 0
            ? 'text-[var(--color-danger)]'
            : weekday === 6
              ? 'text-[#3a82f6]'
              : 'text-[var(--color-text-primary)]';
        return (
          <div key={dateStr} className="border-b border-[var(--color-separator)] last:border-b-0">
            <div className="sticky top-0 z-10 flex items-center gap-3 border-b border-[var(--color-separator)] bg-[var(--color-surface)]/95 px-4 py-2 backdrop-blur">
              <span
                className={`flex h-9 w-9 items-center justify-center rounded-full text-[15px] font-semibold ${dayColor}`}
              >
                {dt.day}
              </span>
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="text-[13px] font-medium text-[var(--color-text-secondary)]">
                  {dt.toFormat('yyyy.MM')} ·{' '}
                  {dt.setLocale(locale === 'en' ? 'en' : 'ja').toFormat('ccc')}
                </span>
                {isHoliday && (
                  <span className="truncate text-[12px] text-[var(--color-danger)]">
                    {holiday.name}
                  </span>
                )}
              </div>
              <span className="text-[12px] text-[var(--color-text-tertiary)]">{evts.length}</span>
            </div>
            <ul className="divide-y divide-[var(--color-separator)]">
              {evts.map((evt) => {
                const start = fromISOInZone(evt.startAt, timezone);
                const end = fromISOInZone(evt.endAt, timezone);
                return (
                  <li key={evt.id}>
                    <button
                      type="button"
                      onClick={() => openEventModal(evt.id)}
                      className="flex w-full items-start gap-3 px-4 py-3 text-left transition hover:bg-[var(--color-hover)] focus-visible:bg-[var(--color-hover)] focus-visible:outline-none"
                    >
                      <span
                        aria-hidden
                        className="mt-1 h-9 w-1 shrink-0 rounded-full"
                        style={{ backgroundColor: evt.color }}
                      />
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-[14px] font-semibold text-[var(--color-text-primary)]">
                          {evt.title}
                        </p>
                        <p className="text-[12px] text-[var(--color-text-secondary)]">
                          {evt.allDay
                            ? t('calendar.allDay')
                            : `${start.toFormat('HH:mm')} – ${end.toFormat('HH:mm')}`}
                          {evt.location && ` · ${evt.location}`}
                        </p>
                      </div>
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        );
      })}
    </div>
  );
}
