import { useEffect, useMemo, useState } from 'react';
import { preloadHolidays } from '@/lib/holidays';

/** Triggers a re-render once the optional holiday-data chunk is ready. */
export function useHolidayLoader(country: string | null, years: number[]): void {
  const yearKey = useMemo(() => [...new Set(years)].sort((a, b) => a - b).join(','), [years]);
  const [, setRevision] = useState(0);

  useEffect(() => {
    let cancelled = false;
    const requestedYears = yearKey === '' ? [] : yearKey.split(',').map(Number);
    preloadHolidays(country, requestedYears).then(() => {
      if (!cancelled) setRevision((revision) => revision + 1);
    });
    return () => {
      cancelled = true;
    };
  }, [country, yearKey]);
}
