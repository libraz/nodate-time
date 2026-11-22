import { DateTime } from 'luxon';
import type { ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { encode } from 'uqr';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { LoadFailureBanner } from '@/components/load-failure';
import { useT } from '@/i18n';
import type { components } from '@/lib/api-schema';
import { client, errorMessage, isAbortError } from '@/lib/client';
import { copyText } from '@/lib/clipboard';
import { formatDateTime } from '@/lib/format-datetime';
import { toast } from '@/lib/toast';
import { useUiStore } from '@/stores/ui-store';

type AppPassword = components['schemas']['AppPasswordResponse'];
type CreatedAppPassword = components['schemas']['CreatedAppPasswordResponse'];
type ProfileLink = components['schemas']['ProfileLinkResponse'];

/** How many live app passwords one account may hold. */
const APP_PASSWORD_LIMIT = 20;

/** The plaintext in groups of four, which is easier to read off and to type. */
function groupPassword(password: string): string {
  return (password.match(/.{1,4}/g) ?? []).join(' ');
}

/**
 * The body of the calendar-app sync section: the account's app passwords, the
 * form that makes a new one, and the instructions for using it.
 *
 * A new password is shown in a card inside the section and only once; the
 * server keeps a hash, so closing the card is the end of it.
 */
export function AppPasswords() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);

  const [items, setItems] = useState<AppPassword[] | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedAppPassword | null>(null);
  const [profile, setProfile] = useState<ProfileLink | null>(null);
  const [profiling, setProfiling] = useState(false);
  // Outlives the card so the steps keep naming the host the server reported.
  const [serverHost, setServerHost] = useState<string | null>(null);
  const [pendingRevoke, setPendingRevoke] = useState<AppPassword | null>(null);
  const [revoking, setRevoking] = useState(false);

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const { data } = await client.GET('/user/app-passwords', signal ? { signal } : {});
      setItems(data ?? []);
      setLoadFailed(false);
    } catch (e) {
      if (isAbortError(e)) return;
      setLoadFailed(true);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const atLimit = (items?.length ?? 0) >= APP_PASSWORD_LIMIT;
  const trimmed = name.trim();
  const busy = creating || profiling;

  const create = async () => {
    setCreating(true);
    setCreateError(null);
    try {
      const { data } = await client.POST('/user/app-passwords', { body: { name: trimmed } });
      if (!data) return;
      setCreated(data);
      setServerHost(data.serverHost);
      setName('');
      setItems((cur) => [
        { id: data.id, name: data.name, prefix: data.prefix, createdAt: data.createdAt },
        ...(cur ?? []),
      ]);
    } catch (e) {
      setCreateError(errorMessage(e));
    } finally {
      setCreating(false);
    }
  };

  const createProfile = async () => {
    setProfiling(true);
    setCreateError(null);
    try {
      const { data } = await client.POST('/user/app-passwords/profile-links', {
        body: { name: trimmed },
      });
      if (!data) return;
      setProfile(data);
      setName('');
    } catch (e) {
      setCreateError(errorMessage(e));
    } finally {
      setProfiling(false);
    }
  };

  // The row is made when the device first opens the link, so the list is stale until then.
  const closeProfile = () => {
    setProfile(null);
    void load();
  };

  const revoke = async (target: AppPassword) => {
    setRevoking(true);
    try {
      await client.DELETE('/user/app-passwords/{appPasswordId}', {
        params: { path: { appPasswordId: target.id } },
      });
      setItems((cur) => (cur ?? []).filter((p) => p.id !== target.id));
      toast.success(t('appPasswords.revoked'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setRevoking(false);
      setPendingRevoke(null);
    }
  };

  const copyPassword = async (password: string) => {
    if (await copyText(password)) toast.success(t('common.copied'));
    else toast.error(t('common.copyFailed'));
  };

  const date = (iso: string) => formatDateTime(iso, timezone, locale, DateTime.DATE_MED);

  return (
    <div className="space-y-5">
      {loadFailed && (
        <div className="overflow-hidden rounded-[var(--radius-sm)]">
          <LoadFailureBanner title={t('appPasswords.loadFailed')} onRetry={() => load()} />
        </div>
      )}

      {created && (
        <CreatedCard
          created={created}
          onCopy={() => copyPassword(created.password)}
          onClose={() => setCreated(null)}
        />
      )}

      {profile && (
        <ProfileCard
          profile={profile}
          expires={formatDateTime(profile.expiresAt, timezone, locale, DateTime.TIME_SIMPLE)}
          onClose={closeProfile}
        />
      )}

      {items !== null &&
        (items.length === 0 ? (
          <p className="text-body text-[var(--color-text-secondary)]">{t('appPasswords.empty')}</p>
        ) : (
          <ul className="-mt-2 divide-y divide-[var(--color-separator)]">
            {items.map((p) => (
              <li key={p.id} className="flex items-center gap-3 py-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-default text-[var(--color-text-primary)]">{p.name}</p>
                  <p className="truncate font-mono text-footnote text-[var(--color-text-secondary)]">
                    {p.prefix}…
                  </p>
                  <p className="text-footnote tabular-nums text-[var(--color-text-secondary)]">
                    {p.lastUsedAt
                      ? t('appPasswords.lastUsed', { date: date(p.lastUsedAt) })
                      : t('appPasswords.neverUsed')}
                    {' · '}
                    {t('appPasswords.createdOn', { date: date(p.createdAt) })}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setPendingRevoke(p)}
                  className="btn-secondary btn-sm btn-danger shrink-0"
                >
                  {t('appPasswords.revoke')}
                </button>
              </li>
            ))}
          </ul>
        ))}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (trimmed && !busy && !atLimit && items !== null) void createProfile();
        }}
      >
        <label
          htmlFor="app-password-name"
          className="mb-1.5 block text-body font-medium text-[var(--color-text-primary)]"
        >
          {t('appPasswords.nameLabel')}
        </label>
        <div className="flex flex-col gap-2 sm:flex-row">
          <input
            id="app-password-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('appPasswords.namePlaceholder')}
            maxLength={100}
            className="input-modern w-full sm:max-w-[280px]"
          />
          <button
            type="submit"
            disabled={!trimmed || busy || atLimit || items === null}
            className="btn-primary px-5 text-default disabled:opacity-50"
          >
            {profiling ? t('appPasswords.creating') : t('appPasswords.profileCreate')}
          </button>
          <button
            type="button"
            onClick={() => void create()}
            disabled={!trimmed || busy || atLimit || items === null}
            className="btn-secondary px-5 text-default disabled:opacity-50"
          >
            {creating ? t('appPasswords.creating') : t('appPasswords.create')}
          </button>
        </div>
        <p className="mt-1.5 text-footnote text-[var(--color-text-secondary)]">
          {t('appPasswords.profileRecommended')}
        </p>
        {atLimit && (
          <p className="mt-1.5 text-footnote text-[var(--color-text-secondary)]">
            {t('appPasswords.limitReached', { max: String(APP_PASSWORD_LIMIT) })}
          </p>
        )}
        {createError && (
          <p role="alert" className="mt-1.5 text-footnote text-[var(--color-danger)]">
            {createError}
          </p>
        )}
      </form>

      <details className="rounded-[var(--radius-sm)] border border-[var(--color-border)] px-4 py-3">
        <summary className="cursor-pointer list-none text-default font-medium text-[var(--color-text-primary)]">
          {t('appPasswords.stepsTitle')}
        </summary>
        <div className="mt-3 space-y-3 text-body text-[var(--color-text-secondary)]">
          <p>{t('appPasswords.stepsServer', { server: serverHost ?? window.location.host })}</p>
          <div>
            <p className="font-medium text-[var(--color-text-primary)]">
              {t('appPasswords.stepsIphone')}
            </p>
            <p>{t('appPasswords.stepsIphonePath')}</p>
          </div>
          <div>
            <p className="font-medium text-[var(--color-text-primary)]">
              {t('appPasswords.stepsMac')}
            </p>
            <p>{t('appPasswords.stepsMacPath')}</p>
          </div>
        </div>
      </details>

      <div>
        <h3 className="mb-2 text-default font-medium text-[var(--color-text-primary)]">
          {t('appPasswords.limitsTitle')}
        </h3>
        <ul className="list-disc space-y-1 pl-5 text-footnote text-[var(--color-text-secondary)]">
          <li>{t('appPasswords.limitsHidden')}</li>
          <li>{t('appPasswords.limitsViewer')}</li>
          <li>{t('appPasswords.limitsOthers')}</li>
          <li>{t('appPasswords.limitsMove')}</li>
          <li>{t('appPasswords.limitsRepeat')}</li>
        </ul>
      </div>

      <ConfirmSheet
        open={pendingRevoke !== null}
        title={t('appPasswords.confirmRevoke', { name: pendingRevoke?.name ?? '' })}
        hint={t('appPasswords.confirmRevokeHint')}
        confirmLabel={t('appPasswords.revoke')}
        busy={revoking}
        onConfirm={() => pendingRevoke && revoke(pendingRevoke)}
        onCancel={() => setPendingRevoke(null)}
      />
    </div>
  );
}

interface CreatedCardProps {
  created: CreatedAppPassword;
  onCopy: () => void;
  onClose: () => void;
}

/** The one place the plaintext appears, with what a calendar app asks for. */
function CreatedCard({ created, onCopy, onClose }: CreatedCardProps) {
  const t = useT();
  return (
    <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-4">
      <p className="text-default font-semibold text-[var(--color-text-primary)]">
        {t('appPasswords.createdTitle')}
      </p>
      <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
        {t('appPasswords.shownOnce')}
      </p>
      <dl className="mt-3 space-y-2 text-body">
        <CardRow label={t('appPasswords.server')}>
          <span className="break-all font-mono">{created.serverHost}</span>
        </CardRow>
        <CardRow label={t('appPasswords.username')}>
          <span className="break-all font-mono">{created.username}</span>
        </CardRow>
        <CardRow label={t('appPasswords.password')}>
          <span className="break-all font-mono tracking-wide">
            {groupPassword(created.password)}
          </span>
        </CardRow>
      </dl>
      <div className="mt-4 flex gap-2">
        <button type="button" onClick={onCopy} className="btn-primary px-5 text-default">
          {t('appPasswords.copyPassword')}
        </button>
        <button type="button" onClick={onClose} className="btn-secondary px-5 text-default">
          {t('common.close')}
        </button>
      </div>
    </div>
  );
}

interface ProfileCardProps {
  profile: ProfileLink;
  expires: string;
  onClose: () => void;
}

/** The link that installs the calendar account, as a QR code and a plain link. */
function ProfileCard({ profile, expires, onClose }: ProfileCardProps) {
  const t = useT();
  return (
    <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-4">
      <p className="text-default font-semibold text-[var(--color-text-primary)]">
        {t('appPasswords.profileTitle')}
      </p>
      <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
        {t('appPasswords.profileExpires', { time: expires })}
      </p>
      <div className="mt-3 hidden sm:block">
        <QrCode value={profile.url} label={t('appPasswords.profileQrLabel')} />
      </div>
      <ul className="mt-3 space-y-1 text-body text-[var(--color-text-secondary)]">
        <li>{t('appPasswords.profileIphone')}</li>
        <li>{t('appPasswords.profileMac')}</li>
        <li>{t('appPasswords.profileAndroid')}</li>
      </ul>
      <p className="mt-3 text-footnote text-[var(--color-danger)]">
        {t('appPasswords.profileWarning')}
      </p>
      <div className="mt-4 flex gap-2">
        <a href={profile.url} className="btn-primary px-5 text-default">
          {t('appPasswords.profileOpen')}
        </a>
        <button type="button" onClick={onClose} className="btn-secondary px-5 text-default">
          {t('common.close')}
        </button>
      </div>
    </div>
  );
}

/** Quiet zone in modules, as the QR specification asks of a scanner's input. */
const QR_QUIET_ZONE = 4;
const QR_MODULE_PX = 5;

/** Always dark on white, whatever the theme, so a camera can read it. */
function QrCode({ value, label }: { value: string; label: string }) {
  const { size, data } = useMemo(() => encode(value, { ecc: 'M', border: QR_QUIET_ZONE }), [value]);
  const rects = [];
  for (let y = 0; y < size; y++) {
    const row = data[y] ?? [];
    for (let x = 0; x < size; x++) {
      if (row[x]) rects.push(<rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} />);
    }
  }
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${size} ${size}`}
      width={size * QR_MODULE_PX}
      height={size * QR_MODULE_PX}
      shapeRendering="crispEdges"
      className="h-auto max-w-full rounded-[var(--radius-sm)]"
    >
      <rect width={size} height={size} fill="#ffffff" />
      <g fill="#000000">{rects}</g>
    </svg>
  );
}

function CardRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 sm:flex-row sm:gap-3">
      <dt className="shrink-0 text-footnote text-[var(--color-text-secondary)] sm:w-24 sm:pt-0.5">
        {label}
      </dt>
      <dd className="min-w-0 text-[var(--color-text-primary)]">{children}</dd>
    </div>
  );
}
