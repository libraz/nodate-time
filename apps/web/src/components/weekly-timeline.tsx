import { DateTime } from 'luxon';
import {
  type PointerEvent as ReactPointerEvent,
  useCallback,
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
import { canEdit, roleForCalendar } from '@/lib/permissions';
import { useEventDrag } from '@/lib/use-event-drag';
import { useScopedUpdate } from '@/lib/use-scoped-update';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

const HOUR_HEIGHT = 48;
const HOURS = Array.from({ length: 24 }, (_, i) => i);

export function WeeklyTimeline() {
  const t = useT();
  const selectedDate = useUiStore((s) => s.selectedDate);
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const me = useAuthStore((s) => s.user);
  const { requestUpdate, dialog: scopeDialog } = useScopedUpdate();
  const scrollRef = useRef<HTMLDivElement>(null);

  /** Minutes a clicked slot snaps to. */
  const SNAP_MINUTES = 30;
  /** Shortest event a resize may produce. */
  const MIN_DURATION_MIN = 30;

  // Live end-minute preview while resizing, so the block follows the cursor.
  const [resizePreview, setResizePreview] = useState<{ id: string; endMin: number } | null>(null);
  const resizeRef = useRef<{ evt: CalendarEvent; colTop: number; startMin: number } | null>(null);
  // A resize ends with a pointerup inside the block, which the browser turns into
  // a click on the parent button. Suppress that click so it doesn't open the modal.
  const suppressClick = useRef(false);
  // Size of the block being dragged, so the move ghost mirrors it exactly.
  const dragGeom = useRef<{ width: number; height: number }>({ width: 160, height: 40 });

  const canMove = useCallback(
    (evt: CalendarEvent) => canEdit(roleForCalendar(membersMap[evt.calendarId], me?.email)),
    [membersMap, me],
  );

  // Weekly drag changes both the day (column under cursor) and the time (cursor Y,
  // minus the in-block grab offset, snapped); duration is preserved.
  const handleMoveDrop = useCallback(
    (evt: CalendarEvent, x: number, y: number, ctx: { meta: unknown }) => {
      const el = document.elementFromPoint(x, y) as HTMLElement | null;
      const col = el?.closest('[data-daycol]') as HTMLElement | null;
      if (!col) return;
      const dayStr = col.getAttribute('data-daycol');
      if (!dayStr) return;
      const grabOffsetY = typeof ctx.meta === 'number' ? ctx.meta : 0;
      const offsetY = y - col.getBoundingClientRect().top - grabOffsetY;
      const rawMinutes = (offsetY / HOUR_HEIGHT) * 60;
      const snapped = Math.round(rawMinutes / SNAP_MINUTES) * SNAP_MINUTES;
      const clamped = Math.min(Math.max(snapped, 0), 23 * 60 + 30);
      const newStart = DateTime.fromFormat(dayStr, 'yyyy-MM-dd', { zone: timezone })
        .startOf('day')
        .plus({ minutes: clamped });
      requestUpdate(evt, buildMovedEvent(evt, newStart));
    },
    [timezone, requestUpdate],
  );

  const { drag, start: startDrag, consumeClick } = useEventDrag({ onDrop: handleMoveDrop });

  // Resize: drag the bottom edge of a timed block to change its end time. The
  // start and day stay fixed; the end snaps and keeps a minimum duration.
  const startResize = useCallback(
    (evt: CalendarEvent, blockTop: number, e: ReactPointerEvent) => {
      e.stopPropagation();
      if (e.pointerType === 'mouse' && e.button !== 0) return;
      if (!canMove(evt)) return;
      const col = (e.currentTarget as HTMLElement).closest('[data-daycol]') as HTMLElement | null;
      if (!col) return;
      const colTop = col.getBoundingClientRect().top;
      const startMin = Math.round((blockTop / HOUR_HEIGHT) * 60);
      resizeRef.current = { evt, colTop, startMin };

      const ctrl = new AbortController();
      let moved = false;
      const computeEnd = (clientY: number) => {
        const rawMin = ((clientY - colTop) / HOUR_HEIGHT) * 60;
        const snapped = Math.round(rawMin / SNAP_MINUTES) * SNAP_MINUTES;
        return Math.min(Math.max(snapped, startMin + MIN_DURATION_MIN), 24 * 60);
      };
      const onMove = (ev: globalThis.PointerEvent) => {
        moved = true;
        setResizePreview({ id: evt.id, endMin: computeEnd(ev.clientY) });
      };
      const onUp = (ev: globalThis.PointerEvent) => {
        ctrl.abort();
        const r = resizeRef.current;
        resizeRef.current = null;
        setResizePreview(null);
        // A plain tap on the handle (no drag) should fall through to the click,
        // which opens the event modal. Only a real resize commits and is guarded.
        if (!r || !moved) return;
        suppressClick.current = true;
        window.setTimeout(() => {
          suppressClick.current = false;
        }, 0);
        const endMin = computeEnd(ev.clientY);
        const dayStart = fromISOInZone(evt.startAt, timezone).startOf('day');
        requestUpdate(evt, buildResizedEvent(evt, dayStart.plus({ minutes: endMin })));
      };
      window.addEventListener('pointermove', onMove, { signal: ctrl.signal });
      window.addEventListener('pointerup', onUp, { signal: ctrl.signal });
      window.addEventListener('pointercancel', onUp, { signal: ctrl.signal });
    },
    [canMove, timezone, requestUpdate],
  );

  /** Open the new-event modal at the clicked time within a day column. */
  const handleSlotClick = (day: DateTime, e: React.MouseEvent<HTMLButtonElement>) => {
    const y = e.nativeEvent.offsetY;
    const rawMinutes = (y / HOUR_HEIGHT) * 60;
    const snapped = Math.round(rawMinutes / SNAP_MINUTES) * SNAP_MINUTES;
    const clamped = Math.min(Math.max(snapped, 0), 23 * 60 + 30);
    const start = day.startOf('day').plus({ minutes: clamped });
    setSelectedDate(start);
    openEventModal(undefined, start);
  };

  // Anchor week days to the user's timezone so cell boundaries match event bucketing.
  const days = useMemo(
    () => getWeekDays(selectedDate.setZone(timezone, { keepLocalTime: true })),
    [selectedDate, timezone],
  );

  const { allDayEvents, timedEvents } = useMemo(() => {
    const allDay: Map<string, CalendarEvent[]> = new Map();
    const timed: Map<string, CalendarEvent[]> = new Map();

    for (const evt of events) {
      if (!activeCalendarIds.includes(evt.calendarId)) continue;
      const evtStartMs = fromISOInZone(evt.startAt, timezone).toMillis();
      const evtEndMs = fromISOInZone(evt.endAt, timezone).toMillis();
      for (const day of days) {
        const dayStart = day.startOf('day').toMillis();
        const dayEnd = day.endOf('day').toMillis() + 1;
        if (evtStartMs < dayEnd && evtEndMs > dayStart) {
          const key = day.toFormat('yyyy-MM-dd');
          if (evt.allDay) {
            const arr = allDay.get(key) ?? [];
            arr.push(evt);
            allDay.set(key, arr);
          } else {
            const arr = timed.get(key) ?? [];
            arr.push(evt);
            timed.set(key, arr);
          }
        }
      }
    }
    return { allDayEvents: allDay, timedEvents: timed };
  }, [events, activeCalendarIds, days, timezone]);

  /** Convert ISO string to pixel offset within the day column. */
  const timeToY = (iso: string, dayStartMs: number): number => {
    const ms = fromISOInZone(iso, timezone).toMillis() - dayStartMs;
    const hours = ms / 3600000;
    return Math.max(0, hours * HOUR_HEIGHT);
  };

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
          const today = isToday(day);
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
            const dayTimed = timedEvents.get(key) ?? [];
            const dayStartMs = day.startOf('day').toMillis();
            const today = isToday(day);

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
                {dayTimed.map((evt) => {
                  const evtStartMs = fromISOInZone(evt.startAt, timezone).toMillis();
                  const evtEndMs = fromISOInZone(evt.endAt, timezone).toMillis();
                  const clampedStart =
                    DateTime.fromMillis(Math.max(evtStartMs, dayStartMs)).toISO() ?? evt.startAt;
                  const clampedEnd =
                    DateTime.fromMillis(Math.min(evtEndMs, dayStartMs + 86400000)).toISO() ??
                    evt.endAt;
                  const top = timeToY(clampedStart, dayStartMs);
                  const baseHeight = Math.max(timeToY(clampedEnd, dayStartMs) - top, 20);
                  const startDt = fromISOInZone(evt.startAt, timezone);
                  const endDt = fromISOInZone(evt.endAt, timezone);
                  const resizing = resizePreview?.id === evt.id;
                  const height = resizing
                    ? Math.max((resizePreview.endMin * HOUR_HEIGHT) / 60 - top, 20)
                    : baseHeight;
                  const endLabel = resizing
                    ? DateTime.fromMillis(dayStartMs, { zone: timezone })
                        .plus({ minutes: resizePreview.endMin })
                        .toFormat('H:mm')
                    : endDt.toFormat('H:mm');
                  const movable = canMove(evt);

                  return (
                    <button
                      key={evt.id}
                      type="button"
                      onPointerDown={(e) => {
                        if (!movable) return;
                        const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
                        dragGeom.current = { width: rect.width, height: rect.height };
                        startDrag(evt, e, e.nativeEvent.offsetY);
                      }}
                      onClick={() => {
                        if (consumeClick() || suppressClick.current) {
                          suppressClick.current = false;
                          return;
                        }
                        openEventModal(evt.id);
                      }}
                      className={`absolute left-0.5 right-0.5 z-[5] overflow-hidden rounded-md px-1.5 pt-1 text-left ${
                        movable ? 'cursor-grab active:cursor-grabbing' : ''
                      }`}
                      style={{
                        top,
                        height,
                        backgroundColor: `${evt.color}15`,
                        border: `1px solid ${evt.color}40`,
                      }}
                    >
                      <p className="truncate text-body font-semibold text-[var(--color-text-primary)]">
                        {evt.title}
                      </p>
                      <p className="text-caption tabular-nums text-[var(--color-text-secondary)]">
                        {startDt.toFormat('H:mm')} - {endLabel}
                      </p>
                      {movable && (
                        <div
                          aria-hidden
                          onPointerDown={(e) => startResize(evt, top, e)}
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
