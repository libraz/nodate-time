import { useCallback } from 'react';
import { canEditEvent, roleOnCalendar } from '@/lib/permissions';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

/** Whether the signed-in user may drag or resize an event. */
export function useCanMoveEvent(): (evt: CalendarEvent) => boolean {
  const calendars = useCalendarStore((s) => s.calendars);
  const me = useAuthStore((s) => s.user);
  return useCallback(
    (evt: CalendarEvent) => canEditEvent(evt, roleOnCalendar(calendars, evt.calendarId), me?.id),
    [calendars, me],
  );
}
