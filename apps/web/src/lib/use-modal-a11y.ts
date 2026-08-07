import { type RefObject, useEffect, useRef } from 'react';

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function visibleFocusable(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (el) => el.getClientRects().length > 0 && el.getAttribute('aria-hidden') !== 'true',
  );
}

/**
 * Wires the behaviour a modal owes the keyboard: a scroll lock, Escape, a Tab
 * trap, and returning focus to wherever it was when the modal took over.
 *
 * `initialFocus` names the control that should receive focus on open; without
 * it the first focusable element in the container does.
 */
export function useModalA11y<T extends HTMLElement>(
  active: boolean,
  onClose: () => void,
  initialFocus?: RefObject<HTMLElement | null>,
): RefObject<T | null> {
  const ref = useRef<T | null>(null);
  // Read through a ref so a caller passing an inline closure does not re-run
  // the effect: re-running would restore and re-take focus on every render.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!active) return;
    // What had focus before the modal opened, so the keyboard lands back there
    // rather than at the top of the document once it closes.
    const restoreTo = document.activeElement as HTMLElement | null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const focusTimer = window.setTimeout(() => {
      const container = ref.current;
      const first = initialFocus?.current ?? (container ? visibleFocusable(container)[0] : null);
      first?.focus();
    }, 0);

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onCloseRef.current();
        return;
      }
      if (e.key !== 'Tab') return;
      const container = ref.current;
      if (!container) return;
      const focusable = visibleFocusable(container);
      if (focusable.length === 0) {
        e.preventDefault();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last?.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first?.focus();
      }
    };

    document.addEventListener('keydown', onKeyDown);
    return () => {
      window.clearTimeout(focusTimer);
      document.removeEventListener('keydown', onKeyDown);
      document.body.style.overflow = previousOverflow;
      // An element that left the document while the modal was open cannot be
      // focused; leaving focus where it is beats throwing an error.
      if (restoreTo?.isConnected) restoreTo.focus();
    };
  }, [active, initialFocus]);

  return ref;
}
