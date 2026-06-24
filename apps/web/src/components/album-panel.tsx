import { useCallback, useEffect, useRef, useState } from 'react';
import { useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { resizeImageForAlbum } from '@/lib/image-resize';
import { canEdit, roleForCalendar } from '@/lib/permissions';
import { uploadViaPresign } from '@/lib/upload';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

interface AlbumPhoto {
  id: string;
  caption: string;
  imageUrl: string;
  createdAt: string;
  takenAt: string;
  uploadedBy: { id: string; name: string; avatarUrl?: string };
}

interface AlbumListResponse {
  items: AlbumPhoto[];
  nextCursor?: string;
}

interface PresignResponse {
  photoId: string;
  uploadUrl: string;
}

export function AlbumPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const me = useAuthStore((s) => s.user);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [photos, setPhotos] = useState<AlbumPhoto[]>([]);
  const [loading, setLoading] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lightbox, setLightbox] = useState<AlbumPhoto | null>(null);
  const [captionDraft, setCaptionDraft] = useState('');
  const [savingCaption, setSavingCaption] = useState(false);

  const activeCalendarId = activeCalendarIds[0] ?? calendars[0]?.id ?? '';
  const myRole = roleForCalendar(membersMap[activeCalendarId], me?.email);
  const editable = canEdit(myRole);

  const reload = useCallback(async () => {
    if (!activeCalendarId) {
      setPhotos([]);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const data = await api.get<AlbumListResponse>(`/calendars/${activeCalendarId}/albums`);
      setPhotos(data.items ?? []);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, [activeCalendarId]);

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
        const resized = await resizeImageForAlbum(file);
        const presign = await uploadViaPresign<PresignResponse>({
          kind: 'album',
          presignPath: `/calendars/${activeCalendarId}/albums/presign`,
          presignBody: {
            contentType: resized.contentType,
            byteSize: resized.bytes.byteLength,
            width: resized.width,
            height: resized.height,
          },
          contentType: resized.contentType,
          body: resized.bytes,
          byteSize: resized.bytes.byteLength,
        });
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
      <div className="glass-surface-heavy side-panel fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]">
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
            <div className="grid grid-cols-3 gap-1">
              {photos.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => openLightbox(p)}
                  className="relative aspect-square overflow-hidden rounded-md bg-[var(--color-surface-secondary)]"
                >
                  <img
                    src={p.imageUrl}
                    alt={p.caption}
                    loading="lazy"
                    className="h-full w-full object-cover"
                  />
                </button>
              ))}
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
          <div className="relative flex max-h-full max-w-full flex-col gap-3">
            <img
              src={lightbox.imageUrl}
              alt={lightbox.caption}
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
          </div>
        </div>
      )}
    </>
  );
}
