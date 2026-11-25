import { useState } from 'react';
import { Callout } from '@/components/callout';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { CustomSelect } from '@/components/custom-select';
import { RoleHint } from '@/components/role-hint';
import { UrlRow } from '@/components/url-row';
import { useT } from '@/i18n';
import { formatDateTime } from '@/lib/format-datetime';
import {
  DEFAULT_INVITE_ROLE,
  INVITE_ROLE_OPTIONS,
  type Role,
  roleLabelKey,
} from '@/lib/permissions';
import type { CalendarInvites } from '@/lib/use-invites';
import { useUiStore } from '@/stores/ui-store';
import {
  INVITE_EXPIRY_HOURS,
  INVITE_MAX_USES,
  inviteExpiryLabelKey,
  inviteUsesLabelKey,
} from '@/types/invite';

const SELECT_TRIGGER =
  'rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-body text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]';

/** The key a join link is tracked under once it has been copied or shared. */
export const inviteKey = (id: string) => `invite-${id}`;

interface InviteLinksProps {
  invites: CalendarInvites;
  /** Told when a link has been copied or shared, so the caller knows it went out. */
  onHandedOff: (key: string) => void;
}

/**
 * Join links for people outside the workspace: the terms a new link carries,
 * the links made so far, and revoking them. The link state is the caller's, so
 * a link shown only once survives leaving this view.
 */
export function InviteLinks({ invites, onHandedOff }: InviteLinksProps) {
  const t = useT();
  const timezone = useUiStore((s) => s.timezone);
  const locale = useUiStore((s) => s.locale);
  const { joinInvites, busy, createInvite, revokeInvite } = invites;
  const [role, setRole] = useState<Role>(DEFAULT_INVITE_ROLE);
  // Bounded by default: a link that never expires and admits anyone is the
  // one you cannot take back once it has been forwarded.
  const [expiry, setExpiry] = useState<number>(168);
  const [uses, setUses] = useState<number>(1);
  const [pendingRevoke, setPendingRevoke] = useState<string | null>(null);
  const [revoking, setRevoking] = useState(false);
  const origin = typeof window === 'undefined' ? '' : window.location.origin;
  const canShare = typeof navigator !== 'undefined' && typeof navigator.share === 'function';

  const share = async (key: string, url: string) => {
    try {
      await navigator.share({ title: t('members.inviteTab'), url });
      onHandedOff(key);
    } catch {
      // Dismissing the system share sheet is not a failure.
    }
  };

  return (
    <section className="space-y-2">
      <p className="text-caption text-[var(--color-text-secondary)]">{t('share.inviteLead')}</p>
      <p className="text-caption text-[var(--color-text-secondary)]">
        {t('share.inviteTermsNote')}
      </p>
      <CustomSelect
        value={role}
        onChange={(r) => setRole(r as Role)}
        ariaLabel={t('members.role')}
        options={INVITE_ROLE_OPTIONS.map((r) => ({ value: r, label: t(roleLabelKey(r)) }))}
        triggerClassName={SELECT_TRIGGER}
      />
      <RoleHint role={role} />
      <div className="flex gap-2">
        <CustomSelect
          className="flex-1"
          value={String(expiry)}
          onChange={(v) => setExpiry(Number(v))}
          ariaLabel={t('invites.expiry')}
          options={INVITE_EXPIRY_HOURS.map((h) => ({
            value: String(h),
            label: t(inviteExpiryLabelKey(h)),
          }))}
          triggerClassName={SELECT_TRIGGER}
        />
        <CustomSelect
          className="flex-1"
          value={String(uses)}
          onChange={(v) => setUses(Number(v))}
          ariaLabel={t('invites.uses')}
          options={INVITE_MAX_USES.map((u) => ({
            value: String(u),
            label: t(inviteUsesLabelKey(u)),
          }))}
          triggerClassName={SELECT_TRIGGER}
        />
      </div>
      {joinInvites.length > 0 && (
        <div className="space-y-3">
          {joinInvites.map((inv) => {
            const url = inv.token ? `${origin}/share/${inv.token}` : '';
            const key = inviteKey(inv.id);
            return (
              <div key={inv.id} className="space-y-2">
                {url ? (
                  <>
                    <Callout tone="warning">{t('invites.linkShownOnce')}</Callout>
                    <UrlRow
                      value={url}
                      copyLabel={t('common.copy')}
                      onCopied={() => onHandedOff(key)}
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
                  </>
                ) : (
                  <p className="rounded-[var(--radius-sm)] bg-[var(--color-surface-inset)] px-3 py-2 text-caption text-[var(--color-text-secondary)]">
                    {t('invites.linkUnavailable')}
                  </p>
                )}
                {inv.expiresAt && (
                  <p className="text-caption text-[var(--color-text-tertiary)]">
                    {t('invites.expiresAt')}: {formatDateTime(inv.expiresAt, timezone, locale)}
                  </p>
                )}
                <div className="flex items-center justify-between">
                  <span
                    className="rounded-full px-2 py-0.5 text-caption font-medium"
                    style={{
                      backgroundColor: inv.useCount
                        ? 'var(--color-danger-bg)'
                        : 'var(--color-accent-bg)',
                      color: inv.useCount ? 'var(--color-danger)' : 'var(--color-accent-ink)',
                    }}
                  >
                    {inv.useCount ? t('share.inviteUsed') : t('share.inviteUnused')}
                  </span>
                  <button
                    type="button"
                    onClick={() => setPendingRevoke(inv.id)}
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
        onClick={() => createInvite({ role, expiryHours: expiry, maxUses: uses })}
        disabled={busy === 'invite'}
        className={`${joinInvites.length > 0 ? 'btn-secondary' : 'btn-primary'} w-full text-body disabled:opacity-50`}
      >
        {busy === 'invite'
          ? t('share.creating')
          : joinInvites.length > 0
            ? t('share.createAnotherInvite')
            : t('share.createInvite')}
      </button>

      <ConfirmSheet
        open={pendingRevoke !== null}
        title={t('confirm.revokeInvite')}
        confirmLabel={t('confirm.revoke')}
        busy={revoking}
        onConfirm={async () => {
          if (!pendingRevoke) return;
          setRevoking(true);
          await revokeInvite(pendingRevoke);
          setRevoking(false);
          setPendingRevoke(null);
        }}
        onCancel={() => setPendingRevoke(null)}
      />
    </section>
  );
}
