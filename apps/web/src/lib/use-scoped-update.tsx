import { useCallback, useState } from 'react';
import { useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { isRecurringEvent, moveSeriesRule, restoreInput } from '@/lib/event-move';
import { toast } from '@/lib/toast';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { type EventInput, useCalendarStore } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

interface PendingUpdate {
  event: CalendarEvent;
  payload: EventInput;
}

/** The failures that mean the copy on screen is out of date rather than that the write was bad. */
function staleOrGone(e: unknown): boolean {
  if (typeof e !== 'object' || e === null) return false;
  const { code, status } = e as { code?: unknown; status?: unknown };
  return code === 'EVENT.STALE' || status === 404;
}

/**
 * Wraps event updates from drag/resize. A non-recurring event commits
 * immediately; a recurring one first asks whether to change this occurrence or
 * the whole series. Render the returned `dialog` somewhere in the view.
 *
 * The update replaces every field, so it is only sent when the copy it was
 * built from is still the stored one, and it names that revision.
 */
export function useScopedUpdate() {
  const t = useT();
  const updateEvent = useCalendarStore((s) => s.updateEvent);
  const fetchEvents = useCalendarStore((s) => s.fetchEvents);
  const visibleRange = useCalendarStore((s) => s.visibleRange);
  const [pending, setPending] = useState<PendingUpdate | null>(null);
  const dialogRef = useModalA11y<HTMLDivElement>(pending !== null, () => setPending(null));

  const refetch = useCallback(() => {
    const { start, end } = visibleRange();
    void fetchEvents(start, end);
  }, [fetchEvents, visibleRange]);

  const commit = useCallback(
    async (
      event: CalendarEvent,
      payload: EventInput,
      scope?: 'this' | 'all',
      undoable = true,
    ): Promise<boolean> => {
      try {
        const { data: current, revision } = await api.getWithRevision<CalendarEvent>(
          `/calendars/${event.calendarId}/events/${event.id}`,
        );
        // A copy someone has changed since is refused here; the If-Match below
        // covers a change landing between this read and the write.
        if (current.updatedAt !== event.updatedAt) {
          toast.error(t('apiError.EVENT.STALE'));
          refetch();
          return false;
        }
        await updateEvent(event.calendarId, event.id, payload, scope, revision);
        // A drag is quick to get wrong and slow to put back by hand. Only a
        // single event offers it: a series edit rewrites more than this copy.
        if (undoable && scope === undefined) {
          toast.info(t('event.changed'), {
            label: t('common.undo'),
            run: () => {
              const moved = useCalendarStore.getState().events.find((e) => e.id === event.id);
              if (!moved) return;
              void commit(moved, restoreInput(event), undefined, false).then((ok) => {
                if (ok) toast.success(t('event.changeUndone'));
              });
            },
          });
        }
        return true;
      } catch (e) {
        toast.error(errorMessage(e, t('error.saveFailed')));
        if (staleOrGone(e)) refetch();
        return false;
      }
    },
    [updateEvent, refetch, t],
  );

  /** Commit an update, prompting for scope when the event is recurring. */
  const requestUpdate = useCallback(
    (event: CalendarEvent, payload: EventInput) => {
      // Only an expanded occurrence (composite id) has a scope to choose.
      if (isRecurringEvent(event) && event.id.includes('_')) setPending({ event, payload });
      else void commit(event, payload);
    },
    [commit],
  );

  const choose = useCallback(
    (scope: 'this' | 'all') => {
      if (pending) {
        const { event, payload } = pending;
        void commit(event, scope === 'all' ? moveSeriesRule(event, payload) : payload, scope);
      }
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
          ref={dialogRef}
          role="dialog"
          aria-modal="true"
          aria-label={t('event.scopeEditTitle')}
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
