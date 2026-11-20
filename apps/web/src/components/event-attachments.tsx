import { useCallback, useEffect, useRef, useState } from 'react';
import { LabelledButton } from '@/components/labelled-button';
import { useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { toast } from '@/lib/toast';
import { uploadViaPresign } from '@/lib/upload';
import type { EventAttachment } from '@/types/calendar';

export function AttachmentsSection({
  calendarId,
  eventId,
  editable,
}: {
  calendarId: string;
  eventId: string;
  editable: boolean;
}) {
  const t = useT();
  const [attachments, setAttachments] = useState<EventAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  // A listing that could not be read says so where the files would be: an
  // empty section otherwise claims the event has no attachments.
  const [loadError, setLoadError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const fetchAttachments = useCallback(async () => {
    try {
      const data = await api.get<EventAttachment[]>(
        `/calendars/${calendarId}/events/${eventId}/attachments`,
      );
      setAttachments(data);
      setLoadError(null);
    } catch (err) {
      setLoadError(errorMessage(err));
    }
  }, [calendarId, eventId]);

  useEffect(() => {
    fetchAttachments();
  }, [fetchAttachments]);

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    const contentType = file.type || 'application/octet-stream';
    try {
      const presign = await uploadViaPresign<{ attachmentId: string; uploadUrl: string }>({
        kind: 'attachment',
        presignPath: `/calendars/${calendarId}/events/${eventId}/attachments/presign`,
        presignBody: { filename: file.name, contentType, byteSize: file.size },
        contentAddressed: true,
        contentType,
        body: file,
        byteSize: file.size,
      });
      // The row is created disabled; confirm enables it once the object is stored.
      await api.post(
        `/calendars/${calendarId}/events/${eventId}/attachments/${presign.attachmentId}/confirm`,
      );
      await fetchAttachments();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setUploading(false);
      e.target.value = '';
    }
  };

  const handleDownload = async (att: EventAttachment) => {
    try {
      const { downloadUrl } = await api.get<{ downloadUrl: string }>(
        `/calendars/${calendarId}/events/${eventId}/attachments/${att.id}/download`,
      );
      window.open(downloadUrl, '_blank');
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.delete(`/calendars/${calendarId}/events/${eventId}/attachments/${id}`);
      await fetchAttachments();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const formatSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  return (
    <div className="card-section mx-6 mt-3 bg-[var(--color-surface-secondary)] p-4">
      <div className="mb-3 flex items-center gap-2">
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--color-text-secondary)"
          strokeWidth="2"
        >
          <path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" />
        </svg>
        <span className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('event.attachments')}
        </span>
      </div>

      {loadError && attachments.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-danger)]">{loadError}</p>
      )}

      {attachments.length > 0 && (
        <div className="mb-3 space-y-2">
          {attachments.map((att) => (
            <div
              key={att.id}
              className="group flex items-center gap-2 bg-[var(--color-surface-inset)] px-3 py-2"
              style={{ borderRadius: 'var(--radius-sm)' }}
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="var(--color-text-tertiary)"
                strokeWidth="2"
                className="shrink-0"
              >
                <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
                <path d="M14 2v6h6" />
              </svg>
              <div className="min-w-0 flex-1">
                <p className="truncate text-body text-[var(--color-text-primary)]">
                  {att.filename}
                </p>
                <p className="text-caption text-[var(--color-text-tertiary)]">
                  {formatSize(att.byteSize)}
                </p>
              </div>
              <button
                type="button"
                onClick={() => handleDownload(att)}
                className="shrink-0 text-[var(--color-accent-ink)] hover:underline text-footnote"
              >
                {t('event.download')}
              </button>
              {editable && (
                <LabelledButton
                  label={t('common.delete')}
                  onClick={() => handleDelete(att.id)}
                  className="reveal-on-hover shrink-0 text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
                >
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M18 6L6 18M6 6l12 12" />
                  </svg>
                </LabelledButton>
              )}
            </div>
          ))}
        </div>
      )}

      {editable && (
        <>
          <input ref={fileInputRef} type="file" onChange={handleUpload} className="hidden" />
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            disabled={uploading}
            className="flex w-full items-center justify-center gap-2 border border-dashed border-[var(--color-border)] py-2.5 text-body text-[var(--color-text-tertiary)] hover:border-[var(--color-accent)] hover:text-[var(--color-accent-ink)] transition-colors disabled:opacity-50"
            style={{ borderRadius: 'var(--radius-md)' }}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
              <polyline points="17 8 12 3 7 8" />
              <line x1="12" y1="3" x2="12" y2="15" />
            </svg>
            {uploading ? t('event.uploading') : t('event.uploadAttachment')}
          </button>
        </>
      )}
    </div>
  );
}
