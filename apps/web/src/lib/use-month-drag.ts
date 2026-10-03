import { DateTime } from 'luxon';
import { type PointerEvent as ReactPointerEvent, useCallback, useMemo, useRef } from 'react';
import type { DragLanding } from '@/components/month-week-row';
import { fromISOInZone } from '@/lib/date-utils';
import { buildMovedEvent } from '@/lib/event-move';
import { eventDayZone } from '@/lib/event-zone';
import type { EventInput } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';
import { useCanMoveEvent } from './use-can-move-event';
import { useEventDrag } from './use-event-drag';
import { eventEndDay, eventStartDay, weekKeysInRange } from './week-layout';

/** Whole days between two `yyyy-MM-dd` cell keys, counted on dates rather than hours. */
function dayDelta(fromKey: string, toKey: string): number {
  const from = DateTime.fromISO(fromKey, { zone: 'UTC' });
  const to = DateTime.fromISO(toKey, { zone: 'UTC' });
  return Math.round(to.diff(from, 'days').days);
}

/**
 * The start an event moves to when dragged from one month cell to another.
 *
 * The days are added on the wall clock of the zone the event's days are
 * measured in, so an all-day event stays at midnight in its own zone however
 * many DST changes the viewer's zone crosses between the two cells. Null when
 * the drop lands on the cell it started from.
 */
export function monthDropStart(
  evt: CalendarEvent,
  originKey: string,
  targetKey: string,
  zone: string,
): DateTime | null {
  const delta = dayDelta(originKey, targetKey);
  if (delta === 0) return null;
  return fromISOInZone(evt.startAt, eventDayZone(evt, zone)).plus({ days: delta });
}

/** The cells a dragged event would cover if dropped on `hoverKey`, in the grid's zone. */
export function monthDragLanding(
  evt: CalendarEvent,
  originKey: string,
  hoverKey: string,
  zone: string,
): { start: DateTime; end: DateTime } {
  const startDay = eventStartDay(evt, zone);
  const span = Math.max(1, Math.round(eventEndDay(evt, zone).diff(startDay, 'days').days) + 1);
  const start = startDay.plus({ days: dayDelta(originKey, hoverKey) });
  return { start, end: start.plus({ days: span - 1 }) };
}

// Multi-day bars live in an overlay outside the day cells, so the topmost
// element under the cursor may not be inside a [data-day] cell. Walk every
// element at the point and use the first one that resolves to a date cell.
function resolveDayKey(x: number, y: number): string | null {
  for (const el of document.elementsFromPoint(x, y)) {
    const day = el.closest('[data-day]')?.getAttribute('data-day');
    if (day) return day;
  }
  return null;
}

/**
 * Drag-to-move between month cells, shared by every month surface so that the
 * same gesture produces the same payload wherever it is made.
 */
export function useMonthDrag({
  timezone,
  requestUpdate,
}: {
  timezone: string;
  requestUpdate: (evt: CalendarEvent, payload: EventInput) => void;
}) {
  const canMove = useCanMoveEvent();
  const dragEnded = useRef(false);

  const handleDrop = useCallback(
    (evt: CalendarEvent, x: number, y: number, ctx: { originKey: string | null }) => {
      dragEnded.current = true;
      const targetKey = resolveDayKey(x, y);
      if (!targetKey || !ctx.originKey) return;
      const newStart = monthDropStart(evt, ctx.originKey, targetKey, timezone);
      if (newStart) requestUpdate(evt, buildMovedEvent(evt, newStart));
    },
    [timezone, requestUpdate],
  );

  const { drag, start, consumeClick } = useEventDrag({
    onDrop: handleDrop,
    resolveKey: resolveDayKey,
  });

  /**
   * True once after a drag has ended. A touch that moved an event ends in a
   * touchend carrying the whole travel, which must not be read as a swipe.
   */
  const consumeDragEnd = useCallback(() => {
    const ended = dragEnded.current;
    dragEnded.current = false;
    return ended;
  }, []);

  const dragLanding = useMemo<DragLanding | null>(() => {
    if (!drag?.hoverKey || !drag.originKey) return null;
    return {
      event: drag.event,
      ...monthDragLanding(drag.event, drag.originKey, drag.hoverKey, timezone),
    };
  }, [drag, timezone]);

  // Only the rows the ghost crosses need it; every other row keeps the same
  // (null) prop and stays out of the re-render a pointer sample causes.
  const landingWeeks = useMemo(
    () => (dragLanding ? weekKeysInRange(dragLanding.start, dragLanding.end) : null),
    [dragLanding],
  );

  const handleEventPointerDown = useCallback(
    (evt: CalendarEvent, e: ReactPointerEvent) => {
      if (canMove(evt)) start(evt, e);
    },
    [canMove, start],
  );

  return {
    drag,
    dragLanding,
    landingWeeks,
    canMove,
    consumeClick,
    consumeDragEnd,
    handleEventPointerDown,
  };
}
