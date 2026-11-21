import { getT } from '@/i18n';
import { ApiError, errorMessage } from '@/lib/api';
import { client } from '@/lib/client';
import { isStaleError } from '@/lib/stale';
import { toast } from '@/lib/toast';

/** What the toast needs to put a deletion back. */
export interface RestoreOffer {
  calendarId: string;
  eventId: string;
  title: string;
  /** Called once the event is back, to reload what the grid shows. */
  onRestored: () => void;
}

/** Restores a deleted event or series; an expired grace period is reported as such. */
export async function restoreDeletedEvent(offer: RestoreOffer): Promise<boolean> {
  try {
    await client.POST('/calendars/{calendarId}/events/{eventId}/restore', {
      params: { path: { calendarId: offer.calendarId, eventId: offer.eventId } },
    });
  } catch (e) {
    const t = getT();
    toast.error(
      e instanceof ApiError && e.code === 'EVENT.RESTORE_EXPIRED'
        ? t('restore.expired')
        : errorMessage(e),
    );
    return false;
  }
  offer.onRestored();
  return true;
}

/**
 * Puts one cancelled occurrence back. The master's etag is the one read
 * before the cancellation; a master changed since refuses with a stale error.
 */
export async function restoreCancelledOccurrence(
  offer: Omit<RestoreOffer, 'eventId'> & {
    seriesId: string;
    occurrenceDate: string;
    seriesEtag: string;
  },
): Promise<boolean> {
  try {
    await client.DELETE('/calendars/{calendarId}/events/{eventId}/exceptions/{occurrenceDate}', {
      params: {
        path: {
          calendarId: offer.calendarId,
          eventId: offer.seriesId,
          occurrenceDate: offer.occurrenceDate,
        },
        header: { 'If-Match': offer.seriesEtag },
      },
    });
  } catch (e) {
    const t = getT();
    toast.error(isStaleError(e) ? t('conflict.stale') : errorMessage(e));
    offer.onRestored();
    return false;
  }
  offer.onRestored();
  return true;
}

/** Shows the deletion toast whose action undoes it. */
export function offerRestore(offer: RestoreOffer): void {
  const t = getT();
  toast.info(t('restore.toast', { name: offer.title }), {
    label: t('restore.undo'),
    run: () => void restoreDeletedEvent(offer),
  });
}

/** Shows the toast for a cancelled single occurrence. */
export function offerOccurrenceRestore(
  offer: Parameters<typeof restoreCancelledOccurrence>[0],
): void {
  const t = getT();
  toast.info(t('restore.toast', { name: offer.title }), {
    label: t('restore.undo'),
    run: () => void restoreCancelledOccurrence(offer),
  });
}
