import { useCallback, useState } from 'react';
import { useT } from '@/i18n';
import { errorMessage, isStaleError } from '@/lib/client';
import {
  type EventScope,
  isRecurringEvent,
  moveSeriesRule,
  restoreInput,
  scopeEtag,
} from '@/lib/event-move';
import { toast } from '@/lib/toast';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { type EventPatch, useCalendarStore } from '@/stores/calendar-store';
import type { CalendarEvent } from '@/types/calendar';

interface PendingUpdate {
  event: CalendarEvent;
  payload: EventPatch;
  /** Set once `following` is picked and the user is asked to confirm it. */
  confirmingFollowing: boolean;
}

/** The failures that mean the copy on screen is out of date rather than that the write was bad. */
function staleOrGone(e: unknown): boolean {
  if (isStaleError(e)) return true;
  return typeof e === 'object' && e !== null && (e as { status?: unknown }).status === 404;
}

/**
 * Wraps event updates from drag/resize. A non-recurring event commits
 * immediately; a recurring one first asks whether to change this occurrence or
 * the whole series. Render the returned `dialog` somewhere in the view.
 *
 * The write names the `etag` the listing carried, so a copy someone else has
 * changed since is refused (and the view refetched) instead of overwritten.
 * Only the changed fields are sent.
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
      payload: EventPatch,
      scope?: EventScope,
      undoable = true,
    ): Promise<boolean> => {
      try {
        await updateEvent(event.calendarId, event.id, payload, scope, scopeEtag(event, scope));
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
        toast.error(isStaleError(e) ? t('conflict.stale') : errorMessage(e, t('error.saveFailed')));
        if (staleOrGone(e)) refetch();
        return false;
      }
    },
    [updateEvent, refetch, t],
  );

  /** Commit an update, prompting for scope when the event is recurring. */
  const requestUpdate = useCallback(
    (event: CalendarEvent, payload: EventPatch) => {
      // Only an expanded occurrence (composite id) has a scope to choose.
      if (isRecurringEvent(event) && event.id.includes('_')) {
        setPending({ event, payload, confirmingFollowing: false });
      } else void commit(event, payload);
    },
    [commit],
  );

  const choose = useCallback(
    (scope: EventScope) => {
      if (!pending) return;
      // Moving this occurrence and the ones after it can overwrite edits made to
      // those later occurrences, so it is confirmed before anything is sent.
      if (scope === 'following' && !pending.confirmingFollowing) {
        setPending({ ...pending, confirmingFollowing: true });
        return;
      }
      const { event, payload } = pending;
      // `following` sends no rule: the server derives it from the moved start.
      void commit(event, scope === 'all' ? moveSeriesRule(event, payload) : payload, scope);
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
          {pending.confirmingFollowing ? (
            <>
              <p className="text-default text-[var(--color-text-secondary)]">
                {t('scope.followingDropsChanges')}
              </p>
              <button
                type="button"
                onClick={() => choose('following')}
                className="btn-secondary text-default font-medium"
              >
                {t('scope.following')}
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={() => choose('this')}
                className="btn-secondary text-default font-medium"
              >
                {t('event.scopeThis')}
              </button>
              <button
                type="button"
                onClick={() => choose('following')}
                className="btn-secondary text-default font-medium"
              >
                {t('scope.following')}
              </button>
              <button
                type="button"
                onClick={() => choose('all')}
                className="btn-secondary text-default font-medium"
              >
                {t('event.scopeAll')}
              </button>
            </>
          )}
          <button
            type="button"
            onClick={() => setPending(null)}
            className="py-2 text-default text-[var(--color-text-secondary)]"
          >
            {t('common.cancel')}
          </button>
        </div>
      </div>
    </>
  ) : null;

  return { requestUpdate, dialog };
}
