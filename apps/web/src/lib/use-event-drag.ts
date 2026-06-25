import {
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from 'react';
import type { CalendarEvent } from '@/types/calendar';

/** Context handed to onDrop describing where the drag began. */
export interface DragDropContext {
  /** Key (from resolveKey) of the cell where the drag started. */
  originKey: string | null;
  /** Arbitrary metadata captured at drag start (e.g. grab offset). */
  meta: unknown;
}

/** Live drag state for rendering a cursor-following ghost / target highlight. */
export interface DragState {
  event: CalendarEvent;
  x: number;
  y: number;
  /** Key (from resolveKey) of the cell currently under the cursor. */
  hoverKey: string | null;
  /** Key of the cell where the drag began (for computing the landing span). */
  originKey: string | null;
}

interface Options {
  onDrop: (event: CalendarEvent, x: number, y: number, ctx: DragDropContext) => void;
  /** Resolves the drop-target key under a viewport point (e.g. a date cell). */
  resolveKey?: (x: number, y: number) => string | null;
  /** Pixels the mouse must travel before a press becomes a drag. */
  threshold?: number;
  /** Hold duration (ms) before a touch press becomes a drag. */
  longPressMs?: number;
}

interface Pending {
  event: CalendarEvent;
  startX: number;
  startY: number;
  pointerType: string;
  originKey: string | null;
  meta: unknown;
  active: boolean;
  ctrl: AbortController;
  timer: number;
}

/** Distance (px) a touch may drift during the hold before it counts as a scroll. */
const TOUCH_CANCEL_PX = 12;

/**
 * Drag-to-move for calendar events. The mouse starts dragging after a small
 * movement threshold; touch requires a long press first so taps and scrolling
 * keep working. The click that follows a real drag is suppressed.
 */
export function useEventDrag({ onDrop, resolveKey, threshold = 5, longPressMs = 350 }: Options) {
  const [drag, setDrag] = useState<DragState | null>(null);
  const pending = useRef<Pending | null>(null);
  const suppressClick = useRef(false);

  const endPending = useCallback(() => {
    const s = pending.current;
    if (!s) return;
    s.ctrl.abort();
    if (s.timer) clearTimeout(s.timer);
    pending.current = null;
  }, []);

  const handleMove = useCallback(
    (e: globalThis.PointerEvent) => {
      const s = pending.current;
      if (!s) return;
      if (!s.active) {
        const dist = Math.hypot(e.clientX - s.startX, e.clientY - s.startY);
        if (s.pointerType === 'mouse') {
          if (dist < threshold) return;
          s.active = true;
        } else {
          // Movement before the long press fires means the user is scrolling.
          if (dist > TOUCH_CANCEL_PX) endPending();
          return;
        }
      }
      setDrag({
        event: s.event,
        x: e.clientX,
        y: e.clientY,
        hoverKey: resolveKey ? resolveKey(e.clientX, e.clientY) : null,
        originKey: s.originKey,
      });
    },
    [resolveKey, threshold, endPending],
  );

  const handleUp = useCallback(
    (e: globalThis.PointerEvent) => {
      const s = pending.current;
      endPending();
      setDrag(null);
      if (s?.active) {
        suppressClick.current = true;
        // A drop onto a different element fires no click, so clear the guard on
        // the next tick (the same-element click, if any, runs first).
        window.setTimeout(() => {
          suppressClick.current = false;
        }, 0);
        onDrop(s.event, e.clientX, e.clientY, { originKey: s.originKey, meta: s.meta });
      }
    },
    [onDrop, endPending],
  );

  // Blocks page scrolling once a touch drag is active.
  const preventTouchScroll = useCallback((e: TouchEvent) => {
    if (pending.current?.active) e.preventDefault();
  }, []);

  const start = useCallback(
    (event: CalendarEvent, e: ReactPointerEvent, meta?: unknown) => {
      if (e.pointerType === 'mouse' && e.button !== 0) return;
      const ctrl = new AbortController();
      const { signal } = ctrl;
      const p: Pending = {
        event,
        startX: e.clientX,
        startY: e.clientY,
        pointerType: e.pointerType,
        originKey: resolveKey ? resolveKey(e.clientX, e.clientY) : null,
        meta,
        active: false,
        ctrl,
        timer: 0,
      };
      pending.current = p;
      window.addEventListener('pointermove', handleMove, { signal });
      window.addEventListener('pointerup', handleUp, { signal });
      window.addEventListener('pointercancel', handleUp, { signal });
      if (e.pointerType !== 'mouse') {
        document.addEventListener('touchmove', preventTouchScroll, { signal, passive: false });
        p.timer = window.setTimeout(() => {
          if (pending.current !== p) return;
          p.active = true;
          navigator.vibrate?.(10);
          setDrag({
            event: p.event,
            x: p.startX,
            y: p.startY,
            hoverKey: resolveKey ? resolveKey(p.startX, p.startY) : null,
            originKey: p.originKey,
          });
        }, longPressMs);
      }
    },
    [handleMove, handleUp, preventTouchScroll, resolveKey, longPressMs],
  );

  /** Returns true if the click follows a drag and should be ignored. */
  const consumeClick = useCallback(() => {
    if (suppressClick.current) {
      suppressClick.current = false;
      return true;
    }
    return false;
  }, []);

  useEffect(() => () => endPending(), [endPending]);

  return { drag, start, consumeClick };
}
