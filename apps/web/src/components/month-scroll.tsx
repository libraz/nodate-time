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
import {
  formatMonthYear,
  fromISOInZone,
  getWeekdayLabel,
  isToday,
  jsDayOfWeek,
} from '@/lib/date-utils';
import { buildMovedEvent } from '@/lib/event-move';
import { getHoliday } from '@/lib/holidays';
import { canEdit, roleForCalendar } from '@/lib/permissions';
import { useEventDrag } from '@/lib/use-event-drag';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import {
  eventEndDay,
  isMultiDay,
  layoutWeek,
  MAX_VISIBLE_TRACKS,
  type PositionedEvent,
} from '@/lib/week-layout';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

/** How many months of weeks to keep mounted before/after the moving anchor month. */
const RANGE_MONTHS = 18;
const RANGE_SHIFT_MONTHS = 12;
const EDGE_EXTEND_PX = 700;

/** Fixed vertical metrics (px) that keep single-day chips aligned with multi-day bars. */
const DATE_ROW_H = 24;
const SLOT_H = 15;
const MONTH_HEADER_H = 28;

type ScrollItem =
  | { kind: 'header'; key: string; month: DateTime }
  | { kind: 'week'; key: string; weekStart: DateTime };

function buildItems(anchor: DateTime): { items: ScrollItem[]; todayKey: string } {
  const rangeStart = anchor.startOf('month').minus({ months: RANGE_MONTHS });
  const rangeEnd = anchor.startOf('month').plus({ months: RANGE_MONTHS }).endOf('month');
  const today = DateTime.now().startOf('day');

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
    const weekKey = `w-${ws.toFormat('yyyy-MM-dd')}`;
    if (today >= ws && today < ws.plus({ days: 7 })) {
      todayKey = weekKey;
    }
    items.push({ kind: 'week', key: weekKey, weekStart: ws });
    ws = ws.plus({ weeks: 1 });
  }

  return { items, todayKey };
}

/** Landing range of the event currently being dragged. */
interface DragLanding {
  event: CalendarEvent;
  start: DateTime;
  end: DateTime;
}

interface WeekRowProps {
  weekStart: DateTime;
  events: CalendarEvent[];
  zone: string;
  holidaysCountry: string | null;
  selectedDate: DateTime;
  onDayClick: (date: DateTime) => void;
  onEventClick: (eventId: string) => void;
  onEventPointerDown: (evt: CalendarEvent, e: ReactPointerEvent) => void;
  canMoveEvent: (evt: CalendarEvent) => boolean;
  dragLanding: DragLanding | null;
}

function WeekRow({
  weekStart,
  events,
  zone,
  holidaysCountry,
  selectedDate,
  onDayClick,
  onEventClick,
  onEventPointerDown,
  canMoveEvent,
  dragLanding,
}: WeekRowProps) {
  const week = useMemo(
    () => Array.from({ length: 7 }, (_, i) => weekStart.plus({ days: i })),
    [weekStart],
  );

  const positioned = useMemo(() => layoutWeek(weekStart, events, zone), [weekStart, events, zone]);

  // Single-day events grouped by yyyy-MM-dd within this week.
  const singleDayMap = useMemo(() => {
    const map = new Map<string, CalendarEvent[]>();
    for (const evt of events) {
      if (isMultiDay(evt, zone)) continue;
      const startDt = fromISOInZone(evt.startAt, zone).startOf('day');
      const inWeek = week.find((d) => d.hasSame(startDt, 'day'));
      if (!inWeek) continue;
      const key = inWeek.toFormat('yyyy-MM-dd');
      const arr = map.get(key) ?? [];
      arr.push(evt);
      map.set(key, arr);
    }
    return map;
  }, [events, week, zone]);

  const reservedTracksByDay = useMemo(
    () =>
      week.map((dt) => {
        const col = jsDayOfWeek(dt);
        const reserved: number[] = [];
        for (const p of positioned) {
          if (col >= p.startCol && col < p.startCol + p.span) reserved.push(p.track);
        }
        return reserved;
      }),
    [week, positioned],
  );

  const getDateColor = (dow: number, isHoliday: boolean): string => {
    if (isHoliday || dow === 0) return 'var(--color-sunday)';
    if (dow === 6) return 'var(--color-saturday)';
    return 'var(--color-text-primary)';
  };

  const bodyHeight = MAX_VISIBLE_TRACKS * SLOT_H;

  // Segment of the drag ghost that falls inside this week (grid-aligned, spans
  // the event's real length and wraps across weeks).
  const previewSeg = useMemo(() => {
    if (!dragLanding) return null;
    const weekEnd = weekStart.plus({ days: 6 });
    if (dragLanding.end < weekStart || dragLanding.start > weekEnd) return null;
    const segStart = dragLanding.start < weekStart ? weekStart : dragLanding.start;
    const segEnd = dragLanding.end > weekEnd ? weekEnd : dragLanding.end;
    return {
      startCol: jsDayOfWeek(segStart),
      span: Math.round(segEnd.diff(segStart, 'days').days) + 1,
    };
  }, [dragLanding, weekStart]);

  return (
    <div className="relative grid grid-cols-7" data-week={weekStart.toFormat('yyyy-MM-dd')}>
      {week.map((dt, dIdx) => {
        const today = isToday(dt);
        const dow = jsDayOfWeek(dt);
        const isoDate = dt.toFormat('yyyy-MM-dd');
        const holiday = getHoliday(holidaysCountry, isoDate);
        const reserved = reservedTracksByDay[dIdx] ?? [];
        const singles = singleDayMap.get(isoDate) ?? [];
        const usedTracks = new Set(reserved);
        const singleSlots: { track: number; evt: CalendarEvent }[] = [];
        let nextTrack = 0;
        for (const evt of singles) {
          while (usedTracks.has(nextTrack)) nextTrack++;
          singleSlots.push({ track: nextTrack, evt });
          usedTracks.add(nextTrack);
          nextTrack++;
          if (singleSlots.length >= MAX_VISIBLE_TRACKS + 5) break;
        }
        const totalEventsHere = reserved.length + singles.length;
        const overflow = totalEventsHere - MAX_VISIBLE_TRACKS;
        const isSelected = dt.hasSame(selectedDate, 'day');

        return (
          <div
            key={isoDate}
            data-day={isoDate}
            className={`group relative flex flex-col items-start overflow-hidden border-b border-r border-[var(--color-separator)] px-1 pt-1 pb-1 ${
              isSelected ? 'day-selected' : ''
            }`}
          >
            {/* Background target: tap opens the day detail, double-tap starts a
                new event on this day. */}
            <button
              type="button"
              onClick={() => onDayClick(dt)}
              className="absolute inset-0 z-0 touch-manipulation transition-colors active:bg-[var(--color-active)]"
              aria-label={`${isoDate}${holiday ? ` (${holiday.name})` : ''}`}
            />

            {/* Content passes pointer events through to the day button, except event chips. */}
            <div className="pointer-events-none relative z-10 flex w-full flex-col">
              <div className="flex w-full items-center pl-0.5" style={{ height: DATE_ROW_H }}>
                {today ? (
                  <span className="today-badge flex h-6 w-6 items-center justify-center rounded-full bg-[var(--color-accent)] text-body font-medium tabular-nums text-white">
                    {dt.day}
                  </span>
                ) : (
                  <span
                    className="flex h-6 w-6 items-center justify-center text-body font-medium tabular-nums"
                    style={{ color: getDateColor(dow, !!holiday) }}
                  >
                    {dt.day}
                  </span>
                )}
              </div>

              <div className="flex w-full flex-col" style={{ height: bodyHeight }}>
                {(['t0', 't1', 't2'] as const).map((slotKey, slot) => {
                  if (reserved.includes(slot)) {
                    return <div key={`${isoDate}-spacer-${slotKey}`} style={{ height: SLOT_H }} />;
                  }
                  const filler = singleSlots.find((s) => s.track === slot);
                  if (filler) {
                    const evt = filler.evt;
                    const start = fromISOInZone(evt.startAt, zone);
                    return (
                      <button
                        key={evt.id}
                        type="button"
                        onPointerDown={(e) => onEventPointerDown(evt, e)}
                        onClick={() => onEventClick(evt.id)}
                        className={`pointer-events-auto mx-0.5 flex items-center gap-1 rounded-[4px] px-1 text-left text-micro font-semibold tabular-nums ${
                          canMoveEvent(evt) ? 'active:cursor-grabbing' : ''
                        }`}
                        style={{
                          height: SLOT_H,
                          backgroundColor: `${evt.color}1f`,
                          color: evt.color,
                          opacity: dragLanding?.event.id === evt.id ? 0.4 : undefined,
                        }}
                      >
                        <span
                          aria-hidden
                          className="h-1 w-1 shrink-0 rounded-full"
                          style={{ backgroundColor: evt.color }}
                        />
                        <span className="truncate">
                          {evt.allDay ? '' : `${start.toFormat('H:mm')} `}
                          {evt.title}
                        </span>
                      </button>
                    );
                  }
                  return <div key={`${isoDate}-empty-${slotKey}`} style={{ height: SLOT_H }} />;
                })}
                {overflow > 0 && (
                  <span className="text-center text-micro font-medium text-[var(--color-accent)]">
                    +{overflow}
                  </span>
                )}
              </div>
            </div>
          </div>
        );
      })}

      {/* Multi-day bar overlay */}
      <div
        className="pointer-events-none absolute inset-x-0 grid grid-cols-7 px-0.5"
        style={{ top: DATE_ROW_H }}
      >
        {positioned.map((p: PositionedEvent) => {
          if (p.track >= MAX_VISIBLE_TRACKS) return null;
          const left = `calc(${(p.startCol * 100) / 7}% + 2px)`;
          const width = `calc(${(p.span * 100) / 7}% - 4px)`;
          const top = `${p.track * SLOT_H}px`;
          const start = fromISOInZone(p.event.startAt, zone);
          const radius =
            p.continuesLeft && p.continuesRight
              ? '0'
              : p.continuesLeft
                ? '0 5px 5px 0'
                : p.continuesRight
                  ? '5px 0 0 5px'
                  : '5px';
          return (
            <button
              key={`${p.event.id}-${p.startCol}`}
              type="button"
              onPointerDown={(e) => onEventPointerDown(p.event, e)}
              onClick={() => onEventClick(p.event.id)}
              className={`event-bar pointer-events-auto absolute flex items-center gap-1 truncate px-2 text-micro font-semibold tabular-nums text-white ${
                canMoveEvent(p.event) ? 'active:cursor-grabbing' : ''
              }`}
              style={{
                left,
                width,
                top,
                height: SLOT_H,
                lineHeight: `${SLOT_H}px`,
                backgroundColor: p.event.color,
                borderRadius: radius,
                opacity: dragLanding?.event.id === p.event.id ? 0.4 : undefined,
              }}
              title={p.event.title}
            >
              {p.continuesLeft && (
                <span aria-hidden className="opacity-80">
                  ‹
                </span>
              )}
              <span className="truncate">
                {!p.event.allDay && !p.continuesLeft && (
                  <span className="mr-1 opacity-90">{start.toFormat('H:mm')}</span>
                )}
                {p.event.title}
              </span>
              {p.continuesRight && (
                <span aria-hidden className="ml-auto opacity-80">
                  ›
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Grid-aligned drag ghost: real-width preview at the landing spot. */}
      {previewSeg && dragLanding && (
        <div
          className="pointer-events-none absolute z-20 flex items-center truncate px-2 text-micro font-semibold text-white shadow-lg"
          style={{
            top: DATE_ROW_H,
            left: `calc(${(previewSeg.startCol * 100) / 7}% + 2px)`,
            width: `calc(${(previewSeg.span * 100) / 7}% - 4px)`,
            height: SLOT_H,
            lineHeight: `${SLOT_H}px`,
            backgroundColor: dragLanding.event.color,
            borderRadius: '5px',
            opacity: 0.85,
          }}
        >
          {dragLanding.event.title}
        </div>
      )}
    </div>
  );
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
  const membersMap = useCalendarStore((s) => s.membersMap);
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
  const [anchorMonth, setAnchorMonth] = useState(DateTime.now().startOf('month'));
  const anchorKey = anchorMonth.toFormat('yyyy-MM');

  /** Window for treating a second tap on the same day as a double-tap (ms). */
  const DOUBLE_TAP_MS = 260;

  const { items, todayKey } = useMemo(() => buildItems(anchorMonth), [anchorMonth]);

  const visibleEvents = useMemo(
    () => events.filter((e) => activeCalendarIds.includes(e.calendarId)),
    [events, activeCalendarIds],
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
    (evt: CalendarEvent) => canEdit(roleForCalendar(membersMap[evt.calendarId], me?.email)),
    [membersMap, me],
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

  // Centralize the drag-suppressed click so WeekRow stays presentational.
  const handleEventClick = useCallback(
    (eventId: string) => {
      if (consumeClick()) return;
      openEventModal(eventId);
    },
    [consumeClick, openEventModal],
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
      setAnchorMonth(DateTime.now().startOf('month'));
    },
    [scrollToWeek, todayKey],
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
            <WeekRow
              key={item.key}
              weekStart={item.weekStart}
              events={visibleEvents}
              zone={timezone}
              holidaysCountry={holidaysCountry}
              selectedDate={selectedDate}
              onDayClick={handleDayClick}
              onEventClick={handleEventClick}
              onEventPointerDown={(evt, e) => {
                if (canMove(evt)) startDrag(evt, e);
              }}
              canMoveEvent={canMove}
              dragLanding={dragLanding}
            />
          ),
        )}
      </div>

      {scopeDialog}
    </div>
  );
}
