import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

/* ============================================
   useFloating — positions a floating element
   relative to an anchor, rendered via portal.
   Clamps to viewport edges and supports
   above/below flip when space is tight.
   ============================================ */

export function useFloating(open: boolean, floatingWidth = 280) {
  const anchorRef = useRef<HTMLButtonElement>(null);
  const floatingRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ top: 0, left: 0, width: 0 });

  // Recompute position when open changes or after floating renders
  const reposition = useCallback(() => {
    if (!open || !anchorRef.current) return;
    const rect = anchorRef.current.getBoundingClientRect();
    const floatingHeight = floatingRef.current?.offsetHeight ?? 340;
    const spaceBelow = window.innerHeight - rect.bottom;
    const goAbove = spaceBelow < floatingHeight + 8 && rect.top > spaceBelow;
    // Clamp using the floating element's real width once mounted; a long-label
    // dropdown can be far wider than floatingWidth and would clip off-screen.
    const fw = floatingRef.current?.offsetWidth ?? floatingWidth;

    setPos({
      top: goAbove ? Math.max(8, rect.top - floatingHeight - 4) : rect.bottom + 4,
      left: Math.max(8, Math.min(rect.left, window.innerWidth - fw - 8)),
      width: rect.width,
    });
  }, [open, floatingWidth]);

  useLayoutEffect(reposition, [reposition]);

  // Re-measure after floating element mounts (first render has no ref)
  useEffect(() => {
    if (open && floatingRef.current) reposition();
  }, [open, reposition]);

  return { anchorRef, floatingRef, pos };
}

/* ============================================
   Shared: outside-click + Escape handler
   ============================================ */

export function useOutsideClose(
  refs: React.RefObject<HTMLElement | null>[],
  onClose: () => void,
  active: boolean,
) {
  // Read both through a ref, as useModalA11y does with its own onClose. Every
  // caller builds the array inline and some pass an inline closure, so naming
  // them here would re-run the effect on every render the dropdown survives,
  // taking the document listeners off and putting them back each time.
  const latest = useRef({ refs, onClose });
  useEffect(() => {
    latest.current = { refs, onClose };
  });

  useEffect(() => {
    if (!active) return;
    const handlePointer = (e: MouseEvent | TouchEvent) => {
      const target = e.target as Node;
      const { refs: current, onClose: close } = latest.current;
      if (current.every((r) => r.current && !r.current.contains(target))) close();
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') latest.current.onClose();
    };
    document.addEventListener('mousedown', handlePointer);
    document.addEventListener('touchstart', handlePointer);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handlePointer);
      document.removeEventListener('touchstart', handlePointer);
      document.removeEventListener('keydown', handleKey);
    };
  }, [active]);
}

const POPUP_FOCUSABLE = 'button:not([disabled]):not([tabindex="-1"]), input:not([disabled])';

/**
 * Keys every floating panel owes the keyboard: Escape closes it without also
 * closing the dialog behind, and Tab stays inside it. The panel is portalled
 * out of the dialog, so the dialog's own trap cannot see it.
 */
export function handlePopupKeys(e: React.KeyboardEvent<HTMLElement>, close: () => void) {
  if (e.key === 'Escape') {
    e.preventDefault();
    e.stopPropagation();
    close();
    return;
  }
  if (e.key !== 'Tab') return;
  e.stopPropagation();
  const stops = Array.from(e.currentTarget.querySelectorAll<HTMLElement>(POPUP_FOCUSABLE));
  const first = stops[0];
  const last = stops[stops.length - 1];
  if (!first || !last) {
    e.preventDefault();
    return;
  }
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}

/** Below `sm`, pickers open as a bottom sheet with thumb-sized targets instead of a popover. */
export function useSheetPicker(): boolean {
  const query = '(max-width: 639px)';
  const [sheet, setSheet] = useState(() => window.matchMedia?.(query).matches === true);
  useEffect(() => {
    const mql = window.matchMedia?.(query);
    if (!mql) return;
    const update = () => setSheet(mql.matches);
    mql.addEventListener('change', update);
    return () => mql.removeEventListener('change', update);
  }, []);
  return sheet;
}

/**
 * The surface every picker panel sits on: a popover anchored to its field, or
 * on a phone a bottom sheet over a dimmed backdrop. A tap on the backdrop is
 * outside `floatingRef`, so the panel's outside-click handler closes it.
 */
export function PickerSurface({
  sheet,
  title,
  dialogLabel,
  floatingRef,
  style,
  popoverClassName,
  onKeyDown,
  children,
}: {
  sheet: boolean;
  title?: string | undefined;
  /**
   * Makes the panel a modal dialog with this name. A panel the user works in
   * rather than picks one value from needs it, so the calendar's own keyboard
   * shortcuts stand down while it is open.
   */
  dialogLabel?: string | undefined;
  floatingRef: React.RefObject<HTMLDivElement | null>;
  style: React.CSSProperties;
  popoverClassName: string;
  onKeyDown: (e: React.KeyboardEvent<HTMLDivElement>) => void;
  children: React.ReactNode;
}) {
  if (sheet) {
    return createPortal(
      <>
        <div
          aria-hidden
          className="modal-backdrop fixed inset-0 z-[9998] bg-[var(--color-overlay)]"
        />
        {/* biome-ignore lint/a11y/noStaticElementInteractions: the sheet only receives key events bubbling from its controls */}
        <div
          ref={floatingRef}
          onKeyDown={onKeyDown}
          {...(dialogLabel
            ? { role: 'dialog', 'aria-modal': true, 'aria-label': dialogLabel }
            : {})}
          className="glass-surface-heavy bottom-sheet fixed inset-x-0 bottom-0 z-[9999] flex max-h-[85dvh] flex-col rounded-t-[var(--radius-xl)] px-4 pt-2 pb-[max(env(safe-area-inset-bottom),16px)] ring-1 ring-[var(--color-border)]"
          style={{ boxShadow: 'var(--shadow-elevated)' }}
        >
          <div
            aria-hidden
            className="mx-auto mb-2 h-1 w-9 shrink-0 rounded-full bg-[var(--color-border-strong)]"
          />
          {title && (
            <p className="mb-1 px-1 text-footnote font-medium text-[var(--color-text-secondary)]">
              {title}
            </p>
          )}
          {children}
        </div>
      </>,
      document.body,
    );
  }
  return createPortal(
    // biome-ignore lint/a11y/noStaticElementInteractions: the panel only receives key events bubbling from its controls
    <div
      ref={floatingRef}
      onKeyDown={onKeyDown}
      {...(dialogLabel ? { role: 'dialog', 'aria-modal': true, 'aria-label': dialogLabel } : {})}
      className={`dropdown-panel fixed z-[9999] bg-[var(--color-surface-elevated)] ring-1 ring-[var(--color-border)] ${popoverClassName}`}
      style={{ ...style, boxShadow: 'var(--shadow-elevated)', backdropFilter: 'blur(20px)' }}
    >
      {children}
    </div>,
    document.body,
  );
}

export function Chevron({ dir }: { dir: 'left' | 'right' | 'down' }) {
  const d = dir === 'left' ? 'M15 18l-6-6 6-6' : dir === 'right' ? 'M9 18l6-6-6-6' : 'M6 9l6 6 6-6';
  return (
    <svg
      width={dir === 'down' ? 14 : 16}
      height={dir === 'down' ? 14 : 16}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={d} />
    </svg>
  );
}

/**
 * The other end of a date range the field belongs to. The panel draws the
 * span between it and the day under the pointer, so picking one end shows the
 * period it produces before it is committed.
 */
