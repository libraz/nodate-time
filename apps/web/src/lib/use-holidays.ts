import { useEffect, useMemo, useState } from 'react';
import type { HolidayCountry } from '@/lib/holidays';
import { fallbackHolidayCountries, loadHolidayCountries, preloadHolidays } from '@/lib/holidays';

/**
 * Loads the optional holiday-data chunk for `years`, and returns a number that
 * changes once it has arrived.
 *
 * `getHoliday` reads a module cache, so a subtree that draws holidays has no
 * prop or state of its own that would report the load. Re-rendering the caller
 * is enough for a view that draws its own days, and those callers ignore the
 * return; one whose rows are memoised has to pass this revision down, or it
 * keeps drawing the month it first rendered, with no holidays on it.
 */
export function useHolidayLoader(country: string | null, years: number[]): number {
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

/** The countries a holiday picker can offer, seeded so it is never empty. */
export function useHolidayCountries(locale: string): HolidayCountry[] {
  const [countries, setCountries] = useState<HolidayCountry[]>(() =>
    fallbackHolidayCountries(locale),
  );

  useEffect(() => {
    let cancelled = false;
    setCountries(fallbackHolidayCountries(locale));
    loadHolidayCountries(locale).then((list) => {
      if (!cancelled) setCountries(list);
    });
    return () => {
      cancelled = true;
    };
  }, [locale]);

  return countries;
}
