import { useMemo } from 'react';
import { OwnerMark } from '@/components/owner-mark';
import { useT } from '@/i18n';
import { writableVisibleCalendars } from '@/lib/create-event';
import { formatTime, fromISOInZone, getWeekdayLabel, jsDayOfWeek } from '@/lib/date-utils';
import { isGroupEvent } from '@/lib/event-group';
import { flexibilityLabelKey } from '@/lib/event-style';
import { showAsLabelKey } from '@/lib/permissions';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useSheetDrag } from '@/lib/use-sheet-drag';
import { eventOccupiesDay } from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

/**
 * The time a row states: a span reads from one time to another, and an event
 * whose end equals its start is a moment rather than a span. Rendering it as a
 * range gives "9:00 - 9:00", which reads as a mistake in the data.
 */
function eventTimeLabel(evt: { startAt: string; endAt: string }, zone: string): string {
  const start = formatTime(evt.startAt, zone);
  // Compared as instants: the same moment reaches here written more than one
  // way, and a marker is a marker whichever spelling it arrives in.
  const marker = +fromISOInZone(evt.endAt, zone) === +fromISOInZone(evt.startAt, zone);
  return marker ? start : `${start} - ${formatTime(evt.endAt, zone)}`;
}

export function DayDetail() {
  const t = useT();
  const selectedDate = useUiStore((s) => s.selectedDate);
  const locale = useUiStore((s) => s.locale);
  const showDayDetail = useUiStore((s) => s.showDayDetail);
  const closeDayDetail = useUiStore((s) => s.closeDayDetail);
  const sheetRef = useModalA11y<HTMLDivElement>(showDayDetail, closeDayDetail);
  const sheetDrag = useSheetDrag(closeDayDetail);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const calendars = useCalendarStore((s) => s.calendars);

  // Allow adding when the user can edit at least one active calendar.
  const canAdd = writableVisibleCalendars(calendars, activeCalendarIds).length > 0;

  const timezone = useUiStore((s) => s.timezone);
  const dayEvents = useMemo(
    () =>
      events.filter(
        (e) =>
          activeCalendarIds.includes(e.calendarId) && eventOccupiesDay(e, selectedDate, timezone),
      ),
    [events, activeCalendarIds, selectedDate, timezone],
  );

  if (!showDayDetail) return null;

  const month = selectedDate.month;
  const date = selectedDate.day;
  const dayLabel = getWeekdayLabel(jsDayOfWeek(selectedDate), locale);

  // Locale-aware header date
  const headerDate =
    locale === 'en'
      ? `${dayLabel}, ${selectedDate.setLocale(locale).toFormat('MMM d')}`
      : `${month}\u6708${date}\u65E5(${dayLabel})`;

  return (
    <div>
      {/* Backdrop */}
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)]"
        onClick={closeDayDetail}
      />

      {/* A sheet rising from the bottom on SP, a centred dialog on PC. The
          wrapper passes clicks through so the backdrop behind it still
          closes the day. */}
      <div className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex justify-center sm:inset-0 sm:items-center sm:p-4">
        <div
          ref={sheetRef}
          role="dialog"
          aria-modal="true"
          aria-label={headerDate}
          className="glass-surface-heavy bottom-sheet pointer-events-auto flex max-h-[85vh] w-full flex-col overflow-hidden sm:max-w-[420px] sm:rounded-b-[var(--radius-xl)]"
          style={sheetDrag.style}
        >
          {/* Grab zone: drag down to dismiss */}
          <div
            className="shrink-0 cursor-grab touch-none pt-3 pb-2.5 active:cursor-grabbing sm:hidden"
            onPointerDown={sheetDrag.onPointerDown}
          >
            <div className="drag-handle mx-auto h-1 w-10 rounded-full bg-[var(--color-text-tertiary)] opacity-30" />
          </div>

          {/* Header */}
          <div className="flex items-center justify-between px-6 py-3">
            <h2 className="text-heading font-semibold text-[var(--color-text-primary)]">
              {headerDate}
            </h2>
            {canAdd && (
              <button
                type="button"
                onClick={() => {
                  closeDayDetail();
                  openEventModal();
                }}
                className="text-callout font-medium text-[var(--color-accent-ink)]"
              >
                {t('event.createEvent')}
              </button>
            )}
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
                <p className="mt-3 text-callout text-[var(--color-text-tertiary)]">
                  {t('calendar.noEvents')}
                </p>
              </div>
            ) : (
              <div className="flex flex-col gap-2 px-4 pt-1">
                {dayEvents
                  .sort((a, b) => a.startAt.localeCompare(b.startAt))
                  .map((evt) => {
                    const flexKey = flexibilityLabelKey(evt.flexibility, { short: true });
                    const state = [
                      evt.showAs !== 'busy' ? t(showAsLabelKey(evt.showAs)) : '',
                      flexKey ? t(flexKey) : '',
                    ]
                      .filter(Boolean)
                      .join(' · ');
                    return (
                      <button
                        key={evt.id}
                        type="button"
                        onClick={() => {
                          closeDayDetail();
                          openEventModal(evt.id);
                        }}
                        className="card-section flex w-full items-start gap-3 bg-[var(--color-surface-secondary)] p-4 text-left transition-colors hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
                      >
                        <OwnerMark
                          name={evt.ownerName}
                          avatarUrl={evt.ownerAvatarUrl}
                          color={evt.color}
                          size={24}
                          group={isGroupEvent(evt)}
                          className="mt-0.5"
                        />
                        <span className="min-w-0 flex-1">
                          <p className="text-subhead font-medium text-[var(--color-text-primary)]">
                            {evt.title}
                          </p>
                          <p className="mt-1 text-default tabular-nums text-[var(--color-text-secondary)]">
                            {evt.allDay ? t('calendar.allDay') : eventTimeLabel(evt, timezone)}
                          </p>
                          {evt.location && (
                            <p className="mt-0.5 text-body text-[var(--color-text-tertiary)]">
                              {evt.location}
                            </p>
                          )}
                          {state && (
                            <p className="mt-0.5 text-body text-[var(--color-text-tertiary)]">
                              {state}
                            </p>
                          )}
                          {evt.memo.trim() && (
                            <p
                              aria-hidden="true"
                              className="mt-0.5 line-clamp-2 whitespace-pre-line break-words text-body text-[var(--color-text-tertiary)]"
                            >
                              {evt.memo}
                            </p>
                          )}
                        </span>
                      </button>
                    );
                  })}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
