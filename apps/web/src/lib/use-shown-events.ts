import { useMemo } from 'react';
import { matchesFilter } from '@/lib/event-filter';
import { useCalendarStore } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

/**
 * Whether an event is on screen: its calendar is shown and it passes the
 * member filter. Every view asks this one question, so a new condition reaches
 * all of them at once. The predicate keeps its identity until either input
 * changes, so it can sit in a memo's dependencies.
 */
export function useIsEventShown(): (event: CalendarEvent) => boolean {
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const eventFilter = useCalendarStore((s) => s.eventFilter);
  return useMemo(() => {
    const shown = new Set(activeCalendarIds);
    return (event: CalendarEvent) =>
      shown.has(event.calendarId) && matchesFilter(event, eventFilter);
  }, [activeCalendarIds, eventFilter]);
}
