import type { DateTime } from 'luxon';
import { useT } from '@/i18n';
import { formatTime } from '@/lib/date-utils';
import type { PublicEvent } from '@/lib/public-calendar';

/** The day as the sheet title and the tap target name it. */
export function formatSheetDay(day: DateTime, locale: string): string {
  return day.setLocale(locale).toLocaleString({
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'short',
  });
}

interface PublicDaySheetProps {
  day: DateTime;
  events: PublicEvent[];
  timezone: string;
  locale: string;
  fallbackColor: string;
  onClose: () => void;
}

/**
 * One day of a published calendar, read-only. A full-screen overlay on narrow
 * screens and an inline panel under the grid from `sm:` up.
 */
export function PublicDaySheet({
  day,
  events,
  timezone,
  locale,
  fallbackColor,
  onClose,
}: PublicDaySheetProps) {
  const t = useT();
  const title = t('share.daySheetTitle', { date: formatSheetDay(day, locale) });

  return (
    <section
      aria-label={title}
      className="fixed inset-0 z-50 flex flex-col bg-[var(--color-surface)] sm:static sm:z-auto sm:border-t sm:border-[var(--color-border)]"
    >
      <div className="flex items-center justify-between px-4 py-3 sm:px-6">
        <h2 className="text-subhead font-semibold text-[var(--color-text-primary)]">{title}</h2>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('common.close')}
          className="icon-btn h-9 w-9"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>
      <ul className="flex flex-1 flex-col gap-2 overflow-y-auto px-4 pb-4 sm:px-6">
        {events.length === 0 ? (
          <li className="py-8 text-center text-callout text-[var(--color-text-tertiary)]">
            {t('calendar.noEvents')}
          </li>
        ) : (
          events.map((evt) => (
            <li
              key={evt.id}
              className="flex items-center gap-3 rounded-[var(--radius-md)] bg-[var(--color-surface-inset)] px-3 py-2"
            >
              <span
                aria-hidden="true"
                className="h-8 w-1 shrink-0 rounded-full"
                style={{ backgroundColor: evt.color || fallbackColor }}
              />
              <div className="min-w-0">
                <div className="truncate text-callout font-medium text-[var(--color-text-primary)]">
                  {evt.detailsHidden ? t('share.busy') : (evt.title ?? '')}
                </div>
                <div className="text-footnote tabular-nums text-[var(--color-text-secondary)]">
                  {evt.allDay
                    ? t('calendar.allDay')
                    : `${formatTime(evt.startAt, timezone)} - ${formatTime(evt.endAt, timezone)}`}
                </div>
                {!evt.detailsHidden && evt.location && (
                  <div className="truncate text-footnote text-[var(--color-text-tertiary)]">
                    {evt.location}
                  </div>
                )}
              </div>
            </li>
          ))
        )}
      </ul>
    </section>
  );
}
