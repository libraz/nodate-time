import { useState } from 'react';
import { Callout } from '@/components/callout';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { TipCard } from '@/components/guide';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { SidePanel } from '@/components/side-panel';
import { UrlRow } from '@/components/url-row';
import { useT } from '@/i18n';
import { copyText } from '@/lib/clipboard';
import { canManage } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useInvites } from '@/lib/use-invites';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { usePresence } from '@/lib/use-presence';
import { useUiStore } from '@/stores/ui-store';
import type { Calendar } from '@/types/calendar';

const manages = (calendar: Calendar) => canManage(calendar.role);

/**
 * The calendar's public link and embed code, for people who only look and
 * never join. Bringing people in as members is the members panel's job.
 */
export function PublicPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const presence = usePresence(rightPanel === 'public');
  // Publishing requires admin; only offer calendars the user administers and is viewing.
  const { options, target, setTargetId } = usePanelCalendar(manages);
  const calendarId = target?.id ?? '';

  const [embedCopied, setEmbedCopied] = useState(false);
  const [confirmingPublic, setConfirmingPublic] = useState(false);
  const [confirmingRevoke, setConfirmingRevoke] = useState(false);
  const [confirmBusy, setConfirmBusy] = useState(false);

  const { publicLink, busy, createPublicLink, revokeInvite } = useInvites(
    calendarId,
    presence.mounted,
  );

  const origin = typeof window === 'undefined' ? '' : window.location.origin;
  const publicUrl = publicLink?.token ? `${origin}/embed/${publicLink.token}` : '';
  const embedSnippet = publicUrl
    ? `<iframe src="${publicUrl}" width="100%" height="640" style="border:1px solid #e5e7eb;border-radius:12px" loading="lazy"></iframe>`
    : '';

  const copyEmbed = async () => {
    if (!(await copyText(embedSnippet))) {
      toast.error(t('common.copyFailed'));
      return;
    }
    setEmbedCopied(true);
    setTimeout(() => setEmbedCopied(false), 2000);
  };

  if (!presence.mounted) return null;

  return (
    <SidePanel
      presence={presence}
      onClose={() => toggleRightPanel('public')}
      title={t('panel.public')}
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

            <TipCard id="public" topic="invite">
              {t('tip.public')}
            </TipCard>

            <section className="space-y-2">
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
                  className="btn-primary w-full text-body disabled:opacity-50"
                >
                  {busy === 'public' ? t('share.creating') : t('share.createPublic')}
                </button>
              ) : (
                <div className="space-y-3">
                  {publicUrl ? (
                    <>
                      <Callout tone="warning">{t('invites.linkShownOnce')}</Callout>
                      <UrlRow value={publicUrl} copyLabel={t('share.copyUrl')} />
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
                          onClick={copyEmbed}
                          className="btn-secondary w-full text-footnote"
                        >
                          {embedCopied ? t('common.copied') : t('share.copyEmbed')}
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
                    onClick={() => setConfirmingRevoke(true)}
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
        open={confirmingRevoke}
        title={t('confirm.revokeLink')}
        confirmLabel={t('confirm.revoke')}
        busy={confirmBusy}
        onConfirm={async () => {
          if (!publicLink) return;
          setConfirmBusy(true);
          await revokeInvite(publicLink.id);
          setConfirmBusy(false);
          setConfirmingRevoke(false);
        }}
        onCancel={() => setConfirmingRevoke(false)}
      />
    </SidePanel>
  );
}
