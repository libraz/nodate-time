import { useEffect, useState } from 'react';

/** How long a surface stays mounted after it is closed, so its exit can play. */
export const EXIT_MS = 220;

export interface Presence {
  /** Whether the surface should be in the tree at all. */
  mounted: boolean;
  /** True while it is on its way out: still mounted, no longer open. */
  closing: boolean;
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  );
}

/**
 * Keeps a closed surface mounted for its exit animation. With reduced motion
 * there is no exit to wait for, so it unmounts at once.
 */
export function usePresence(open: boolean, exitMs = EXIT_MS): Presence {
  const [mounted, setMounted] = useState(open);

  useEffect(() => {
    if (open) {
      setMounted(true);
      return;
    }
    if (prefersReducedMotion()) {
      setMounted(false);
      return;
    }
    const id = window.setTimeout(() => setMounted(false), exitMs);
    return () => window.clearTimeout(id);
  }, [open, exitMs]);

  return { mounted: open || mounted, closing: !open && mounted };
}

/**
 * True from the first time `value` is true onwards. Lets a lazily loaded
 * surface stay mounted after it closes, so it can animate out and reopen
 * without loading again.
 */
export function useLatch(value: boolean): boolean {
  const [latched, setLatched] = useState(value);
  useEffect(() => {
    if (value) setLatched(true);
  }, [value]);
  return latched || value;
}
