import { useState } from 'react';
import { useCalendarStore } from '@/stores/calendar-store';
import type { Calendar } from '@/types/calendar';

export interface PanelCalendar {
  /** The calendars on screen the panel may act on, in sidebar order. */
  options: Calendar[];
  /** The one it acts on, or null when there is none to offer. */
  target: Calendar | null;
  setTargetId: (id: string) => void;
}

/**
 * The calendar a side panel acts on: one of those currently shown, chosen by
 * the reader and named on screen, rather than whichever happens to be first.
 * A choice that stops being offered -- hidden, left -- falls back to the first
 * calendar that still is.
 */
export function usePanelCalendar(eligible?: (calendar: Calendar) => boolean): PanelCalendar {
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const [targetId, setTargetId] = useState('');

  const options = calendars.filter(
    (c) => activeCalendarIds.includes(c.id) && (eligible ? eligible(c) : true),
  );
  const target = options.find((c) => c.id === targetId) ?? options[0] ?? null;
  return { options, target, setTargetId };
}
