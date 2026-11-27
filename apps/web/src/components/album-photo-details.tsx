import { useEffect, useMemo, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { CustomSelect } from '@/components/custom-select';
import { useT } from '@/i18n';
import type { AlbumPhoto } from '@/lib/album';
import { fromISOInZone } from '@/lib/date-utils';
import { seriesId } from '@/lib/event-move';
import { eventStartDay } from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

interface AlbumPhotoDetailsProps {
  photo: AlbumPhoto;
  calendarId: string;
  editable: boolean;
  onSaveCaption: (photoId: string, caption: string) => Promise<void>;
  onLinkEvent: (photoId: string, eventId: string) => Promise<void>;
}

/**
 * A photo's caption, the event it belongs to and who added it, on the
 * viewer's dark surface. The phone shows it in a sheet and the desktop in a
 * sidebar; the editing lives here so both behave the same.
 */
export function AlbumPhotoDetails({
  photo,
  calendarId,
  editable,
  onSaveCaption,
  onLinkEvent,
}: AlbumPhotoDetailsProps) {
  const t = useT();
  const timezone = useUiStore((s) => s.timezone);
  const events = useCalendarStore((s) => s.events);
  const [captionDraft, setCaptionDraft] = useState(photo.caption);
  const [savingCaption, setSavingCaption] = useState(false);
  const [linkingEvent, setLinkingEvent] = useState(false);

  // Keyed by photo, so this only follows a caption saved while it is open.
  useEffect(() => {
    setCaptionDraft(photo.caption);
  }, [photo.caption]);

  /**
   * Events the photo could belong to: the ones on the day it was taken.
   *
   * Offering every loaded event would be a list of hundreds ordered by nothing
   * the viewer can see. A photo belongs to what was happening when it was
   * taken, and the capture time is read off the file precisely so this list
   * can be short enough to pick from.
   */
  const linkableEvents = useMemo(() => {
    const day = fromISOInZone(photo.takenAt, timezone).toISODate();
    // A photo links to the series, so an occurrence is offered under its
    // series id, once.
    const offered = new Map<string, { id: string; title: string; startAt: string }>();
    for (const evt of events) {
      if (evt.calendarId !== calendarId) continue;
      if (eventStartDay(evt, timezone).toISODate() !== day) continue;
      const id = seriesId(evt.id);
      if (!offered.has(id)) offered.set(id, { id, title: evt.title, startAt: evt.startAt });
    }
    return [...offered.values()].sort((a, b) => a.startAt.localeCompare(b.startAt));
  }, [photo.takenAt, events, calendarId, timezone]);

  const linkedTitle = linkableEvents.find((evt) => evt.id === photo.eventId)?.title;

  const handleSaveCaption = async () => {
    setSavingCaption(true);
    try {
      await onSaveCaption(photo.id, captionDraft);
    } catch {
      // The failure is reported by the caller; the typed caption stays.
    } finally {
      setSavingCaption(false);
    }
  };

  const handleLinkEvent = async (eventId: string) => {
    setLinkingEvent(true);
    try {
      await onLinkEvent(photo.id, eventId);
    } catch {
      // The failure is reported by the caller.
    } finally {
      setLinkingEvent(false);
    }
  };

  return (
    <>
      {editable ? (
        <div className="flex items-center gap-2">
          <input
            data-1p-ignore
            type="text"
            value={captionDraft}
            onChange={(e) => setCaptionDraft(e.target.value)}
            placeholder={t('album.captionPlaceholder')}
            aria-label={t('album.caption')}
            className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-white/10 px-3 py-2.5 text-body text-white outline-none placeholder:text-white/45 focus:bg-white/15"
          />
          <button
            type="button"
            onClick={handleSaveCaption}
            disabled={savingCaption || captionDraft === photo.caption}
            className="btn-primary btn-sm shrink-0 disabled:opacity-40"
          >
            {savingCaption ? t('profile.saving') : t('album.saveCaption')}
          </button>
        </div>
      ) : (
        photo.caption && <p className="text-body text-white">{photo.caption}</p>
      )}

      <dl className="mt-4 divide-y divide-white/10 rounded-[var(--radius-sm)] bg-white/[0.06] text-footnote">
        <div className="flex items-center gap-3 px-3.5 py-2.5">
          <dt className="w-16 shrink-0 text-white/60">{t('album.event')}</dt>
          <dd className="flex min-w-0 flex-1 items-center gap-2">
            {editable ? (
              <CustomSelect
                value={photo.eventId ?? ''}
                onChange={handleLinkEvent}
                ariaLabel={t('album.event')}
                className="min-w-0 flex-1"
                triggerClassName="rounded-[var(--radius-control)] bg-white/10 px-3 py-1.5 text-footnote text-white hover:bg-white/15"
                options={[
                  { value: '', label: t('album.noEvent') },
                  ...linkableEvents.map((evt) => ({ value: evt.id, label: evt.title })),
                ]}
              />
            ) : (
              <span className="truncate">{linkedTitle ?? t('album.noEvent')}</span>
            )}
            {linkingEvent && <span className="shrink-0 text-white/60">{t('profile.saving')}</span>}
          </dd>
        </div>
        <div className="flex items-center gap-3 px-3.5 py-2.5">
          <dt className="w-16 shrink-0 text-white/60">{t('album.addedBy')}</dt>
          <dd className="flex min-w-0 items-center gap-2">
            <Avatar src={photo.uploaderAvatarUrl} name={photo.uploaderName} size="h-5 w-5" round />
            <span className="truncate">{photo.uploaderName}</span>
          </dd>
        </div>
      </dl>
    </>
  );
}
