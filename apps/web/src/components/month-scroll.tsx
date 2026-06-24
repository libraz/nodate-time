import { DateTime } from 'luxon';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef } from 'react';
import {
  formatMonthYear,
  fromISOInZone,
  getWeekdayLabel,
  isToday,
  jsDayOfWeek,
} from '@/lib/date-utils';
import { getHoliday } from '@/lib/holidays';
import {
  isMultiDay,
  layoutWeek,
  MAX_VISIBLE_TRACKS,
  type PositionedEvent,
} from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

/** How many months of weeks to render before/after the current month. */
const RANGE_MONTHS = 18;

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

interface WeekRowProps {
  weekStart: DateTime;
  events: CalendarEvent[];
  zone: string;
  holidaysCountry: string | null;
  selectedDate: DateTime;
  onDayClick: (date: DateTime) => void;
}

function WeekRow({
  weekStart,
  events,
  zone,
  holidaysCountry,
  selectedDate,
  onDayClick,
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
          <button
            key={isoDate}
            type="button"
            onClick={() => onDayClick(dt)}
            className={`group relative flex flex-col items-start overflow-hidden border-b border-r border-[var(--color-separator)] px-1 pt-1 pb-1 transition-colors active:bg-[var(--color-active)] ${
              isSelected ? 'day-selected' : ''
            }`}
            aria-label={`${isoDate}${holiday ? ` (${holiday.name})` : ''}`}
          >
            <div className="flex w-full items-center pl-0.5" style={{ height: DATE_ROW_H }}>
              {today ? (
                <span className="today-badge flex h-6 w-6 items-center justify-center rounded-full bg-[var(--color-accent)] text-[13px] font-medium tabular-nums text-white">
                  {dt.day}
                </span>
              ) : (
                <span
                  className="flex h-6 w-6 items-center justify-center text-[13px] font-medium tabular-nums"
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
                    <div
                      key={evt.id}
                      className="mx-0.5 truncate rounded border-l-[3px] px-1 text-[10px] font-semibold tabular-nums"
                      style={{
                        height: SLOT_H,
                        lineHeight: `${SLOT_H}px`,
                        backgroundColor: `${evt.color}1f`,
                        borderLeftColor: evt.color,
                        color: evt.color,
                      }}
                    >
                      {evt.allDay ? '' : `${start.toFormat('H:mm')} `}
                      {evt.title}
                    </div>
                  );
                }
                return <div key={`${isoDate}-empty-${slotKey}`} style={{ height: SLOT_H }} />;
              })}
              {overflow > 0 && (
                <span className="text-center text-[10px] font-medium text-[var(--color-accent)]">
                  +{overflow}
                </span>
              )}
            </div>
          </button>
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
                ? '0 9999px 9999px 0'
                : p.continuesRight
                  ? '9999px 0 0 9999px'
                  : '9999px';
          return (
            <div
              key={`${p.event.id}-${p.startCol}`}
              className="event-bar absolute flex items-center gap-1 truncate px-2 text-[10px] font-semibold tabular-nums text-white"
              style={{
                left,
                width,
                top,
                height: SLOT_H,
                lineHeight: `${SLOT_H}px`,
                backgroundColor: p.event.color,
                borderRadius: radius,
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
            </div>
          );
        })}
      </div>
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
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);

  const scrollRef = useRef<HTMLDivElement>(null);
  const rafRef = useRef(0);
  const activeMonthRef = useRef('');

  // Build the week range once (anchored on mount); today never moves within a session.
  const { items, todayKey } = useMemo(() => buildItems(DateTime.now()), []);

  const visibleEvents = useMemo(
    () => events.filter((e) => activeCalendarIds.includes(e.calendarId)),
    [events, activeCalendarIds],
  );

  const handleDayClick = useCallback(
    (date: DateTime) => {
      setSelectedDate(date);
      openDayDetail(date);
    },
    [setSelectedDate, openDayDetail],
  );

  const scrollToToday = useCallback(
    (smooth: boolean) => {
      const container = scrollRef.current;
      if (!container) return;
      const el = container.querySelector<HTMLElement>(`[data-week="${todayKey.slice(2)}"]`);
      if (!el) return;
      const cRect = container.getBoundingClientRect();
      const tRect = el.getBoundingClientRect();
      const delta = tRect.top - cRect.top - MONTH_HEADER_H;
      container.scrollTo({
        top: container.scrollTop + delta,
        behavior: smooth ? 'smooth' : 'auto',
      });
    },
    [todayKey],
  );

  // Initial position: align today's week just under the pinned month header.
  useLayoutEffect(() => {
    scrollToToday(false);
  }, [scrollToToday]);

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

  const handleScroll = useCallback(() => {
    if (rafRef.current) return;
    rafRef.current = requestAnimationFrame(() => {
      rafRef.current = 0;
      updateActiveMonth();
    });
  }, [updateActiveMonth]);

  useEffect(() => {
    return () => {
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
    };
  }, []);

  return (
    <div className="flex h-full select-none flex-col">
      {/* Fixed weekday labels */}
      <div className="grid shrink-0 grid-cols-7 border-b border-[var(--color-separator)]">
        {[0, 1, 2, 3, 4, 5, 6].map((i) => (
          <div
            key={i}
            className="py-1.5 text-center text-[11px] font-medium uppercase tracking-wide text-[var(--color-text-secondary)]"
          >
            {getWeekdayLabel(i, locale)}
          </div>
        ))}
      </div>

      {/* Infinite scroll body */}
      <div ref={scrollRef} onScroll={handleScroll} className="relative flex-1 overflow-y-auto">
        {items.map((item) =>
          item.kind === 'header' ? (
            <div
              key={item.key}
              data-month={item.month.toFormat('yyyy-MM')}
              className="glass-surface-heavy sticky top-0 z-10 flex items-center px-3 text-[13px] font-bold text-[var(--color-text-primary)]"
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
            />
          ),
        )}
      </div>
    </div>
  );
}
