import { useCallback, useEffect, useState } from 'react';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { TipCard } from '@/components/guide';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { CustomSelect } from '@/components/pickers';
import { SidePanel } from '@/components/side-panel';
import { useT } from '@/i18n';
import { copyText } from '@/lib/clipboard';
import { formatDateTime } from '@/lib/format-datetime';
import {
  canManage,
  DEFAULT_INVITE_ROLE,
  INVITE_ROLE_OPTIONS,
  type Role,
  roleLabelKey,
} from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useInvites } from '@/lib/use-invites';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { usePresence } from '@/lib/use-presence';
import { useUiStore } from '@/stores/ui-store';
import type { Calendar } from '@/types/calendar';
import {
  INVITE_EXPIRY_HOURS,
  INVITE_MAX_USES,
  inviteExpiryLabelKey,
  inviteUsesLabelKey,
} from '@/types/invite';

const manages = (calendar: Calendar) => canManage(calendar.role);

export function SharePanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const timezone = useUiStore((s) => s.timezone);
  const locale = useUiStore((s) => s.locale);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const openPanelFor = useUiStore((s) => s.openPanelFor);
  const presence = usePresence(rightPanel === 'share');
  // Inviting requires admin; only offer calendars the user administers and is viewing.
  const { options, target, setTargetId } = usePanelCalendar(manages);
  const calendarId = target?.id ?? '';

  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  // Every link copied or shared since the panel opened. A join link is shown
  // only once, so closing with one outside this set loses it for good.
  const [handedOff, setHandedOff] = useState<ReadonlySet<string>>(new Set());
  const [confirmingClose, setConfirmingClose] = useState(false);
  const [inviteRole, setInviteRole] = useState<Role>(DEFAULT_INVITE_ROLE);
  // Bounded by default: a link that never expires and admits anyone is the
  // one you cannot take back once it has been forwarded.
  const [inviteExpiry, setInviteExpiry] = useState<number>(168);
  const [inviteUses, setInviteUses] = useState<number>(1);
  const [confirmingPublic, setConfirmingPublic] = useState(false);
  const [pendingRevoke, setPendingRevoke] = useState<{ id: string; isPublic: boolean } | null>(
    null,
  );
  const [confirmBusy, setConfirmBusy] = useState(false);

  // Several single-use invite links may coexist; the public link is at most one.
  const { joinInvites, publicLink, busy, createInvite, createPublicLink, revokeInvite } =
    useInvites(calendarId, presence.mounted);

  const origin = typeof window === 'undefined' ? '' : window.location.origin;
  const publicUrl = publicLink?.token ? `${origin}/embed/${publicLink.token}` : '';
  const embedSnippet = publicUrl
    ? `<iframe src="${publicUrl}" width="100%" height="640" style="border:1px solid #e5e7eb;border-radius:12px" loading="lazy"></iframe>`
    : '';

  // A copied-link acknowledgement outlives neither the panel nor the link it
  // was shown against.
  useEffect(() => {
    if (!presence.mounted) {
      setCopiedKey(null);
      setHandedOff(new Set());
      setConfirmingClose(false);
    }
  }, [presence.mounted]);

  const handleCreateInvite = useCallback(
    () => createInvite({ role: inviteRole, expiryHours: inviteExpiry, maxUses: inviteUses }),
    [createInvite, inviteRole, inviteExpiry, inviteUses],
  );

  const copy = useCallback(
    async (key: string, text: string) => {
      if (!(await copyText(text))) {
        toast.error(t('common.copyFailed'));
        return;
      }
      setCopiedKey(key);
      setHandedOff((prev) => new Set(prev).add(key));
      setTimeout(() => setCopiedKey(null), 2000);
    },
    [t],
  );

  const share = useCallback(
    async (key: string, url: string) => {
      try {
        await navigator.share({ title: t('panel.share'), url });
        setHandedOff((prev) => new Set(prev).add(key));
      } catch {
        // Dismissing the system share sheet is not a failure.
      }
    },
    [t],
  );
  const canShare = typeof navigator !== 'undefined' && typeof navigator.share === 'function';
  const hasUncopiedLink = joinInvites.some(
    (inv) => inv.token && !handedOff.has(`invite-${inv.id}`),
  );
  const requestClose = () => {
    if (hasUncopiedLink) setConfirmingClose(true);
    else toggleRightPanel('share');
  };

  if (!presence.mounted) return null;

  return (
    <SidePanel
      presence={presence}
      onClose={requestClose}
      title={t('panel.share')}
      helpTopic="invite"
    >
      <div className="flex-1 overflow-y-auto px-5 py-4">
        {!target ? (
          <p className="rounded-[var(--radius-md)] bg-[var(--color-surface-inset)] px-4 py-6 text-center text-body text-[var(--color-text-secondary)]">
            {t('invites.empty')}
          </p>
        ) : (
          <div className="space-y-6">
            <PanelCalendarPicker
              label={t('panel.targetCalendar')}
              options={options}
              target={target}
              onChange={setTargetId}
            />

            <TipCard id="share" topic="invite">
              {t('tip.share')}
            </TipCard>

            <button
              type="button"
              onClick={() => openPanelFor('members', calendarId, { addMembers: true })}
              className="flex w-full items-center gap-3 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-elevated)] p-3 text-left transition-colors hover:bg-[var(--color-hover)]"
            >
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)]">
                <svg
                  width="18"
                  height="18"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.8"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <circle cx="9" cy="8" r="3.5" />
                  <path d="M2.5 20a6.5 6.5 0 0113 0M19 8v6M16 11h6" />
                </svg>
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-body font-semibold text-[var(--color-text-primary)]">
                  {t('members.addSection')}
                </span>
                <span className="block text-caption text-[var(--color-text-secondary)]">
                  {t('share.directAddLead')}
                </span>
              </span>
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
                className="shrink-0 text-[var(--color-text-tertiary)]"
              >
                <path d="M9 6l6 6-6 6" />
              </svg>
            </button>

            {/* Invite links (single-use) — multiple may coexist */}
            <section className="space-y-2">
              <h3 className="text-footnote font-semibold uppercase tracking-wide text-[var(--color-text-tertiary)]">
                {t('share.inviteSection')}
              </h3>
              <p className="text-caption text-[var(--color-text-secondary)]">
                {t('share.inviteLead')}
              </p>
              <p className="text-caption text-[var(--color-text-secondary)]">
                {t('share.inviteTermsNote')}
              </p>
              <CustomSelect
                value={inviteRole}
                onChange={(role) => setInviteRole(role as Role)}
                ariaLabel={t('members.role')}
                options={INVITE_ROLE_OPTIONS.map((role) => ({
                  value: role,
                  label: t(roleLabelKey(role)),
                }))}
                triggerClassName="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-body text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
              />
              <div className="flex gap-2">
                <CustomSelect
                  className="flex-1"
                  value={String(inviteExpiry)}
                  onChange={(v) => setInviteExpiry(Number(v))}
                  ariaLabel={t('invites.expiry')}
                  options={INVITE_EXPIRY_HOURS.map((h) => ({
                    value: String(h),
                    label: t(inviteExpiryLabelKey(h)),
                  }))}
                  triggerClassName="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-body text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
                />
                <CustomSelect
                  className="flex-1"
                  value={String(inviteUses)}
                  onChange={(v) => setInviteUses(Number(v))}
                  ariaLabel={t('invites.uses')}
                  options={INVITE_MAX_USES.map((u) => ({
                    value: String(u),
                    label: t(inviteUsesLabelKey(u)),
                  }))}
                  triggerClassName="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-body text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
                />
              </div>
              {joinInvites.length > 0 && (
                <div className="space-y-3">
                  {joinInvites.map((inv) => {
                    const url = inv.token ? `${origin}/share/${inv.token}` : '';
                    const key = `invite-${inv.id}`;
                    return (
                      <div key={inv.id} className="space-y-2">
                        {url ? (
                          <>
                            <UrlRow
                              value={url}
                              copied={copiedKey === key}
                              onCopy={() => copy(key, url)}
                              copyLabel={t('common.copy')}
                              copiedLabel={t('common.copied')}
                            />
                            {canShare && (
                              <button
                                type="button"
                                onClick={() => share(key, url)}
                                className="btn-secondary h-9 w-full px-4 text-body"
                              >
                                {t('invites.shareLink')}
                              </button>
                            )}
                            <p className="text-caption text-[var(--color-text-tertiary)]">
                              {t('invites.linkShownOnce')}
                            </p>
                          </>
                        ) : (
                          <p className="rounded-[var(--radius-sm)] bg-[var(--color-surface-inset)] px-3 py-2 text-caption text-[var(--color-text-secondary)]">
                            {t('invites.linkUnavailable')}
                          </p>
                        )}
                        {inv.expiresAt && (
                          <p className="text-caption text-[var(--color-text-tertiary)]">
                            {t('invites.expiresAt')}:{' '}
                            {formatDateTime(inv.expiresAt, timezone, locale)}
                          </p>
                        )}
                        <div className="flex items-center justify-between">
                          <span
                            className="rounded-full px-2 py-0.5 text-caption font-medium"
                            style={{
                              backgroundColor: inv.useCount
                                ? 'var(--color-danger-bg)'
                                : 'var(--color-accent-bg)',
                              color: inv.useCount
                                ? 'var(--color-danger)'
                                : 'var(--color-accent-ink)',
                            }}
                          >
                            {inv.useCount ? t('share.inviteUsed') : t('share.inviteUnused')}
                          </span>
                          <button
                            type="button"
                            onClick={() => setPendingRevoke({ id: inv.id, isPublic: false })}
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
                onClick={handleCreateInvite}
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
                <div role="status" className="callout callout-danger reveal-in">
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    className="callout-icon"
                    aria-hidden="true"
                  >
                    <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
                    <line x1="12" y1="9" x2="12" y2="13" />
                    <line x1="12" y1="17" x2="12.01" y2="17" />
                  </svg>
                  <span className="callout-title text-caption">
                    {t('share.publicActiveWarning')}
                  </span>
                </div>
              )}
              {!publicLink ? (
                <button
                  type="button"
                  onClick={() => setConfirmingPublic(true)}
                  disabled={busy === 'public'}
                  className="btn-secondary w-full text-body disabled:opacity-50"
                >
                  {busy === 'public' ? t('share.creating') : t('share.createPublic')}
                </button>
              ) : (
                <div className="space-y-3">
                  {publicUrl ? (
                    <>
                      <UrlRow
                        value={publicUrl}
                        copied={copiedKey === 'public'}
                        onCopy={() => copy('public', publicUrl)}
                        copyLabel={t('share.copyUrl')}
                        copiedLabel={t('common.copied')}
                      />
                      <p className="text-caption text-[var(--color-text-tertiary)]">
                        {t('invites.linkShownOnce')}
                      </p>
                      <div className="space-y-1.5">
                        <span className="text-caption font-medium text-[var(--color-text-secondary)]">
                          {t('share.embedCode')}
                        </span>
                        <textarea
                          data-1p-ignore
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
                    </>
                  ) : (
                    <p className="rounded-[var(--radius-sm)] bg-[var(--color-surface-inset)] px-3 py-2 text-caption text-[var(--color-text-secondary)]">
                      {t('invites.linkUnavailable')}
                    </p>
                  )}
                  <button
                    type="button"
                    onClick={() => setPendingRevoke({ id: publicLink.id, isPublic: true })}
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

      <ConfirmSheet
        open={confirmingClose}
        title={t('invites.closeTitle')}
        hint={t('invites.linkShownOnce')}
        confirmLabel={t('invites.closeAnyway')}
        onConfirm={() => {
          setConfirmingClose(false);
          toggleRightPanel('share');
        }}
        onCancel={() => setConfirmingClose(false)}
      />
      <ConfirmSheet
        open={confirmingPublic}
        title={t('confirm.publicLinkTitle')}
        hint={t('share.publicConfirm')}
        confirmLabel={t('common.create')}
        destructive={false}
        busy={confirmBusy}
        onConfirm={async () => {
          setConfirmBusy(true);
          await createPublicLink();
          setConfirmBusy(false);
          setConfirmingPublic(false);
        }}
        onCancel={() => setConfirmingPublic(false)}
      />
      <ConfirmSheet
        open={pendingRevoke !== null}
        title={pendingRevoke?.isPublic ? t('confirm.revokeLink') : t('confirm.revokeInvite')}
        confirmLabel={t('confirm.revoke')}
        busy={confirmBusy}
        onConfirm={async () => {
          if (!pendingRevoke) return;
          setConfirmBusy(true);
          await revokeInvite(pendingRevoke.id);
          setConfirmBusy(false);
          setPendingRevoke(null);
        }}
        onCancel={() => setPendingRevoke(null)}
      />
    </SidePanel>
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
        data-1p-ignore
        type="text"
        readOnly
        value={value}
        onFocus={(e) => e.currentTarget.select()}
        className="flex-1 truncate bg-transparent text-footnote text-[var(--color-text-secondary)] outline-none"
      />
      <button type="button" onClick={onCopy} className="btn-primary btn-sm shrink-0">
        {copied ? copiedLabel : copyLabel}
      </button>
    </div>
  );
}
