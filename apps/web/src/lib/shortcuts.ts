import { useEffect } from 'react';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarView } from '@/types/calendar';

/**
 * Desktop calendar shortcuts: the key each action answers to, and the glyph a
 * tooltip shows for it. One table drives both the listener and the hints, so
 * a hint can never name a key the listener does not answer.
 */
export const SHORTCUTS = {
  prev: { key: 'ArrowLeft', glyph: '←' },
  next: { key: 'ArrowRight', glyph: '→' },
  today: { key: 't', glyph: 'T' },
  month: { key: 'm', glyph: 'M' },
  week: { key: 'w', glyph: 'W' },
  list: { key: 'l', glyph: 'L' },
  year: { key: 'y', glyph: 'Y' },
  create: { key: 'n', glyph: 'N' },
  search: { key: '/', glyph: '/' },
} as const;

export type ShortcutAction = keyof typeof SHORTCUTS;

/** Tooltip text and ARIA attribute for a control that has a shortcut. */
export function shortcutProps(action: ShortcutAction, label: string) {
  const { key, glyph } = SHORTCUTS[action];
  return { title: `${label} (${glyph})`, 'aria-keyshortcuts': key };
}

const VIEW_ACTIONS: Partial<Record<ShortcutAction, CalendarView>> = {
  month: 'month',
  week: 'week',
  list: 'list',
  year: 'year',
};

/** Typing into a field, or working inside an open dialog, owns the keyboard. */
function keyboardIsTaken(e: KeyboardEvent): boolean {
  if (e.metaKey || e.ctrlKey || e.altKey || e.isComposing) return true;
  const target = e.target as HTMLElement | null;
  if (target?.closest('input, textarea, select, [contenteditable="true"], [role="listbox"]')) {
    return true;
  }
  return document.querySelector('[aria-modal="true"]') !== null;
}

/** Listens for the calendar shortcuts while mounted. Desktop only: a phone has no keys. */
export function useCalendarShortcuts(enabled: boolean) {
  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (keyboardIsTaken(e)) return;
      const action = (Object.keys(SHORTCUTS) as ShortcutAction[]).find(
        (a) => SHORTCUTS[a].key === (e.key.length === 1 ? e.key.toLowerCase() : e.key),
      );
      if (!action) return;
      const ui = useUiStore.getState();
      const view = VIEW_ACTIONS[action];
      if (view) ui.setCalendarView(view);
      else if (action === 'prev') ui.stepView(-1);
      else if (action === 'next') ui.stepView(1);
      else if (action === 'today') ui.goToToday();
      else if (action === 'create') ui.openEventModal();
      else if (action === 'search' && !ui.showSearch) ui.toggleSearch();
      e.preventDefault();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [enabled]);
}
