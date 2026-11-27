import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AppPasswords } from '@/components/app-passwords';
import { CalendarCoverImage } from '@/components/calendar-cover-image';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { CustomSelect } from '@/components/custom-select';
import { ImageUploadTile } from '@/components/image-upload-tile';
import { LoadFailureBanner } from '@/components/load-failure';
import { PageTopBar } from '@/components/page-top-bar';
import { SubmitHint } from '@/components/submit-hint';
import { Switch } from '@/components/switch';
import { type Locale, useT } from '@/i18n';
import type { components } from '@/lib/api-schema';
import { useAuthConfig } from '@/lib/auth-config';
import { APP_NAME } from '@/lib/brand';
import {
  ApiError,
  client,
  errorMessage,
  filenameFromDisposition,
  isAbortError,
} from '@/lib/client';
import type { WeekStart } from '@/lib/date-utils';
import { formatDateTime } from '@/lib/format-datetime';
import { detectHolidayCountry } from '@/lib/holidays';
import { isSubmitShortcut } from '@/lib/keys';
import { canManage } from '@/lib/permissions';
import { detectTimezone } from '@/lib/preferences';
import { THEME_OPTIONS } from '@/lib/theme';
import { toast } from '@/lib/toast';
import { useEnsureCalendars } from '@/lib/use-ensure-calendars';
import { useHolidayCountries } from '@/lib/use-holidays';
import { describeUserAgent } from '@/lib/user-agent';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

export interface SettingsSearch {
  tab?: TabId | undefined;
}

const TAB_IDS = ['profile', 'appearance', 'calendars', 'export', 'admin'] as const;
type TabId = (typeof TAB_IDS)[number];

export const Route = createFileRoute('/settings')({
  validateSearch: (search: Record<string, unknown>): SettingsSearch => {
    const raw = typeof search.tab === 'string' ? search.tab : undefined;
    const tab = TAB_IDS.find((t) => t === raw);
    return { tab };
  },
  component: SettingsPage,
});

const TIMEZONE_OPTIONS = [
  'UTC',
  'Asia/Tokyo',
  'Asia/Seoul',
  'Asia/Shanghai',
  'Asia/Singapore',
  'Asia/Bangkok',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'America/New_York',
  'America/Chicago',
  'America/Los_Angeles',
  'Australia/Sydney',
];

interface TabDef {
  id: TabId;
  label: string;
  description: string;
  icon: ReactNode;
}

function tabIcons(): Record<TabId, ReactNode> {
  const stroke = 'currentColor';
  return {
    profile: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke={stroke} strokeWidth="1.8">
        <circle cx="12" cy="8" r="4" />
        <path d="M4 21c1.5-4 4.5-6 8-6s6.5 2 8 6" strokeLinecap="round" />
      </svg>
    ),
    appearance: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke={stroke} strokeWidth="1.8">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 3v18M3 12h18" />
      </svg>
    ),
    calendars: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke={stroke} strokeWidth="1.8">
        <rect x="3" y="5" width="18" height="16" rx="2" />
        <path d="M3 10h18M8 3v4M16 3v4" strokeLinecap="round" />
      </svg>
    ),
    export: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke={stroke} strokeWidth="1.8">
        <path d="M12 4v12M7 11l5 5 5-5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M5 20h14" strokeLinecap="round" />
      </svg>
    ),
    admin: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke={stroke} strokeWidth="1.8">
        <path
          d="M12 2l8 4v6c0 5-3.5 9-8 10-4.5-1-8-5-8-10V6l8-4z"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
        <path d="M9 12l2 2 4-4" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    ),
  };
}

function SettingsPage() {
  const t = useT();
  const navigate = useNavigate();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const search = Route.useSearch();
  const tab = search.tab ?? 'profile';

  // The calendar and export tabs are reachable by URL before anything has
  // loaded the list they offer.
  useEnsureCalendars(isAuthenticated);

  useEffect(() => {
    if (!isAuthenticated) {
      navigate({ to: '/login', search: { redirect: '/settings' } });
    }
  }, [isAuthenticated, navigate]);

  const setTab = useCallback(
    (next: TabId) => {
      navigate({ to: '/settings', search: { tab: next === 'profile' ? undefined : next } });
    },
    [navigate],
  );

  const icons = useMemo(() => tabIcons(), []);
  const me = useAuthStore((s) => s.user);
  const isAdmin = !!me?.isAdmin;
  const tabs: TabDef[] = [
    {
      id: 'profile',
      label: t('settings.profile'),
      description: t('profile.edit'),
      icon: icons.profile,
    },
    {
      id: 'appearance',
      label: t('settings.appearance'),
      description: t('settings.theme'),
      icon: icons.appearance,
    },
    {
      id: 'calendars',
      label: t('settings.calendars'),
      description: t('settings.members'),
      icon: icons.calendars,
    },
    {
      id: 'export',
      label: t('settings.exportImport'),
      description: t('settings.importIcal'),
      icon: icons.export,
    },
    ...(isAdmin
      ? [
          {
            id: 'admin' as TabId,
            label: t('settings.admin'),
            description: t('settings.adminOAuth'),
            icon: icons.admin,
          },
        ]
      : []),
  ];

  return (
    <div className="app-bg h-full">
      <div className="mx-auto flex h-full max-w-[1080px] flex-col px-4 py-6 sm:px-6">
        <PageTopBar to="/" label={t('route.backToCalendar')} />
        <header className="mb-6">
          <h1 className="text-display font-bold text-[var(--color-text-primary)] sm:text-hero">
            {t('settings.title')}
          </h1>
        </header>

        {/* Mobile tab strip: bleeds to the screen edge so the overflow reads as scrollable. */}
        <nav
          className="-mx-4 mb-4 flex gap-2 overflow-x-auto px-4 pb-1 sm:hidden"
          aria-label={t('settings.title')}
        >
          {tabs.map((td) => (
            <button
              key={td.id}
              type="button"
              onClick={() => setTab(td.id)}
              aria-current={tab === td.id ? 'page' : undefined}
              className={`flex shrink-0 items-center gap-2 rounded-full px-4 py-2 text-body font-medium transition ${
                tab === td.id
                  ? 'bg-[var(--color-accent)] text-[var(--color-text-on-accent)] shadow-sm'
                  : 'bg-[var(--color-surface-inset)] text-[var(--color-text-primary)]'
              }`}
            >
              <span aria-hidden>{td.icon}</span>
              {td.label}
            </button>
          ))}
        </nav>

        <div className="flex flex-1 gap-6 overflow-hidden">
          {/* Desktop sidebar */}
          <nav
            aria-label={t('settings.title')}
            className="hidden w-[240px] shrink-0 flex-col gap-1 sm:flex"
          >
            {tabs.map((td) => (
              <button
                key={td.id}
                type="button"
                onClick={() => setTab(td.id)}
                aria-current={tab === td.id ? 'page' : undefined}
                className={`flex items-center gap-3 rounded-[var(--radius-sm)] px-3 py-2.5 text-left transition ${
                  tab === td.id
                    ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)]'
                    : 'text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]'
                }`}
              >
                <span
                  aria-hidden
                  className={`flex h-9 w-9 items-center justify-center rounded-[var(--radius-control)] ${
                    tab === td.id
                      ? 'bg-[var(--color-accent)] text-[var(--color-text-on-accent)]'
                      : 'bg-[var(--color-surface-inset)] text-[var(--color-text-secondary)]'
                  }`}
                >
                  {td.icon}
                </span>
                <span className="flex flex-col">
                  <span className="text-default font-semibold">{td.label}</span>
                  <span className="text-footnote text-[var(--color-text-tertiary)]">
                    {td.description}
                  </span>
                </span>
              </button>
            ))}
          </nav>

          <main key={tab} className="page-scroll calendar-enter flex-1 overflow-y-auto pb-12">
            {tab === 'profile' && <ProfileSection />}
            {tab === 'appearance' && <AppearanceSection />}
            {tab === 'calendars' && <CalendarsSection />}
            {tab === 'export' && <ExportSection />}
            {tab === 'admin' && isAdmin && <AdminSection />}
          </main>
        </div>
      </div>
    </div>
  );
}

interface SectionProps {
  /** Anchor for links that jump straight to the section. */
  id?: string;
  title: string;
  description?: string;
  children: React.ReactNode;
}

/** The image types the avatar and cover endpoints accept. */
const IMAGE_UPLOAD_TYPES = 'image/jpeg,image/png,image/webp';

function Section({ id, title, description, children }: SectionProps) {
  return (
    <section id={id} className="mb-6">
      <header className="mb-3">
        <h2 className="text-subhead font-semibold text-[var(--color-text-primary)]">{title}</h2>
        {description && (
          <p className="mt-0.5 text-body text-[var(--color-text-secondary)]">{description}</p>
        )}
      </header>
      <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-sm">
        {children}
      </div>
    </section>
  );
}

function FieldRow({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string | undefined;
  children: React.ReactNode;
}) {
  return (
    <div className="mb-5 last:mb-0">
      <div className="mb-1.5">
        <span className="text-body font-medium text-[var(--color-text-primary)]">{label}</span>
      </div>
      {children}
      {hint && <p className="mt-1.5 text-footnote text-[var(--color-text-secondary)]">{hint}</p>}
    </div>
  );
}

export function ProfileSection() {
  const t = useT();
  const user = useAuthStore((s) => s.user);
  const updateProfile = useAuthStore((s) => s.updateProfile);
  const uploadAvatar = useAuthStore((s) => s.uploadAvatar);
  const removeAvatar = useAuthStore((s) => s.removeAvatar);
  const changePasswordAction = useAuthStore((s) => s.changePassword);
  const [name, setName] = useState(user?.name ?? '');
  const [saving, setSaving] = useState(false);
  const [avatarBusy, setAvatarBusy] = useState(false);
  const [confirmingAvatar, setConfirmingAvatar] = useState(false);
  const avatarInputRef = useRef<HTMLInputElement>(null);

  const [currentPw, setCurrentPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [pwSaving, setPwSaving] = useState(false);
  const [resendingVerification, setResendingVerification] = useState(false);
  const authConfig = useAuthConfig();

  // The name last loaded from the account; a reload (an avatar upload, say)
  // only overwrites the field when the user has not typed over it.
  const loadedName = useRef<string | null>(null);
  const [justSaved, setJustSaved] = useState(false);
  useEffect(() => {
    if (!user) return;
    setName((current) =>
      loadedName.current === null || current === loadedName.current ? user.name : current,
    );
    loadedName.current = user.name;
  }, [user]);

  const dirty = !!user && name !== user.name;
  useEffect(() => {
    if (dirty) setJustSaved(false);
  }, [dirty]);

  const handleAvatarFile = useCallback(
    async (file: File) => {
      setAvatarBusy(true);
      try {
        await uploadAvatar(file);
        toast.success(t('panel.updated'));
      } catch (e) {
        toast.error(errorMessage(e));
      } finally {
        setAvatarBusy(false);
      }
    },
    [uploadAvatar, t],
  );

  const handleAvatarRemove = useCallback(async () => {
    setAvatarBusy(true);
    try {
      await removeAvatar();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setAvatarBusy(false);
      setConfirmingAvatar(false);
    }
  }, [removeAvatar]);

  const save = async () => {
    setSaving(true);
    try {
      await updateProfile({ name });
      setJustSaved(true);
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const changePassword = async () => {
    if (newPw.length < 8) {
      toast.error(t('profile.passwordMinLength'));
      return;
    }
    setPwSaving(true);
    try {
      await changePasswordAction(currentPw, newPw);
      toast.success(t('profile.passwordChanged'));
      setCurrentPw('');
      setNewPw('');
    } catch (e) {
      toast.error(errorMessage(e, t('profile.passwordChangeFailed')));
    } finally {
      setPwSaving(false);
    }
  };

  const handleResendVerification = async () => {
    setResendingVerification(true);
    try {
      await client.POST('/user/verify-email/resend');
      toast.success(t('profile.verificationSent'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setResendingVerification(false);
    }
  };

  return (
    <>
      {authConfig.status === 'error' && (
        <LoadFailureBanner title={t('error.authConfigUnavailable')} onRetry={authConfig.retry} />
      )}
      <Section title={t('settings.profile')} description={t('profile.edit')}>
        <div className="mb-6 flex items-center gap-4">
          <ImageUploadTile
            label={t('profile.avatar')}
            accept={IMAGE_UPLOAD_TYPES}
            inputRef={avatarInputRef}
            onFile={handleAvatarFile}
            busy={avatarBusy}
            hasImage
            className="h-16 w-16 rounded-[var(--radius-lg)] shadow-sm"
          >
            {user?.avatarUrl ? (
              <img src={user.avatarUrl} alt="" className="h-full w-full object-cover" />
            ) : (
              <span className="flex h-full w-full items-center justify-center bg-[var(--color-accent)] text-hero font-bold text-[var(--color-text-on-accent)]">
                {name ? name.slice(0, 1) : '\u{1F464}'}
              </span>
            )}
          </ImageUploadTile>
          <div className="min-w-0">
            <p className="text-subhead font-semibold text-[var(--color-text-primary)]">
              {name || '—'}
            </p>
            <p className="text-body text-[var(--color-text-secondary)]">{user?.email}</p>
            <div className="mt-1 flex items-center gap-3">
              <button
                type="button"
                onClick={() => avatarInputRef.current?.click()}
                disabled={avatarBusy}
                className="text-caption text-[var(--color-accent-ink)] hover:underline disabled:opacity-50"
              >
                {t('profile.avatar')}
              </button>
              {user?.avatarUrl && (
                <button
                  type="button"
                  onClick={() => setConfirmingAvatar(true)}
                  disabled={avatarBusy}
                  className="text-caption text-[var(--color-danger)] hover:underline disabled:opacity-50"
                >
                  {t('profile.removeAvatar')}
                </button>
              )}
              <ConfirmSheet
                open={confirmingAvatar}
                title={t('confirm.removeAvatar')}
                confirmLabel={t('common.delete')}
                busy={avatarBusy}
                onConfirm={handleAvatarRemove}
                onCancel={() => setConfirmingAvatar(false)}
              />
            </div>
          </div>
        </div>

        {user && user.emailVerified === false && (
          <div role="status" className="callout callout-warning reveal-in mb-5">
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="callout-icon"
              aria-hidden="true"
            >
              <rect x="3" y="5" width="18" height="14" rx="2" />
              <path d="M3 7l9 6 9-6" />
            </svg>
            <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center sm:gap-4">
              <div className="min-w-0 flex-1">
                <p className="callout-title text-default">{t('profile.emailUnverified')}</p>
                <p className="mt-0.5 text-caption text-[var(--color-text-secondary)]">
                  {t('profile.emailUnverifiedHint')}
                </p>
              </div>
              {authConfig.status === 'ready' && authConfig.mailEnabled && (
                <button
                  type="button"
                  onClick={handleResendVerification}
                  disabled={resendingVerification}
                  className="btn-secondary btn-sm shrink-0 self-start sm:self-center"
                >
                  {t('profile.resendVerification')}
                </button>
              )}
            </div>
          </div>
        )}

        <FieldRow label={t('profile.name')}>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="input-modern w-full"
            autoComplete="name"
          />
        </FieldRow>
        <div className="mt-5 flex items-center gap-3">
          <button
            type="button"
            onClick={save}
            disabled={saving || !dirty}
            className="btn-primary px-5 text-default"
          >
            {saving ? t('common.saving') : t('common.save')}
          </button>
          {justSaved && !dirty && !saving && (
            <span className="text-footnote text-[var(--color-text-tertiary)]">
              {t('panel.updated')}
            </span>
          )}
        </div>
      </Section>

      {authConfig.status === 'ready' && authConfig.passwordEnabled && user?.hasPassword && (
        <Section title={t('settings.security')}>
          <FieldRow label={t('profile.currentPassword')}>
            <input
              type="password"
              value={currentPw}
              onChange={(e) => setCurrentPw(e.target.value)}
              className="input-modern w-full"
              autoComplete="current-password"
            />
          </FieldRow>
          <FieldRow label={t('profile.newPassword')} hint={t('auth.passwordMinLength')}>
            <input
              type="password"
              value={newPw}
              onChange={(e) => setNewPw(e.target.value)}
              className="input-modern w-full"
              autoComplete="new-password"
              minLength={8}
            />
          </FieldRow>
          <button
            type="button"
            onClick={changePassword}
            disabled={pwSaving || !currentPw || !newPw}
            className="btn-primary px-5 text-default"
          >
            {pwSaving ? t('profile.changing') : t('profile.changePassword')}
          </button>
        </Section>
      )}

      <SessionsSection />

      <Section
        id="app-passwords"
        title={t('settings.appPasswords')}
        description={t('appPasswords.description', { app: APP_NAME })}
      >
        <AppPasswords />
      </Section>
    </>
  );
}

interface SessionData {
  id: string;
  current: boolean;
  userAgent?: string;
  ipAddress?: string;
  createdAt: string;
  expiresAt: string;
}

/**
 * The live sign-ins on this account.
 *
 * A session is where access is actually revoked, so seeing the list is how a
 * person notices one they do not recognise, and ending it is the only remedy
 * short of changing the password and signing every device out at once.
 */
function SessionsSection() {
  const t = useT();
  const [sessions, setSessions] = useState<SessionData[]>([]);
  const [loading, setLoading] = useState(false);
  const [pendingRevokeId, setPendingRevokeId] = useState<string | null>(null);
  const [revoking, setRevoking] = useState(false);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const { data } = await client.GET('/user/sessions', signal ? { signal } : {});
      setSessions(data ?? []);
    } catch (e) {
      // A request the screen itself called off is not a failure to report.
      if (isAbortError(e)) return;
      toast.error(errorMessage(e));
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, []);

  // The list is dropped rather than landing late: a response that arrives
  // after the screen is gone, or after a newer request overtook it, would
  // otherwise put a stale set of sign-ins on a screen nobody asked again.
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const revoke = async (id: string) => {
    setRevoking(true);
    try {
      await client.DELETE('/user/sessions/{sessionId}', { params: { path: { sessionId: id } } });
      setSessions((cur) => cur.filter((s) => s.id !== id));
      toast.success(t('sessions.revoked'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setRevoking(false);
      setPendingRevokeId(null);
    }
  };

  return (
    <Section title={t('settings.sessions')} description={t('sessions.description')}>
      {loading && sessions.length === 0 ? (
        <p className="py-2 text-body text-[var(--color-text-secondary)]">—</p>
      ) : (
        <ul className="-my-2 divide-y divide-[var(--color-separator)]">
          {sessions.map((s) => (
            <SessionRow key={s.id} session={s} onRevoke={() => setPendingRevokeId(s.id)} />
          ))}
        </ul>
      )}
      <p className="mt-3 text-footnote text-[var(--color-text-secondary)]">
        {t('appPasswords.sessionsNote')}{' '}
        <Link
          to="/settings"
          search={{ tab: undefined }}
          hash="app-passwords"
          className="font-medium text-[var(--color-accent)] underline"
        >
          {t('appPasswords.sessionsLink')}
        </Link>
      </p>
      <ConfirmSheet
        open={pendingRevokeId !== null}
        title={t('confirm.revokeSession')}
        confirmLabel={t('confirm.signOut')}
        busy={revoking}
        onConfirm={() => pendingRevokeId && revoke(pendingRevokeId)}
        onCancel={() => setPendingRevokeId(null)}
      />
    </Section>
  );
}

/** One sign-in: the device it came from, where and when, and the way to end it. */
function SessionRow({ session: s, onRevoke }: { session: SessionData; onRevoke: () => void }) {
  const t = useT();
  const locale = useUiStore((st) => st.locale);
  const timezone = useUiStore((st) => st.timezone);
  const device = s.userAgent ? describeUserAgent(s.userAgent) : null;
  const name =
    [device?.browser, device?.os].filter(Boolean).join(' · ') ||
    s.userAgent ||
    t('sessions.unknownDevice');
  return (
    <li className="flex items-center gap-3 py-3">
      <span
        aria-hidden
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--color-surface-inset)] text-[var(--color-text-secondary)]"
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {device?.mobile ? (
            <>
              <rect x="7" y="2.5" width="10" height="19" rx="2" />
              <path d="M11 18.5h2" />
            </>
          ) : (
            <>
              <rect x="3" y="4" width="18" height="12" rx="2" />
              <path d="M8 20h8M12 16v4" />
            </>
          )}
        </svg>
      </span>
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-center gap-2 text-default text-[var(--color-text-primary)]">
          <span className="truncate" title={s.userAgent}>
            {name}
          </span>
          {s.current && (
            <span className="shrink-0 rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-caption font-medium text-[var(--color-accent-ink)]">
              {t('sessions.current')}
            </span>
          )}
        </p>
        <p className="truncate text-footnote tabular-nums text-[var(--color-text-secondary)]">
          {[s.ipAddress, formatDateTime(s.createdAt, timezone, locale)].filter(Boolean).join(' · ')}
        </p>
      </div>
      {!s.current && (
        <button
          type="button"
          onClick={onRevoke}
          className="btn-secondary btn-sm btn-danger shrink-0"
        >
          {t('sessions.revoke')}
        </button>
      )}
    </li>
  );
}

function SegmentedControl<V extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: { value: V; label: string }[];
  value: V;
  onChange: (v: V) => void;
  ariaLabel: string;
}) {
  return (
    <fieldset
      aria-label={ariaLabel}
      className="segmented-control w-full max-w-[420px] border-0 p-0"
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          data-active={value === o.value}
          onClick={() => onChange(o.value)}
          className="flex-1"
        >
          {o.label}
        </button>
      ))}
    </fieldset>
  );
}

export function AppearanceSection() {
  const t = useT();
  const theme = useUiStore((s) => s.theme);
  const colorMode = useUiStore((s) => s.colorMode);
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const weekStart = useUiStore((s) => s.weekStart);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const setTheme = useUiStore((s) => s.setTheme);
  const setColorMode = useUiStore((s) => s.setColorMode);
  const setHolidaysCountry = useUiStore((s) => s.setHolidaysCountry);
  const saveAccountPreference = useAuthStore((s) => s.saveAccountPreference);

  const detectedTz = useMemo(detectTimezone, []);
  // Every country the bundled holiday data covers, named for the language in
  // use. The list is long enough that the picker filters rather than scrolls.
  const holidayCountries = useHolidayCountries(locale);

  // Language and timezone belong to the account: they decide what every date
  // on screen says, and a person reading the same calendar on a phone and a
  // laptop is entitled to the same answer on both.
  const savePreference = (prefs: { locale?: Locale; timezone?: string; weekStart?: WeekStart }) => {
    saveAccountPreference(prefs).catch((e) => toast.error(errorMessage(e)));
  };

  return (
    <>
      <Section title={t('settings.appearance')}>
        <FieldRow label={t('settings.theme')}>
          <SegmentedControl
            ariaLabel={t('settings.theme')}
            value={theme}
            onChange={setTheme}
            options={THEME_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) }))}
          />
        </FieldRow>
        <FieldRow label={t('settings.colorMode')}>
          <SegmentedControl
            ariaLabel={t('settings.colorMode')}
            value={colorMode}
            onChange={setColorMode}
            options={[
              { value: 'light', label: t('settings.modeLight') },
              { value: 'dark', label: t('settings.modeDark') },
              { value: 'system', label: t('settings.modeSystem') },
            ]}
          />
        </FieldRow>
        <FieldRow label={t('settings.language')}>
          <SegmentedControl
            ariaLabel={t('settings.language')}
            value={locale}
            onChange={(next) => savePreference({ locale: next })}
            options={[
              { value: 'ja', label: '日本語' },
              { value: 'en', label: 'English' },
            ]}
          />
        </FieldRow>
        <FieldRow label={t('settings.weekStart')}>
          <SegmentedControl
            ariaLabel={t('settings.weekStart')}
            value={weekStart}
            onChange={(next) => savePreference({ weekStart: next })}
            options={[
              { value: 'mon', label: t('settings.monday') },
              { value: 'sun', label: t('settings.sunday') },
              { value: 'sat', label: t('settings.saturday') },
            ]}
          />
        </FieldRow>
      </Section>

      <Section title={t('settings.timezone')}>
        <FieldRow
          label={t('settings.timezone')}
          hint={
            detectedTz === timezone ? undefined : t('settings.timezoneDevice', { tz: detectedTz })
          }
        >
          <CustomSelect
            value={timezone}
            onChange={(next) => savePreference({ timezone: next })}
            ariaLabel={t('settings.timezone')}
            className="w-full max-w-[420px]"
            triggerClassName="input-modern"
            searchable
            options={Array.from(new Set([detectedTz, ...TIMEZONE_OPTIONS])).map((tz) => ({
              value: tz,
              label: tz,
            }))}
          />
        </FieldRow>
      </Section>

      <Section title={t('settings.holidays')}>
        <div className="mb-4 flex items-center justify-between gap-4">
          <span>
            <span className="block text-default font-medium text-[var(--color-text-primary)]">
              {t('settings.holidays')}
            </span>
            <span className="text-footnote text-[var(--color-text-secondary)]">
              {t('settings.holidaysHint')}
            </span>
          </span>
          <Switch
            label={t('settings.holidays')}
            checked={holidaysCountry !== null}
            onChange={(on) => setHolidaysCountry(on ? detectHolidayCountry() : null)}
          />
        </div>
        {holidaysCountry !== null && (
          <FieldRow label={t('settings.holidaysCountry')}>
            <CustomSelect
              value={holidaysCountry}
              onChange={(v) => setHolidaysCountry(v)}
              ariaLabel={t('settings.holidaysCountry')}
              className="w-full max-w-[420px]"
              triggerClassName="input-modern"
              searchable
              options={holidayCountries.map((c) => ({ value: c.code, label: c.name }))}
            />
          </FieldRow>
        )}
      </Section>
    </>
  );
}

function CalendarDetailsSection({ calendarId }: { calendarId: string }) {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const updateCalendar = useCalendarStore((s) => s.updateCalendar);
  const uploadCalendarCover = useCalendarStore((s) => s.uploadCalendarCover);
  const deleteCalendarCover = useCalendarStore((s) => s.deleteCalendarCover);
  const calendar = calendars.find((c) => c.id === calendarId);

  const [name, setName] = useState(calendar?.name ?? '');
  const [color, setColor] = useState(calendar?.color ?? '#42A5F5');
  const [saving, setSaving] = useState(false);
  const [coverBusy, setCoverBusy] = useState(false);
  const [confirmingCover, setConfirmingCover] = useState(false);
  const coverInputRef = useRef<HTMLInputElement>(null);

  const canUploadCover = calendar?.coverUploadAllowed === true;
  const hasCover = Boolean(calendar?.coverImageUrl);
  const draftCover = { color, coverImageUrl: calendar?.coverImageUrl ?? '' };

  useEffect(() => {
    setName(calendar?.name ?? '');
  }, [calendar?.name]);

  useEffect(() => {
    setColor(calendar?.color ?? '#42A5F5');
  }, [calendar?.color]);

  const dirty = !!calendar && (name !== calendar.name || color !== calendar.color);

  const save = async () => {
    if (!name.trim()) return;
    setSaving(true);
    try {
      await updateCalendar(calendarId, { name: name.trim(), color });
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const handleCoverFile = async (file: File) => {
    if (file.type && !IMAGE_UPLOAD_TYPES.split(',').includes(file.type)) {
      toast.error(t('settings.calendarCoverInvalidType'));
      return;
    }

    setCoverBusy(true);
    try {
      await uploadCalendarCover(calendarId, file);
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setCoverBusy(false);
    }
  };

  const removeCover = async () => {
    setCoverBusy(true);
    try {
      await deleteCalendarCover(calendarId);
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setCoverBusy(false);
      setConfirmingCover(false);
    }
  };

  return (
    <Section title={t('settings.calendarDetails')}>
      <FieldRow label={t('settings.calendarName')}>
        <input
          data-1p-ignore
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="input-modern w-full max-w-[420px]"
          maxLength={200}
        />
      </FieldRow>
      <FieldRow label={t('settings.calendarColor')}>
        <input
          data-1p-ignore
          type="color"
          value={color}
          onChange={(e) => setColor(e.target.value)}
          aria-label={t('settings.calendarColor')}
          className="h-9 w-9 cursor-pointer rounded-full border-2 border-[var(--color-border)] bg-transparent"
        />
      </FieldRow>
      {(canUploadCover || hasCover) && (
        <FieldRow
          label={t('settings.calendarCover')}
          hint={canUploadCover ? t('settings.calendarCoverHint') : undefined}
        >
          <div className="flex items-start gap-4">
            {canUploadCover ? (
              <ImageUploadTile
                label={t('settings.calendarCoverUpload')}
                accept={IMAGE_UPLOAD_TYPES}
                inputRef={coverInputRef}
                onFile={handleCoverFile}
                busy={coverBusy}
                hasImage={hasCover}
                className="h-20 w-20 rounded-[var(--radius-lg)]"
              >
                {hasCover && (
                  <CalendarCoverImage
                    calendar={draftCover}
                    alt={calendar?.name ?? ''}
                    className="h-full w-full object-cover"
                    fallbackClassName="block h-full w-full"
                  />
                )}
              </ImageUploadTile>
            ) : (
              hasCover && (
                <CalendarCoverImage
                  calendar={draftCover}
                  alt={calendar?.name ?? ''}
                  className="h-20 w-20 shrink-0 rounded-[var(--radius-lg)] object-cover"
                  fallbackClassName="block h-20 w-20 shrink-0 rounded-[var(--radius-lg)]"
                />
              )
            )}
            <div className="min-w-0 flex-1">
              <p className="mb-1 text-caption text-[var(--color-text-tertiary)]">
                {t('settings.calendarCoverPreview')}
              </p>
              <div
                aria-hidden="true"
                className="flex max-w-[280px] items-center gap-3 rounded-lg border border-[var(--color-border)] px-3 py-2"
              >
                <CalendarCoverImage
                  calendar={draftCover}
                  className="h-7 w-7 shrink-0 rounded-md object-cover"
                  fallbackClassName="h-3 w-3 shrink-0 rounded-full"
                />
                <span className="flex-1 truncate text-default text-[var(--color-text-primary)]">
                  {name.trim() || calendar?.name}
                </span>
              </div>
              {(canUploadCover || calendar?.coverImageUrl) && (
                <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1">
                  {canUploadCover && (
                    <button
                      type="button"
                      onClick={() => coverInputRef.current?.click()}
                      disabled={coverBusy}
                      className="text-caption text-[var(--color-accent-ink)] hover:underline disabled:opacity-50"
                    >
                      {t(
                        hasCover ? 'settings.calendarCoverReplace' : 'settings.calendarCoverUpload',
                      )}
                    </button>
                  )}
                  {calendar?.coverImageUrl && (
                    <button
                      type="button"
                      onClick={() => setConfirmingCover(true)}
                      disabled={coverBusy}
                      className="text-caption text-[var(--color-danger)] hover:underline disabled:opacity-50"
                    >
                      {t('settings.calendarCoverRemove')}
                    </button>
                  )}
                  <ConfirmSheet
                    open={confirmingCover}
                    title={t('confirm.removeCover')}
                    confirmLabel={t('common.delete')}
                    busy={coverBusy}
                    onConfirm={removeCover}
                    onCancel={() => setConfirmingCover(false)}
                  />
                </div>
              )}
            </div>
          </div>
        </FieldRow>
      )}
      <button
        type="button"
        onClick={save}
        disabled={saving || !dirty || !name.trim()}
        className="btn-primary px-5 text-default"
      >
        {saving ? t('common.saving') : t('common.save')}
      </button>
    </Section>
  );
}

/** Exported for testing: the invite listing is role-gated on the client too. */
export function CalendarsSection() {
  const t = useT();
  const navigate = useNavigate();
  const calendars = useCalendarStore((s) => s.calendars);
  const openPanelFor = useUiStore((s) => s.openPanelFor);
  const [selectedId, setSelectedId] = useState<string>(calendars[0]?.id ?? '');

  useEffect(() => {
    if (!selectedId && calendars.length > 0) {
      const first = calendars[0];
      if (first) setSelectedId(first.id);
    }
  }, [calendars, selectedId]);

  // The caller's own role arrives with the calendar, so what this screen may
  // offer is settled without reading the member list.
  const isAdmin = canManage(calendars.find((c) => c.id === selectedId)?.role);

  // Members and their invite links live in one place, the members panel, so
  // this screen sends the reader there rather than keeping a second copy.
  const openMembers = () => {
    openPanelFor('members', selectedId);
    navigate({ to: '/' });
  };

  return (
    <>
      <Section title={t('settings.calendars')}>
        <FieldRow label={t('calendar.calendarName')}>
          <CustomSelect
            value={selectedId}
            onChange={setSelectedId}
            ariaLabel={t('calendar.calendarName')}
            className="w-full max-w-[420px]"
            triggerClassName="input-modern"
            options={calendars.map((c) => ({ value: c.id, label: c.name }))}
          />
        </FieldRow>
        <Link
          to="/settings"
          search={{ tab: undefined }}
          hash="app-passwords"
          className="text-body font-medium text-[var(--color-accent)] underline"
        >
          {t('appPasswords.calendarsLink')}
        </Link>
      </Section>

      {isAdmin && selectedId && <CalendarDetailsSection key={selectedId} calendarId={selectedId} />}

      {selectedId && (
        <Section title={t('settings.members')} description={t('settings.membersMoved')}>
          <button type="button" onClick={openMembers} className="btn-secondary btn-sm">
            {t('settings.openMembers')}
          </button>
        </Section>
      )}
    </>
  );
}

interface ImportResult {
  imported: number;
  skipped: number;
  failed: number;
  /** The part of `failed` the file caused, which a retry cannot change. */
  rejected: number;
  /** Events the calendar already held under the name the file gave them. */
  duplicates: number;
  truncated: number;
  /** Imported events whose zone was not recognised, so read as UTC. */
  unknownTimezones: number;
  /** The body held nothing that could be read as a calendar. */
  unreadable: boolean;
}

/**
 * The covered range an export reports, as text to show. `full` and an absent
 * header mean the whole calendar; either side of a window may be open.
 */
export function exportWindowLabel(header: string): string {
  if (!header || header === 'full') return '';
  const [from = '', to = ''] = header.split('/');
  return `${from || '…'} – ${to || '…'}`;
}

/** Exported for testing: an import refreshes the range the app is showing. */
export function ExportSection() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const fetchEvents = useCalendarStore((s) => s.fetchEvents);
  const visibleRange = useCalendarStore((s) => s.visibleRange);
  const timezone = useUiStore((s) => s.timezone);
  const [selectedId, setSelectedId] = useState<string>(calendars[0]?.id ?? '');
  const [icsText, setIcsText] = useState('');
  const [importing, setImporting] = useState(false);
  const [exporting, setExporting] = useState<'ics' | 'csv' | null>(null);

  useEffect(() => {
    if (!selectedId && calendars.length > 0) {
      const first = calendars[0];
      if (first) setSelectedId(first.id);
    }
  }, [calendars, selectedId]);

  const downloadFile = async (format: 'ics' | 'csv') => {
    setExporting(format);
    try {
      const { data: blob, response } = await client.GET('/calendars/{calendarId}/export', {
        // The same zone the grid reads days in, so the export's window and times match it.
        params: { path: { calendarId: selectedId }, query: { format, tz: timezone } },
        parseAs: 'blob',
      });
      const filename = filenameFromDisposition(response.headers.get('Content-Disposition'));
      const coverage = response.headers.get('X-Export-Window') ?? '';
      const url = URL.createObjectURL(blob ?? new Blob());
      const a = document.createElement('a');
      a.href = url;
      // The server names the export after the calendar it came from. Naming it
      // here instead gives every calendar the same filename, so exporting a
      // second one saves over the first in the downloads folder.
      a.download = filename || `calendar.${format}`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      // A backup that covers only a window looks whole once downloaded unless it is said.
      const range = exportWindowLabel(coverage);
      if (range) toast.info(t('settings.exportWindow', { range }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setExporting(null);
    }
  };

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      const text = await file.text();
      setIcsText(text);
    }
  };

  const handleImport = async () => {
    if (!icsText.trim() || !selectedId || importing) return;
    setImporting(true);
    try {
      const { data } = await client.POST('/calendars/{calendarId}/import', {
        params: { path: { calendarId: selectedId } },
        body: { ics: icsText },
      });
      if (!data) return;
      const res: ImportResult = data;
      if (res.unreadable) {
        // Nothing was read, so nothing was written -- and every counter
        // sitting at zero cannot tell that apart from a calendar that
        // genuinely had no events in it. The pasted text is left where it is:
        // none of it was consumed, and there is nothing to refresh.
        toast.error(t('settings.importUnreadable'));
        return;
      }
      // Every event the file contained is accounted for. Reporting only the
      // successes turns a migration that lost a slice of the calendar into
      // one that reads as clean.
      if (res.imported > 0) {
        toast.success(t('settings.imported', { count: String(res.imported) }));
      } else if (res.duplicates + res.skipped + res.failed + res.truncated === 0) {
        // Readable, and it held nothing. Said only when no other outcome is
        // there to say it: "imported 0" next to a reason is the reason twice.
        toast.info(t('settings.importEmpty'));
      }
      if (res.duplicates > 0) {
        // These events are on the calendar, put there by an earlier upload of
        // the same file. That is the answer somebody re-uploading to check was
        // after, so it is told as one rather than filed with the failures.
        toast.success(t('settings.importDuplicates', { count: String(res.duplicates) }));
      }
      if (res.skipped > 0) {
        toast.error(t('settings.importSkipped', { count: String(res.skipped) }));
      }
      // A rejection is counted inside the failures, so reporting both numbers
      // as given states the same events twice. They are split by what the
      // reader can do: the remainder is worth another attempt, the rejected
      // part never will be.
      const retryable = res.failed - res.rejected;
      if (retryable > 0) {
        toast.error(t('settings.importFailed', { count: String(retryable) }));
      }
      if (res.rejected > 0) {
        toast.error(t('settings.importRejected', { count: String(res.rejected) }));
      }
      if (res.truncated > 0) {
        toast.error(t('settings.importTruncated', { count: String(res.truncated) }));
      }
      if (res.unknownTimezones > 0) {
        // These events landed; only their times are wrong, which is the one
        // thing nobody goes back and checks. Told as a problem rather than as
        // a note, because it is one -- just not one that failed.
        toast.error(t('settings.importUnknownTimezones', { count: String(res.unknownTimezones) }));
      }
      setIcsText('');
      // The window the app is already showing, sized by the view and read in
      // the calendar's own zone. A range built here from the browser clock
      // answered with a different day for anyone whose machine sits on the
      // other side of midnight from the calendar, so the imported events at
      // the edge of the span did not appear.
      const { start, end } = visibleRange();
      await fetchEvents(start, end);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setImporting(false);
    }
  };

  return (
    <>
      <Section title={t('settings.calendars')}>
        <FieldRow label={t('calendar.calendarName')}>
          <CustomSelect
            value={selectedId}
            onChange={setSelectedId}
            ariaLabel={t('calendar.calendarName')}
            className="w-full max-w-[420px]"
            triggerClassName="input-modern"
            options={calendars.map((c) => ({ value: c.id, label: c.name }))}
          />
        </FieldRow>
      </Section>

      <Section title={t('settings.exportImport')}>
        <div className="mb-6 flex flex-wrap gap-3">
          <button
            type="button"
            onClick={() => downloadFile('ics')}
            disabled={!selectedId || exporting !== null}
            className="btn-primary px-5 text-default"
          >
            {exporting === 'ics' ? '...' : t('settings.exportIcal')}
          </button>
          <button
            type="button"
            onClick={() => downloadFile('csv')}
            disabled={!selectedId || exporting !== null}
            className="btn-secondary px-5 text-default disabled:opacity-50"
          >
            {exporting === 'csv' ? '...' : t('settings.exportCsv')}
          </button>
        </div>

        <p className="mb-4 text-footnote text-[var(--color-text-secondary)]">
          {t('settings.importSkipsDuplicates')}
        </p>
        <FieldRow label={t('settings.importIcal')}>
          <input
            type="file"
            accept=".ics,text/calendar"
            onChange={handleFileChange}
            className="block w-full max-w-[420px] text-body text-[var(--color-text-secondary)] file:mr-3 file:rounded-lg file:border-0 file:bg-[var(--color-surface-inset)] file:px-3 file:py-1.5 file:text-body file:font-medium file:text-[var(--color-text-primary)]"
          />
        </FieldRow>
        <FieldRow label={t('settings.importPasted')}>
          <textarea
            data-1p-ignore
            value={icsText}
            onChange={(e) => setIcsText(e.target.value)}
            onKeyDown={(e) => {
              if (!isSubmitShortcut(e)) return;
              e.preventDefault();
              void handleImport();
            }}
            placeholder={t('settings.importPlaceholder')}
            className="input-modern h-32 w-full font-mono text-footnote"
          />
        </FieldRow>
        <div className="flex flex-wrap items-center gap-3">
          <SubmitHint />
          <button
            type="button"
            onClick={handleImport}
            disabled={!icsText.trim() || !selectedId || importing}
            className="btn-primary px-5 text-default"
          >
            {importing ? '...' : t('settings.importPasted')}
          </button>
        </div>
      </Section>
    </>
  );
}

interface OAuthProviderInfo {
  provider: 'google' | 'line';
  clientId: string;
  hasSecret: boolean;
  enabled: boolean;
  source: 'db' | 'env' | 'none';
  updatedAt?: string;
}

const PROVIDER_LABELS: Record<OAuthProviderInfo['provider'], { label: string; help: string }> = {
  google: {
    label: 'Google',
    help: 'console.cloud.google.com → APIs & Services → Credentials',
  },
  line: {
    label: 'LINE',
    help: 'developers.line.biz → Channels → LINE Login',
  },
};

function AdminSection() {
  const t = useT();
  const [providers, setProviders] = useState<OAuthProviderInfo[]>([]);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const { data } = await client.GET('/admin/oauth-providers', signal ? { signal } : {});
      setProviders((data ?? []) as OAuthProviderInfo[]);
    } catch (e) {
      if (isAbortError(e)) return;
      toast.error(errorMessage(e));
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, []);

  // Called off with the screen. A provider list that lands afterwards would
  // report credentials the reader has since navigated away from.
  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  return (
    <>
      <Section title={t('settings.admin')} description={t('settings.adminOAuthDescription')}>
        <p className="text-body text-[var(--color-text-secondary)]">
          {t('settings.adminOAuthHelp')}
        </p>
      </Section>

      {loading ? (
        <p className="text-body text-[var(--color-text-secondary)]">{t('common.loading')}</p>
      ) : (
        providers.map((p) => <ProviderCard key={p.provider} info={p} onChange={refresh} />)
      )}

      <InstanceAdminsSection />

      <AllowedEmailsSection />
    </>
  );
}

interface InstanceAdminRow {
  userId: string;
  name: string;
  email: string;
  grantedAt: string;
}

/** Lists the instance administrators and grants or revokes the right by email. */
export function InstanceAdminsSection() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const [admins, setAdmins] = useState<InstanceAdminRow[] | null>(null);
  const [email, setEmail] = useState('');
  const [saving, setSaving] = useState(false);
  const [pendingRevoke, setPendingRevoke] = useState<InstanceAdminRow | null>(null);
  const [revoking, setRevoking] = useState(false);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    try {
      const { data } = await client.GET('/admin/instance-admins', signal ? { signal } : {});
      setAdmins(data ?? []);
    } catch (e) {
      if (isAbortError(e)) return;
      toast.error(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  const grant = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim()) return;
    setSaving(true);
    try {
      await client.POST('/admin/instance-admins', { body: { email: email.trim() } });
      setEmail('');
      await refresh();
      toast.success(t('panel.updated'));
    } catch (err) {
      toast.error(
        err instanceof ApiError && err.code === 'ADMIN.USER_NOT_FOUND'
          ? t('admin.userNotFound')
          : errorMessage(err),
      );
    } finally {
      setSaving(false);
    }
  };

  const revoke = async (row: InstanceAdminRow) => {
    setRevoking(true);
    try {
      await client.DELETE('/admin/instance-admins/{userId}', {
        params: { path: { userId: row.userId } },
      });
      await refresh();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setRevoking(false);
      setPendingRevoke(null);
    }
  };

  if (!admins) return null;

  return (
    <Section title={t('admin.instanceAdmins')}>
      <form onSubmit={grant} className="mb-4 flex flex-wrap items-end gap-2">
        <input
          data-1p-ignore
          autoComplete="off"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          aria-label={t('auth.email')}
          placeholder={t('settings.adminAllowedEmailsEmailPlaceholder')}
          className="input-modern min-w-[220px] flex-1"
        />
        <button type="submit" disabled={saving} className="btn-primary px-4">
          {t('admin.grant')}
        </button>
      </form>

      <ul className="divide-y divide-[var(--color-separator)]">
        {admins.map((row) => (
          <li key={row.userId} className="flex items-center justify-between gap-2 py-2">
            <div className="min-w-0">
              <p className="truncate text-default text-[var(--color-text-primary)]">{row.name}</p>
              <p className="truncate text-footnote text-[var(--color-text-tertiary)]">
                {row.email} · {formatDateTime(row.grantedAt, timezone, locale)}
              </p>
            </div>
            <button
              type="button"
              onClick={() => setPendingRevoke(row)}
              className="shrink-0 text-body text-[var(--color-danger)] hover:underline"
            >
              {t('admin.revoke')}
            </button>
          </li>
        ))}
      </ul>
      <ConfirmSheet
        open={pendingRevoke !== null}
        title={t('admin.revoke')}
        confirmLabel={t('confirm.remove')}
        busy={revoking}
        onConfirm={() => pendingRevoke && revoke(pendingRevoke)}
        onCancel={() => setPendingRevoke(null)}
      />
    </Section>
  );
}

/** The allow list with its absent lists read as empty. */
interface AllowedEmails {
  allowedDomains: string[];
  restricted: boolean;
  emails: Omit<components['schemas']['AllowedEmail'], '$schema'>[];
}

export function AllowedEmailsSection() {
  const t = useT();
  const [data, setData] = useState<AllowedEmails | null>(null);
  const [email, setEmail] = useState('');
  const [reason, setReason] = useState('');
  const [saving, setSaving] = useState(false);
  const [pendingRemoveId, setPendingRemoveId] = useState<string | null>(null);
  const [removing, setRemoving] = useState(false);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    try {
      const { data: list } = await client.GET('/admin/allowed-emails', signal ? { signal } : {});
      if (!list) return;
      setData({
        allowedDomains: list.allowedDomains ?? [],
        restricted: list.restricted,
        emails: list.emails ?? [],
      });
    } catch (e) {
      if (isAbortError(e)) return;
      toast.error(errorMessage(e));
    }
  }, []);

  // As above: the allow list is not written to a screen that has gone.
  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim()) return;
    setSaving(true);
    try {
      await client.POST('/admin/allowed-emails', {
        body: { email: email.trim(), reason: reason.trim() },
      });
      setEmail('');
      setReason('');
      await refresh();
      toast.success(t('panel.updated'));
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const remove = async (id: string) => {
    setRemoving(true);
    try {
      await client.DELETE('/admin/allowed-emails/{emailId}', {
        params: { path: { emailId: id } },
      });
      await refresh();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setRemoving(false);
      setPendingRemoveId(null);
    }
  };

  if (!data) return null;

  return (
    <Section
      title={t('settings.adminAllowedEmails')}
      description={t('settings.adminAllowedEmailsDescription')}
    >
      <p className="mb-3 text-body text-[var(--color-text-secondary)]">
        {data.restricted
          ? t('settings.adminAllowedEmailsRestricted', { domains: data.allowedDomains.join(', ') })
          : t('settings.adminAllowedEmailsUnrestricted')}
      </p>

      <form onSubmit={add} className="mb-4 flex flex-wrap items-end gap-2">
        <input
          data-1p-ignore
          autoComplete="off"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          placeholder={t('settings.adminAllowedEmailsEmailPlaceholder')}
          className="input-modern min-w-[220px] flex-1"
        />
        <input
          data-1p-ignore
          type="text"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder={t('settings.adminAllowedEmailsNotePlaceholder')}
          className="input-modern min-w-[160px] flex-1"
        />
        <button type="submit" disabled={saving} className="btn-primary px-4">
          {t('settings.adminAllowedEmailsAdd')}
        </button>
      </form>

      {data.emails.length === 0 ? (
        <p className="text-body text-[var(--color-text-tertiary)]">
          {t('settings.adminAllowedEmailsEmpty')}
        </p>
      ) : (
        <ul className="divide-y divide-[var(--color-separator)]">
          {data.emails.map((row) => (
            <li key={row.id} className="flex items-center justify-between gap-2 py-2">
              <div className="min-w-0">
                <p className="truncate text-default text-[var(--color-text-primary)]">
                  {row.email}
                </p>
                {row.reason && (
                  <p className="truncate text-footnote text-[var(--color-text-tertiary)]">
                    {row.reason}
                  </p>
                )}
              </div>
              <button
                type="button"
                onClick={() => setPendingRemoveId(row.id)}
                className="shrink-0 text-body text-[var(--color-danger)] hover:underline"
              >
                {t('settings.adminAllowedEmailsRemove')}
              </button>
            </li>
          ))}
        </ul>
      )}
      <ConfirmSheet
        open={pendingRemoveId !== null}
        title={t('confirm.removeAllowedEmail')}
        confirmLabel={t('confirm.remove')}
        busy={removing}
        onConfirm={() => pendingRemoveId && remove(pendingRemoveId)}
        onCancel={() => setPendingRemoveId(null)}
      />
    </Section>
  );
}

/** Exported for testing: a save sends only what changed. */
export function ProviderCard({
  info,
  onChange,
}: {
  info: OAuthProviderInfo;
  onChange: () => Promise<void> | void;
}) {
  const t = useT();
  const meta = PROVIDER_LABELS[info.provider];
  const [clientId, setClientId] = useState(info.clientId);
  const [secret, setSecret] = useState('');
  const [enabled, setEnabled] = useState(info.enabled);
  const [editingSecret, setEditingSecret] = useState(!info.hasSecret);
  const [clearingSecret, setClearingSecret] = useState(false);
  const [saving, setSaving] = useState(false);
  const [confirmingRemove, setConfirmingRemove] = useState(false);
  const [removing, setRemoving] = useState(false);

  useEffect(() => {
    setClientId(info.clientId);
    setEnabled(info.enabled);
    setEditingSecret(!info.hasSecret);
    setClearingSecret(false);
    setSecret('');
  }, [info]);

  const save = async () => {
    setSaving(true);
    try {
      // Omitting the secret keeps the stored one; a typed value replaces it and null removes it.
      const secretChange = clearingSecret
        ? { clientSecret: null }
        : editingSecret && secret
          ? { clientSecret: secret }
          : {};
      await client.PATCH('/admin/oauth-providers/{provider}', {
        params: { path: { provider: info.provider } },
        body: { clientId, enabled, ...secretChange },
      });
      toast.success(t('panel.updated'));
      setSecret('');
      await onChange();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    setRemoving(true);
    try {
      await client.DELETE('/admin/oauth-providers/{provider}', {
        params: { path: { provider: info.provider } },
      });
      toast.success(t('panel.updated'));
      await onChange();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setRemoving(false);
      setConfirmingRemove(false);
    }
  };

  const sourceBadge =
    info.source === 'db' ? (
      <span className="rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-caption font-medium text-[var(--color-accent-ink)]">
        DB
      </span>
    ) : info.source === 'env' ? (
      <span className="rounded-full bg-[var(--color-surface-inset)] px-2 py-0.5 text-caption font-medium text-[var(--color-text-secondary)]">
        ENV
      </span>
    ) : (
      <span className="rounded-full bg-[var(--color-surface-inset)] px-2 py-0.5 text-caption font-medium text-[var(--color-text-tertiary)]">
        {t('settings.adminProviderUnconfigured')}
      </span>
    );

  return (
    <Section title={meta.label} description={meta.help}>
      <div className="mb-4 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          {sourceBadge}
          {info.source !== 'none' && (
            <span
              className={`rounded-full px-2 py-0.5 text-caption font-medium ${
                info.enabled
                  ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)]'
                  : 'bg-[var(--color-surface-inset)] text-[var(--color-text-tertiary)]'
              }`}
            >
              {info.enabled
                ? t('settings.adminProviderEnabled')
                : t('settings.adminProviderDisabled')}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 text-body text-[var(--color-text-secondary)]">
          <span>{t('settings.adminProviderEnable')}</span>
          <Switch
            size="sm"
            label={t('settings.adminProviderEnable')}
            checked={enabled}
            onChange={setEnabled}
          />
        </div>
      </div>

      <FieldRow label={t('settings.adminProviderClientId')}>
        <input
          data-1p-ignore
          type="text"
          value={clientId}
          onChange={(e) => setClientId(e.target.value)}
          className="input-modern w-full max-w-[520px] font-mono text-body"
          placeholder="xxxxxxxx.apps.googleusercontent.com"
          autoComplete="off"
          spellCheck={false}
        />
      </FieldRow>

      <FieldRow
        label={t('settings.adminProviderClientSecret')}
        hint={info.hasSecret ? t('settings.adminProviderSecretStored') : ''}
      >
        {editingSecret ? (
          <div className="flex items-center gap-2">
            <input
              data-1p-ignore
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              className="input-modern w-full max-w-[520px] font-mono text-body"
              autoComplete="off"
              spellCheck={false}
            />
            {info.hasSecret && (
              <button
                type="button"
                onClick={() => {
                  setEditingSecret(false);
                  setSecret('');
                }}
                className="text-footnote text-[var(--color-text-secondary)] hover:underline"
              >
                {t('common.cancel')}
              </button>
            )}
          </div>
        ) : clearingSecret ? (
          <div className="flex items-center gap-3">
            <span className="text-body text-[var(--color-danger)]">
              {t('settings.adminProviderSecretWillBeRemoved')}
            </span>
            <button
              type="button"
              onClick={() => setClearingSecret(false)}
              className="text-footnote text-[var(--color-text-secondary)] hover:underline"
            >
              {t('common.cancel')}
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-3">
            <span className="font-mono text-body text-[var(--color-text-secondary)]">
              ••••••••••••
            </span>
            <button
              type="button"
              onClick={() => setEditingSecret(true)}
              className="text-footnote font-medium text-[var(--color-accent-ink)] hover:underline"
            >
              {t('settings.adminProviderReplaceSecret')}
            </button>
            <button
              type="button"
              onClick={() => setClearingSecret(true)}
              className="text-footnote font-medium text-[var(--color-danger)] hover:underline"
            >
              {t('settings.adminProviderRemoveSecret')}
            </button>
          </div>
        )}
      </FieldRow>

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={save}
          disabled={
            saving ||
            (!clientId && !info.hasSecret) ||
            (editingSecret && !secret && !info.hasSecret)
          }
          className="btn-primary px-5 text-default"
        >
          {saving ? t('common.saving') : t('common.save')}
        </button>
        {info.source === 'db' && (
          <button
            type="button"
            onClick={() => setConfirmingRemove(true)}
            className="text-body font-medium text-[var(--color-danger)] hover:underline"
          >
            {t('settings.adminProviderClear')}
          </button>
        )}
        <ConfirmSheet
          open={confirmingRemove}
          title={t('confirm.removeProvider')}
          hint={t('settings.adminProviderRemoveConfirm', { provider: meta.label })}
          confirmLabel={t('common.delete')}
          busy={removing}
          onConfirm={remove}
          onCancel={() => setConfirmingRemove(false)}
        />
      </div>
    </Section>
  );
}
