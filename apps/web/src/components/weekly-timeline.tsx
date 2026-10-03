import { DateTime } from 'luxon';
import {
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { useT } from '@/i18n';
import {
  fromISOInZone,
  getWeekDays,
  getWeekdayLabel,
  isToday,
  jsDayOfWeek,
} from '@/lib/date-utils';
import { buildMovedEvent, buildResizedEvent } from '@/lib/event-move';
import { atMinutesIntoDay, minutesIntoDay } from '@/lib/event-times';
import {
  layoutTimedEventsForDay,
  MIN_RENDERED_MINUTES,
  resizedEndForDaySegment,
} from '@/lib/timed-layout';
import { useCanMoveEvent } from '@/lib/use-can-move-event';
import { type DragDropContext, useEventDrag, useEventResize } from '@/lib/use-event-drag';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { weekDropStart } from '@/lib/week-drag';
import { eventEndDay, eventOccupiesDay } from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

const HOUR_HEIGHT = 48;
const HOURS = Array.from({ length: 24 }, (_, i) => i);

/** The day segment a resize was started on, resolved at the press. */
interface ResizeSegment {
  dayKey: string;
  colTop: number;
  dayStart: DateTime;
}

/** The `yyyy-MM-dd` of the day column under a viewport point. */
function resolveDayColumn(x: number, y: number): string | null {
  for (const el of document.elementsFromPoint(x, y)) {
    const day = el.closest('[data-daycol]')?.getAttribute('data-daycol');
    if (day) return day;
  }
  return null;
}

export function WeeklyTimeline() {
  const t = useT();
  const selectedDate = useUiStore((s) => s.selectedDate);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();
  const scrollRef = useRef<HTMLDivElement>(null);

  /** Minutes a clicked slot snaps to. */
  const SNAP_MINUTES = 30;
  /** Shortest event a resize may produce. */
  const MIN_DURATION_MIN = 30;

  // Live end-minute preview while resizing, so the block follows the cursor.
  const [resizePreview, setResizePreview] = useState<{
    id: string;
    dayKey: string;
    endMin: number;
  } | null>(null);
  // Size of the block being dragged, so the move ghost mirrors it exactly.
  const dragGeom = useRef<{ width: number; height: number }>({ width: 160, height: 40 });

  const canMove = useCanMoveEvent();

  // The block moves by the pointer's travel: columns crossed and vertical
  // distance, snapped. Duration is preserved.
  const handleMoveDrop = useCallback(
    (evt: CalendarEvent, x: number, y: number, ctx: DragDropContext) => {
      const targetKey = resolveDayColumn(x, y);
      if (!targetKey || !ctx.originKey) return;
      const newStart = weekDropStart({
        evt,
        originKey: ctx.originKey,
        targetKey,
        travelPx: y - ctx.startY,
        hourHeight: HOUR_HEIGHT,
        snapMinutes: SNAP_MINUTES,
        zone: timezone,
      });
      if (newStart) requestUpdate(evt, buildMovedEvent(evt, newStart));
    },
    [timezone, requestUpdate],
  );

  const {
    drag,
    start: startDrag,
    consumeClick,
  } = useEventDrag({ onDrop: handleMoveDrop, resolveKey: resolveDayColumn });

  // Resize: drag the bottom edge of a timed block to change its end time. The
  // start stays fixed; the end snaps and keeps a minimum duration.
  const resizedEnd = useCallback(
    (evt: CalendarEvent, clientY: number, seg: ResizeSegment) =>
      resizedEndForDaySegment({
        eventStartISO: evt.startAt,
        dayStart: seg.dayStart,
        clientY,
        colTop: seg.colTop,
        hourHeight: HOUR_HEIGHT,
        snapMinutes: SNAP_MINUTES,
        minDurationMinutes: MIN_DURATION_MIN,
        zone: timezone,
      }),
    [timezone],
  );

  const { start: startResizeGesture, consumeClick: consumeResizeClick } =
    useEventResize<ResizeSegment>({
      onPreview: (evt, clientY, seg) =>
        setResizePreview({
          id: evt.id,
          dayKey: seg.dayKey,
          // Where the preview is drawn, so on the same clock as the grid.
          endMin: minutesIntoDay(resizedEnd(evt, clientY, seg), seg.dayStart),
        }),
      onCommit: (evt, clientY, seg) =>
        requestUpdate(evt, buildResizedEvent(evt, resizedEnd(evt, clientY, seg))),
      onEnd: () => setResizePreview(null),
    });

  const startResize = (evt: CalendarEvent, e: ReactPointerEvent) => {
    const col = (e.currentTarget as HTMLElement).closest('[data-daycol]') as HTMLElement | null;
    const dayKey = col?.getAttribute('data-daycol');
    if (!col || !dayKey) return;
    startResizeGesture(evt, e, {
      dayKey,
      colTop: col.getBoundingClientRect().top,
      dayStart: DateTime.fromFormat(dayKey, 'yyyy-MM-dd', { zone: timezone }).startOf('day'),
    });
  };

  /** Open the new-event modal at the clicked time within a day column. */
  const handleSlotClick = (day: DateTime, e: React.MouseEvent<HTMLButtonElement>) => {
    const y = e.nativeEvent.offsetY;
    const rawMinutes = (y / HOUR_HEIGHT) * 60;
    const snapped = Math.round(rawMinutes / SNAP_MINUTES) * SNAP_MINUTES;
    const clamped = Math.min(Math.max(snapped, 0), 23 * 60 + 30);
    const start = atMinutesIntoDay(day, clamped);
    setSelectedDate(start);
    openEventModal(undefined, start);
  };

  // The one week this view draws: its columns, in the viewer's zone. The
  // fetch window follows currentMonth, so that is re-anchored to this week
  // below rather than left to whichever path led into the view.
  const days = useMemo(() => getWeekDays(selectedDate, timezone), [selectedDate, timezone]);

  useEffect(() => {
    if (selectedDate.year !== currentMonth.year || selectedDate.month !== currentMonth.month) {
      setCurrentMonth(selectedDate.startOf('month'));
    }
  }, [selectedDate, currentMonth, setCurrentMonth]);

  // All-day events by the dates they occupy in their own zone, as the month
  // grid draws them; timed events go to the layout, which owns the rule for
  // which day a span or a marker belongs to.
  const { allDayEvents, timedLayouts } = useMemo(() => {
    const allDay: Map<string, CalendarEvent[]> = new Map();
    const timed: CalendarEvent[] = [];
    for (const evt of events) {
      if (!activeCalendarIds.includes(evt.calendarId)) continue;
      if (!evt.allDay) {
        timed.push(evt);
        continue;
      }
      for (const day of days) {
        if (!eventOccupiesDay(evt, day, timezone)) continue;
        const key = day.toFormat('yyyy-MM-dd');
        const arr = allDay.get(key) ?? [];
        arr.push(evt);
        allDay.set(key, arr);
      }
    }
    const layouts: Map<string, ReturnType<typeof layoutTimedEventsForDay>> = new Map();
    for (const day of days) {
      layouts.set(
        day.toFormat('yyyy-MM-dd'),
        layoutTimedEventsForDay(timed, day.startOf('day'), timezone),
      );
    }
    return { allDayEvents: allDay, timedLayouts: layouts };
  }, [events, activeCalendarIds, days, timezone]);

  /** Pixel offset within the day column of a wall-clock minute of the day. */
  const minutesToY = (minutes: number): number => (minutes / 60) * HOUR_HEIGHT;

  const now = DateTime.now().setZone(timezone);
  const nowMinutes = now.hour * 60 + now.minute;
  const currentTimeY = (nowMinutes / 60) * HOUR_HEIGHT;

  return (
    <div className="flex h-full flex-col">
      {/* Day headers with dates */}
      <div className="flex border-b border-[var(--color-separator)]">
        {/* Time gutter spacer */}
        <div className="w-14 shrink-0" />
        {days.map((day) => {
          const today = isToday(day, timezone);
          const dow = jsDayOfWeek(day);
          return (
            <div key={day.toISO()} className="flex flex-1 flex-col items-center py-2.5">
              <span className="text-body font-medium tracking-wide text-[var(--color-text-secondary)]">
                {getWeekdayLabel(dow, locale)}
              </span>
              <span
                className={`mt-0.5 flex h-7 w-7 items-center justify-center rounded-full text-title font-semibold tabular-nums ${
                  today ? 'today-badge' : ''
                }`}
                style={{
                  backgroundColor: today ? 'var(--color-accent)' : 'transparent',
                  color: today
                    ? '#ffffff'
                    : dow === 0
                      ? 'var(--color-sunday)'
                      : dow === 6
                        ? 'var(--color-saturday)'
                        : 'var(--color-text-primary)',
                }}
              >
                {day.day}
              </span>
            </div>
          );
        })}
      </div>

      {/* All-day events row */}
      {Array.from(allDayEvents.values()).some((a) => a.length > 0) && (
        <div className="flex border-b border-[var(--color-separator)]">
          <div className="flex w-14 shrink-0 items-start justify-end pr-2 pt-1 text-caption font-medium text-[var(--color-text-tertiary)]">
            {t('calendar.allDay')}
          </div>
          {days.map((day) => {
            const key = day.toFormat('yyyy-MM-dd');
            const dayAllDay = allDayEvents.get(key) ?? [];
            return (
              <div
                key={key}
                className="flex flex-1 flex-col gap-px border-l border-[var(--color-separator)] p-0.5"
              >
                {dayAllDay.map((evt) => (
                  <button
                    key={evt.id}
                    type="button"
                    onClick={() => openEventModal(evt.id)}
                    className="truncate rounded-full px-2 text-caption font-semibold leading-[18px]"
                    style={{
                      backgroundColor: `${evt.color}20`,
                      color: evt.color,
                    }}
                  >
                    {evt.title}
                  </button>
                ))}
              </div>
            );
          })}
        </div>
      )}

      {/* Scrollable timeline grid */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="relative flex" style={{ height: 24 * HOUR_HEIGHT }}>
          {/* Time labels gutter */}
          <div className="relative w-14 shrink-0">
            {HOURS.map((h) => (
              <div
                key={h}
                className="absolute right-2 text-caption font-medium tabular-nums text-[var(--color-text-tertiary)]"
                style={{ top: h * HOUR_HEIGHT - 6 }}
              >
                {h}
              </div>
            ))}
          </div>

          {/* Day columns */}
          {days.map((day) => {
            const key = day.toFormat('yyyy-MM-dd');
            const dayLayouts = timedLayouts.get(key) ?? [];
            const dayStart = day.startOf('day');
            const today = isToday(day, timezone);

            return (
              <div
                key={key}
                data-daycol={key}
                className="relative flex-1 border-l border-[var(--color-separator)]"
              >
                {/* Background target: click an empty slot to create an event at
                    that time. Sits below event blocks and the time indicator. */}
                <button
                  type="button"
                  onClick={(e) => handleSlotClick(day, e)}
                  className="absolute inset-0 z-0 touch-manipulation"
                  aria-label={`${key} ${t('event.createEvent')}`}
                />

                {/* Hour grid lines */}
                {HOURS.map((h) => (
                  <div
                    key={h}
                    className="pointer-events-none absolute left-0 right-0 border-t border-[var(--color-separator)]"
                    style={{ top: h * HOUR_HEIGHT }}
                  />
                ))}

                {/* Current time indicator */}
                {today && (
                  <div
                    className="pointer-events-none absolute left-0 right-0 z-10"
                    style={{ top: currentTimeY }}
                  >
                    <div className="flex items-start">
                      <div className="now-dot -ml-1 -mt-[3px] h-2 w-2 shrink-0 rounded-full bg-[var(--color-accent)]" />
                      <div className="h-[2px] w-full bg-[var(--color-accent)]" />
                    </div>
                  </div>
                )}

                {/* Timed event blocks */}
                {dayLayouts.map(({ event: evt, leftPct, widthPct }) => {
                  const startDt = fromISOInZone(evt.startAt, timezone);
                  const endDt = fromISOInZone(evt.endAt, timezone);
                  // Clamped to this day, so a block that spans midnight fills
                  // the column rather than running off it.
                  const top = minutesToY(minutesIntoDay(startDt, dayStart));
                  // The floor the layout packs against, in this view's pixels:
                  // a block shorter than this -- a marker at a moment most of
                  // all -- is still drawn tall enough to read and to click.
                  const minHeight = minutesToY(MIN_RENDERED_MINUTES);
                  const baseHeight = Math.max(
                    minutesToY(minutesIntoDay(endDt, dayStart)) - top,
                    minHeight,
                  );
                  const resizing = resizePreview?.id === evt.id && resizePreview.dayKey === key;
                  const height = resizing
                    ? Math.max(minutesToY(resizePreview.endMin) - top, minHeight)
                    : baseHeight;
                  const endLabel = resizing
                    ? atMinutesIntoDay(dayStart, resizePreview.endMin).toFormat('H:mm')
                    : endDt.toFormat('H:mm');
                  // A moment has one time, not a range from itself to itself.
                  // While the end is being dragged there is a range again.
                  const marker = !resizing && +endDt === +startDt;
                  const movable = canMove(evt);
                  // Only the segment holding the real end can move it.
                  const holdsEnd = eventEndDay(evt, timezone).toFormat('yyyy-MM-dd') === key;

                  return (
                    <button
                      key={evt.id}
                      type="button"
                      onPointerDown={(e) => {
                        if (!movable) return;
                        const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
                        dragGeom.current = { width: rect.width, height: rect.height };
                        startDrag(evt, e);
                      }}
                      onClick={() => {
                        if (consumeClick() || consumeResizeClick()) return;
                        openEventModal(evt.id);
                      }}
                      className={`absolute z-[5] overflow-hidden rounded-md px-1.5 pt-1 text-left ${
                        movable ? 'cursor-grab active:cursor-grabbing' : ''
                      }`}
                      style={{
                        top,
                        height,
                        left: `calc(${leftPct}% + 2px)`,
                        width: `calc(${widthPct}% - 4px)`,
                        backgroundColor: `${evt.color}15`,
                        border: `1px solid ${evt.color}40`,
                      }}
                    >
                      <p className="truncate text-body font-semibold text-[var(--color-text-primary)]">
                        {evt.title}
                      </p>
                      <p className="text-caption tabular-nums text-[var(--color-text-secondary)]">
                        {marker
                          ? startDt.toFormat('H:mm')
                          : `${startDt.toFormat('H:mm')} - ${endLabel}`}
                      </p>
                      {movable && holdsEnd && (
                        <div
                          aria-hidden
                          onPointerDown={(e) => startResize(evt, e)}
                          className="absolute inset-x-0 bottom-0 h-2 cursor-ns-resize touch-none"
                        />
                      )}
                    </button>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>

      {/* Drag ghost: a lifted copy of the event block, same frame and contents. */}
      {drag && (
        <div
          className="pointer-events-none fixed z-50 overflow-hidden rounded-md px-1.5 pt-1 text-left opacity-90 shadow-lg"
          style={{
            left: drag.x - 8,
            top: drag.y - 8,
            width: dragGeom.current.width,
            height: dragGeom.current.height,
            // Surface base + color tint reproduces the on-grid block appearance,
            // since the live block sits over the (surface-colored) day column.
            backgroundColor: 'var(--color-surface)',
            backgroundImage: `linear-gradient(${drag.event.color}15, ${drag.event.color}15)`,
            border: `1px solid ${drag.event.color}40`,
          }}
        >
          <p className="truncate text-body font-semibold text-[var(--color-text-primary)]">
            {drag.event.title}
          </p>
          <p className="text-caption tabular-nums text-[var(--color-text-secondary)]">
            {fromISOInZone(drag.event.startAt, timezone).toFormat('H:mm')} -{' '}
            {fromISOInZone(drag.event.endAt, timezone).toFormat('H:mm')}
          </p>
        </div>
      )}

      {scopeDialog}
    </div>
  );
}
