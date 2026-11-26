import type { DateTime } from 'luxon';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { MonthWeekRow } from '@/components/month-week-row';
import {
  dayInZone,
  formatMonthYear,
  getWeekdayLabel,
  nowInZone,
  startOfWeekFor,
  type WeekStart,
  weekdayOrder,
} from '@/lib/date-utils';
import { useHolidayLoader } from '@/lib/use-holidays';
import { useMonthDrag } from '@/lib/use-month-drag';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { useIsEventShown } from '@/lib/use-shown-events';
import { bucketEventsByWeek, NO_EVENTS } from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

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

/** Longest a smooth scroll is assumed to run where `scrollend` never fires. */
const SMOOTH_SCROLL_MS = 500;

type ScrollItem =
  | { kind: 'header'; key: string; month: DateTime }
  | { kind: 'week'; key: string; dayKey: string; weekStart: DateTime };

function buildItems(
  anchor: DateTime,
  zone: string,
  weekStart: WeekStart,
): { items: ScrollItem[]; todayKey: string } {
  const rangeStart = anchor.startOf('month').minus({ months: RANGE_MONTHS });
  const rangeEnd = anchor.startOf('month').plus({ months: RANGE_MONTHS }).endOf('month');
  const today = nowInZone(zone).startOf('day');

  const items: ScrollItem[] = [];
  const seen = new Set<string>();
  let todayKey = '';

  // First week-start day on or before the range start.
  let ws = startOfWeekFor(rangeStart, weekStart);

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
  const weekStart = useUiStore((s) => s.weekStart);
  const scrollToTodaySignal = useUiStore((s) => s.scrollToTodaySignal);
  const openDayDetail = useUiStore((s) => s.openDayDetail);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const events = useCalendarStore((s) => s.events);
  const isShown = useIsEventShown();
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();

  const scrollRef = useRef<HTMLDivElement>(null);
  const rafRef = useRef(0);
  const activeMonthRef = useRef('');
  const initialScrollDoneRef = useRef(false);
  const pendingScrollRef = useRef<{ month?: string; today?: boolean; smooth: boolean } | null>(
    null,
  );
  // The week at the top of the viewport and its offset from the container top.
  const readAnchorRef = useRef<{ week: string; offset: number } | null>(null);
  // Ends the smooth scroll in flight; null when none is.
  const endSmoothRef = useRef<(() => void) | null>(null);
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
    () => buildItems(anchorMonth, timezone, weekStart),
    [anchorMonth, timezone, weekStart],
  );

  const visibleEvents = useMemo(() => events.filter(isShown), [events, isShown]);

  // Three years of weeks stay mounted, so a row that filtered the whole event
  // list on every render would do that work a few hundred times per frame.
  const buckets = useMemo(
    () => bucketEventsByWeek(visibleEvents, timezone, weekStart),
    [visibleEvents, timezone, weekStart],
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

  const { drag, dragLanding, landingWeeks, canMove, consumeClick, handleEventPointerDown } =
    useMonthDrag({ timezone, weekStart, requestUpdate });

  // Centralize the drag-suppressed click so the week row stays presentational.
  const handleEventClick = useCallback(
    (eventId: string) => {
      if (consumeClick()) return;
      openEventModal(eventId);
    },
    [consumeClick, openEventModal],
  );

  // Rows grow with their events, so the week being read is recorded to be held in place.
  const recordReadAnchor = useCallback(() => {
    const container = scrollRef.current;
    if (!container) return;
    const cTop = container.getBoundingClientRect().top;
    for (const row of container.querySelectorAll<HTMLElement>('[data-week]')) {
      const rect = row.getBoundingClientRect();
      if (rect.bottom > cTop + MONTH_HEADER_H) {
        readAnchorRef.current = { week: row.dataset.week ?? '', offset: rect.top - cTop };
        return;
      }
    }
    readAnchorRef.current = null;
  }, []);

  const beginSmoothScroll = useCallback((container: HTMLElement) => {
    endSmoothRef.current?.();
    const end = () => {
      window.clearTimeout(timer);
      container.removeEventListener('scrollend', end);
      if (endSmoothRef.current === end) endSmoothRef.current = null;
    };
    const timer = window.setTimeout(end, SMOOTH_SCROLL_MS);
    container.addEventListener('scrollend', end);
    endSmoothRef.current = end;
  }, []);

  const scrollToTarget = useCallback(
    (selector: string, smooth: boolean) => {
      const container = scrollRef.current;
      if (!container) return false;
      const el = container.querySelector<HTMLElement>(selector);
      if (!el) return false;
      const cRect = container.getBoundingClientRect();
      const tRect = el.getBoundingClientRect();
      const delta = tRect.top - cRect.top - MONTH_HEADER_H;
      if (smooth) beginSmoothScroll(container);
      container.scrollTo({
        top: container.scrollTop + delta,
        behavior: smooth ? 'smooth' : 'auto',
      });
      recordReadAnchor();
      return true;
    },
    [beginSmoothScroll, recordReadAnchor],
  );

  const scrollToWeek = useCallback(
    (weekKey: string, smooth: boolean) =>
      !!weekKey && scrollToTarget(`[data-week="${weekKey.slice(2)}"]`, smooth),
    [scrollToTarget],
  );

  const scrollToMonth = useCallback(
    (monthKey: string, smooth: boolean) =>
      !!monthKey && scrollToTarget(`[data-month="${monthKey}"]`, smooth),
    [scrollToTarget],
  );

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
      if (y && m) setCurrentMonth(dayInZone(y, m, 1, timezone));
    }
  }, [setCurrentMonth, timezone]);

  // Holds the read week in place as rows resize. Declared before the pending-scroll
  // effect so a rebuild landing in the same commit still reads as pending.
  // biome-ignore lint/correctness/useExhaustiveDependencies: `buckets` is what resizes the rows
  useLayoutEffect(() => {
    const container = scrollRef.current;
    const anchor = readAnchorRef.current;
    if (!container || !anchor) return;
    if (pendingScrollRef.current !== null || endSmoothRef.current !== null) return;
    const row = container.querySelector<HTMLElement>(`[data-week="${anchor.week}"]`);
    if (!row) return;
    const drift =
      row.getBoundingClientRect().top - container.getBoundingClientRect().top - anchor.offset;
    if (drift) container.scrollTop += drift;
    recordReadAnchor();
  }, [buckets, recordReadAnchor]);

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
      recordReadAnchor();
      maybeExtendRange();
    });
  }, [maybeExtendRange, recordReadAnchor, updateActiveMonth]);

  useEffect(() => {
    return () => {
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
      if (tapTimerRef.current) clearTimeout(tapTimerRef.current);
      endSmoothRef.current?.();
    };
  }, []);

  return (
    <div className="flex h-full select-none flex-col">
      {/* Fixed weekday labels */}
      <div className="grid shrink-0 grid-cols-7 border-b border-[var(--color-separator)]">
        {weekdayOrder(weekStart).map((i) => (
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
              className="month-sticky-header sticky top-0 z-10 flex items-center px-3 text-body font-bold tabular-nums text-[var(--color-text-primary)]"
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
