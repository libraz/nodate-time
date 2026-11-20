import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AlbumDeleteSheet, AlbumViewer, TrashIcon } from '@/components/album-viewer';
import { EmptyState, GUIDE_ICONS, TipCard } from '@/components/guide';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { SidePanel } from '@/components/side-panel';
import { useT } from '@/i18n';
import { type AlbumPhoto, formatAlbumDay, groupByDay } from '@/lib/album';
import { api, errorMessage } from '@/lib/api';
import { readCaptureTime } from '@/lib/exif';
import { prepareAlbumThumbnail, prepareImageForAlbum } from '@/lib/image-resize';
import { canEdit, roleOnCalendar } from '@/lib/permissions';
import { uploadViaPresign } from '@/lib/upload';
import { useCalendarPages } from '@/lib/use-calendar-pages';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { usePresence } from '@/lib/use-presence';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

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

/** How far below the visible grid the next page starts loading. */
const LOAD_AHEAD_MARGIN = '600px';

export function AlbumPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const presence = usePresence(rightPanel === 'album');
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const calendars = useCalendarStore((s) => s.calendars);
  const timezone = useUiStore((s) => s.timezone);
  const locale = useUiStore((s) => s.locale);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [upload, setUpload] = useState<{ done: number; total: number } | null>(null);
  const [viewerId, setViewerId] = useState<string | null>(null);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // Photos already retried once. A URL that expired comes back working; an
  // object that is genuinely missing does not, and without this the second
  // failure would ask for another listing, forever.
  const retriedRef = useRef<Set<string>>(new Set());
  const refreshingRef = useRef(false);

  const { options, target, setTargetId } = usePanelCalendar();
  const activeCalendarId = target?.id ?? '';
  const {
    items: photos,
    nextCursor,
    // The cursors of the pages on screen. An image URL is signed for a limited
    // time and thumbnails load as they are scrolled to, so one can expire while
    // the panel is open; re-listing these pages replaces the dead URLs without
    // throwing away what was paged in.
    cursors: loadedCursors,
    loading,
    loadingMore,
    error,
    setError,
    reload,
    loadMore,
    updateItems,
  } = useCalendarPages<AlbumPhoto>(activeCalendarId, 'albums', presence.mounted);
  const editable = canEdit(roleOnCalendar(calendars, activeCalendarId));
  const days = useMemo(() => groupByDay(photos, timezone), [photos, timezone]);

  // The panel does not answer Escape while a surface above it is open.
  const dismissPanel = () => {
    if (viewerId) return;
    if (confirmingDelete) {
      setConfirmingDelete(false);
      return;
    }
    if (selecting) {
      endSelecting();
      return;
    }
    toggleRightPanel('album');
  };

  useEffect(() => {
    if (!activeCalendarId) return;
    retriedRef.current.clear();
    setSelecting(false);
    setSelected(new Set());
  }, [activeCalendarId]);

  // Scrolling to the end of the grid reads the next page; the button under it
  // stays for keyboard readers and as the place a slow page shows progress.
  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel || !nextCursor) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) loadMore();
      },
      { rootMargin: LOAD_AHEAD_MARGIN },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [nextCursor, loadMore]);

  /**
   * Re-lists every page on screen so each photo gets a freshly signed URL.
   * Runs on the first image that fails to load, which is how an expired
   * signature shows up: the browser reports a broken image, not an error the
   * fetch layer ever sees.
   */
  const refreshImageUrls = useCallback(async () => {
    if (!activeCalendarId || refreshingRef.current) return;
    const calendarId = activeCalendarId;
    refreshingRef.current = true;
    try {
      const refreshed: AlbumPhoto[] = [];
      for (const cursor of loadedCursors) {
        const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
        const data = await api.get<AlbumListResponse>(
          `/calendars/${activeCalendarId}/albums${query}`,
        );
        refreshed.push(...(data.items ?? []));
      }
      updateItems(calendarId, () => refreshed);
    } catch {
      // Leave what is on screen alone: a failed refresh is no reason to empty
      // an album the reader is looking at.
    } finally {
      refreshingRef.current = false;
    }
  }, [activeCalendarId, loadedCursors, updateItems]);

  const handleImageError = useCallback(
    (photoId: string) => {
      if (retriedRef.current.has(photoId)) return;
      retriedRef.current.add(photoId);
      refreshImageUrls();
    },
    [refreshImageUrls],
  );

  const uploadOne = useCallback(
    async (file: File) => {
      // Read the capture time before preparing the file: re-encoding drops
      // the EXIF block, and this is the value the album orders by.
      const takenAt = await readCaptureTime(file, timezone);
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
    },
    [activeCalendarId, timezone],
  );

  /** Uploads one file after another, so a failed one does not take the rest with it. */
  const handleFiles = useCallback(
    async (files: File[]) => {
      if (!activeCalendarId || files.length === 0) return;
      setError(null);
      setUpload({ done: 0, total: files.length });
      let failure: unknown = null;
      for (const [i, file] of files.entries()) {
        try {
          await uploadOne(file);
        } catch (e) {
          failure = e;
        }
        setUpload({ done: i + 1, total: files.length });
      }
      retriedRef.current.clear();
      await reload();
      if (failure) setError(errorMessage(failure));
      setUpload(null);
    },
    [activeCalendarId, uploadOne, reload, setError],
  );

  /** Deletes photos one by one; returns the ids that are gone. */
  const deletePhotos = useCallback(
    async (ids: string[]) => {
      if (!activeCalendarId) return new Set<string>();
      const removed = new Set<string>();
      let failure: unknown = null;
      for (const id of ids) {
        try {
          await api.delete(`/calendars/${activeCalendarId}/albums/${id}`);
          removed.add(id);
        } catch (e) {
          failure = e;
        }
      }
      if (removed.size > 0) {
        updateItems(activeCalendarId, (cur) => cur.filter((p) => !removed.has(p.id)));
      }
      if (failure) setError(errorMessage(failure));
      return removed;
    },
    [activeCalendarId, updateItems, setError],
  );

  const handleViewerDelete = useCallback(
    async (photoId: string) => (await deletePhotos([photoId])).has(photoId),
    [deletePhotos],
  );

  const handleBulkDelete = async () => {
    setDeleting(true);
    const removed = await deletePhotos([...selected]);
    setDeleting(false);
    setConfirmingDelete(false);
    setSelected((cur) => new Set([...cur].filter((id) => !removed.has(id))));
    if (removed.size === selected.size) setSelecting(false);
  };

  const savePhoto = useCallback(
    async (photoId: string, body: { caption: string; eventId?: string }) => {
      if (!activeCalendarId) return;
      try {
        const updated = await api.put<AlbumPhoto>(
          `/calendars/${activeCalendarId}/albums/${photoId}`,
          body,
        );
        updateItems(activeCalendarId, (cur) =>
          cur.map((p) => (p.id === photoId ? { ...p, ...updated } : p)),
        );
      } catch (e) {
        setError(errorMessage(e));
      }
    },
    [activeCalendarId, updateItems, setError],
  );

  const handleSaveCaption = useCallback(
    (photoId: string, caption: string) => savePhoto(photoId, { caption }),
    [savePhoto],
  );

  const handleLinkEvent = useCallback(
    (photoId: string, eventId: string) => {
      const caption = photos.find((p) => p.id === photoId)?.caption ?? '';
      // An empty string is how the API is told to clear the link, so a photo
      // can be detached from an event without being deleted and re-uploaded.
      return savePhoto(photoId, { caption, eventId });
    },
    [photos, savePhoto],
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
    [activeCalendarId, setError],
  );

  function endSelecting() {
    setSelecting(false);
    setSelected(new Set());
  }

  const handleTile = (photo: AlbumPhoto) => {
    if (!selecting) {
      setViewerId(photo.id);
      return;
    }
    setSelected((cur) => {
      const nextSet = new Set(cur);
      if (nextSet.has(photo.id)) nextSet.delete(photo.id);
      else nextSet.add(photo.id);
      return nextSet;
    });
  };

  if (!presence.mounted) return null;

  const uploading = upload !== null;

  return (
    <>
      <SidePanel
        presence={presence}
        onClose={dismissPanel}
        title={t('panel.album')}
        helpTopic="memos"
        actions={
          editable && (
            <>
              {photos.length > 0 && (
                <button
                  type="button"
                  onClick={() => (selecting ? endSelecting() : setSelecting(true))}
                  aria-pressed={selecting}
                  className="rounded-full px-3 py-1 text-footnote font-semibold text-[var(--color-accent-ink)] transition-colors hover:bg-[var(--color-accent-bg)]"
                >
                  {selecting ? t('common.cancel') : t('album.select')}
                </button>
              )}
              {!selecting && (
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={uploading || !activeCalendarId}
                  aria-label={uploading ? t('event.uploading') : t('album.add')}
                  aria-busy={uploading}
                  title={t('album.add')}
                  className="icon-btn icon-btn-accent h-8 w-8"
                >
                  {uploading ? (
                    <span className="spinner h-4 w-4" aria-hidden="true" />
                  ) : (
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2.25"
                      strokeLinecap="round"
                      aria-hidden="true"
                    >
                      <path d="M12 5v14M5 12h14" />
                    </svg>
                  )}
                </button>
              )}
              <input
                ref={fileInputRef}
                type="file"
                accept="image/*"
                multiple
                className="hidden"
                onChange={(e) => {
                  const files = Array.from(e.target.files ?? []);
                  e.target.value = '';
                  handleFiles(files);
                }}
              />
            </>
          )
        }
      >
        {target && !selecting && (
          <div className="border-b border-[var(--color-border)] px-5 py-3">
            <PanelCalendarPicker
              label={t('panel.targetCalendar')}
              options={options}
              target={target}
              onChange={setTargetId}
            />
          </div>
        )}

        {upload && (
          <div className="border-b border-[var(--color-border)] px-5 py-2.5" aria-live="polite">
            <div className="flex items-center justify-between text-caption text-[var(--color-text-secondary)]">
              <span>{t('album.uploadProgress', { done: upload.done, total: upload.total })}</span>
            </div>
            <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-[var(--color-surface-inset)]">
              <div
                className="h-full rounded-full bg-[var(--color-accent)] transition-[width] duration-300"
                style={{ width: `${(upload.done / upload.total) * 100}%` }}
              />
            </div>
          </div>
        )}

        {error && <div className="px-5 py-2 text-footnote text-[var(--color-danger)]">{error}</div>}

        <div className="flex-1 overflow-y-auto">
          {activeCalendarId && !selecting && (
            <TipCard id="album" topic="memos" className="mx-4 mt-4 mb-2">
              {t('tip.album')}
            </TipCard>
          )}
          {!activeCalendarId ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">—</p>
          ) : loading && photos.length === 0 ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">…</p>
          ) : photos.length === 0 ? (
            <div className="p-4">
              <EmptyState
                icon={GUIDE_ICONS.photo}
                title={t('empty.photosTitle')}
                description={t('empty.photosDesc')}
              />
            </div>
          ) : (
            <div className="pb-4">
              {days.map((group) => (
                <section key={group.day} aria-label={formatAlbumDay(group.date, timezone, locale)}>
                  <h3 className="album-day-heading sticky top-0 z-[1] px-4 pt-3 pb-2 text-footnote font-semibold text-[var(--color-text-primary)]">
                    {formatAlbumDay(group.date, timezone, locale)}
                  </h3>
                  <div className="grid grid-cols-3 gap-[2px]">
                    {group.photos.map((p) => {
                      const isSelected = selected.has(p.id);
                      return (
                        <button
                          key={p.id}
                          type="button"
                          onClick={() => handleTile(p)}
                          aria-pressed={selecting ? isSelected : undefined}
                          className="album-tile relative aspect-square overflow-hidden bg-[var(--color-surface-secondary)]"
                        >
                          <img
                            // A tile is about 140px, so the stored photo here is
                            // megabytes per screenful. Photos uploaded before
                            // thumbnails existed have none and never will, which
                            // makes the fallback permanent rather than temporary.
                            src={p.thumbnailUrl ?? p.imageUrl}
                            alt={p.caption}
                            loading="lazy"
                            draggable={false}
                            onError={() => handleImageError(p.id)}
                            className={`h-full w-full object-cover transition-[opacity,transform] duration-150 ${
                              isSelected ? 'scale-[0.94] opacity-80' : ''
                            }`}
                          />
                          {selecting && <SelectMark selected={isSelected} />}
                        </button>
                      );
                    })}
                  </div>
                </section>
              ))}
              {nextCursor && (
                <div ref={sentinelRef} className="px-4 pt-4">
                  <button
                    type="button"
                    onClick={loadMore}
                    disabled={loadingMore}
                    className="w-full rounded-[var(--radius-control)] py-2 text-footnote text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-hover)] disabled:opacity-60"
                  >
                    {loadingMore ? t('common.loading') : t('album.loadMore')}
                  </button>
                </div>
              )}
              {!nextCursor && (
                <p className="pt-4 text-center text-caption text-[var(--color-text-tertiary)] tabular-nums">
                  {t('album.count', { count: photos.length })}
                </p>
              )}
            </div>
          )}
        </div>

        {selecting && (
          <div className="flex items-center justify-between gap-3 border-t border-[var(--color-border)] px-5 py-3 pb-[max(env(safe-area-inset-bottom),0.75rem)]">
            <span className="w-8" aria-hidden="true" />
            <span
              className="text-footnote font-semibold text-[var(--color-text-primary)] tabular-nums"
              aria-live="polite"
            >
              {selected.size === 0
                ? t('album.selectPrompt')
                : t('album.selectedCount', { count: selected.size })}
            </span>
            <button
              type="button"
              onClick={() => setConfirmingDelete(true)}
              disabled={selected.size === 0}
              aria-label={t('common.delete')}
              title={t('common.delete')}
              className="icon-btn h-8 w-8 text-[var(--color-danger)] disabled:text-[var(--color-text-tertiary)] disabled:opacity-60"
            >
              <TrashIcon />
            </button>
          </div>
        )}

        {confirmingDelete && (
          <AlbumDeleteSheet
            title={t('album.deleteMany', { count: selected.size })}
            hint={t('album.deleteHint', { name: target?.name ?? '' })}
            busy={deleting}
            tone="theme"
            onConfirm={handleBulkDelete}
            onCancel={() => setConfirmingDelete(false)}
          />
        )}
      </SidePanel>

      {viewerId && (
        <AlbumViewer
          photos={photos}
          photoId={viewerId}
          calendarId={activeCalendarId}
          calendarName={target?.name ?? ''}
          editable={editable}
          hasMore={Boolean(nextCursor)}
          onLoadMore={loadMore}
          onNavigate={setViewerId}
          onClose={() => setViewerId(null)}
          onImageError={handleImageError}
          onDownload={handleDownload}
          onDelete={handleViewerDelete}
          onSaveCaption={handleSaveCaption}
          onLinkEvent={handleLinkEvent}
        />
      )}
    </>
  );
}

/** The check circle a tile wears in selection mode. */
function SelectMark({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={`absolute right-1.5 bottom-1.5 flex h-[22px] w-[22px] items-center justify-center rounded-full border-[1.5px] border-white shadow-[0_1px_3px_rgba(0,0,0,0.35)] ${
        selected ? 'bg-[var(--color-accent)]' : 'bg-black/15'
      }`}
    >
      {selected && (
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none">
          <path
            d="M5 12.5l4.5 4.5L19 7.5"
            stroke="white"
            strokeWidth="3"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      )}
    </span>
  );
}
