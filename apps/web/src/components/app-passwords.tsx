import { DateTime } from 'luxon';
import type { ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { encode } from 'uqr';
import { Callout } from '@/components/callout';
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

/** The kind of device this browser runs on, as far as setting up sync goes. */
type Device = 'iphone' | 'ipad' | 'mac' | 'android' | 'other';

const DEVICE_NAMES: Record<Device, string> = {
  iphone: 'iPhone',
  ipad: 'iPad',
  mac: 'Mac',
  android: 'Android',
  other: '',
};

/** The device this browser runs on. iPadOS reports a Mac, told apart by touch. */
function detectDevice(): Device {
  const ua = navigator.userAgent;
  if (/iPhone|iPod/.test(ua)) return 'iphone';
  if (/iPad/.test(ua)) return 'ipad';
  if (/Android/.test(ua)) return 'android';
  if (/Macintosh/.test(ua)) return navigator.maxTouchPoints > 1 ? 'ipad' : 'mac';
  return 'other';
}

/**
 * Where the new account goes: this Apple device by its profile, an iPhone or
 * iPad by a QR code shown here, a Mac from another computer (which has to
 * open this page on the Mac), or any app by a password typed in by hand.
 */
type Target = 'here' | 'qr' | 'mac' | 'manual';

/** The choices offered on each device, the first being the default. */
const TARGETS: Record<Device, readonly [Target, ...Target[]]> = {
  mac: ['here', 'qr', 'manual'],
  iphone: ['here', 'manual'],
  ipad: ['here', 'manual'],
  android: ['manual', 'qr'],
  other: ['qr', 'mac', 'manual'],
};

/** The name a target suggests for the new device. */
function suggestedName(target: Target, device: Device): string {
  switch (target) {
    case 'here':
      return DEVICE_NAMES[device];
    case 'qr':
      return 'iPhone';
    case 'mac':
      return 'Mac';
    case 'manual':
      return device === 'android' ? 'Android' : '';
  }
}

/** How often the list is re-read while a profile link waits to be opened. */
const PROFILE_POLL_MS = 4000;

/** The plaintext in groups of four, which is easier to read off and to type. */
function groupPassword(password: string): string {
  return (password.match(/.{1,4}/g) ?? []).join(' ');
}

/**
 * The body of the calendar-app sync section: adding a device (by profile link,
 * or by a password typed in by hand), the devices that sync, and how to stop.
 *
 * A manual password is shown once; the server keeps a hash, so closing the
 * card is the end of it. A profile link is watched until the device connects.
 */
export function AppPasswords() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);

  const [items, setItems] = useState<AppPassword[] | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [device] = useState(detectDevice);
  const targets = TARGETS[device];
  const [target, setTarget] = useState<Target>(targets[0]);
  const [name, setName] = useState(() => suggestedName(targets[0], device));
  // A name the person typed is theirs; switching the target no longer replaces it.
  const [nameTouched, setNameTouched] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedAppPassword | null>(null);
  const [profile, setProfile] = useState<PendingProfile | null>(null);
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
  const canStart = Boolean(trimmed) && !busy && !atLimit && items !== null && target !== 'mac';

  const chooseTarget = (next: Target) => {
    setTarget(next);
    if (!nameTouched) setName(suggestedName(next, device));
  };

  const resetForm = () => {
    setTarget(targets[0]);
    setName(suggestedName(targets[0], device));
    setNameTouched(false);
  };

  const create = async () => {
    setCreating(true);
    setCreateError(null);
    try {
      const { data } = await client.POST('/user/app-passwords', { body: { name: trimmed } });
      if (!data) return;
      setCreated(data);
      setServerHost(data.serverHost);
      resetForm();
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

  const createProfile = async (deviceName: string, onThisDevice: boolean) => {
    setProfiling(true);
    setCreateError(null);
    try {
      const { data } = await client.POST('/user/app-passwords/profile-links', {
        body: { name: deviceName },
      });
      if (!data) return;
      setProfile({
        ...data,
        name: deviceName,
        known: new Set((items ?? []).map((p) => p.id)),
        installOn: onThisDevice && device !== 'other' && device !== 'android' ? device : null,
      });
      resetForm();
    } catch (e) {
      setCreateError(errorMessage(e));
    } finally {
      setProfiling(false);
    }
  };

  // The row appears when the device opens the link and gains a last use when it syncs.
  const added = profile
    ? (items ?? []).find((p) => !profile.known.has(p.id) && p.name === profile.name)
    : undefined;
  const progress: ProfileProgress = added ? (added.lastUsedAt ? 'synced' : 'opened') : 'waiting';

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
    <div className="space-y-6">
      {loadFailed && (
        <div className="overflow-hidden rounded-[var(--radius-sm)]">
          <LoadFailureBanner title={t('appPasswords.loadFailed')} onRetry={() => load()} />
        </div>
      )}

      {profile ? (
        <ProfileCard
          profile={profile}
          progress={progress}
          poll={() => load()}
          recreating={profiling}
          onRecreate={() => void createProfile(profile.name, profile.installOn !== null)}
          onClose={closeProfile}
        />
      ) : created ? (
        <CreatedCard
          created={created}
          onCopy={() => copyPassword(created.password)}
          onClose={() => setCreated(null)}
        />
      ) : (
        <form
          aria-labelledby="app-password-add-title"
          onSubmit={(e) => {
            e.preventDefault();
            if (!canStart) return;
            if (target === 'manual') void create();
            else void createProfile(trimmed, target === 'here');
          }}
        >
          <h3
            id="app-password-add-title"
            className="text-default font-semibold text-[var(--color-text-primary)]"
          >
            {t('appPasswords.addTitle')}
          </h3>
          <div
            role="radiogroup"
            aria-label={t('appPasswords.targetLabel')}
            className="mt-3 flex w-full gap-1 rounded-[var(--radius-sm)] bg-[var(--color-surface-inset)] p-1"
          >
            {targets.map((option) => (
              // biome-ignore lint/a11y/useSemanticElements: a segmented control, styled as one row of buttons
              <button
                key={option}
                type="button"
                role="radio"
                aria-checked={target === option}
                onClick={() => chooseTarget(option)}
                className="min-w-0 flex-1 basis-0 truncate rounded-[calc(var(--radius-sm)-4px)] px-2 py-1.5 text-footnote font-medium text-[var(--color-text-secondary)] transition-colors aria-checked:bg-[var(--color-surface)] aria-checked:text-[var(--color-text-primary)] aria-checked:shadow-sm"
              >
                {targetLabel(t, option, device)}
              </button>
            ))}
          </div>
          <p className="mt-2 text-footnote text-[var(--color-text-secondary)]">
            {targetHint(t, target, device)}
          </p>

          {target !== 'mac' && (
            <>
              <label
                htmlFor="app-password-name"
                className="mt-4 mb-1.5 block text-body font-medium text-[var(--color-text-primary)]"
              >
                {t('appPasswords.nameLabel')}
              </label>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                <div className="min-w-0 sm:w-[280px]">
                  <input
                    id="app-password-name"
                    type="text"
                    value={name}
                    onChange={(e) => {
                      setName(e.target.value);
                      setNameTouched(true);
                    }}
                    placeholder={t('appPasswords.namePlaceholder')}
                    aria-describedby="app-password-name-hint"
                    maxLength={100}
                    className="input-modern w-full"
                  />
                  <p
                    id="app-password-name-hint"
                    className="mt-1.5 text-footnote text-[var(--color-text-tertiary)]"
                  >
                    {t('appPasswords.nameHint')}
                  </p>
                </div>
                <button
                  type="submit"
                  disabled={!canStart}
                  className="btn-primary px-5 text-default disabled:opacity-50"
                >
                  {busy ? t('appPasswords.creating') : actionLabel(t, target, device)}
                </button>
              </div>
            </>
          )}
          {atLimit && (
            <p className="mt-2 text-footnote text-[var(--color-text-secondary)]">
              {t('appPasswords.limitReached', { max: String(APP_PASSWORD_LIMIT) })}
            </p>
          )}
          {createError && (
            <p role="alert" className="mt-2 text-footnote text-[var(--color-danger)]">
              {createError}
            </p>
          )}
        </form>
      )}

      <section aria-labelledby="app-password-devices-title">
        <div className="flex items-baseline justify-between gap-3">
          <h3
            id="app-password-devices-title"
            className="text-default font-semibold text-[var(--color-text-primary)]"
          >
            {t('appPasswords.devicesTitle')}
          </h3>
          {items !== null && items.length > 0 && (
            <span className="text-footnote tabular-nums text-[var(--color-text-tertiary)]">
              {t('appPasswords.devicesCount', {
                count: String(items.length),
                max: String(APP_PASSWORD_LIMIT),
              })}
            </span>
          )}
        </div>
        {items !== null &&
          (items.length === 0 ? (
            <p className="mt-2 text-body text-[var(--color-text-secondary)]">
              {t('appPasswords.empty')}
            </p>
          ) : (
            <ul className="mt-1 divide-y divide-[var(--color-separator)]">
              {items.map((p) => (
                <li key={p.id} className="flex items-center gap-3 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="flex items-center gap-2 text-default text-[var(--color-text-primary)]">
                      <span
                        aria-hidden="true"
                        className={`size-2 shrink-0 rounded-full ${p.lastUsedAt ? 'bg-[var(--color-success)]' : 'bg-[var(--color-border-strong)]'}`}
                      />
                      <span className="truncate">{p.name}</span>
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
      </section>

      <div className="space-y-2">
        <HelpDisclosure title={t('appPasswords.stopTitle')}>
          <ol className="list-decimal space-y-2 pl-5">
            <li>{t('appPasswords.stopStepRevoke')}</li>
            <li>{t('appPasswords.stopStepIphone')}</li>
            <li>{t('appPasswords.stopStepMac')}</li>
          </ol>
          <p>{t('appPasswords.stopNotePassword')}</p>
          <p>{t('appPasswords.stopNoteMember')}</p>
        </HelpDisclosure>
        <HelpDisclosure title={t('appPasswords.stepsTitle')}>
          <p>{t('appPasswords.whatIsAppPassword')}</p>
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
        </HelpDisclosure>
        <HelpDisclosure title={t('appPasswords.limitsTitle')}>
          <ul className="list-disc space-y-1 pl-5">
            <li>{t('appPasswords.limitsHidden')}</li>
            <li>{t('appPasswords.limitsViewer')}</li>
            <li>{t('appPasswords.limitsOthers')}</li>
            <li>{t('appPasswords.limitsMove')}</li>
            <li>{t('appPasswords.limitsTentative')}</li>
            <li>{t('appPasswords.limitsRepeat')}</li>
          </ul>
        </HelpDisclosure>
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

/** A profile link with the device it is for and the rows that existed before it. */
interface PendingProfile extends ProfileLink {
  name: string;
  known: Set<string>;
  /** The device this browser is, when the profile is for it; null shows a QR code. */
  installOn: 'iphone' | 'ipad' | 'mac' | null;
}

type T = ReturnType<typeof useT>;

function targetLabel(t: T, target: Target, device: Device): string {
  switch (target) {
    case 'here':
      return t('appPasswords.targetHere', { device: DEVICE_NAMES[device] });
    case 'qr':
      return t('appPasswords.targetQr');
    case 'mac':
      return t('appPasswords.targetMac');
    case 'manual':
      return device === 'android'
        ? t('appPasswords.targetHere', { device: DEVICE_NAMES.android })
        : t('appPasswords.targetManual');
  }
}

function targetHint(t: T, target: Target, device: Device): string {
  switch (target) {
    case 'here':
      return t('appPasswords.hintHere', { device: DEVICE_NAMES[device] });
    case 'qr':
      return t('appPasswords.hintQr');
    case 'mac':
      return t('appPasswords.hintMac');
    case 'manual':
      return t('appPasswords.hintManual');
  }
}

function actionLabel(t: T, target: Target, device: Device): string {
  switch (target) {
    case 'here':
      return t('appPasswords.actionHere', { device: DEVICE_NAMES[device] });
    case 'qr':
      return t('appPasswords.actionQr');
    case 'mac':
    case 'manual':
      return t('appPasswords.actionManual');
  }
}

type ProfileProgress = 'waiting' | 'opened' | 'synced';

/** A collapsed block of help text below the device list. */
function HelpDisclosure({ title, children }: { title: string; children: ReactNode }) {
  return (
    <details className="group rounded-[var(--radius-sm)] border border-[var(--color-border)] px-4 py-3">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-default font-medium text-[var(--color-text-primary)]">
        {title}
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
          className="shrink-0 text-[var(--color-text-tertiary)] transition-transform group-open:rotate-180"
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </summary>
      <div className="mt-3 space-y-3 text-body text-[var(--color-text-secondary)]">{children}</div>
    </details>
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
    <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-4 sm:p-5">
      <p className="text-default font-semibold text-[var(--color-text-primary)]">
        {t('appPasswords.createdTitle')}
      </p>
      <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
        {t('appPasswords.whatIsAppPassword')}
      </p>
      <dl className="mt-4 space-y-2 text-body">
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
      <Callout tone="warning" className="mt-4">
        {t('appPasswords.shownOnce')}
      </Callout>
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
  profile: PendingProfile;
  progress: ProfileProgress;
  poll: () => void;
  recreating: boolean;
  onRecreate: () => void;
  onClose: () => void;
}

/** Remaining time as m:ss. */
function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
}

/**
 * The link that installs the calendar account: a plain link when the profile
 * is for this device, else a QR code for the iPhone or iPad to scan, with a
 * status that follows the device.
 */
function ProfileCard({
  profile,
  progress,
  poll,
  recreating,
  onRecreate,
  onClose,
}: ProfileCardProps) {
  const t = useT();
  const expiresAt = useMemo(() => Date.parse(profile.expiresAt), [profile.expiresAt]);
  const [now, setNow] = useState(() => Date.now());
  const remaining = expiresAt - now;
  // Once the device has the profile, the link running out no longer matters.
  const expired = remaining <= 0 && progress === 'waiting';
  const pollRef = useRef(poll);
  pollRef.current = poll;

  const ticking = progress !== 'synced' && remaining > 0;
  const polling = progress !== 'synced' && !expired;

  useEffect(() => {
    if (!ticking) return;
    const tick = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(tick);
  }, [ticking]);

  useEffect(() => {
    if (!polling) return;
    const timer = window.setInterval(() => pollRef.current(), PROFILE_POLL_MS);
    return () => window.clearInterval(timer);
  }, [polling]);

  const here = profile.installOn;
  const status = {
    waiting: t('appPasswords.profileWaiting'),
    opened: t('appPasswords.profileOpened'),
    synced: t('appPasswords.profileSynced'),
  }[progress];

  return (
    <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-4 sm:p-5">
      <div className="flex items-baseline justify-between gap-3">
        <p className="min-w-0 truncate text-default font-semibold text-[var(--color-text-primary)]">
          {t('appPasswords.profileTitle', { name: profile.name })}
        </p>
        {!expired && progress === 'waiting' && (
          <span className="shrink-0 text-footnote tabular-nums text-[var(--color-text-secondary)]">
            {t('appPasswords.profileRemaining', { time: formatRemaining(remaining) })}
          </span>
        )}
      </div>

      <div
        className={here ? 'mt-4' : 'mt-4 sm:grid sm:grid-cols-[auto_1fr] sm:items-start sm:gap-6'}
      >
        {!here && (
          <div className="relative mb-4 flex justify-center sm:mb-0 sm:block">
            <QrCode value={profile.url} label={t('appPasswords.profileQrLabel')} />
            {expired && (
              <div className="absolute inset-0 rounded-[var(--radius-sm)] bg-[var(--color-surface-inset)]/90" />
            )}
          </div>
        )}
        <ol className="space-y-3 text-body text-[var(--color-text-secondary)]">
          <Step n={1}>
            {here === 'mac'
              ? t('appPasswords.profileStepOpenMac')
              : here
                ? t('appPasswords.profileStepOpenHere')
                : t('appPasswords.profileStepScan')}
          </Step>
          <Step n={2}>
            {here === 'mac'
              ? t('appPasswords.profileStepInstallMac')
              : t('appPasswords.profileStepInstallIos')}
          </Step>
          <Step n={3}>{t('appPasswords.profileStepDone')}</Step>
        </ol>
      </div>

      <p
        aria-live="polite"
        className="mt-4 flex items-center gap-2 text-footnote text-[var(--color-text-primary)]"
      >
        {expired ? (
          <span className="text-[var(--color-text-secondary)]">
            {t('appPasswords.profileExpired')}
          </span>
        ) : (
          <>
            <span
              aria-hidden="true"
              className={`size-2 shrink-0 rounded-full ${progress === 'synced' ? 'bg-[var(--color-success)]' : 'animate-pulse bg-[var(--color-accent)]'}`}
            />
            {status}
          </>
        )}
      </p>

      {progress === 'waiting' && !expired && (
        <Callout tone="warning" className="mt-4">
          {t('appPasswords.profileWarning')}
        </Callout>
      )}

      <div className="mt-4 flex flex-col gap-2 sm:flex-row">
        {expired ? (
          <button
            type="button"
            onClick={onRecreate}
            disabled={recreating}
            className="btn-primary px-5 text-default disabled:opacity-50"
          >
            {recreating ? t('appPasswords.creating') : t('appPasswords.profileRecreate')}
          </button>
        ) : (
          progress === 'waiting' &&
          here && (
            <a
              href={profile.url}
              className="btn-primary inline-flex items-center justify-center px-5 text-default"
            >
              {t('appPasswords.profileOpen')}
            </a>
          )
        )}
        <button
          type="button"
          onClick={onClose}
          className={`${progress === 'synced' ? 'btn-primary' : 'btn-secondary'} px-5 text-default`}
        >
          {t('common.close')}
        </button>
      </div>
    </div>
  );
}

function Step({ n, children }: { n: number; children: ReactNode }) {
  return (
    <li className="flex gap-3">
      <span
        aria-hidden="true"
        className="flex size-6 shrink-0 items-center justify-center rounded-full bg-[var(--color-accent-subtle)] text-footnote font-semibold tabular-nums text-[var(--color-accent)]"
      >
        {n}
      </span>
      <span className="min-w-0 pt-0.5">{children}</span>
    </li>
  );
}

/** Quiet zone in modules, as the QR specification asks of a scanner's input. */
const QR_QUIET_ZONE = 4;
const QR_MODULE_PX = 4;

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
      className="h-auto w-[240px] max-w-full rounded-[var(--radius-sm)]"
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
