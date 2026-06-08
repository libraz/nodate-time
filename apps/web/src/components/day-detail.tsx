import { useMemo } from 'react';
import { useT } from '@/i18n';
import { fromISOInZone, getWeekdayLabel, jsDayOfWeek } from '@/lib/date-utils';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

export function DayDetail() {
  const t = useT();
  const selectedDate = useUiStore((s) => s.selectedDate);
  const locale = useUiStore((s) => s.locale);
  const showDayDetail = useUiStore((s) => s.showDayDetail);
  const closeDayDetail = useUiStore((s) => s.closeDayDetail);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);

  const timezone = useUiStore((s) => s.timezone);
  const dayEvents = useMemo(() => {
    const dayStartMs = selectedDate.startOf('day').toMillis();
    const dayEndMs = selectedDate.endOf('day').toMillis() + 1;
    return events.filter((e) => {
      if (!activeCalendarIds.includes(e.calendarId)) return false;
      const s = fromISOInZone(e.startAt, timezone).toMillis();
      const en = fromISOInZone(e.endAt, timezone).toMillis();
      return s < dayEndMs && en > dayStartMs;
    });
  }, [events, activeCalendarIds, selectedDate, timezone]);

  if (!showDayDetail) return null;

  const month = selectedDate.month;
  const date = selectedDate.day;
  const dayLabel = getWeekdayLabel(jsDayOfWeek(selectedDate), locale);

  // Locale-aware header date
  const headerDate =
    locale === 'en'
      ? `${dayLabel}, ${selectedDate.toFormat('MMM d')}`
      : `${month}\u6708${date}\u65E5(${dayLabel})`;

  return (
    <div className="sm:hidden">
      {/* Backdrop */}
      <button
        type="button"
        aria-label={t('common.close')}
        className="fixed inset-0 z-50 bg-[var(--color-overlay)]"
        onClick={closeDayDetail}
      />

      {/* Bottom sheet */}
      <div className="glass-surface-heavy bottom-sheet fixed inset-x-0 bottom-0 z-50 flex max-h-[85vh] flex-col overflow-hidden">
        {/* Drag handle */}
        <div className="drag-handle mx-auto mt-2 mb-1 h-1 w-10 rounded-full bg-[var(--color-text-tertiary)] opacity-30" />

        {/* Header */}
        <div className="flex items-center justify-between px-6 py-3">
          <h2 className="text-[20px] font-semibold text-[var(--color-text-primary)]">
            {headerDate}
          </h2>
          <button
            type="button"
            onClick={() => {
              closeDayDetail();
              openEventModal();
            }}
            className="text-[15px] font-medium text-[var(--color-accent)]"
          >
            {t('event.createEvent')}
          </button>
        </div>

        {/* Event list */}
        <div className="flex-1 overflow-y-auto pb-6">
          {dayEvents.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16">
              <svg
                width="48"
                height="48"
                viewBox="0 0 24 24"
                fill="none"
                stroke="var(--color-text-tertiary)"
                strokeWidth="1.5"
                opacity="0.35"
              >
                <rect x="3" y="4" width="18" height="17" rx="2" />
                <path d="M3 9h18M8 2v4M16 2v4" />
              </svg>
              <p className="mt-3 text-[15px] text-[var(--color-text-tertiary)]">
                {t('calendar.noEvents')}
              </p>
            </div>
          ) : (
            <div className="flex flex-col gap-2 px-4 pt-1">
              {dayEvents
                .sort((a, b) => a.startAt.localeCompare(b.startAt))
                .map((evt) => (
                  <button
                    key={evt.id}
                    type="button"
                    onClick={() => {
                      closeDayDetail();
                      openEventModal(evt.id);
                    }}
                    className="card-section w-full bg-[var(--color-surface-secondary)] p-4 text-left transition-colors hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
                    style={{ borderLeft: `3px solid ${evt.color}` }}
                  >
                    <p className="text-[16px] font-medium text-[var(--color-text-primary)]">
                      {evt.title}
                    </p>
                    <p className="mt-1 text-[14px] text-[var(--color-text-secondary)]">
                      {evt.allDay
                        ? t('calendar.allDay')
                        : `${fromISOInZone(evt.startAt, timezone).toFormat('HH:mm')} - ${fromISOInZone(evt.endAt, timezone).toFormat('HH:mm')}`}
                    </p>
                    {evt.location && (
                      <p className="mt-0.5 text-[13px] text-[var(--color-text-tertiary)]">
                        {evt.location}
                      </p>
                    )}
                  </button>
                ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
