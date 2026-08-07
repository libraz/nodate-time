import { DateTime } from 'luxon';
import { type PointerEvent as ReactPointerEvent, useCallback, useMemo, useRef } from 'react';
import { type DragLanding, MonthWeekRow } from '@/components/month-week-row';
import { fromISOInZone, getMonthDays, getWeekDays, getWeekdayLabel } from '@/lib/date-utils';
import { buildMovedEvent } from '@/lib/event-move';
import { canEditEvent, roleOnCalendar } from '@/lib/permissions';
import { useEventDrag } from '@/lib/use-event-drag';
import { useHolidayRevision } from '@/lib/use-holiday-revision';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { bucketEventsByWeek, eventEndDay, NO_EVENTS, weekKeysInRange } from '@/lib/week-layout';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

export function CalendarGrid() {
  const locale = useUiStore((s) => s.locale);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const calendarView = useUiStore((s) => s.calendarView);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const holidayRevision = useHolidayRevision(holidaysCountry, [
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
  const calendars = useCalendarStore((s) => s.calendars);
  const me = useAuthStore((s) => s.user);
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();

  const touchStartRef = useRef({ x: 0, y: 0 });

  const canMove = useCallback(
    (evt: CalendarEvent) => canEditEvent(evt, roleOnCalendar(calendars, evt.calendarId), me?.id),
    [calendars, me],
  );

  // Multi-day bars live in an overlay outside the day cells, so the topmost
  // element under the cursor may not be inside a [data-day] cell. Walk every
  // element at the point and use the first one that resolves to a date cell.
  const resolveDayKey = useCallback((x: number, y: number): string | null => {
    for (const el of document.elementsFromPoint(x, y)) {
      const day = el.closest('[data-day]')?.getAttribute('data-day');
      if (day) return day;
    }
    return null;
  }, []);

  // Month drag preserves time of day: shift start by the whole-day delta between
  // the grabbed cell and the drop cell.
  const handleMoveDrop = useCallback(
    (evt: CalendarEvent, x: number, y: number, ctx: { originKey: string | null }) => {
      const targetKey = resolveDayKey(x, y);
      if (!targetKey || !ctx.originKey) return;
      const target = DateTime.fromFormat(targetKey, 'yyyy-MM-dd', { zone: timezone });
      const origin = DateTime.fromFormat(ctx.originKey, 'yyyy-MM-dd', { zone: timezone });
      const deltaDays = Math.round(target.diff(origin, 'days').days);
      if (deltaDays === 0) return;
      const newStart = fromISOInZone(evt.startAt, timezone).plus({ days: deltaDays });
      requestUpdate(evt, buildMovedEvent(evt, newStart));
    },
    [resolveDayKey, timezone, requestUpdate],
  );

  const {
    drag,
    start: startDrag,
    consumeClick,
  } = useEventDrag({
    onDrop: handleMoveDrop,
    resolveKey: resolveDayKey,
  });

  // Where the dragged event would land after dropping. Used both to highlight
  // the full span of destination cells and to draw a real-width ghost bar.
  const dragLanding = useMemo<DragLanding | null>(() => {
    if (!drag?.hoverKey || !drag.originKey) return null;
    const startDay = fromISOInZone(drag.event.startAt, timezone).startOf('day');
    const endDay = eventEndDay(drag.event, timezone);
    const hover = DateTime.fromFormat(drag.hoverKey, 'yyyy-MM-dd', { zone: timezone });
    const origin = DateTime.fromFormat(drag.originKey, 'yyyy-MM-dd', { zone: timezone });
    const delta = Math.round(hover.diff(origin, 'days').days);
    const span = Math.max(1, Math.round(endDay.diff(startDay, 'days').days) + 1);
    const start = startDay.plus({ days: delta });
    const end = start.plus({ days: span - 1 });
    return { event: drag.event, start, end };
  }, [drag, timezone]);

  // Only the rows the ghost crosses need it; every other row keeps the same
  // (null) prop and stays out of the re-render a pointer sample causes.
  const landingWeeks = useMemo(
    () => (dragLanding ? weekKeysInRange(dragLanding.start, dragLanding.end) : null),
    [dragLanding],
  );

  const days = useMemo(() => {
    if (calendarView === 'week') {
      return getWeekDays(selectedDate, timezone);
    }
    return getMonthDays(currentMonth.year, currentMonth.month - 1, timezone);
  }, [calendarView, currentMonth, selectedDate, timezone]);

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

  // The week view shows one week wherever it falls, so nothing is out of month.
  const pagedMonth = useMemo(
    () => (calendarView === 'week' ? null : { year: currentMonth.year, month: currentMonth.month }),
    [calendarView, currentMonth],
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
        if (calendarView === 'month' && dy < 0) setCalendarView('week');
        else if (calendarView === 'week' && dy > 0) setCalendarView('month');
      } else if (Math.abs(dx) > Math.abs(dy) && Math.abs(dx) > 80) {
        navigateMonth(dx < 0 ? 1 : -1);
      }
    },
    [calendarView, setCalendarView, navigateMonth],
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

  const handleEventPointerDown = useCallback(
    (evt: CalendarEvent, e: ReactPointerEvent) => {
      if (canMove(evt)) startDrag(evt, e);
    },
    [canMove, startDrag],
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
        key={`${currentMonth.year}-${currentMonth.month}-${calendarView}`}
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
