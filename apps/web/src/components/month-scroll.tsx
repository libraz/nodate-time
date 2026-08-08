import { DateTime } from 'luxon';
import {
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { type DragLanding, MonthWeekRow } from '@/components/month-week-row';
import { formatMonthYear, fromISOInZone, getWeekdayLabel, nowInZone } from '@/lib/date-utils';
import { buildMovedEvent } from '@/lib/event-move';
import { canEditEvent, roleOnCalendar } from '@/lib/permissions';
import { useEventDrag } from '@/lib/use-event-drag';
import { useHolidayLoader } from '@/lib/use-holidays';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { bucketEventsByWeek, eventEndDay, NO_EVENTS, weekKeysInRange } from '@/lib/week-layout';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

/**
 * How many months of weeks stay mounted either side of the anchor, and how far
 * the anchor jumps when the reader reaches an edge.
 *
 * These two move together. The range is not what the scroll needs on screen --
 * a phone shows about one month and a hard flick covers three -- it is how
 * rarely the list has to be rebuilt: reaching an edge re-anchors by the shift
 * and rebuilds every row. A wider range buys fewer rebuilds with a cost paid
 * continuously, and the grid is what costs, not the events on it. Eighteen
 * months held about 11,700 elements with an empty calendar and added roughly
 * 40ms to every re-render the view took; six holds about 4,100.
 *
 * Lower than this trades a steady cost for an intermittent visible one: at
 * three months a reader reaches an edge within a couple of flicks, and a
 * rebuild is a full remount with the scroll position restored underneath it.
 */
const RANGE_MONTHS = 6;
const RANGE_SHIFT_MONTHS = 6;
const EDGE_EXTEND_PX = 700;

const MONTH_HEADER_H = 28;

type ScrollItem =
  | { kind: 'header'; key: string; month: DateTime }
  | { kind: 'week'; key: string; dayKey: string; weekStart: DateTime };

function buildItems(anchor: DateTime, zone: string): { items: ScrollItem[]; todayKey: string } {
  const rangeStart = anchor.startOf('month').minus({ months: RANGE_MONTHS });
  const rangeEnd = anchor.startOf('month').plus({ months: RANGE_MONTHS }).endOf('month');
  const today = nowInZone(zone).startOf('day');

  const items: ScrollItem[] = [];
  const seen = new Set<string>();
  let todayKey = '';

  // First Sunday on or before the range start.
  let ws = rangeStart.minus({ days: rangeStart.weekday % 7 }).startOf('day');

  while (ws <= rangeEnd) {
    if (items.length === 0) {
      const m = ws.startOf('month');
      seen.add(m.toFormat('yyyy-MM'));
      items.push({ kind: 'header', key: `h-${m.toFormat('yyyy-MM')}`, month: m });
    }
    for (let i = 0; i < 7; i++) {
      const d = ws.plus({ days: i });
      if (d.day === 1) {
        const k = d.toFormat('yyyy-MM');
        if (!seen.has(k)) {
          seen.add(k);
          items.push({ kind: 'header', key: `h-${k}`, month: d.startOf('month') });
        }
      }
    }
    const dayKey = ws.toFormat('yyyy-MM-dd');
    const weekKey = `w-${dayKey}`;
    if (today >= ws && today < ws.plus({ days: 7 })) {
      todayKey = weekKey;
    }
    items.push({ kind: 'week', key: weekKey, dayKey, weekStart: ws });
    ws = ws.plus({ weeks: 1 });
  }

  return { items, todayKey };
}

export function MonthScroll() {
  const locale = useUiStore((s) => s.locale);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const timezone = useUiStore((s) => s.timezone);
  const scrollToTodaySignal = useUiStore((s) => s.scrollToTodaySignal);
  const openDayDetail = useUiStore((s) => s.openDayDetail);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const calendars = useCalendarStore((s) => s.calendars);
  const me = useAuthStore((s) => s.user);
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();

  const scrollRef = useRef<HTMLDivElement>(null);
  const rafRef = useRef(0);
  const activeMonthRef = useRef('');
  const initialScrollDoneRef = useRef(false);
  const pendingScrollRef = useRef<{ month?: string; today?: boolean; smooth: boolean } | null>(
    null,
  );
  const lastTapRef = useRef({ key: '', time: 0 });
  const tapTimerRef = useRef(0);
  const [anchorMonth, setAnchorMonth] = useState(() => nowInZone(timezone).startOf('month'));
  const holidayRevision = useHolidayLoader(holidaysCountry, [
    anchorMonth.year - 2,
    anchorMonth.year - 1,
    anchorMonth.year,
    anchorMonth.year + 1,
    anchorMonth.year + 2,
  ]);
  const anchorKey = anchorMonth.toFormat('yyyy-MM');

  /** Window for treating a second tap on the same day as a double-tap (ms). */
  const DOUBLE_TAP_MS = 260;

  const { items, todayKey } = useMemo(
    () => buildItems(anchorMonth, timezone),
    [anchorMonth, timezone],
  );

  const visibleEvents = useMemo(
    () => events.filter((e) => activeCalendarIds.includes(e.calendarId)),
    [events, activeCalendarIds],
  );

  // Three years of weeks stay mounted, so a row that filtered the whole event
  // list on every render would do that work a few hundred times per frame.
  const buckets = useMemo(
    () => bucketEventsByWeek(visibleEvents, timezone),
    [visibleEvents, timezone],
  );

  // Single tap opens the day detail; a quick second tap on the same day starts a
  // new event instead. The single-tap action is deferred briefly so a double-tap
  // can cancel it.
  const handleDayClick = useCallback(
    (date: DateTime) => {
      const key = date.toFormat('yyyy-MM-dd');
      const now = Date.now();
      const prev = lastTapRef.current;
      if (prev.key === key && now - prev.time < DOUBLE_TAP_MS) {
        if (tapTimerRef.current) {
          clearTimeout(tapTimerRef.current);
          tapTimerRef.current = 0;
        }
        lastTapRef.current = { key: '', time: 0 };
        setSelectedDate(date);
        openEventModal();
        return;
      }
      lastTapRef.current = { key, time: now };
      if (tapTimerRef.current) clearTimeout(tapTimerRef.current);
      tapTimerRef.current = window.setTimeout(() => {
        tapTimerRef.current = 0;
        setSelectedDate(date);
        openDayDetail(date);
      }, DOUBLE_TAP_MS);
    },
    [setSelectedDate, openDayDetail, openEventModal],
  );

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

  // Month drag preserves time of day: shift the start by the whole-day delta
  // between the grabbed cell and the drop cell.
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
  } = useEventDrag({ onDrop: handleMoveDrop, resolveKey: resolveDayKey });

  // Where the dragged event would land after dropping. Drives both the span
  // highlight and the grid-aligned ghost bar drawn per week.
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

  // Centralize the drag-suppressed click so the week row stays presentational.
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

  const scrollToWeek = useCallback((weekKey: string, smooth: boolean) => {
    const container = scrollRef.current;
    if (!container || !weekKey) return false;
    const el = container.querySelector<HTMLElement>(`[data-week="${weekKey.slice(2)}"]`);
    if (!el) return false;
    const cRect = container.getBoundingClientRect();
    const tRect = el.getBoundingClientRect();
    const delta = tRect.top - cRect.top - MONTH_HEADER_H;
    container.scrollTo({
      top: container.scrollTop + delta,
      behavior: smooth ? 'smooth' : 'auto',
    });
    return true;
  }, []);

  const scrollToMonth = useCallback((monthKey: string, smooth: boolean) => {
    const container = scrollRef.current;
    if (!container || !monthKey) return false;
    const el = container.querySelector<HTMLElement>(`[data-month="${monthKey}"]`);
    if (!el) return false;
    const cRect = container.getBoundingClientRect();
    const tRect = el.getBoundingClientRect();
    const delta = tRect.top - cRect.top - MONTH_HEADER_H;
    container.scrollTo({
      top: container.scrollTop + delta,
      behavior: smooth ? 'smooth' : 'auto',
    });
    return true;
  }, []);

  const scrollToToday = useCallback(
    (smooth: boolean) => {
      if (scrollToWeek(todayKey, smooth)) return;
      pendingScrollRef.current = { today: true, smooth };
      setAnchorMonth(nowInZone(timezone).startOf('month'));
    },
    [scrollToWeek, todayKey, timezone],
  );

  const extendRange = useCallback((direction: -1 | 1) => {
    const keepMonth = activeMonthRef.current;
    if (keepMonth) pendingScrollRef.current = { month: keepMonth, smooth: false };
    setAnchorMonth((m) => m.plus({ months: direction * RANGE_SHIFT_MONTHS }));
  }, []);

  // "Today" button (header) bumps this signal; scroll only on an actual change.
  const lastSignal = useRef(scrollToTodaySignal);
  useEffect(() => {
    if (lastSignal.current === scrollToTodaySignal) return;
    lastSignal.current = scrollToTodaySignal;
    scrollToToday(true);
  }, [scrollToTodaySignal, scrollToToday]);

  // Drive the toolbar month label (and event-fetch window) from the pinned header.
  const updateActiveMonth = useCallback(() => {
    const container = scrollRef.current;
    if (!container) return;
    const cTop = container.getBoundingClientRect().top;
    const headers = container.querySelectorAll<HTMLElement>('[data-month]');
    let active = '';
    for (const h of headers) {
      const top = h.getBoundingClientRect().top - cTop;
      if (top <= MONTH_HEADER_H + 1) active = h.dataset.month ?? '';
      else break;
    }
    if (!active && headers.length > 0) active = headers[0]?.dataset.month ?? '';
    if (active && active !== activeMonthRef.current) {
      activeMonthRef.current = active;
      const [y, m] = active.split('-').map(Number);
      if (y && m) setCurrentMonth(DateTime.local(y, m, 1));
    }
  }, [setCurrentMonth]);

  useLayoutEffect(() => {
    if (!anchorKey) return;
    const pending = pendingScrollRef.current;
    if (pending) {
      const done = pending.today
        ? scrollToWeek(todayKey, pending.smooth)
        : scrollToMonth(pending.month ?? '', pending.smooth);
      if (done) {
        pendingScrollRef.current = null;
        updateActiveMonth();
      }
      return;
    }
    if (!initialScrollDoneRef.current) {
      initialScrollDoneRef.current = true;
      scrollToToday(false);
    }
  }, [anchorKey, scrollToMonth, scrollToToday, scrollToWeek, todayKey, updateActiveMonth]);

  const maybeExtendRange = useCallback(() => {
    const container = scrollRef.current;
    if (!container) return;
    if (container.scrollTop < EDGE_EXTEND_PX) {
      extendRange(-1);
      return;
    }
    const remaining = container.scrollHeight - container.clientHeight - container.scrollTop;
    if (remaining < EDGE_EXTEND_PX) extendRange(1);
  }, [extendRange]);

  const handleScroll = useCallback(() => {
    if (rafRef.current) return;
    rafRef.current = requestAnimationFrame(() => {
      rafRef.current = 0;
      updateActiveMonth();
      maybeExtendRange();
    });
  }, [maybeExtendRange, updateActiveMonth]);

  useEffect(() => {
    return () => {
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
      if (tapTimerRef.current) clearTimeout(tapTimerRef.current);
    };
  }, []);

  return (
    <div className="flex h-full select-none flex-col">
      {/* Fixed weekday labels */}
      <div className="grid shrink-0 grid-cols-7 border-b border-[var(--color-separator)]">
        {[0, 1, 2, 3, 4, 5, 6].map((i) => (
          <div
            key={i}
            className="py-1.5 text-center text-caption font-medium uppercase tracking-wide text-[var(--color-text-secondary)]"
          >
            {getWeekdayLabel(i, locale)}
          </div>
        ))}
      </div>

      {/* Month range extends as the user approaches either edge. */}
      <div ref={scrollRef} onScroll={handleScroll} className="relative flex-1 overflow-y-auto">
        {items.map((item) =>
          item.kind === 'header' ? (
            <div
              key={item.key}
              data-month={item.month.toFormat('yyyy-MM')}
              className="glass-surface-heavy sticky top-0 z-10 flex items-center px-3 text-body font-bold text-[var(--color-text-primary)]"
              style={{ height: MONTH_HEADER_H }}
            >
              {formatMonthYear(item.month, locale)}
            </div>
          ) : (
            <MonthWeekRow
              key={item.key}
              weekStart={item.weekStart}
              events={buckets.get(item.dayKey) ?? NO_EVENTS}
              zone={timezone}
              holidaysCountry={holidaysCountry}
              holidayRevision={holidayRevision}
              selectedDate={selectedDate}
              density="compact"
              draggingEventId={drag?.event.id ?? null}
              dragLanding={landingWeeks?.has(item.dayKey) ? dragLanding : null}
              onDayClick={handleDayClick}
              onEventClick={handleEventClick}
              onEventPointerDown={handleEventPointerDown}
              canMoveEvent={canMove}
            />
          ),
        )}
      </div>

      {scopeDialog}
    </div>
  );
}
