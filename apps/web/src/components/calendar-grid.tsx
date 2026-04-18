import { useT } from '@/i18n';
import { getMonthDays, getWeekDays, getWeekdayLabel, isToday, jsDayOfWeek } from '@/lib/date-utils';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';
import { DateTime } from 'luxon';
import { useCallback, useMemo, useRef } from 'react';

/** Max visible event bars per cell before showing "+N" overflow. */
const MAX_VISIBLE_EVENTS = 3;

export function CalendarGrid() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const calendarView = useUiStore((s) => s.calendarView);
  const openDayDetail = useUiStore((s) => s.openDayDetail);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setCalendarView = useUiStore((s) => s.setCalendarView);
  const navigateMonth = useUiStore((s) => s.navigateMonth);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);

  const touchStartRef = useRef({ x: 0, y: 0 });

  const days = useMemo(() => {
    if (calendarView === 'week') {
      return getWeekDays(selectedDate);
    }
    return getMonthDays(currentMonth.year, currentMonth.month - 1);
  }, [calendarView, currentMonth, selectedDate]);

  const eventsMap = useMemo(() => {
    const map = new Map<string, CalendarEvent[]>();
    for (const evt of events) {
      if (!activeCalendarIds.includes(evt.calendarId)) continue;
      const startDt = DateTime.fromISO(evt.startAt).startOf('day');
      const endDt = DateTime.fromISO(evt.endAt).startOf('day');
      let current = startDt;

      while (current <= endDt) {
        const key = current.toFormat('yyyy-MM-dd');
        const arr = map.get(key) ?? [];
        arr.push(evt);
        map.set(key, arr);
        current = current.plus({ days: 1 });
      }
    }
    return map;
  }, [events, activeCalendarIds]);

  const getEventsForDay = useCallback(
    (date: DateTime) => eventsMap.get(date.toFormat('yyyy-MM-dd')) ?? [],
    [eventsMap],
  );

  const handleTouchStart = useCallback((e: React.TouchEvent) => {
    const touch = e.touches[0];
    if (touch) {
      touchStartRef.current = { x: touch.clientX, y: touch.clientY };
    }
  }, []);

  const handleTouchEnd = useCallback(
    (e: React.TouchEvent) => {
      const touch = e.changedTouches[0];
      if (!touch) return;
      const dx = touch.clientX - touchStartRef.current.x;
      const dy = touch.clientY - touchStartRef.current.y;

      if (Math.abs(dy) > Math.abs(dx) && Math.abs(dy) > 60) {
        if (calendarView === 'month' && dy < 0) {
          setCalendarView('week');
        } else if (calendarView === 'week' && dy > 0) {
          setCalendarView('month');
        }
      } else if (Math.abs(dx) > Math.abs(dy) && Math.abs(dx) > 80) {
        navigateMonth(dx < 0 ? 1 : -1);
      }
    },
    [calendarView, setCalendarView, navigateMonth],
  );

  const isCurrentMonth = useCallback(
    (date: DateTime) => date.month === currentMonth.month && date.year === currentMonth.year,
    [currentMonth],
  );

  /** Resolve date number color based on day-of-week. */
  const getDateColor = (dow: number): string => {
    if (dow === 0) return 'var(--color-sunday)';
    if (dow === 6) return 'var(--color-saturday)';
    return 'var(--color-text-primary)';
  };

  const weekRows = calendarView === 'month' ? 6 : 1;

  // Suppress unused variable warning -- t is needed for future additions
  void t;

  return (
    <div
      className="flex h-full select-none flex-col"
      onTouchStart={handleTouchStart}
      onTouchEnd={handleTouchEnd}
    >
      {/* Weekday headers */}
      <div className="grid shrink-0 grid-cols-7 border-b border-[var(--color-separator)]">
        {[0, 1, 2, 3, 4, 5, 6].map((i) => (
          <div
            key={i}
            className="py-2.5 text-center text-[13px] font-medium uppercase tracking-wide text-[var(--color-text-secondary)] max-sm:text-[11px]"
          >
            {getWeekdayLabel(i, locale)}
          </div>
        ))}
      </div>

      {/* Day cell grid -- fills remaining height */}
      <div
        className="grid flex-1 grid-cols-7"
        style={{ gridTemplateRows: `repeat(${weekRows}, minmax(0, 1fr))` }}
      >
        {days.map((dt) => {
          const dayEvents = getEventsForDay(dt);
          const today = isToday(dt);
          const inMonth = calendarView === 'week' || isCurrentMonth(dt);
          const dow = jsDayOfWeek(dt);
          const overflow = dayEvents.length - MAX_VISIBLE_EVENTS;

          return (
            <button
              key={dt.toISO()}
              type="button"
              onClick={() => {
                setSelectedDate(dt);
                openDayDetail(dt);
              }}
              className="relative flex flex-col items-start overflow-hidden border-b border-r border-[var(--color-separator)] px-1 pt-1.5 pb-1 hover:bg-[var(--color-hover)]"
              style={!inMonth ? { opacity: 0.4 } : undefined}
            >
              {/* Date number */}
              <div className="flex w-full justify-start pl-0.5">
                {today ? (
                  <span className="flex h-7 w-7 items-center justify-center rounded-full bg-[var(--color-accent)] text-[15px] font-medium text-white max-sm:text-[13px]">
                    {dt.day}
                  </span>
                ) : (
                  <span
                    className="flex h-7 w-7 items-center justify-center text-[15px] font-medium max-sm:text-[13px]"
                    style={{ color: getDateColor(dow) }}
                  >
                    {dt.day}
                  </span>
                )}
              </div>

              {/* Event bars */}
              <div className="mt-0.5 flex w-full flex-col gap-px">
                {dayEvents.slice(0, MAX_VISIBLE_EVENTS).map((evt) => (
                  <div
                    key={evt.id}
                    className="mx-0.5 truncate rounded-full border-l-[3px] px-1.5 text-[11px] font-semibold leading-[20px] max-sm:rounded-[6px] max-sm:text-[10px] max-sm:leading-[15px]"
                    style={{
                      backgroundColor: `${evt.color}18`,
                      borderLeftColor: evt.color,
                      color: evt.color,
                    }}
                  >
                    {evt.allDay ? '' : `${DateTime.fromISO(evt.startAt).toFormat('H:mm')} `}
                    {evt.title}
                  </div>
                ))}
                {overflow > 0 && (
                  <span className="mt-px text-center text-[11px] font-medium text-[var(--color-accent)] max-sm:text-[10px]">
                    +{overflow}
                  </span>
                )}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
