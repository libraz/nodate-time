import { useEffect, useState } from 'react';

/** Tailwind's `sm` breakpoint, the one boundary between the phone and desktop layouts. */
const DESKTOP_QUERY = '(min-width: 640px)';

/** Whether the viewport is at or past the `sm` breakpoint, tracking resizes. */
export function useIsDesktop(): boolean {
  const [isDesktop, setIsDesktop] = useState(
    () => typeof window !== 'undefined' && window.matchMedia?.(DESKTOP_QUERY).matches === true,
  );
  useEffect(() => {
    const mql = window.matchMedia?.(DESKTOP_QUERY);
    if (!mql) return;
    setIsDesktop(mql.matches);
    const onChange = (e: MediaQueryListEvent) => setIsDesktop(e.matches);
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, []);
  return isDesktop;
}
