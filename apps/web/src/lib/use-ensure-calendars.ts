import { useEffect, useRef } from 'react';
import { useCalendarStore } from '@/stores/calendar-store';

/**
 * Loads the calendar list once when a screen that needs it is the first thing
 * mounted (a reload or deep link), since only the main view fetches it on its
 * own. Asks at most once per mount so an account with no calendars, or a failed
 * load, does not loop.
 */
export function useEnsureCalendars(enabled: boolean): void {
  const empty = useCalendarStore((s) => s.calendars.length === 0);
  const loading = useCalendarStore((s) => s.isLoading);
  const fetchCalendars = useCalendarStore((s) => s.fetchCalendars);
  const asked = useRef(false);

  useEffect(() => {
    if (!enabled || !empty || loading || asked.current) return;
    asked.current = true;
    void fetchCalendars();
  }, [enabled, empty, loading, fetchCalendars]);
}
