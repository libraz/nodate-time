import type { CalendarEvent } from '@/types/calendar';

/**
 * Whether an event is shared by several people. A shared event belongs to no
 * single person, so showing one participant's face on it would mislead.
 */
export function isGroupEvent(evt: Pick<CalendarEvent, 'participants'>): boolean {
  return (evt.participants?.length ?? 0) >= 2;
}
