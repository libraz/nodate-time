import { useCallback, useEffect, useMemo, useState } from 'react';
import { useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { DEFAULT_INVITE_ROLE, isAdmin, roleForCalendar } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

interface InviteData {
  id: number;
  token: string;
  role: string;
  maxUses: number | null;
  useCount: number;
  isPublic: boolean;
  expiresAt: string | null;
  createdAt: string;
}

/** A public/embed link is a non-consuming, read-only viewer link. */
const isPublicLink = (i: InviteData) => i.isPublic;

export function SharePanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const me = useAuthStore((s) => s.user);

  // Inviting requires admin; only offer calendars the user administers and is viewing.
  const adminCalendars = useMemo(
    () =>
      calendars.filter(
        (c) =>
          activeCalendarIds.includes(c.id) && isAdmin(roleForCalendar(membersMap[c.id], me?.email)),
      ),
    [calendars, activeCalendarIds, membersMap, me?.email],
  );

  const [targetId, setTargetId] = useState('');
  const target = adminCalendars.find((c) => c.id === targetId) ?? adminCalendars[0] ?? null;
  const calendarId = target?.id ?? '';

  const [invites, setInvites] = useState<InviteData[]>([]);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [busy, setBusy] = useState<'invite' | 'public' | null>(null);

  // Several single-use invite links may coexist; the public link is at most one.
  const joinInvites = invites.filter((i) => !i.isPublic);
  const publicLink = invites.find(isPublicLink) ?? null;

  const origin = typeof window === 'undefined' ? '' : window.location.origin;
  const publicUrl = publicLink ? `${origin}/embed/${publicLink.token}` : '';
  const embedSnippet = publicLink
    ? `<iframe src="${publicUrl}" width="100%" height="640" style="border:1px solid #e5e7eb;border-radius:12px" loading="lazy"></iframe>`
    : '';

  useEffect(() => {
    if (rightPanel !== 'share') {
      setInvites([]);
      setCopiedKey(null);
      return;
    }
    if (!calendarId) return;
    let cancelled = false;
    (async () => {
      try {
        const list = await api.get<InviteData[]>(`/calendars/${calendarId}/invites`);
        if (!cancelled) setInvites(list);
      } catch (e) {
        if (!cancelled) toast.error(errorMessage(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [rightPanel, calendarId]);

  const createInvite = useCallback(async () => {
    if (!calendarId) return;
    setBusy('invite');
    try {
      const data = await api.post<InviteData>(`/calendars/${calendarId}/invites`, {
        role: DEFAULT_INVITE_ROLE,
        maxUses: 1,
      });
      setInvites((cur) => [data, ...cur]);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(null);
    }
  }, [calendarId]);

  const createPublic = useCallback(async () => {
    if (!calendarId) return;
    // Guard against accidental external exposure.
    if (!window.confirm(t('share.publicConfirm'))) return;
    setBusy('public');
    try {
      const data = await api.post<InviteData>(`/calendars/${calendarId}/invites`, {
        role: 'viewer',
        isPublic: true,
      });
      setInvites((cur) => [data, ...cur.filter((i) => !isPublicLink(i))]);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(null);
    }
  }, [calendarId, t]);

  const revoke = useCallback(
    async (id: number) => {
      if (!calendarId) return;
      try {
        await api.delete(`/calendars/${calendarId}/invites/${id}`);
        setInvites((cur) => cur.filter((i) => i.id !== id));
      } catch (e) {
        toast.error(errorMessage(e));
      }
    },
    [calendarId],
  );

  const copy = useCallback((key: string, text: string) => {
    navigator.clipboard.writeText(text);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(null), 2000);
  }, []);

  if (rightPanel !== 'share') return null;

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={() => toggleRightPanel('share')}
      />
      <div className="glass-surface-heavy side-panel fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="truncate text-subhead font-semibold">{t('panel.share')}</h2>
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
          {!target ? (
            <p className="rounded-xl bg-[var(--color-surface-inset)] px-4 py-6 text-center text-body text-[var(--color-text-secondary)]">
              {t('invites.empty')}
            </p>
          ) : (
            <div className="space-y-6">
              {/* Target calendar */}
              <div className="space-y-1.5">
                <span className="block text-caption font-medium text-[var(--color-text-secondary)]">
                  {t('share.targetCalendar')}
                </span>
                {adminCalendars.length > 1 ? (
                  <select
                    value={target.id}
                    onChange={(e) => setTargetId(e.target.value)}
                    className="input-modern h-10 w-full text-sm"
                  >
                    {adminCalendars.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <div className="flex items-center gap-2">
                    <span
                      className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                      style={{ backgroundColor: target.color }}
                    />
                    <span className="truncate text-callout font-medium text-[var(--color-text-primary)]">
                      {target.name}
                    </span>
                  </div>
                )}
              </div>

              {/* Invite links (single-use) — multiple may coexist */}
              <section className="space-y-2">
                <h3 className="text-footnote font-semibold uppercase tracking-wide text-[var(--color-text-tertiary)]">
                  {t('share.inviteSection')}
                </h3>
                <p className="text-caption text-[var(--color-text-secondary)]">
                  {t('share.inviteSingleUseNote')}
                </p>
                {joinInvites.length > 0 && (
                  <div className="space-y-3">
                    {joinInvites.map((inv) => {
                      const url = `${origin}/share/${inv.token}`;
                      const key = `invite-${inv.id}`;
                      return (
                        <div key={inv.id} className="space-y-2">
                          <UrlRow
                            value={url}
                            copied={copiedKey === key}
                            onCopy={() => copy(key, url)}
                            copyLabel={t('common.copy')}
                            copiedLabel={t('common.copied')}
                          />
                          <div className="flex items-center justify-between">
                            <span
                              className="rounded-full px-2 py-0.5 text-caption font-medium"
                              style={{
                                backgroundColor: inv.useCount
                                  ? 'var(--color-danger-bg)'
                                  : 'var(--color-accent-bg)',
                                color: inv.useCount ? 'var(--color-danger)' : 'var(--color-accent)',
                              }}
                            >
                              {inv.useCount ? t('share.inviteUsed') : t('share.inviteUnused')}
                            </span>
                            <button
                              type="button"
                              onClick={() => revoke(inv.id)}
                              className="text-footnote text-[var(--color-danger)] hover:underline"
                            >
                              {t('share.revokeLink')}
                            </button>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}
                <button
                  type="button"
                  onClick={createInvite}
                  disabled={busy === 'invite'}
                  className={`${joinInvites.length > 0 ? 'btn-secondary' : 'btn-primary'} w-full text-body disabled:opacity-50`}
                >
                  {busy === 'invite'
                    ? t('share.creating')
                    : joinInvites.length > 0
                      ? t('share.createAnotherInvite')
                      : t('share.createInvite')}
                </button>
              </section>

              <div className="border-t border-[var(--color-border)] opacity-60" />

              {/* Public / embed link */}
              <section className="space-y-2">
                <h3 className="text-footnote font-semibold uppercase tracking-wide text-[var(--color-text-tertiary)]">
                  {t('share.publicSection')}
                </h3>
                <p className="text-caption text-[var(--color-text-secondary)]">
                  {t('share.publicNote')}
                </p>
                {publicLink && (
                  <div
                    className="flex items-start gap-2 rounded-[var(--radius-md)] border px-3 py-2"
                    style={{
                      borderColor: 'var(--color-danger)',
                      backgroundColor: 'var(--color-danger-bg)',
                      color: 'var(--color-danger)',
                    }}
                  >
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      className="mt-0.5 shrink-0"
                    >
                      <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
                      <line x1="12" y1="9" x2="12" y2="13" />
                      <line x1="12" y1="17" x2="12.01" y2="17" />
                    </svg>
                    <span className="text-caption font-medium">
                      {t('share.publicActiveWarning')}
                    </span>
                  </div>
                )}
                {!publicLink ? (
                  <button
                    type="button"
                    onClick={createPublic}
                    disabled={busy === 'public'}
                    className="btn-secondary w-full text-body disabled:opacity-50"
                  >
                    {busy === 'public' ? t('share.creating') : t('share.createPublic')}
                  </button>
                ) : (
                  <div className="space-y-3">
                    <UrlRow
                      value={publicUrl}
                      copied={copiedKey === 'public'}
                      onCopy={() => copy('public', publicUrl)}
                      copyLabel={t('share.copyUrl')}
                      copiedLabel={t('common.copied')}
                    />
                    <div className="space-y-1.5">
                      <span className="text-caption font-medium text-[var(--color-text-secondary)]">
                        {t('share.embedCode')}
                      </span>
                      <textarea
                        readOnly
                        value={embedSnippet}
                        rows={3}
                        onFocus={(e) => e.currentTarget.select()}
                        className="w-full resize-none rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-surface-secondary)] px-3 py-2 font-mono text-micro text-[var(--color-text-secondary)] outline-none"
                      />
                      <button
                        type="button"
                        onClick={() => copy('embed', embedSnippet)}
                        className="btn-secondary w-full text-footnote"
                      >
                        {copiedKey === 'embed' ? t('common.copied') : t('share.copyEmbed')}
                      </button>
                    </div>
                    <button
                      type="button"
                      onClick={() => revoke(publicLink.id)}
                      className="text-footnote text-[var(--color-danger)] hover:underline"
                    >
                      {t('share.revokeLink')}
                    </button>
                  </div>
                )}
              </section>
            </div>
          )}
        </div>
      </div>
    </>
  );
}

interface UrlRowProps {
  value: string;
  copied: boolean;
  onCopy: () => void;
  copyLabel: string;
  copiedLabel: string;
}

function UrlRow({ value, copied, onCopy, copyLabel, copiedLabel }: UrlRowProps) {
  return (
    <div
      className="flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface-secondary)] px-3 py-2"
      style={{ borderRadius: 'var(--radius-sm)' }}
    >
      <input
        type="text"
        readOnly
        value={value}
        onFocus={(e) => e.currentTarget.select()}
        className="flex-1 truncate bg-transparent text-footnote text-[var(--color-text-secondary)] outline-none"
      />
      <button
        type="button"
        onClick={onCopy}
        className="btn-primary shrink-0 px-3 py-1 text-footnote"
      >
        {copied ? copiedLabel : copyLabel}
      </button>
    </div>
  );
}
