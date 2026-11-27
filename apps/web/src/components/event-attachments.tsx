import { useCallback, useEffect, useRef, useState } from 'react';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { LabelledButton } from '@/components/labelled-button';
import { useT } from '@/i18n';
import { ApiError, client, errorMessage } from '@/lib/client';
import { startDownload } from '@/lib/download';
import { toast } from '@/lib/toast';
import { putToSignedUrl, sha256Hex, validateUpload } from '@/lib/upload';
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
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  // A listing that could not be read says so where the files would be: an
  // empty section otherwise claims the event has no attachments.
  const [loadError, setLoadError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const fetchAttachments = useCallback(async () => {
    try {
      const { data } = await client.GET('/calendars/{calendarId}/events/{eventId}/attachments', {
        params: { path: { calendarId, eventId } },
      });
      setAttachments(data ?? []);
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
      validateUpload('attachment', contentType, file.size);
      const sha256 = await sha256Hex(file);
      const { data: presign } = await client.POST(
        '/calendars/{calendarId}/events/{eventId}/attachments/presign',
        {
          params: { path: { calendarId, eventId } },
          body: { filename: file.name, contentType, byteSize: file.size, sha256 },
        },
      );
      if (!presign) throw new ApiError(502, 'Malformed presign response');
      // An empty URL means these bytes are already stored; only the confirm is left.
      if (presign.uploadUrl) {
        await putToSignedUrl(presign.uploadUrl, contentType, file);
      }
      // The row is created disabled; confirm enables it once the object is stored.
      await client.POST(
        '/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}/confirm',
        { params: { path: { calendarId, eventId, attachmentId: presign.attachmentId } } },
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
      const { data } = await client.GET(
        '/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}/download',
        { params: { path: { calendarId, eventId, attachmentId: att.id } } },
      );
      if (data) startDownload(data.downloadUrl);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const handleDelete = async (id: string) => {
    setDeleting(true);
    try {
      await client.DELETE('/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}', {
        params: { path: { calendarId, eventId, attachmentId: id } },
      });
      await fetchAttachments();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setDeleting(false);
      setPendingDelete(null);
    }
  };

  const formatSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  // Nothing attached and nothing to attach: no card at all.
  if (!editable && attachments.length === 0 && !loadError) return null;
  return (
    <div className="card-section mx-6 mt-2 bg-[var(--color-surface-secondary)] px-4 py-1.5">
      {/* Empty, the attach row is the whole section and names it. */}
      {attachments.length > 0 && (
        <div className="mt-1.5 mb-2 flex items-center gap-2 text-[var(--color-text-secondary)]">
          <svg
            aria-hidden
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" />
          </svg>
          <span className="text-footnote font-semibold">{t('event.attachments')}</span>
          <span className="ml-auto text-footnote tabular-nums text-[var(--color-text-tertiary)]">
            {attachments.length}
          </span>
        </div>
      )}

      {loadError && attachments.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-danger)]">{loadError}</p>
      )}

      {attachments.length > 0 && (
        <div className="mb-1 space-y-2">
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
                  onClick={() => setPendingDelete(att.id)}
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
            className="flex w-full items-center gap-3 py-1.5 text-left text-default text-[var(--color-text-secondary)] transition-colors hover:text-[var(--color-accent-ink)] disabled:opacity-50"
          >
            <svg
              aria-hidden
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-text-tertiary)"
              strokeWidth="2"
              className="shrink-0"
            >
              <path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" />
            </svg>
            {uploading ? t('event.uploading') : t('event.uploadAttachment')}
          </button>
        </>
      )}

      <ConfirmSheet
        open={pendingDelete !== null}
        title={t('confirm.deleteAttachment')}
        confirmLabel={t('common.delete')}
        busy={deleting}
        onConfirm={() => pendingDelete && handleDelete(pendingDelete)}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
