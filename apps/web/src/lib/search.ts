import type { CalendarEvent } from '@/types/calendar';

export function filterEventsForSearch(events: CalendarEvent[], query: string): CalendarEvent[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  return events.filter((evt) =>
    [evt.title, evt.location, evt.memo].some((field) => field.toLowerCase().includes(q)),
  );
}
