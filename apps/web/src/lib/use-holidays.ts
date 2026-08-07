import { useEffect, useMemo, useState } from 'react';
import type { HolidayCountry } from '@/lib/holidays';
import { fallbackHolidayCountries, loadHolidayCountries, preloadHolidays } from '@/lib/holidays';

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
