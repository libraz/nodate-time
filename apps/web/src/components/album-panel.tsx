import { useCallback, useEffect, useRef, useState } from 'react';
import { useT } from '@/i18n';
import { ApiError, api } from '@/lib/api';
import { resizeImageForAlbum } from '@/lib/image-resize';
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
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [photos, setPhotos] = useState<AlbumPhoto[]>([]);
  const [loading, setLoading] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lightbox, setLightbox] = useState<AlbumPhoto | null>(null);

  const activeCalendarId = activeCalendarIds[0] ?? calendars[0]?.id ?? '';

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
      if (e instanceof ApiError) setError(e.detail);
      else setError('failed to load');
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
        const pres = await api.post<PresignResponse>(
          `/calendars/${activeCalendarId}/albums/presign`,
          {
            contentType: resized.contentType,
            byteSize: resized.bytes.byteLength,
            width: resized.width,
            height: resized.height,
          },
        );
        const putRes = await fetch(pres.uploadUrl, {
          method: 'PUT',
          headers: { 'Content-Type': resized.contentType },
          body: resized.bytes,
        });
        if (!putRes.ok) throw new ApiError(putRes.status, 'upload failed');
        await reload();
      } catch (e) {
        if (e instanceof ApiError) setError(e.detail);
        else setError('failed to upload');
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
      } catch {
        // ignore
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
        className="fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={() => toggleRightPanel('album')}
      />
      <div className="glass-surface-heavy fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="text-[16px] font-semibold">{t('panel.album')}</h2>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              disabled={uploading || !activeCalendarId}
              className="btn-primary px-3 py-1.5 text-[12px] disabled:opacity-50"
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

        {error && <div className="px-5 py-2 text-[12px] text-[var(--color-danger)]">{error}</div>}

        <div className="flex-1 overflow-y-auto p-4">
          {!activeCalendarId ? (
            <p className="py-10 text-center text-[13px] text-[var(--color-text-tertiary)]">—</p>
          ) : loading && photos.length === 0 ? (
            <p className="py-10 text-center text-[13px] text-[var(--color-text-tertiary)]">…</p>
          ) : photos.length === 0 ? (
            <p className="py-10 text-center text-[13px] text-[var(--color-text-tertiary)]">
              {t('panel.noPhotos')}
            </p>
          ) : (
            <div className="grid grid-cols-3 gap-1">
              {photos.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => setLightbox(p)}
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
          <div className="relative max-h-full max-w-full">
            <img
              src={lightbox.imageUrl}
              alt={lightbox.caption}
              className="max-h-[80vh] max-w-[90vw] rounded-lg object-contain"
            />
            <div className="absolute right-2 top-2 flex gap-2">
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  handleDelete(lightbox.id);
                }}
                className="rounded-md bg-black/60 px-3 py-1 text-[12px] text-white hover:bg-[var(--color-danger)]"
              >
                {t('common.delete')}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
