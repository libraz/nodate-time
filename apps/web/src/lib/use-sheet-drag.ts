import type { CSSProperties, PointerEvent as ReactPointerEvent } from 'react';
import { useRef, useState } from 'react';

interface SheetDrag {
  /** Attach to the grab zone at the top of the sheet. */
  onPointerDown: (e: ReactPointerEvent) => void;
  /** Merge into the sheet's own style; empty while the sheet is at rest. */
  style: CSSProperties;
}

/**
 * Swipe-down-to-dismiss for a mobile bottom sheet, driven from its handle.
 *
 * The sheet tracks the finger 1:1 while held, then resolves on release: a long
 * pull or a quick downward flick calls `onDismiss`, anything else snaps back.
 * Window listeners (not pointer capture) guarantee the release is caught even
 * when the finger drifts off the handle.
 */
export function useSheetDrag(onDismiss: () => void): SheetDrag {
  const [dragY, setDragY] = useState(0);
  const dismiss = useRef(onDismiss);
  dismiss.current = onDismiss;

  const onPointerDown = (e: ReactPointerEvent) => {
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    const start = { startY: e.clientY, lastY: e.clientY, lastT: e.timeStamp, vy: 0 };
    const ctrl = new AbortController();
    const move = (ev: PointerEvent) => {
      const dt = ev.timeStamp - start.lastT;
      if (dt > 0) start.vy = (ev.clientY - start.lastY) / dt;
      start.lastY = ev.clientY;
      start.lastT = ev.timeStamp;
      const dy = ev.clientY - start.startY;
      setDragY(dy > 0 ? dy : 0);
    };
    const end = () => {
      ctrl.abort();
      const dy = start.lastY - start.startY;
      setDragY(0);
      if (dy > 160 || (dy > 48 && start.vy > 0.55)) dismiss.current();
    };
    window.addEventListener('pointermove', move, { signal: ctrl.signal });
    window.addEventListener('pointerup', end, { signal: ctrl.signal });
    window.addEventListener('pointercancel', end, { signal: ctrl.signal });
  };

  return {
    onPointerDown,
    style: dragY ? { transform: `translateY(${dragY}px)`, transition: 'none' } : {},
  };
}
