import { useEffect, useMemo, useState } from 'react';
import { preloadHolidays } from '@/lib/holidays';

/**
 * Loads holiday data like `useHolidayLoader`, and returns a number that changes
 * once it has arrived.
 *
 * `getHoliday` reads a module cache, so a subtree that draws holidays has no
 * prop or state of its own that would report the load. A view whose rows are
 * memoised has to pass this revision down, or it keeps drawing the month it
 * first rendered, with no holidays on it.
 */
export function useHolidayRevision(country: string | null, years: number[]): number {
  const yearKey = useMemo(() => [...new Set(years)].sort((a, b) => a - b).join(','), [years]);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    let cancelled = false;
    const requestedYears = yearKey === '' ? [] : yearKey.split(',').map(Number);
    preloadHolidays(country, requestedYears).then(() => {
      if (!cancelled) setRevision((current) => current + 1);
    });
    return () => {
      cancelled = true;
    };
  }, [country, yearKey]);

  return revision;
}
