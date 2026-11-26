import type { DateTime } from 'luxon';
import { useCallback, useMemo, useRef, useState } from 'react';
import { MonthWeekRow } from '@/components/month-week-row';
import { getMonthDays, getWeekdayLabel, weekdayOrder } from '@/lib/date-utils';
import { useHolidayLoader } from '@/lib/use-holidays';
import { useMonthDrag } from '@/lib/use-month-drag';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { useIsEventShown } from '@/lib/use-shown-events';
import { bucketEventsByWeek, NO_EVENTS } from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

/** Wheel travel (px) that turns the month; a mouse notch is ~100, a trackpad flick far more. */
const WHEEL_STEP_PX = 60;
/** Quiet time that ends a wheel gesture, so one flick's inertia turns one month. */
const WHEEL_IDLE_MS = 220;

/** Whether the rows are taller than the grid and scroll. */
function overflows(el: HTMLElement | null): el is HTMLElement {
  return !!el && el.scrollHeight > el.clientHeight;
}

/** Whether a wheel in `dir` has nothing left to scroll, so it may turn the month. */
function atScrollEdge(el: HTMLElement | null, dir: number): boolean {
  if (!overflows(el)) return true;
  return dir < 0 ? el.scrollTop <= 0 : el.scrollTop + el.clientHeight >= el.scrollHeight - 1;
}

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
  const ws = useUiStore((s) => s.weekStart);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const openDayDetail = useUiStore((s) => s.openDayDetail);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setCalendarView = useUiStore((s) => s.setCalendarView);
  const navigateMonth = useUiStore((s) => s.navigateMonth);
  const events = useCalendarStore((s) => s.events);
  const isShown = useIsEventShown();
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();

  const touchStartRef = useRef({ x: 0, y: 0, inScroller: false });
  const wheelRef = useRef({ travel: 0, spent: false, active: false, timer: 0 });
  const rowsRef = useRef<HTMLDivElement>(null);

  // Which way the month just turned, so the new one arrives from that side:
  // later months come up from below, as they would in a scrolled list.
  const monthIndex = currentMonth.year * 12 + currentMonth.month;
  const [shownIndex, setShownIndex] = useState(monthIndex);
  const [direction, setDirection] = useState(0);
  if (shownIndex !== monthIndex) {
    setDirection(Math.sign(monthIndex - shownIndex));
    setShownIndex(monthIndex);
  }

  // The month view reads as a vertical strip of months: a wheel gesture turns
  // one month, however long its inertia runs. A month taller than the grid
  // scrolls first, and only a gesture that starts at the edge turns it.
  const handleWheel = useCallback(
    (e: React.WheelEvent) => {
      if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
      const w = wheelRef.current;
      window.clearTimeout(w.timer);
      w.timer = window.setTimeout(() => {
        w.travel = 0;
        w.spent = false;
        w.active = false;
      }, WHEEL_IDLE_MS);
      if (!w.active) {
        w.active = true;
        // Left to native scrolling for the whole gesture, inertia included.
        if (!atScrollEdge(rowsRef.current, Math.sign(e.deltaY))) w.spent = true;
      }
      if (w.spent) return;
      w.travel += e.deltaY;
      if (Math.abs(w.travel) >= WHEEL_STEP_PX) {
        navigateMonth(w.travel > 0 ? 1 : -1);
        w.spent = true;
      }
    },
    [navigateMonth],
  );

  const {
    drag,
    dragLanding,
    landingWeeks,
    canMove,
    consumeClick,
    consumeDragEnd,
    handleEventPointerDown,
  } = useMonthDrag({ timezone, weekStart: ws, requestUpdate });

  // The month surface, and only that: the app mounts this for the month view
  // and the weekly timeline for the week. A week branch here was a second
  // implementation of a view this component never renders.
  const days = useMemo(
    () => getMonthDays(currentMonth.year, currentMonth.month - 1, ws, timezone),
    [currentMonth, timezone, ws],
  );

  const visibleEvents = useMemo(() => events.filter(isShown), [events, isShown]);

  const weekStarts = useMemo(() => {
    const result: DateTime[] = [];
    for (let i = 0; i < days.length; i += 7) {
      const first = days[i];
      if (first) result.push(first.startOf('day'));
    }
    return result;
  }, [days]);

  const buckets = useMemo(
    () => bucketEventsByWeek(visibleEvents, timezone, ws),
    [visibleEvents, timezone, ws],
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
        const rows = rowsRef.current;
        touchStartRef.current = {
          x: touch.clientX,
          y: touch.clientY,
          inScroller: overflows(rows) && rows.contains(e.target as Node),
        };
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
      // Up switches to the week; there is no swipe back from it. Inside a
      // month taller than the grid the swipe scrolls instead.
      if (Math.abs(dy) > Math.abs(dx) && Math.abs(dy) > 60) {
        if (dy < 0 && !touchStartRef.current.inScroller) setCalendarView('week');
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
      onWheel={handleWheel}
    >
      <div className="grid shrink-0 grid-cols-7 border-b border-[var(--color-separator)]">
        {weekdayOrder(ws).map((i) => (
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
        ref={rowsRef}
        className="calendar-enter grid min-h-0 flex-1 overflow-y-auto"
        data-dir={direction}
        // A row holds its tracks and shares out whatever height is left.
        style={{ gridTemplateRows: `repeat(${weekStarts.length}, minmax(min-content, 1fr))` }}
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
              onCreateOnDay={handleDayDoubleClick}
              onDateClick={openDayDetail}
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
