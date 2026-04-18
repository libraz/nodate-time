import { useT } from '@/i18n';
import { getWeekDays, getWeekdayLabel, isToday, jsDayOfWeek } from '@/lib/date-utils';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';
import { DateTime } from 'luxon';
import { useMemo, useRef } from 'react';

const HOUR_HEIGHT = 48;
const HOURS = Array.from({ length: 24 }, (_, i) => i);

export function WeeklyTimeline() {
  const t = useT();
  const selectedDate = useUiStore((s) => s.selectedDate);
  const locale = useUiStore((s) => s.locale);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const scrollRef = useRef<HTMLDivElement>(null);

  const days = useMemo(() => getWeekDays(selectedDate), [selectedDate]);

  const { allDayEvents, timedEvents } = useMemo(() => {
    const allDay: Map<string, CalendarEvent[]> = new Map();
    const timed: Map<string, CalendarEvent[]> = new Map();

    for (const evt of events) {
      if (!activeCalendarIds.includes(evt.calendarId)) continue;
      const evtStartMs = DateTime.fromISO(evt.startAt).toMillis();
      const evtEndMs = DateTime.fromISO(evt.endAt).toMillis();
      for (const day of days) {
        const dayStart = day.startOf('day').toMillis();
        const dayEnd = day.endOf('day').toMillis() + 1;
        if (evtStartMs < dayEnd && evtEndMs > dayStart) {
          const key = day.toFormat('yyyy-MM-dd');
          if (evt.allDay) {
            const arr = allDay.get(key) ?? [];
            arr.push(evt);
            allDay.set(key, arr);
          } else {
            const arr = timed.get(key) ?? [];
            arr.push(evt);
            timed.set(key, arr);
          }
        }
      }
    }
    return { allDayEvents: allDay, timedEvents: timed };
  }, [events, activeCalendarIds, days]);

  /** Convert ISO string to pixel offset within the day column. */
  const timeToY = (iso: string, dayStartMs: number): number => {
    const ms = DateTime.fromISO(iso).toMillis() - dayStartMs;
    const hours = ms / 3600000;
    return Math.max(0, hours * HOUR_HEIGHT);
  };

  const now = DateTime.now();
  const nowMinutes = now.hour * 60 + now.minute;
  const currentTimeY = (nowMinutes / 60) * HOUR_HEIGHT;

  return (
    <div className="flex h-full flex-col">
      {/* Day headers with dates */}
      <div className="flex border-b border-[var(--color-separator)]">
        {/* Time gutter spacer */}
        <div className="w-14 shrink-0" />
        {days.map((day) => {
          const today = isToday(day);
          const dow = jsDayOfWeek(day);
          return (
            <div key={day.toISO()} className="flex flex-1 flex-col items-center py-2.5">
              <span className="text-[13px] font-medium tracking-wide text-[var(--color-text-secondary)]">
                {getWeekdayLabel(dow, locale)}
              </span>
              <span
                className="mt-0.5 flex h-7 w-7 items-center justify-center rounded-full text-[17px] font-semibold"
                style={{
                  backgroundColor: today ? 'var(--color-accent)' : 'transparent',
                  color: today
                    ? '#ffffff'
                    : dow === 0
                      ? 'var(--color-sunday)'
                      : dow === 6
                        ? 'var(--color-saturday)'
                        : 'var(--color-text-primary)',
                }}
              >
                {day.day}
              </span>
            </div>
          );
        })}
      </div>

      {/* All-day events row */}
      {Array.from(allDayEvents.values()).some((a) => a.length > 0) && (
        <div className="flex border-b border-[var(--color-separator)]">
          <div className="flex w-14 shrink-0 items-start justify-end pr-2 pt-1 text-[11px] font-medium text-[var(--color-text-tertiary)]">
            {t('calendar.allDay')}
          </div>
          {days.map((day) => {
            const key = day.toFormat('yyyy-MM-dd');
            const dayAllDay = allDayEvents.get(key) ?? [];
            return (
              <div
                key={key}
                className="flex flex-1 flex-col gap-px border-l border-[var(--color-separator)] p-0.5"
              >
                {dayAllDay.map((evt) => (
                  <button
                    key={evt.id}
                    type="button"
                    onClick={() => openEventModal(evt.id)}
                    className="truncate rounded-full px-2 text-[11px] font-semibold leading-[18px]"
                    style={{
                      backgroundColor: `${evt.color}20`,
                      color: evt.color,
                    }}
                  >
                    {evt.title}
                  </button>
                ))}
              </div>
            );
          })}
        </div>
      )}

      {/* Scrollable timeline grid */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="relative flex" style={{ height: 24 * HOUR_HEIGHT }}>
          {/* Time labels gutter */}
          <div className="relative w-14 shrink-0">
            {HOURS.map((h) => (
              <div
                key={h}
                className="absolute right-2 text-[11px] font-medium text-[var(--color-text-tertiary)]"
                style={{ top: h * HOUR_HEIGHT - 6 }}
              >
                {h}
              </div>
            ))}
          </div>

          {/* Day columns */}
          {days.map((day) => {
            const key = day.toFormat('yyyy-MM-dd');
            const dayTimed = timedEvents.get(key) ?? [];
            const dayStartMs = day.startOf('day').toMillis();
            const today = isToday(day);

            return (
              <div key={key} className="relative flex-1 border-l border-[var(--color-separator)]">
                {/* Hour grid lines */}
                {HOURS.map((h) => (
                  <div
                    key={h}
                    className="absolute left-0 right-0 border-t border-[var(--color-separator)]"
                    style={{ top: h * HOUR_HEIGHT }}
                  />
                ))}

                {/* Current time indicator */}
                {today && (
                  <div className="absolute left-0 right-0 z-10" style={{ top: currentTimeY }}>
                    <div className="flex items-start">
                      <div className="-ml-1 -mt-[3px] h-2 w-2 shrink-0 rounded-full bg-[var(--color-accent)]" />
                      <div className="h-[2px] w-full bg-[var(--color-accent)]" />
                    </div>
                  </div>
                )}

                {/* Timed event blocks */}
                {dayTimed.map((evt) => {
                  const evtStartMs = DateTime.fromISO(evt.startAt).toMillis();
                  const evtEndMs = DateTime.fromISO(evt.endAt).toMillis();
                  const clampedStart =
                    DateTime.fromMillis(Math.max(evtStartMs, dayStartMs)).toISO() ?? evt.startAt;
                  const clampedEnd =
                    DateTime.fromMillis(Math.min(evtEndMs, dayStartMs + 86400000)).toISO() ??
                    evt.endAt;
                  const top = timeToY(clampedStart, dayStartMs);
                  const height = Math.max(timeToY(clampedEnd, dayStartMs) - top, 20);
                  const startDt = DateTime.fromISO(evt.startAt);
                  const endDt = DateTime.fromISO(evt.endAt);

                  return (
                    <button
                      key={evt.id}
                      type="button"
                      onClick={() => openEventModal(evt.id)}
                      className="absolute left-0.5 right-0.5 z-[5] overflow-hidden rounded-xl px-1.5 pt-1 text-left"
                      style={{
                        top,
                        height,
                        backgroundColor: `${evt.color}15`,
                        borderLeft: `4px solid ${evt.color}`,
                      }}
                    >
                      <p className="truncate text-[13px] font-semibold text-[var(--color-text-primary)]">
                        {evt.title}
                      </p>
                      <p className="text-[11px] text-[var(--color-text-secondary)]">
                        {startDt.toFormat('H:mm')} - {endDt.toFormat('H:mm')}
                      </p>
                    </button>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
