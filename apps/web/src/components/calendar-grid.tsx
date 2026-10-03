import type { DateTime } from 'luxon';
import { useCallback, useMemo, useRef } from 'react';
import { MonthWeekRow } from '@/components/month-week-row';
import { getMonthDays, getWeekdayLabel } from '@/lib/date-utils';
import { useHolidayLoader } from '@/lib/use-holidays';
import { useMonthDrag } from '@/lib/use-month-drag';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { bucketEventsByWeek, NO_EVENTS } from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

export function CalendarGrid() {
  const locale = useUiStore((s) => s.locale);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const holidayRevision = useHolidayLoader(holidaysCountry, [
    currentMonth.year - 1,
    currentMonth.year,
    currentMonth.year + 1,
  ]);
  const timezone = useUiStore((s) => s.timezone);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const openDayDetail = useUiStore((s) => s.openDayDetail);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setCalendarView = useUiStore((s) => s.setCalendarView);
  const navigateMonth = useUiStore((s) => s.navigateMonth);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();

  const touchStartRef = useRef({ x: 0, y: 0 });

  const {
    drag,
    dragLanding,
    landingWeeks,
    canMove,
    consumeClick,
    consumeDragEnd,
    handleEventPointerDown,
  } = useMonthDrag({ timezone, requestUpdate });

  // The month surface, and only that: the app mounts this for the month view
  // and the weekly timeline for the week. A week branch here was a second
  // implementation of a view this component never renders.
  const days = useMemo(
    () => getMonthDays(currentMonth.year, currentMonth.month - 1, timezone),
    [currentMonth, timezone],
  );

  const visibleEvents = useMemo(
    () => events.filter((e) => activeCalendarIds.includes(e.calendarId)),
    [events, activeCalendarIds],
  );

  const weekStarts = useMemo(() => {
    const result: DateTime[] = [];
    for (let i = 0; i < days.length; i += 7) {
      const first = days[i];
      if (first) result.push(first.startOf('day'));
    }
    return result;
  }, [days]);

  const buckets = useMemo(
    () => bucketEventsByWeek(visibleEvents, timezone),
    [visibleEvents, timezone],
  );

  // The month the grid is paging, so days either side of it can be greyed.
  const pagedMonth = useMemo(
    () => ({ year: currentMonth.year, month: currentMonth.month }),
    [currentMonth],
  );

  const handleTouchStart = useCallback(
    (e: React.TouchEvent) => {
      // A drag that ended before this touch says nothing about it.
      consumeDragEnd();
      const touch = e.touches[0];
      if (touch) {
        touchStartRef.current = { x: touch.clientX, y: touch.clientY };
      }
    },
    [consumeDragEnd],
  );

  const handleTouchEnd = useCallback(
    (e: React.TouchEvent) => {
      // The touch moved an event; its travel is not a swipe.
      if (consumeDragEnd()) return;
      const touch = e.changedTouches[0];
      if (!touch) return;
      const dx = touch.clientX - touchStartRef.current.x;
      const dy = touch.clientY - touchStartRef.current.y;
      // Up switches to the week; there is no swipe back from it.
      if (Math.abs(dy) > Math.abs(dx) && Math.abs(dy) > 60) {
        if (dy < 0) setCalendarView('week');
      } else if (Math.abs(dx) > Math.abs(dy) && Math.abs(dx) > 80) {
        navigateMonth(dx < 0 ? 1 : -1);
      }
    },
    [consumeDragEnd, setCalendarView, navigateMonth],
  );

  const handleDayDoubleClick = useCallback(
    (date: DateTime) => {
      setSelectedDate(date);
      openEventModal();
    },
    [setSelectedDate, openEventModal],
  );

  const handleEventClick = useCallback(
    (eventId: string) => {
      if (consumeClick()) return;
      openEventModal(eventId);
    },
    [consumeClick, openEventModal],
  );

  return (
    <div
      className="flex h-full select-none flex-col"
      onTouchStart={handleTouchStart}
      onTouchEnd={handleTouchEnd}
    >
      <div className="grid shrink-0 grid-cols-7 border-b border-[var(--color-separator)]">
        {[0, 1, 2, 3, 4, 5, 6].map((i) => (
          <div
            key={i}
            className="py-2.5 text-center text-body font-medium uppercase tracking-wide text-[var(--color-text-secondary)]"
          >
            {getWeekdayLabel(i, locale)}
          </div>
        ))}
      </div>

      <div
        key={`${currentMonth.year}-${currentMonth.month}`}
        className="calendar-enter grid flex-1"
        style={{ gridTemplateRows: `repeat(${weekStarts.length}, minmax(0, 1fr))` }}
      >
        {weekStarts.map((weekStart) => {
          const key = weekStart.toFormat('yyyy-MM-dd');
          return (
            <MonthWeekRow
              key={key}
              weekStart={weekStart}
              events={buckets.get(key) ?? NO_EVENTS}
              zone={timezone}
              holidaysCountry={holidaysCountry}
              holidayRevision={holidayRevision}
              selectedDate={selectedDate}
              density="comfortable"
              pagedMonth={pagedMonth}
              showHolidayName
              draggingEventId={drag?.event.id ?? null}
              dragLanding={landingWeeks?.has(key) ? dragLanding : null}
              onDayClick={setSelectedDate}
              onDayDoubleClick={handleDayDoubleClick}
              onOverflowClick={openDayDetail}
              onEventClick={handleEventClick}
              onEventPointerDown={handleEventPointerDown}
              canMoveEvent={canMove}
            />
          );
        })}
      </div>

      {scopeDialog}
    </div>
  );
}
