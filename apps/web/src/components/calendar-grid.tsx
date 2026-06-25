import { DateTime } from 'luxon';
import { useCallback, useMemo, useRef } from 'react';
import { useT } from '@/i18n';
import {
  fromISOInZone,
  getMonthDays,
  getWeekDays,
  getWeekdayLabel,
  isToday,
  jsDayOfWeek,
} from '@/lib/date-utils';
import { buildMovedEvent } from '@/lib/event-move';
import { getHoliday } from '@/lib/holidays';
import { canEdit, roleForCalendar } from '@/lib/permissions';
import { useEventDrag } from '@/lib/use-event-drag';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { eventEndDay, isMultiDay, layoutWeek, MAX_VISIBLE_TRACKS } from '@/lib/week-layout';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

export function CalendarGrid() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const calendarView = useUiStore((s) => s.calendarView);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const timezone = useUiStore((s) => s.timezone);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setCalendarView = useUiStore((s) => s.setCalendarView);
  const navigateMonth = useUiStore((s) => s.navigateMonth);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const me = useAuthStore((s) => s.user);
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();

  const touchStartRef = useRef({ x: 0, y: 0 });

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
  const dragLanding = useMemo(() => {
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

  const weeks = useMemo(() => {
    const result: DateTime[][] = [];
    for (let i = 0; i < days.length; i += 7) {
      result.push(days.slice(i, i + 7));
    }
    return result;
  }, [days]);

  const weekLayouts = useMemo(
    () =>
      weeks.map((week) => {
        const first = week[0];
        return first ? layoutWeek(first.startOf('day'), visibleEvents, timezone) : [];
      }),
    [weeks, visibleEvents, timezone],
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

  const isCurrentMonth = useCallback(
    (date: DateTime) => date.month === currentMonth.month && date.year === currentMonth.year,
    [currentMonth],
  );

  const getDateColor = (dow: number, isHoliday: boolean): string => {
    if (isHoliday || dow === 0) return 'var(--color-sunday)';
    if (dow === 6) return 'var(--color-saturday)';
    return 'var(--color-text-primary)';
  };

  void t;

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
            className="py-2.5 text-center text-body font-medium uppercase tracking-wide text-[var(--color-text-secondary)] max-sm:text-caption"
          >
            {getWeekdayLabel(i, locale)}
          </div>
        ))}
      </div>

      <div
        key={`${currentMonth.year}-${currentMonth.month}-${calendarView}`}
        className="calendar-enter grid flex-1"
        style={{ gridTemplateRows: `repeat(${weeks.length}, minmax(0, 1fr))` }}
      >
        {weeks.map((week, wIdx) => {
          const weekKey = week[0]?.toISO() ?? `w-${wIdx}`;
          const positioned = weekLayouts[wIdx] ?? [];

          // Single-day events grouped by yyyy-MM-dd
          const singleDayMap = new Map<string, CalendarEvent[]>();
          for (const evt of visibleEvents) {
            if (isMultiDay(evt, timezone)) continue;
            const startDt = fromISOInZone(evt.startAt, timezone).startOf('day');
            const inWeek = week.find((d) => d.hasSame(startDt, 'day'));
            if (!inWeek) continue;
            const key = inWeek.toFormat('yyyy-MM-dd');
            const arr = singleDayMap.get(key) ?? [];
            arr.push(evt);
            singleDayMap.set(key, arr);
          }

          const reservedTracksByDay = week.map((dt) => {
            const col = jsDayOfWeek(dt);
            const reserved: number[] = [];
            for (const p of positioned) {
              if (col >= p.startCol && col < p.startCol + p.span) {
                reserved.push(p.track);
              }
            }
            return reserved;
          });

          // Segment of the drag ghost that falls inside this week (the ghost
          // is grid-aligned and spans the event's real length, wrapping weeks).
          let previewSeg: { startCol: number; span: number } | null = null;
          if (dragLanding) {
            const weekStart = week[0]?.startOf('day');
            const weekEnd = week[6]?.startOf('day');
            if (
              weekStart &&
              weekEnd &&
              dragLanding.end >= weekStart &&
              dragLanding.start <= weekEnd
            ) {
              const segStart = dragLanding.start < weekStart ? weekStart : dragLanding.start;
              const segEnd = dragLanding.end > weekEnd ? weekEnd : dragLanding.end;
              previewSeg = {
                startCol: jsDayOfWeek(segStart),
                span: Math.round(segEnd.diff(segStart, 'days').days) + 1,
              };
            }
          }

          return (
            <div key={weekKey} className="relative grid grid-cols-7">
              {week.map((dt, dIdx) => {
                const today = isToday(dt);
                const inMonth = calendarView === 'week' || isCurrentMonth(dt);
                const dow = jsDayOfWeek(dt);
                const isoDate = dt.toFormat('yyyy-MM-dd');
                const holiday = getHoliday(holidaysCountry, isoDate);
                const reserved = reservedTracksByDay[dIdx] ?? [];
                const dayKey = dt.toFormat('yyyy-MM-dd');
                const singles = singleDayMap.get(dayKey) ?? [];
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
                    className={`group relative flex flex-col items-start overflow-hidden border-b border-r border-[var(--color-separator)] px-1 pt-1.5 pb-1 ${
                      isSelected ? 'day-selected' : ''
                    }`}
                    style={!inMonth ? { opacity: 0.4 } : undefined}
                  >
                    {/* Background target: click selects the day, double-click
                        starts a new event on it. */}
                    <button
                      type="button"
                      onClick={() => setSelectedDate(dt)}
                      onDoubleClick={() => {
                        setSelectedDate(dt);
                        openEventModal();
                      }}
                      className="absolute inset-0 z-0 transition-colors hover:bg-[var(--color-hover)] focus-visible:bg-[var(--color-hover)] focus-visible:outline-none"
                      aria-label={`${dt.toFormat('yyyy-MM-dd')}${holiday ? ` (${holiday.name})` : ''}`}
                    />

                    {/* Content passes pointer events through to the day button, except event chips. */}
                    <div className="pointer-events-none relative z-10 flex w-full flex-col">
                      <div className="flex w-full items-center justify-between pl-0.5">
                        {today ? (
                          <span className="today-badge flex h-7 w-7 items-center justify-center rounded-full bg-[var(--color-accent)] text-callout font-medium tabular-nums text-white max-sm:text-body">
                            {dt.day}
                          </span>
                        ) : (
                          <span
                            className="flex h-7 w-7 items-center justify-center text-callout font-medium tabular-nums max-sm:text-body"
                            style={{ color: getDateColor(dow, !!holiday) }}
                          >
                            {dt.day}
                          </span>
                        )}
                        {holiday && (
                          <span
                            className="ml-1 truncate rounded-full bg-[var(--color-sunday-bg,rgba(244,67,54,0.12))] px-1.5 text-micro font-medium text-[var(--color-sunday)] max-sm:hidden"
                            title={holiday.name}
                          >
                            {holiday.name}
                          </span>
                        )}
                      </div>

                      <div className="mt-0.5 flex w-full flex-col gap-px">
                        {(['t0', 't1', 't2'] as const).map((slotKey, slot) => {
                          if (reserved.includes(slot)) {
                            return (
                              <div
                                key={`${isoDate}-spacer-${slotKey}`}
                                className="h-[20px] max-sm:h-[15px]"
                              />
                            );
                          }
                          const filler = singleSlots.find((s) => s.track === slot);
                          if (filler) {
                            const evt = filler.evt;
                            const start = fromISOInZone(evt.startAt, timezone);
                            return (
                              <button
                                key={evt.id}
                                type="button"
                                onPointerDown={(e) => {
                                  if (canMove(evt)) startDrag(evt, e);
                                }}
                                onClick={() => {
                                  if (consumeClick()) return;
                                  openEventModal(evt.id);
                                }}
                                className={`pointer-events-auto mx-0.5 flex items-center gap-1 rounded-[5px] px-1.5 text-left text-caption font-semibold leading-[20px] tabular-nums hover:brightness-95 max-sm:gap-0.5 max-sm:text-micro max-sm:leading-[15px] ${
                                  canMove(evt)
                                    ? 'cursor-grab active:cursor-grabbing'
                                    : 'cursor-pointer'
                                }`}
                                style={{
                                  backgroundColor: `${evt.color}1f`,
                                  color: evt.color,
                                  opacity: drag?.event.id === evt.id ? 0.4 : undefined,
                                }}
                              >
                                <span
                                  aria-hidden
                                  className="h-1.5 w-1.5 shrink-0 rounded-full max-sm:h-1 max-sm:w-1"
                                  style={{ backgroundColor: evt.color }}
                                />
                                <span className="truncate">
                                  {evt.allDay ? '' : `${start.toFormat('H:mm')} `}
                                  {evt.title}
                                </span>
                              </button>
                            );
                          }
                          return (
                            <div
                              key={`${isoDate}-empty-${slotKey}`}
                              className="h-[20px] max-sm:h-[15px]"
                            />
                          );
                        })}
                        {overflow > 0 && (
                          <span className="mt-px text-center text-caption font-medium text-[var(--color-accent)] max-sm:text-micro">
                            +{overflow}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                );
              })}

              {/* Multi-day bar overlay */}
              <div className="pointer-events-none absolute inset-x-0 top-[34px] grid grid-cols-7 px-0.5 max-sm:top-[28px]">
                {positioned.map((p) => {
                  if (p.track >= MAX_VISIBLE_TRACKS) return null;
                  const left = `calc(${(p.startCol * 100) / 7}% + 2px)`;
                  const width = `calc(${(p.span * 100) / 7}% - 4px)`;
                  const top = `${p.track * 22}px`;
                  const start = fromISOInZone(p.event.startAt, timezone);
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
                      onPointerDown={(e) => {
                        if (canMove(p.event)) startDrag(p.event, e);
                      }}
                      onClick={() => {
                        if (consumeClick()) return;
                        openEventModal(p.event.id);
                      }}
                      className={`event-bar pointer-events-auto absolute flex items-center gap-1 truncate px-2 text-caption font-semibold leading-[20px] tabular-nums text-white hover:brightness-95 max-sm:text-micro max-sm:leading-[15px] ${
                        canMove(p.event) ? 'cursor-grab active:cursor-grabbing' : 'cursor-pointer'
                      }`}
                      style={{
                        left,
                        width,
                        top,
                        backgroundColor: p.event.color,
                        borderRadius: radius,
                        opacity: drag?.event.id === p.event.id ? 0.4 : undefined,
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
                  className="pointer-events-none absolute top-[34px] z-20 flex h-[20px] items-center truncate px-2 text-caption font-semibold text-white shadow-lg max-sm:top-[28px] max-sm:h-[15px] max-sm:text-micro"
                  style={{
                    left: `calc(${(previewSeg.startCol * 100) / 7}% + 2px)`,
                    width: `calc(${(previewSeg.span * 100) / 7}% - 4px)`,
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
        })}
      </div>

      {scopeDialog}
    </div>
  );
}
