import { getT } from '@/i18n';
import { canEdit } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { Calendar } from '@/types/calendar';

/** The calendars a new event can be filed on: shown in the sidebar and writable by the caller. */
export function writableVisibleCalendars(
  calendars: readonly Calendar[],
  activeCalendarIds: readonly string[],
): Calendar[] {
  return calendars.filter((c) => activeCalendarIds.includes(c.id) && canEdit(c.role));
}

/**
 * Opens the create-event form, or explains why it cannot: with no visible
 * writable calendar the form would have nowhere to save to.
 */
export function startCreateEvent(): void {
  const { calendars, activeCalendarIds } = useCalendarStore.getState();
  if (writableVisibleCalendars(calendars, activeCalendarIds).length === 0) {
    toast.error(getT()('event.noWritableCalendar'));
    return;
  }
  useUiStore.getState().openEventModal();
}
