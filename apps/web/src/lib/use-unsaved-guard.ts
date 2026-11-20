import { useRouter } from '@tanstack/react-router';
import { useCallback, useEffect, useRef, useState } from 'react';

interface UnsavedGuard {
  /** True while an in-app navigation is held back waiting for a decision. */
  blocked: boolean;
  /** Lets the held navigation through, discarding the unsaved work. */
  proceed: () => void;
  /** Cancels the held navigation and stays on the page. */
  stay: () => void;
}

/**
 * Warns before unsaved work is lost: the browser's own prompt on reload or tab
 * close, and (unless `blockNavigation` is false) a held in-app navigation the
 * caller confirms through `blocked` / `proceed` / `stay`, for example with a
 * `ConfirmSheet`.
 *
 * `blockNavigation: false` is for editors that live in a modal and already
 * guard their own close; they only need the unload prompt.
 */
export function useUnsavedGuard(dirty: boolean, blockNavigation = true): UnsavedGuard {
  const router = useRouter({ warn: false });
  const [blocked, setBlocked] = useState(false);
  const decision = useRef<((allow: boolean) => void) | null>(null);

  useEffect(() => {
    if (!dirty) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [dirty]);

  useEffect(() => {
    if (!dirty || !blockNavigation || !router) return;
    return router.history.block({
      enableBeforeUnload: false,
      blockerFn: () =>
        new Promise<boolean>((resolve) => {
          decision.current = (allow) => resolve(!allow);
          setBlocked(true);
        }),
    });
  }, [dirty, blockNavigation, router]);

  const settle = useCallback((allow: boolean) => {
    decision.current?.(allow);
    decision.current = null;
    setBlocked(false);
  }, []);

  const proceed = useCallback(() => settle(true), [settle]);
  const stay = useCallback(() => settle(false), [settle]);
  return { blocked, proceed, stay };
}
