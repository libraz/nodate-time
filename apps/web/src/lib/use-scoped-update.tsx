import { useCallback, useState } from 'react';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/api';
import { isRecurringEvent } from '@/lib/event-move';
import { toast } from '@/lib/toast';
import { type EventInput, useCalendarStore } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

interface PendingUpdate {
  event: CalendarEvent;
  payload: EventInput;
}

/**
 * Wraps event updates from drag/resize. A non-recurring event commits
 * immediately; a recurring one first asks whether to change this occurrence or
 * the whole series. Render the returned `dialog` somewhere in the view.
 */
export function useScopedUpdate() {
  const t = useT();
  const updateEvent = useCalendarStore((s) => s.updateEvent);
  const [pending, setPending] = useState<PendingUpdate | null>(null);

  const commit = useCallback(
    (event: CalendarEvent, payload: EventInput, scope?: 'this' | 'all') => {
      updateEvent(event.calendarId, event.id, payload, scope).catch((e) =>
        toast.error(errorMessage(e, t('error.saveFailed'))),
      );
    },
    [updateEvent, t],
  );

  /** Commit an update, prompting for scope when the event is recurring. */
  const requestUpdate = useCallback(
    (event: CalendarEvent, payload: EventInput) => {
      if (isRecurringEvent(event)) setPending({ event, payload });
      else commit(event, payload);
    },
    [commit],
  );

  const choose = useCallback(
    (scope: 'this' | 'all') => {
      if (pending) commit(pending.event, pending.payload, scope);
      setPending(null);
    },
    [pending, commit],
  );

  const dialog = pending ? (
    <>
      <button
        type="button"
        aria-label={t('common.cancel')}
        className="fixed inset-0 z-[60] bg-[var(--color-overlay)]"
        onClick={() => setPending(null)}
      />
      <div className="pointer-events-none fixed inset-0 z-[60] flex items-center justify-center p-4">
        <div
          className="glass-surface-heavy pointer-events-auto flex w-full max-w-[360px] flex-col gap-3 p-6 ring-1 ring-[var(--color-border)]"
          style={{ borderRadius: 'var(--radius-lg)' }}
        >
          <p className="text-default font-semibold">{t('event.scopeEditTitle')}</p>
          <button
            type="button"
            onClick={() => choose('this')}
            className="btn-secondary py-3 text-default font-medium"
          >
            {t('event.scopeThis')}
          </button>
          <button
            type="button"
            onClick={() => choose('all')}
            className="btn-secondary py-3 text-default font-medium"
          >
            {t('event.scopeAll')}
          </button>
          <button
            type="button"
            onClick={() => setPending(null)}
            className="py-2 text-sm text-[var(--color-text-secondary)]"
          >
            {t('common.cancel')}
          </button>
        </div>
      </div>
    </>
  ) : null;

  return { requestUpdate, dialog };
}
