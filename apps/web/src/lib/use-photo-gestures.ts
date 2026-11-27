import { useCallback, useEffect, useRef, useState } from 'react';

/** Movement under which a press still counts as a tap. */
const TAP_SLOP_PX = 8;
/** Window in which a second tap makes a double tap. */
const DOUBLE_TAP_MS = 280;
/** Share of the stage width a drag has to cover to turn the page. */
const PAGE_FRACTION = 0.22;
/** Release speed, in px/ms, that turns the page or dismisses however short the drag. */
const FLICK_SPEED = 0.45;
/** Downward drag that dismisses the viewer. */
const DISMISS_PX = 110;
/** Vertical drag that opens or closes the details. */
const SWIPE_SHEET_PX = 70;
/** How much of the drag past the first or last photo the track follows. */
const EDGE_RESISTANCE = 0.3;
/** Gap between neighbouring photos on the track. */
export const PAGE_GAP_PX = 16;
/** Duration of a snap, page turn or zoom settling into place. */
export const SETTLE_MS = 260;
const MAX_SCALE = 4;
const DOUBLE_TAP_SCALE = 2.5;

export interface PhotoZoom {
  scale: number;
  x: number;
  y: number;
}

export interface PhotoGestures {
  stageRef: React.RefObject<HTMLDivElement | null>;
  handlers: {
    onPointerDown: (e: React.PointerEvent) => void;
    onPointerMove: (e: React.PointerEvent) => void;
    onPointerUp: (e: React.PointerEvent) => void;
    onPointerCancel: (e: React.PointerEvent) => void;
  };
  /** Horizontal offset of the photo track. */
  pageX: number;
  /** Where the current photo has been dragged while being dismissed. */
  dismiss: { x: number; y: number };
  zoom: PhotoZoom;
  /** True while offsets should animate to their targets rather than follow a finger. */
  settling: boolean;
}

interface PhotoGestureOptions {
  /** Changes when the photo does, resetting zoom. */
  photoKey: string;
  canPrev: boolean;
  canNext: boolean;
  /** Whether a downward drag carries the photo away; off while the details are open. */
  dismissable: boolean;
  onPage: (direction: -1 | 1) => void;
  onDismiss: () => void;
  onSwipeUp: () => void;
  onSwipeDown: () => void;
  onTap: () => void;
}

type Mode = 'pending' | 'page' | 'dismiss' | 'vertical' | 'pan' | 'pinch';

interface Gesture {
  mode: Mode;
  startX: number;
  startY: number;
  lastX: number;
  lastY: number;
  lastT: number;
  vx: number;
  vy: number;
  startZoom: PhotoZoom;
  startDist: number;
  startMid: { x: number; y: number };
}

const NO_ZOOM: PhotoZoom = { scale: 1, x: 0, y: 0 };

function clampZoom(z: PhotoZoom, w: number, h: number): PhotoZoom {
  const maxX = ((z.scale - 1) * w) / 2;
  const maxY = ((z.scale - 1) * h) / 2;
  return {
    scale: z.scale,
    x: Math.max(-maxX, Math.min(maxX, z.x)),
    y: Math.max(-maxY, Math.min(maxY, z.y)),
  };
}

/**
 * The touch model of a phone photo viewer: drag sideways to page with the
 * neighbours following, drag down to carry the photo away, pinch or double
 * tap to zoom and drag to pan while zoomed. A single tap waits out the
 * double-tap window before it is reported.
 */
export function usePhotoGestures({
  photoKey,
  canPrev,
  canNext,
  dismissable,
  onPage,
  onDismiss,
  onSwipeUp,
  onSwipeDown,
  onTap,
}: PhotoGestureOptions): PhotoGestures {
  const stageRef = useRef<HTMLDivElement>(null);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef<Gesture | null>(null);
  const lastTap = useRef<{ t: number; x: number; y: number } | null>(null);
  const tapTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const settleTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [pageX, setPageX] = useState(0);
  const [dismiss, setDismiss] = useState({ x: 0, y: 0 });
  const [zoom, setZoom] = useState<PhotoZoom>(NO_ZOOM);
  const [settling, setSettling] = useState(false);
  const zoomRef = useRef(zoom);
  zoomRef.current = zoom;

  // biome-ignore lint/correctness/useExhaustiveDependencies: a new photo starts unzoomed
  useEffect(() => {
    setZoom(NO_ZOOM);
  }, [photoKey]);

  useEffect(
    () => () => {
      if (tapTimer.current) clearTimeout(tapTimer.current);
      if (settleTimer.current) clearTimeout(settleTimer.current);
    },
    [],
  );

  const stageBox = () => {
    const el = stageRef.current;
    const rect = el?.getBoundingClientRect();
    return {
      w: el?.clientWidth || window.innerWidth,
      h: el?.clientHeight || window.innerHeight,
      cx: rect ? rect.left + rect.width / 2 : window.innerWidth / 2,
      cy: rect ? rect.top + rect.height / 2 : window.innerHeight / 2,
    };
  };

  /** Animates to the given offsets, then runs `after` once they have arrived. */
  const settle = useCallback(
    (
      to: { pageX?: number; dismiss?: { x: number; y: number }; zoom?: PhotoZoom },
      after?: () => void,
    ) => {
      if (settleTimer.current) clearTimeout(settleTimer.current);
      setSettling(true);
      setPageX(to.pageX ?? 0);
      setDismiss(to.dismiss ?? { x: 0, y: 0 });
      if (to.zoom) setZoom(to.zoom);
      settleTimer.current = setTimeout(() => {
        setSettling(false);
        after?.();
      }, SETTLE_MS);
    },
    [],
  );

  const beginPinch = () => {
    const [a, b] = [...pointers.current.values()];
    if (!a || !b) return;
    gesture.current = {
      ...(gesture.current as Gesture),
      mode: 'pinch',
      startDist: Math.hypot(a.x - b.x, a.y - b.y) || 1,
      startMid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
      startZoom: zoomRef.current,
    };
  };

  const onPointerDown = (e: React.PointerEvent) => {
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    if (settling) return;
    stageRef.current?.setPointerCapture?.(e.pointerId);
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.current.size === 2) {
      beginPinch();
      return;
    }
    if (pointers.current.size > 2) return;
    gesture.current = {
      mode: zoomRef.current.scale > 1 ? 'pan' : 'pending',
      startX: e.clientX,
      startY: e.clientY,
      lastX: e.clientX,
      lastY: e.clientY,
      lastT: e.timeStamp,
      vx: 0,
      vy: 0,
      startZoom: zoomRef.current,
      startDist: 1,
      startMid: { x: e.clientX, y: e.clientY },
    };
  };

  const onPointerMove = (e: React.PointerEvent) => {
    const g = gesture.current;
    if (!g || !pointers.current.has(e.pointerId)) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const box = stageBox();

    if (g.mode === 'pinch') {
      const [a, b] = [...pointers.current.values()];
      if (!a || !b) return;
      const dist = Math.hypot(a.x - b.x, a.y - b.y);
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      const scale = Math.max(0.8, Math.min(MAX_SCALE, (g.startZoom.scale * dist) / g.startDist));
      // Keep the point that was under the fingers under them.
      const qx = (g.startMid.x - box.cx - g.startZoom.x) / g.startZoom.scale;
      const qy = (g.startMid.y - box.cy - g.startZoom.y) / g.startZoom.scale;
      setZoom({ scale, x: mid.x - box.cx - scale * qx, y: mid.y - box.cy - scale * qy });
      return;
    }

    const dt = Math.max(1, e.timeStamp - g.lastT);
    g.vx = (e.clientX - g.lastX) / dt;
    g.vy = (e.clientY - g.lastY) / dt;
    g.lastX = e.clientX;
    g.lastY = e.clientY;
    g.lastT = e.timeStamp;
    const dx = e.clientX - g.startX;
    const dy = e.clientY - g.startY;

    if (g.mode === 'pending') {
      if (Math.hypot(dx, dy) < TAP_SLOP_PX) return;
      if (Math.abs(dx) > Math.abs(dy)) g.mode = 'page';
      else g.mode = dy > 0 && dismissable ? 'dismiss' : 'vertical';
    }
    if (g.mode === 'page') {
      const blocked = (dx > 0 && !canPrev) || (dx < 0 && !canNext);
      setPageX(blocked ? dx * EDGE_RESISTANCE : dx);
    } else if (g.mode === 'dismiss') {
      setDismiss({ x: dx, y: Math.max(0, dy) });
    } else if (g.mode === 'pan') {
      setZoom(
        clampZoom(
          { scale: g.startZoom.scale, x: g.startZoom.x + dx, y: g.startZoom.y + dy },
          box.w,
          box.h,
        ),
      );
    }
  };

  const handleTap = (x: number, y: number, t: number) => {
    const prev = lastTap.current;
    if (prev && t - prev.t < DOUBLE_TAP_MS && Math.hypot(x - prev.x, y - prev.y) < 30) {
      lastTap.current = null;
      if (tapTimer.current) clearTimeout(tapTimer.current);
      const box = stageBox();
      if (zoomRef.current.scale > 1) {
        settle({ zoom: NO_ZOOM });
      } else {
        const s = DOUBLE_TAP_SCALE;
        settle({
          zoom: clampZoom(
            { scale: s, x: (x - box.cx) * (1 - s), y: (y - box.cy) * (1 - s) },
            box.w,
            box.h,
          ),
        });
      }
      return;
    }
    lastTap.current = { t, x, y };
    if (tapTimer.current) clearTimeout(tapTimer.current);
    tapTimer.current = setTimeout(() => {
      lastTap.current = null;
      onTap();
    }, DOUBLE_TAP_MS);
  };

  const finish = (e: React.PointerEvent, cancelled: boolean) => {
    const g = gesture.current;
    if (!pointers.current.delete(e.pointerId) || !g) return;
    const box = stageBox();

    if (g.mode === 'pinch') {
      // A finger lifted mid-pinch leaves the other one panning.
      const rest = [...pointers.current.values()][0];
      if (rest) {
        g.mode = 'pan';
        g.startX = rest.x;
        g.startY = rest.y;
        g.startZoom = zoomRef.current;
        return;
      }
      gesture.current = null;
      const z = zoomRef.current;
      if (z.scale <= 1.02) settle({ zoom: NO_ZOOM });
      else if (z.scale > MAX_SCALE)
        settle({ zoom: clampZoom({ ...z, scale: MAX_SCALE }, box.w, box.h) });
      else settle({ zoom: clampZoom(z, box.w, box.h) });
      return;
    }
    if (pointers.current.size > 0) return;
    gesture.current = null;
    if (cancelled) {
      settle({});
      return;
    }

    const dx = e.clientX - g.startX;
    const dy = e.clientY - g.startY;
    switch (g.mode) {
      case 'pending':
        handleTap(e.clientX, e.clientY, e.timeStamp);
        return;
      case 'pan':
        // A zoomed photo still answers a tap, so a double tap can zoom it out.
        if (Math.hypot(dx, dy) < TAP_SLOP_PX) handleTap(e.clientX, e.clientY, e.timeStamp);
        return;
      case 'page': {
        const far = Math.abs(dx) > box.w * PAGE_FRACTION || Math.abs(g.vx) > FLICK_SPEED;
        const direction: -1 | 1 = dx < 0 ? 1 : -1;
        const allowed = direction === 1 ? canNext : canPrev;
        if (far && allowed && Math.sign(g.vx || dx) === Math.sign(dx)) {
          settle({ pageX: -direction * (box.w + PAGE_GAP_PX) }, () => {
            // The track jumps back to rest under the new photo in the same render.
            setPageX(0);
            onPage(direction);
          });
        } else {
          settle({});
        }
        return;
      }
      case 'dismiss':
        // Carried off the bottom while the backdrop fades, then closed.
        if (dy > DISMISS_PX || g.vy > FLICK_SPEED)
          settle({ dismiss: { x: dx, y: box.h } }, onDismiss);
        else settle({});
        return;
      case 'vertical':
        if (dy <= -SWIPE_SHEET_PX) onSwipeUp();
        else if (dy >= SWIPE_SHEET_PX) onSwipeDown();
        return;
      default:
        return;
    }
  };

  return {
    stageRef,
    handlers: {
      onPointerDown,
      onPointerMove,
      onPointerUp: (e) => finish(e, false),
      onPointerCancel: (e) => finish(e, true),
    },
    pageX,
    dismiss,
    zoom,
    settling,
  };
}
