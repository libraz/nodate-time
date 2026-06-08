import type { DateTime } from 'luxon';
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
import { getHoliday } from '@/lib/holidays';
import { isMultiDay, layoutWeek, MAX_VISIBLE_TRACKS } from '@/lib/week-layout';
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
            className="py-2.5 text-center text-[13px] font-medium uppercase tracking-wide text-[var(--color-text-secondary)] max-sm:text-[11px]"
          >
            {getWeekdayLabel(i, locale)}
          </div>
        ))}
      </div>

      <div
        className="grid flex-1"
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
                  <button
                    key={isoDate}
                    type="button"
                    onClick={() => {
                      setSelectedDate(dt);
                      openDayDetail(dt);
                    }}
                    className={`group relative flex flex-col items-start overflow-hidden border-b border-r border-[var(--color-separator)] px-1 pt-1.5 pb-1 transition-colors hover:bg-[var(--color-hover)] focus-visible:bg-[var(--color-hover)] focus-visible:outline-none ${
                      isSelected ? 'ring-1 ring-inset ring-[var(--color-accent)]' : ''
                    }`}
                    style={!inMonth ? { opacity: 0.4 } : undefined}
                    aria-label={`${dt.toFormat('yyyy-MM-dd')}${holiday ? ` (${holiday.name})` : ''}`}
                  >
                    <div className="flex w-full items-center justify-between pl-0.5">
                      {today ? (
                        <span className="flex h-7 w-7 items-center justify-center rounded-full bg-[var(--color-accent)] text-[15px] font-medium text-white shadow-sm max-sm:text-[13px]">
                          {dt.day}
                        </span>
                      ) : (
                        <span
                          className="flex h-7 w-7 items-center justify-center text-[15px] font-medium max-sm:text-[13px]"
                          style={{ color: getDateColor(dow, !!holiday) }}
                        >
                          {dt.day}
                        </span>
                      )}
                      {holiday && (
                        <span
                          className="ml-1 truncate rounded-full bg-[var(--color-sunday-bg,rgba(244,67,54,0.12))] px-1.5 text-[10px] font-medium text-[var(--color-sunday)] max-sm:hidden"
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
                            <div
                              key={evt.id}
                              className="mx-0.5 truncate rounded-md border-l-[3px] px-1.5 text-[11px] font-semibold leading-[20px] max-sm:text-[10px] max-sm:leading-[15px]"
                              style={{
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
                        return (
                          <div
                            key={`${isoDate}-empty-${slotKey}`}
                            className="h-[20px] max-sm:h-[15px]"
                          />
                        );
                      })}
                      {overflow > 0 && (
                        <span className="mt-px text-center text-[11px] font-medium text-[var(--color-accent)] max-sm:text-[10px]">
                          +{overflow}
                        </span>
                      )}
                    </div>
                  </button>
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
                        ? '0 9999px 9999px 0'
                        : p.continuesRight
                          ? '9999px 0 0 9999px'
                          : '9999px';
                  return (
                    <div
                      key={`${p.event.id}-${p.startCol}`}
                      className="pointer-events-auto absolute flex items-center gap-1 truncate px-2 text-[11px] font-semibold leading-[20px] text-white shadow-sm max-sm:text-[10px] max-sm:leading-[15px]"
                      style={{
                        left,
                        width,
                        top,
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
        })}
      </div>
    </div>
  );
}
