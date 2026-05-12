import { useT } from '@/i18n';
import { api } from '@/lib/api';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import { useCallback, useEffect, useState } from 'react';

interface InviteData {
  id: number;
  token: string;
  role: string;
  maxUses: number | null;
  useCount: number;
  expiresAt: string | null;
  createdAt: string;
}

export function SharePanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const calendarId = activeCalendarIds[0] ?? calendars[0]?.id ?? '';
  const calendar = calendars.find((c) => c.id === calendarId);

  const [invite, setInvite] = useState<InviteData | null>(null);
  const [copied, setCopied] = useState(false);
  const [loading, setLoading] = useState(false);

  const linkUrl = invite ? `${window.location.origin}/share/${invite.token}` : '';

  useEffect(() => {
    if (rightPanel !== 'share') {
      setInvite(null);
      setCopied(false);
    }
  }, [rightPanel]);

  const createLink = useCallback(async () => {
    if (!calendarId) return;
    setLoading(true);
    try {
      const data = await api.post<InviteData>(`/calendars/${calendarId}/invites`, {
        role: 'viewer',
      });
      setInvite(data);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [calendarId]);

  const revokeLink = useCallback(async () => {
    if (!invite || !calendarId) return;
    try {
      await api.delete(`/calendars/${calendarId}/invites/${invite.id}`);
      setInvite(null);
    } catch {
      // ignore
    }
  }, [calendarId, invite]);

  const copyLink = useCallback(() => {
    navigator.clipboard.writeText(linkUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [linkUrl]);

  if (rightPanel !== 'share') return null;

  return (
    <>
      <div
        className="fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={() => toggleRightPanel('share')}
        onKeyDown={undefined}
      />
      <div className="glass-surface-heavy fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <div className="min-w-0">
            <h2 className="truncate text-[16px] font-semibold">{t('panel.share')}</h2>
            {calendar && (
              <p className="truncate text-[12px] text-[var(--color-text-secondary)]">
                {calendar.name}
              </p>
            )}
          </div>
          <button
            type="button"
            onClick={() => toggleRightPanel('share')}
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

        <div className="flex-1 overflow-y-auto px-5 py-4">
          {!calendarId ? (
            <p className="py-10 text-center text-[13px] text-[var(--color-text-tertiary)]">—</p>
          ) : !invite ? (
            <button
              type="button"
              onClick={createLink}
              disabled={loading}
              className="btn-primary flex w-full items-center justify-center gap-2 text-[13px] disabled:opacity-50"
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71" />
                <path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71" />
              </svg>
              {loading ? t('share.creating') : t('share.createLink')}
            </button>
          ) : (
            <div className="space-y-3">
              <div
                className="flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface-secondary)] px-3 py-2"
                style={{ borderRadius: 'var(--radius-sm)' }}
              >
                <input
                  type="text"
                  readOnly
                  value={linkUrl}
                  className="flex-1 truncate bg-transparent text-[12px] text-[var(--color-text-secondary)] outline-none"
                />
                <button
                  type="button"
                  onClick={copyLink}
                  className="btn-primary shrink-0 px-3 py-1 text-[12px]"
                >
                  {copied ? t('common.copied') : t('common.copy')}
                </button>
              </div>
              <p className="text-[11px] text-[var(--color-text-secondary)]">
                {t('share.linkDescription')}
              </p>
              <button
                type="button"
                onClick={revokeLink}
                className="text-[12px] text-[var(--color-danger)] hover:underline"
              >
                {t('share.revokeLink')}
              </button>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
