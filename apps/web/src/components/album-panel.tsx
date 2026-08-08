import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CustomSelect } from '@/components/pickers';
import { useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { fromISOInZone } from '@/lib/date-utils';
import { readCaptureTime } from '@/lib/exif';
import { prepareAlbumThumbnail, prepareImageForAlbum } from '@/lib/image-resize';
import { canEdit, roleOnCalendar } from '@/lib/permissions';
import { uploadViaPresign } from '@/lib/upload';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

interface AlbumPhoto {
  id: string;
  caption: string;
  imageUrl: string;
  /**
   * The grid-sized copy of the photo, absent when it has none. Photos uploaded
   * before thumbnails existed never get one, so this stays optional.
   */
  thumbnailUrl?: string;
  createdAt: string;
  takenAt: string;
  /** Public id of the event this photo belongs to, empty when it belongs to none. */
  eventId?: string;
  uploadedBy: { id: string; name: string; avatarUrl?: string };
}

interface AlbumListResponse {
  items: AlbumPhoto[];
  nextCursor?: string;
}

interface PresignResponse {
  photoId: string;
  uploadUrl: string;
  /** Only issued when the presign request declared a thumbnail. */
  thumbnailUploadUrl?: string;
}

export function AlbumPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const events = useCalendarStore((s) => s.events);
  const timezone = useUiStore((s) => s.timezone);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [photos, setPhotos] = useState<AlbumPhoto[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lightbox, setLightbox] = useState<AlbumPhoto | null>(null);
  const [captionDraft, setCaptionDraft] = useState('');
  const [savingCaption, setSavingCaption] = useState(false);
  const [linkingEvent, setLinkingEvent] = useState(false);
  // The cursors of the pages currently on screen, in order. An image URL is
  // signed for a limited time, and thumbnails load as they are scrolled to, so
  // one can expire while the panel is still open; re-listing these same pages
  // is what replaces the dead URLs without throwing away what was paged in.
  const loadedCursorsRef = useRef<string[]>([]);
  // Photos already retried once. A URL that expired comes back working; an
  // object that is genuinely missing does not, and without this the second
  // failure would ask for another listing, forever.
  const retriedRef = useRef<Set<string>>(new Set());
  const refreshingRef = useRef(false);

  const activeCalendarId = activeCalendarIds[0] ?? calendars[0]?.id ?? '';
  const myRole = roleOnCalendar(calendars, activeCalendarId);
  const editable = canEdit(myRole);

  // The innermost open surface owns the keyboard. The lightbox answers first
  // while it is up, and the event picker portals its list out of the lightbox
  // and closes itself on Escape, so the lightbox stands down while one is open.
  //
  // Asking for it by class name couples this to markup pickers.tsx owns, which
  // is deliberate rather than overlooked: a portalled list is not in this
  // subtree, so there is nothing else here to ask. It stops being necessary the
  // day CustomSelect can report its own open state.
  const dismissLightbox = () => {
    if (document.querySelector('.dropdown-panel')) return;
    setLightbox(null);
  };
  const dismissPanel = () => {
    if (lightbox) return;
    toggleRightPanel('album');
  };
  const panelRef = useModalA11y<HTMLDivElement>(rightPanel === 'album', dismissPanel);
  const lightboxRef = useModalA11y<HTMLDivElement>(lightbox !== null, dismissLightbox);
  // The lightbox carries its own trap because its controls sit outside the
  // panel's container, and without one Tab walks straight out of the page. The
  // two traps do not fight -- each only intercepts Tab at the edge of its own
  // container, and the containers are disjoint -- and a picker's portalled list
  // is deliberately outside both, so opening one hands it the keyboard.

  const reload = useCallback(async () => {
    if (!activeCalendarId) {
      setPhotos([]);
      setNextCursor('');
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const data = await api.get<AlbumListResponse>(`/calendars/${activeCalendarId}/albums`);
      setPhotos(data.items ?? []);
      setNextCursor(data.nextCursor ?? '');
      loadedCursorsRef.current = [''];
      retriedRef.current.clear();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, [activeCalendarId]);

  const loadMore = useCallback(async () => {
    if (!activeCalendarId || !nextCursor || loadingMore) return;
    setLoadingMore(true);
    setError(null);
    try {
      const data = await api.get<AlbumListResponse>(
        `/calendars/${activeCalendarId}/albums?cursor=${encodeURIComponent(nextCursor)}`,
      );
      setPhotos((cur) => [...cur, ...(data.items ?? [])]);
      setNextCursor(data.nextCursor ?? '');
      loadedCursorsRef.current = [...loadedCursorsRef.current, nextCursor];
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoadingMore(false);
    }
  }, [activeCalendarId, nextCursor, loadingMore]);

  /**
   * Re-lists every page on screen so each photo gets a freshly signed URL.
   * Runs on the first image that fails to load, which is how an expired
   * signature shows up: the browser reports a broken image, not an error the
   * fetch layer ever sees.
   */
  const refreshImageUrls = useCallback(async () => {
    if (!activeCalendarId || refreshingRef.current) return;
    refreshingRef.current = true;
    try {
      const refreshed: AlbumPhoto[] = [];
      for (const cursor of loadedCursorsRef.current) {
        const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
        const data = await api.get<AlbumListResponse>(
          `/calendars/${activeCalendarId}/albums${query}`,
        );
        refreshed.push(...(data.items ?? []));
      }
      setPhotos(refreshed);
      setLightbox((cur) => (cur ? (refreshed.find((p) => p.id === cur.id) ?? cur) : cur));
    } catch {
      // Leave what is on screen alone: a failed refresh is no reason to empty
      // an album the reader is looking at.
    } finally {
      refreshingRef.current = false;
    }
  }, [activeCalendarId]);

  const handleImageError = useCallback(
    (photoId: string) => {
      if (retriedRef.current.has(photoId)) return;
      retriedRef.current.add(photoId);
      refreshImageUrls();
    },
    [refreshImageUrls],
  );

  useEffect(() => {
    if (rightPanel === 'album') {
      reload();
    }
  }, [rightPanel, reload]);

  const handleUpload = useCallback(
    async (file: File) => {
      if (!activeCalendarId) return;
      setUploading(true);
      setError(null);
      try {
        // Read the capture time before preparing the file: re-encoding drops
        // the EXIF block, and this is the value the album orders by.
        const takenAt = await readCaptureTime(file);
        const resized = await prepareImageForAlbum(file);
        // The grid draws the photo itself when there is no thumbnail, so
        // failing to make one is not a reason to fail the upload.
        const thumbnail = await prepareAlbumThumbnail(file, resized).catch(() => null);
        const presign = await uploadViaPresign<PresignResponse>({
          kind: 'album',
          presignPath: `/calendars/${activeCalendarId}/albums/presign`,
          presignBody: {
            contentType: resized.contentType,
            byteSize: resized.bytes.byteLength,
            width: resized.width,
            height: resized.height,
            // Omitted rather than sent as now: the server treats an absent
            // capture time as the upload time, and saying "taken now" for a
            // photo with no metadata would be a claim rather than a default.
            ...(takenAt ? { takenAt: takenAt.toISOString() } : {}),
            // Declaring nothing is how the server is told this photo has no
            // thumbnail, which is a normal outcome rather than an error.
            ...(thumbnail
              ? {
                  thumbnailContentType: thumbnail.contentType,
                  thumbnailByteSize: thumbnail.bytes.byteLength,
                }
              : {}),
          },
          contentType: resized.contentType,
          body: resized.bytes,
          byteSize: resized.bytes.byteLength,
        });
        if (thumbnail && presign.thumbnailUploadUrl) {
          // Neither a rejection nor a non-ok response is acted on: the photo
          // is already stored, confirm does not need the thumbnail, and the
          // grid falls back to the photo. Losing it costs bandwidth, not the
          // picture, so it must not surface as a failed upload.
          await fetch(presign.thumbnailUploadUrl, {
            method: 'PUT',
            headers: { 'Content-Type': thumbnail.contentType },
            body: thumbnail.bytes,
          }).catch(() => undefined);
        }
        // The row is created disabled; confirm enables it once the object is stored.
        await api.post(`/calendars/${activeCalendarId}/albums/${presign.photoId}/confirm`);
        await reload();
      } catch (e) {
        setError(errorMessage(e));
      } finally {
        setUploading(false);
      }
    },
    [activeCalendarId, reload],
  );

  const handleDelete = useCallback(
    async (photoId: string) => {
      if (!activeCalendarId) return;
      try {
        await api.delete(`/calendars/${activeCalendarId}/albums/${photoId}`);
        setPhotos((cur) => cur.filter((p) => p.id !== photoId));
        setLightbox(null);
      } catch (e) {
        setError(errorMessage(e));
      }
    },
    [activeCalendarId],
  );

  const openLightbox = useCallback((photo: AlbumPhoto) => {
    setLightbox(photo);
    setCaptionDraft(photo.caption);
  }, []);

  const handleSaveCaption = useCallback(async () => {
    if (!activeCalendarId || !lightbox) return;
    setSavingCaption(true);
    try {
      const updated = await api.put<AlbumPhoto>(
        `/calendars/${activeCalendarId}/albums/${lightbox.id}`,
        { caption: captionDraft },
      );
      setPhotos((cur) => cur.map((p) => (p.id === lightbox.id ? { ...p, ...updated } : p)));
      setLightbox((cur) => (cur ? { ...cur, ...updated } : cur));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSavingCaption(false);
    }
  }, [activeCalendarId, lightbox, captionDraft]);

  /**
   * Events the open photo could belong to: the ones on the day it was taken.
   *
   * Offering every loaded event would be a list of hundreds ordered by nothing
   * the viewer can see. A photo belongs to what was happening when it was
   * taken, and the capture time is read off the file precisely so this list
   * can be short enough to pick from.
   */
  const linkableEvents = useMemo(() => {
    if (!lightbox) return [];
    const day = fromISOInZone(lightbox.takenAt, timezone).toISODate();
    return events
      .filter(
        (evt) =>
          evt.calendarId === activeCalendarId &&
          fromISOInZone(evt.startAt, timezone).toISODate() === day,
      )
      .sort((a, b) => a.startAt.localeCompare(b.startAt));
  }, [lightbox, events, activeCalendarId, timezone]);

  const handleLinkEvent = useCallback(
    async (eventId: string) => {
      if (!activeCalendarId || !lightbox) return;
      setLinkingEvent(true);
      try {
        // An empty string is how the API is told to clear the link, so a photo
        // can be detached from an event without being deleted and re-uploaded.
        const updated = await api.put<AlbumPhoto>(
          `/calendars/${activeCalendarId}/albums/${lightbox.id}`,
          { caption: lightbox.caption, eventId },
        );
        setPhotos((cur) => cur.map((p) => (p.id === lightbox.id ? { ...p, ...updated } : p)));
        setLightbox((cur) => (cur ? { ...cur, ...updated } : cur));
      } catch (e) {
        setError(errorMessage(e));
      } finally {
        setLinkingEvent(false);
      }
    },
    [activeCalendarId, lightbox],
  );

  const handleDownload = useCallback(
    async (photo: AlbumPhoto) => {
      if (!activeCalendarId) return;
      try {
        const { downloadUrl } = await api.get<{ downloadUrl: string }>(
          `/calendars/${activeCalendarId}/albums/${photo.id}/download`,
        );
        window.open(downloadUrl, '_blank', 'noopener');
      } catch (e) {
        setError(errorMessage(e));
      }
    },
    [activeCalendarId],
  );

  if (rightPanel !== 'album') return null;

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={() => toggleRightPanel('album')}
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={t('panel.album')}
        className="glass-surface-heavy side-panel fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]"
      >
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="text-subhead font-semibold">{t('panel.album')}</h2>
          <div className="flex items-center gap-2">
            {editable && (
              <>
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={uploading || !activeCalendarId}
                  className="btn-primary px-3 py-1.5 text-footnote disabled:opacity-50"
                >
                  {uploading ? t('profile.saving') : '+'}
                </button>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/*"
                  className="hidden"
                  onChange={(e) => {
                    const f = e.target.files?.[0];
                    if (f) handleUpload(f);
                    if (e.target) e.target.value = '';
                  }}
                />
              </>
            )}
            <button
              type="button"
              onClick={() => toggleRightPanel('album')}
              className="flex h-8 w-8 items-center justify-center text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
              style={{ borderRadius: 'var(--radius-sm)' }}
              aria-label={t('common.close')}
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        {error && <div className="px-5 py-2 text-footnote text-[var(--color-danger)]">{error}</div>}

        <div className="flex-1 overflow-y-auto p-4">
          {!activeCalendarId ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">—</p>
          ) : loading && photos.length === 0 ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">…</p>
          ) : photos.length === 0 ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">
              {t('panel.noPhotos')}
            </p>
          ) : (
            <div className="space-y-4">
              <div className="grid grid-cols-3 gap-1">
                {photos.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => openLightbox(p)}
                    className="relative aspect-square overflow-hidden rounded-md bg-[var(--color-surface-secondary)]"
                  >
                    <img
                      // A tile is about 134px, so the stored photo here is
                      // megabytes per screenful. Photos uploaded before
                      // thumbnails existed have none and never will, which
                      // makes the fallback permanent rather than temporary.
                      src={p.thumbnailUrl ?? p.imageUrl}
                      alt={p.caption}
                      loading="lazy"
                      onError={() => handleImageError(p.id)}
                      className="h-full w-full object-cover"
                    />
                  </button>
                ))}
              </div>
              {nextCursor && (
                <button
                  type="button"
                  onClick={loadMore}
                  disabled={loadingMore}
                  className="btn-secondary w-full text-body disabled:opacity-50"
                >
                  {loadingMore ? t('common.loading') : t('album.loadMore')}
                </button>
              )}
            </div>
          )}
        </div>
      </div>

      {lightbox && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6">
          <button
            type="button"
            aria-label={t('common.close')}
            className="absolute inset-0 cursor-default"
            onClick={() => setLightbox(null)}
          />
          <div
            ref={lightboxRef}
            role="dialog"
            aria-modal="true"
            aria-label={t('album.photo')}
            className="relative flex max-h-full max-w-full flex-col gap-3"
          >
            <img
              src={lightbox.imageUrl}
              alt={lightbox.caption}
              onError={() => handleImageError(lightbox.id)}
              className="max-h-[72vh] max-w-[90vw] rounded-lg object-contain"
            />
            <div className="absolute right-2 top-2 flex gap-2">
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  handleDownload(lightbox);
                }}
                className="rounded-md bg-black/60 px-3 py-1 text-footnote text-white hover:bg-[var(--color-accent)]"
              >
                {t('album.download')}
              </button>
              {editable && (
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDelete(lightbox.id);
                  }}
                  className="rounded-md bg-black/60 px-3 py-1 text-footnote text-white hover:bg-[var(--color-danger)]"
                >
                  {t('common.delete')}
                </button>
              )}
            </div>
            {/* biome-ignore lint/a11y/noStaticElementInteractions: wrapper only stops event propagation so editing the caption does not trigger the parent photo click */}
            <div
              className="flex items-center gap-2"
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => e.stopPropagation()}
              role="presentation"
            >
              {editable ? (
                <>
                  <input
                    type="text"
                    value={captionDraft}
                    onChange={(e) => setCaptionDraft(e.target.value)}
                    placeholder={t('album.captionPlaceholder')}
                    className="flex-1 rounded-md bg-black/50 px-3 py-2 text-body text-white outline-none placeholder:text-white/50"
                  />
                  <button
                    type="button"
                    onClick={handleSaveCaption}
                    disabled={savingCaption || captionDraft === lightbox.caption}
                    className="btn-primary shrink-0 px-3 py-2 text-footnote disabled:opacity-50"
                  >
                    {savingCaption ? t('profile.saving') : t('album.saveCaption')}
                  </button>
                </>
              ) : (
                lightbox.caption && (
                  <p className="flex-1 text-body text-white">{lightbox.caption}</p>
                )
              )}
            </div>
            {/* biome-ignore lint/a11y/noStaticElementInteractions: wrapper only stops event propagation so using the picker does not trigger the parent photo click */}
            <div
              className="mt-2 flex items-center gap-2"
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => e.stopPropagation()}
              role="presentation"
            >
              <span className="shrink-0 text-footnote text-white/70">{t('album.event')}</span>
              {editable ? (
                <CustomSelect
                  value={lightbox.eventId ?? ''}
                  onChange={handleLinkEvent}
                  triggerClassName="flex-1 bg-black/50 text-white"
                  options={[
                    { value: '', label: t('album.noEvent') },
                    ...linkableEvents.map((evt) => ({ value: evt.id, label: evt.title })),
                  ]}
                />
              ) : (
                <span className="text-footnote text-white">
                  {linkableEvents.find((evt) => evt.id === lightbox.eventId)?.title ??
                    t('album.noEvent')}
                </span>
              )}
              {linkingEvent && (
                <span className="shrink-0 text-footnote text-white/70">{t('profile.saving')}</span>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
