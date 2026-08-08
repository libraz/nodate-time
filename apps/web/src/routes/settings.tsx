import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CustomSelect } from '@/components/pickers';
import { type Locale, useT } from '@/i18n';
import { ApiError, api, errorMessage, isAbortError } from '@/lib/api';
import { detectHolidayCountry } from '@/lib/holidays';
import {
  DEFAULT_INVITE_ROLE,
  INVITE_ROLE_OPTIONS,
  type Role,
  roleLabelKey,
} from '@/lib/permissions';
import { detectTimezone } from '@/lib/preferences';
import { THEME_OPTIONS } from '@/lib/theme';
import { toast } from '@/lib/toast';
import { useCalendarMembers } from '@/lib/use-calendar-members';
import { useHolidayCountries } from '@/lib/use-holidays';
import { useInvites } from '@/lib/use-invites';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { Member } from '@/types/calendar';
import {
  INVITE_EXPIRY_HOURS,
  INVITE_MAX_USES,
  inviteExpiryLabelKey,
  inviteUsesLabelKey,
} from '@/types/invite';

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
        <header className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-display font-bold text-[var(--color-text-primary)] sm:text-hero">
              {t('settings.title')}
            </h1>
          </div>
          <Link
            to="/"
            className="rounded-full bg-[var(--color-surface-inset)] px-4 py-2 text-body font-medium text-[var(--color-text-primary)] transition hover:bg-[var(--color-hover)]"
          >
            {t('common.close')}
          </Link>
        </header>

        {/* Mobile tab strip */}
        <nav
          className="mb-4 flex gap-2 overflow-x-auto pb-1 sm:hidden"
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
                  ? 'bg-[var(--color-accent)] text-white shadow-sm'
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
                className={`flex items-center gap-3 rounded-xl px-3 py-2.5 text-left transition ${
                  tab === td.id
                    ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent)]'
                    : 'text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]'
                }`}
              >
                <span
                  aria-hidden
                  className={`flex h-9 w-9 items-center justify-center rounded-xl ${
                    tab === td.id
                      ? 'bg-[var(--color-accent)] text-white'
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

          <main key={tab} className="calendar-enter flex-1 overflow-y-auto pb-12 sm:pr-2">
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
  title: string;
  description?: string;
  children: React.ReactNode;
}

function Section({ title, description, children }: SectionProps) {
  return (
    <section className="mb-6">
      <header className="mb-3">
        <h2 className="text-subhead font-semibold text-[var(--color-text-primary)]">{title}</h2>
        {description && (
          <p className="mt-0.5 text-body text-[var(--color-text-secondary)]">{description}</p>
        )}
      </header>
      <div className="rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-sm">
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
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="mb-5 last:mb-0">
      <div className="mb-1.5 flex items-baseline justify-between">
        <span className="text-body font-medium text-[var(--color-text-primary)]">{label}</span>
        {hint && <span className="text-caption text-[var(--color-text-tertiary)]">{hint}</span>}
      </div>
      {children}
    </div>
  );
}

function ProfileSection() {
  const t = useT();
  const user = useAuthStore((s) => s.user);
  const updateProfile = useAuthStore((s) => s.updateProfile);
  const uploadAvatar = useAuthStore((s) => s.uploadAvatar);
  const removeAvatar = useAuthStore((s) => s.removeAvatar);
  const changePasswordAction = useAuthStore((s) => s.changePassword);
  const [name, setName] = useState(user?.name ?? '');
  const [saving, setSaving] = useState(false);
  const [avatarBusy, setAvatarBusy] = useState(false);
  const avatarInputRef = useRef<HTMLInputElement>(null);

  const [currentPw, setCurrentPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [pwSaving, setPwSaving] = useState(false);
  const [resendingVerification, setResendingVerification] = useState(false);

  useEffect(() => {
    if (user) {
      setName(user.name);
    }
  }, [user]);

  const dirty = !!user && name !== user.name;

  const handleAvatarFile = useCallback(
    async (file: File) => {
      setAvatarBusy(true);
      try {
        await uploadAvatar(file);
        toast.success(t('panel.updated'));
      } catch (e) {
        toast.error(e instanceof ApiError ? e.detail : 'Error');
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
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    } finally {
      setAvatarBusy(false);
    }
  }, [removeAvatar]);

  const save = async () => {
    setSaving(true);
    try {
      await updateProfile({ name });
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
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
      toast.error(e instanceof ApiError ? e.detail : t('profile.passwordChangeFailed'));
    } finally {
      setPwSaving(false);
    }
  };

  const handleResendVerification = async () => {
    setResendingVerification(true);
    try {
      await api.post('/user/verify-email/resend', {});
      toast.success(t('profile.verificationSent'));
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    } finally {
      setResendingVerification(false);
    }
  };

  return (
    <>
      <Section title={t('settings.profile')} description={t('profile.edit')}>
        <div className="mb-6 flex items-center gap-4">
          <button
            type="button"
            onClick={() => avatarInputRef.current?.click()}
            disabled={avatarBusy}
            className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-2xl bg-[var(--color-accent)] text-hero font-bold text-white shadow-sm transition hover:opacity-90 disabled:opacity-50"
            aria-label={t('profile.avatar')}
          >
            {user?.avatarUrl ? (
              <img src={user.avatarUrl} alt="" className="h-full w-full object-cover" />
            ) : (
              <span>{name ? name.slice(0, 1) : '\u{1F464}'}</span>
            )}
          </button>
          <input
            ref={avatarInputRef}
            type="file"
            accept="image/jpeg,image/png,image/webp"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) handleAvatarFile(f);
              if (e.target) e.target.value = '';
            }}
          />
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
                className="text-caption text-[var(--color-accent)] hover:underline disabled:opacity-50"
              >
                {t('profile.avatar')}
              </button>
              {user?.avatarUrl && (
                <button
                  type="button"
                  onClick={handleAvatarRemove}
                  disabled={avatarBusy}
                  className="text-caption text-[var(--color-danger)] hover:underline disabled:opacity-50"
                >
                  {t('profile.removeAvatar')}
                </button>
              )}
            </div>
          </div>
        </div>

        {user && user.emailVerified === false && (
          <div className="mx-6 mt-3 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3">
            <p className="text-default font-medium text-[var(--color-danger)]">
              {t('profile.emailUnverified')}
            </p>
            <p className="mt-1 text-caption text-[var(--color-text-secondary)]">
              {t('profile.emailUnverifiedHint')}
            </p>
            <button
              type="button"
              onClick={handleResendVerification}
              disabled={resendingVerification}
              className="mt-2 text-caption text-[var(--color-accent)] hover:underline disabled:opacity-50"
            >
              {t('profile.resendVerification')}
            </button>
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
            className="btn-primary px-5 text-default disabled:opacity-50"
          >
            {saving ? t('common.saving') : t('common.save')}
          </button>
          {!dirty && !saving && (
            <span className="text-footnote text-[var(--color-text-tertiary)]">
              {t('panel.updated')}
            </span>
          )}
        </div>
      </Section>

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
          className="btn-primary px-5 text-default disabled:opacity-50"
        >
          {pwSaving ? t('profile.changing') : t('profile.changePassword')}
        </button>
      </Section>

      <SessionsSection />
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
  const locale = useUiStore((s) => s.locale);
  const [sessions, setSessions] = useState<SessionData[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      setSessions(await api.get<SessionData[]>('/user/sessions', false, signal));
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
    try {
      await api.delete(`/user/sessions/${id}`);
      setSessions((cur) => cur.filter((s) => s.id !== id));
      toast.success(t('sessions.revoked'));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <Section title={t('settings.sessions')} description={t('sessions.description')}>
      {loading && sessions.length === 0 ? (
        <p className="py-2 text-body text-[var(--color-text-secondary)]">—</p>
      ) : (
        <ul className="-my-2 divide-y divide-[var(--color-separator)]">
          {sessions.map((s) => (
            <li key={s.id} className="flex items-center gap-3 py-3">
              <div className="min-w-0 flex-1">
                <p className="truncate text-default text-[var(--color-text-primary)]">
                  {s.userAgent || t('sessions.unknownDevice')}
                  {s.current && (
                    <span className="ml-2 rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-caption font-medium text-[var(--color-accent)]">
                      {t('sessions.current')}
                    </span>
                  )}
                </p>
                <p className="truncate text-footnote text-[var(--color-text-secondary)]">
                  {[s.ipAddress, new Date(s.createdAt).toLocaleString(locale)]
                    .filter(Boolean)
                    .join(' · ')}
                </p>
              </div>
              {!s.current && (
                <button
                  type="button"
                  onClick={() => revoke(s.id)}
                  className="shrink-0 rounded-lg px-3 py-1 text-footnote text-[var(--color-danger)] transition hover:bg-[var(--color-danger-bg)]"
                >
                  {t('sessions.revoke')}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Section>
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
  const savePreference = (prefs: { locale?: Locale; timezone?: string }) => {
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
      </Section>

      <Section title={t('settings.timezone')}>
        <FieldRow label={t('settings.timezone')} hint={detectedTz}>
          <CustomSelect
            value={timezone}
            onChange={(next) => savePreference({ timezone: next })}
            className="w-full max-w-[420px]"
            triggerClassName="input-modern"
            options={Array.from(new Set([detectedTz, ...TIMEZONE_OPTIONS])).map((tz) => ({
              value: tz,
              label: tz,
            }))}
          />
        </FieldRow>
      </Section>

      <Section title={t('settings.holidays')}>
        <label className="mb-4 flex cursor-pointer items-center justify-between gap-4">
          <span>
            <span className="block text-default font-medium text-[var(--color-text-primary)]">
              {t('settings.holidays')}
            </span>
            <span className="text-footnote text-[var(--color-text-secondary)]">
              {t('calendar.holidayLabel')}
            </span>
          </span>
          <span className="relative inline-flex h-6 w-11 items-center">
            <input
              type="checkbox"
              checked={holidaysCountry !== null}
              onChange={(e) => setHolidaysCountry(e.target.checked ? detectHolidayCountry() : null)}
              className="peer sr-only"
            />
            <span
              aria-hidden
              className="absolute inset-0 rounded-full bg-[var(--color-border)] transition peer-checked:bg-[var(--color-accent)]"
            />
            <span
              aria-hidden
              className="absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow transition peer-checked:translate-x-5"
            />
          </span>
        </label>
        {holidaysCountry !== null && (
          <FieldRow label={t('settings.holidaysCountry')}>
            <CustomSelect
              value={holidaysCountry}
              onChange={(v) => setHolidaysCountry(v)}
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
  const calendar = calendars.find((c) => c.id === calendarId);

  const [name, setName] = useState(calendar?.name ?? '');
  const [color, setColor] = useState(calendar?.color ?? '#42A5F5');
  const [coverUrl, setCoverUrl] = useState(calendar?.coverUrl ?? '');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (calendar) {
      setName(calendar.name);
      setColor(calendar.color);
      setCoverUrl(calendar.coverUrl ?? '');
    }
  }, [calendar]);

  const dirty =
    !!calendar &&
    (name !== calendar.name || color !== calendar.color || coverUrl !== (calendar.coverUrl ?? ''));

  const save = async () => {
    if (!name.trim()) return;
    setSaving(true);
    try {
      await updateCalendar(calendarId, { name: name.trim(), color, coverUrl });
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Section title={t('settings.calendarDetails')}>
      <FieldRow label={t('settings.calendarName')}>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="input-modern w-full max-w-[420px]"
          maxLength={200}
        />
      </FieldRow>
      <FieldRow label={t('settings.calendarColor')}>
        <input
          type="color"
          value={color}
          onChange={(e) => setColor(e.target.value)}
          aria-label={t('settings.calendarColor')}
          className="h-9 w-9 cursor-pointer rounded-full border-2 border-[var(--color-border)] bg-transparent"
        />
      </FieldRow>
      <FieldRow label={t('settings.calendarCover')}>
        <input
          type="url"
          value={coverUrl}
          onChange={(e) => setCoverUrl(e.target.value)}
          placeholder="https://..."
          className="input-modern w-full max-w-[420px]"
          maxLength={500}
        />
      </FieldRow>
      <button
        type="button"
        onClick={save}
        disabled={saving || !dirty || !name.trim()}
        className="btn-primary px-5 text-default disabled:opacity-50"
      >
        {saving ? t('common.saving') : t('common.save')}
      </button>
    </Section>
  );
}

/** Exported for testing: the invite listing is role-gated on the client too. */
export function CalendarsSection() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const me = useAuthStore((s) => s.user);

  const [selectedId, setSelectedId] = useState<string>(calendars[0]?.id ?? '');
  const [inviteRole, setInviteRole] = useState<Role>(DEFAULT_INVITE_ROLE);
  // Bounded by default: an unbounded link cannot be taken back once forwarded.
  const [inviteExpiry, setInviteExpiry] = useState<number>(168);
  const [inviteUses, setInviteUses] = useState<number>(1);

  useEffect(() => {
    if (!selectedId && calendars.length > 0) {
      const first = calendars[0];
      if (first) setSelectedId(first.id);
    }
  }, [calendars, selectedId]);

  // The caller's own role arrives with the calendar, so what this screen may
  // offer is settled before the member list it would otherwise be read from.
  const {
    members,
    canManageMembers: isAdmin,
    amOwner,
    roleOptions,
    ownerCount,
    changeRole,
    removeMember,
  } = useCalendarMembers(selectedId);

  // Only a manager may read a calendar's invites. Asking regardless meant an
  // editor or viewer got a permission error for opening a screen, once per
  // calendar they selected, having done nothing wrong. Public embed links are
  // managed in the share panel with their own /embed URL, so only the joinable
  // ones are taken here.
  const {
    joinInvites,
    loading: loadingInvites,
    busy,
    createInvite,
    revokeInvite,
  } = useInvites(selectedId, isAdmin);

  const handleRemoveMember = async (member: Member) => {
    // Leaving takes the calendar with it, so what is left of the list is what
    // the section has to show rather than a selection pointing at nothing.
    if (await removeMember(member)) {
      setSelectedId(calendars.find((c) => c.id !== selectedId)?.id ?? '');
    }
  };

  const handleCreateInvite = async () => {
    const created = await createInvite({
      role: inviteRole,
      expiryHours: inviteExpiry,
      maxUses: inviteUses,
    });
    if (created) toast.success(t('invites.create'));
  };

  const handleRevokeInvite = async (id: string) => {
    if (await revokeInvite(id)) toast.success(t('invites.revoke'));
  };

  const copyInvite = (token: string) => {
    const url = `${window.location.origin}/share/${token}`;
    void navigator.clipboard?.writeText(url);
    toast.success(t('common.copied'));
  };

  return (
    <>
      <Section title={t('settings.calendars')}>
        <FieldRow label={t('calendar.calendarName')}>
          <CustomSelect
            value={selectedId}
            onChange={setSelectedId}
            className="w-full max-w-[420px]"
            triggerClassName="input-modern"
            options={calendars.map((c) => ({ value: c.id, label: c.name }))}
          />
        </FieldRow>
      </Section>

      {isAdmin && selectedId && <CalendarDetailsSection calendarId={selectedId} />}

      <Section
        title={t('settings.members')}
        description={`${members.length} · ${t('members.role')}`}
      >
        {members.length === 0 ? (
          <p className="py-2 text-body text-[var(--color-text-secondary)]">—</p>
        ) : (
          <ul className="-my-2 divide-y divide-[var(--color-separator)]">
            {members.map((m) => {
              const isMe = m.id === me?.id;
              const cannotChange = m.role === 'owner' && ownerCount <= 1;
              // An owner's role and membership are the owner's own business;
              // a manager sees the row without the controls.
              const mayTouch = isAdmin && (m.role !== 'owner' || amOwner);
              // You manage other members, not yourself: the server refuses a
              // self role change, so offering the picker only produced an error.
              const canChangeRole = mayTouch && !isMe && !cannotChange;
              return (
                <li key={m.id} className="flex items-center gap-3 py-3">
                  <span
                    aria-hidden
                    className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-default font-bold text-white"
                    style={{ backgroundColor: m.color }}
                  >
                    {m.name.slice(0, 1)}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-default font-semibold text-[var(--color-text-primary)]">
                      {m.name}
                      {isMe && (
                        <span className="ml-2 rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-caption font-medium text-[var(--color-accent)]">
                          you
                        </span>
                      )}
                    </p>
                    <p className="truncate text-footnote text-[var(--color-text-secondary)]">
                      {m.email}
                    </p>
                  </div>
                  {canChangeRole ? (
                    <CustomSelect
                      value={m.role}
                      onChange={(v) => changeRole(m, v)}
                      className="shrink-0"
                      triggerClassName="rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
                      options={roleOptions.map((r) => ({ value: r, label: t(roleLabelKey(r)) }))}
                    />
                  ) : (
                    <span className="shrink-0 rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)]">
                      {t(roleLabelKey(m.role))}
                    </span>
                  )}
                  {(mayTouch || isMe) && !(cannotChange && isMe) && (
                    <button
                      type="button"
                      onClick={() => handleRemoveMember(m)}
                      aria-label={t('common.delete')}
                      className="shrink-0 rounded-lg p-2 text-[var(--color-text-tertiary)] transition hover:bg-[var(--color-danger-bg)] hover:text-[var(--color-danger)]"
                    >
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                      >
                        <path
                          d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M5 6l1 14a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-14"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        />
                      </svg>
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </Section>

      {isAdmin && (
        <Section title={t('settings.invites')}>
          <p className="mb-3 text-footnote text-[var(--color-text-secondary)]">
            {t('share.inviteSingleUseNote')}
          </p>
          <CustomSelect
            value={inviteRole}
            onChange={(role) => setInviteRole(role as Role)}
            options={INVITE_ROLE_OPTIONS.map((role) => ({
              value: role,
              label: t(roleLabelKey(role)),
            }))}
            className="mb-3 max-w-xs"
            triggerClassName="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
          />
          <div className="mb-3 flex max-w-md gap-2">
            <CustomSelect
              className="flex-1"
              value={String(inviteExpiry)}
              onChange={(v) => setInviteExpiry(Number(v))}
              options={INVITE_EXPIRY_HOURS.map((h) => ({
                value: String(h),
                label: t(inviteExpiryLabelKey(h)),
              }))}
              triggerClassName="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
            />
            <CustomSelect
              className="flex-1"
              value={String(inviteUses)}
              onChange={(v) => setInviteUses(Number(v))}
              options={INVITE_MAX_USES.map((u) => ({
                value: String(u),
                label: t(inviteUsesLabelKey(u)),
              }))}
              triggerClassName="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
            />
          </div>
          <button
            type="button"
            onClick={handleCreateInvite}
            disabled={busy === 'invite' || !isAdmin}
            className="btn-primary mb-4 px-5 text-default disabled:opacity-50"
          >
            {busy === 'invite' ? t('share.creating') : t('invites.create')}
          </button>
          {loadingInvites ? (
            <p className="text-body text-[var(--color-text-secondary)]">{t('common.loading')}</p>
          ) : joinInvites.length === 0 ? (
            <p className="rounded-xl bg-[var(--color-surface-inset)] px-4 py-6 text-center text-body text-[var(--color-text-secondary)]">
              {t('invites.empty')}
            </p>
          ) : (
            <ul className="-my-2 divide-y divide-[var(--color-separator)]">
              {joinInvites.map((inv) => (
                <li key={inv.id} className="flex flex-wrap items-center gap-3 py-3">
                  <code className="min-w-0 flex-1 truncate rounded-lg bg-[var(--color-surface-inset)] px-3 py-2 text-footnote text-[var(--color-text-secondary)]">
                    {inv.token ? `/share/${inv.token}` : t('invites.linkUnavailable')}
                  </code>
                  <span className="shrink-0 text-footnote text-[var(--color-text-tertiary)]">
                    {inv.useCount}/{inv.maxUses ?? t('invites.unlimited')}
                  </span>
                  <span className="shrink-0 text-footnote text-[var(--color-text-tertiary)]">
                    {inv.expiresAt
                      ? `${t('invites.expiresAt')}: ${new Date(inv.expiresAt).toLocaleDateString()}`
                      : t('invites.noExpiry')}
                  </span>
                  {inv.token && (
                    <button
                      type="button"
                      onClick={() => copyInvite(inv.token as string)}
                      className="shrink-0 rounded-lg px-3 py-1.5 text-footnote font-medium text-[var(--color-accent)] transition hover:bg-[var(--color-accent-bg)]"
                    >
                      {t('invites.copy')}
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => handleRevokeInvite(inv.id)}
                    className="shrink-0 rounded-lg px-3 py-1.5 text-footnote font-medium text-[var(--color-danger)] transition hover:bg-[var(--color-danger-bg)]"
                  >
                    {t('invites.revoke')}
                  </button>
                </li>
              ))}
            </ul>
          )}
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

/** Exported for testing: an import refreshes the range the app is showing. */
export function ExportSection() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const fetchEvents = useCalendarStore((s) => s.fetchEvents);
  const visibleRange = useCalendarStore((s) => s.visibleRange);
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
      const { blob, filename } = await api.getBlob(
        `/calendars/${selectedId}/export?format=${format}`,
      );
      const url = URL.createObjectURL(blob);
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
    if (!icsText.trim()) return;
    setImporting(true);
    try {
      const res = await api.post<ImportResult>(`/calendars/${selectedId}/import`, {
        ics: icsText,
      });
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
      toast.error(e instanceof ApiError ? e.detail : 'Import failed');
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
            className="btn-primary px-5 text-default disabled:opacity-50"
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
            value={icsText}
            onChange={(e) => setIcsText(e.target.value)}
            placeholder={t('settings.importPlaceholder')}
            className="input-modern h-32 w-full font-mono text-footnote"
          />
        </FieldRow>
        <button
          type="button"
          onClick={handleImport}
          disabled={!icsText.trim() || importing}
          className="btn-primary px-5 text-default disabled:opacity-50"
        >
          {importing ? '...' : t('settings.importPasted')}
        </button>
        <p className="mt-3 text-footnote text-muted">{t('settings.importSkipsDuplicates')}</p>
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
      const res = await api.get<{ providers: OAuthProviderInfo[] }>(
        '/admin/oauth-providers',
        false,
        signal,
      );
      setProviders(res.providers);
    } catch (e) {
      if (isAbortError(e)) return;
      toast.error(e instanceof ApiError ? e.detail : 'Error');
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

      <AllowedEmailsSection />
    </>
  );
}

interface AllowedEmail {
  id: string;
  email: string;
  reason: string;
  createdAt: string;
}

interface AllowedEmailsResponse {
  allowedDomains: string[];
  restricted: boolean;
  emails: AllowedEmail[];
}

export function AllowedEmailsSection() {
  const t = useT();
  const [data, setData] = useState<AllowedEmailsResponse | null>(null);
  const [email, setEmail] = useState('');
  const [reason, setReason] = useState('');
  const [saving, setSaving] = useState(false);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    try {
      setData(await api.get<AllowedEmailsResponse>('/admin/allowed-emails', false, signal));
    } catch (e) {
      if (isAbortError(e)) return;
      toast.error(e instanceof ApiError ? e.detail : 'Error');
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
      await api.post('/admin/allowed-emails', { email: email.trim(), reason: reason.trim() });
      setEmail('');
      setReason('');
      await refresh();
      toast.success(t('panel.updated'));
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail : 'Error');
    } finally {
      setSaving(false);
    }
  };

  const remove = async (id: string) => {
    try {
      await api.delete(`/admin/allowed-emails/${id}`);
      await refresh();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail : 'Error');
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
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          placeholder={t('settings.adminAllowedEmailsEmailPlaceholder')}
          className="input-modern min-w-[220px] flex-1"
        />
        <input
          type="text"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder={t('settings.adminAllowedEmailsNotePlaceholder')}
          className="input-modern min-w-[160px] flex-1"
        />
        <button type="submit" disabled={saving} className="btn-primary rounded-xl px-4">
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
                onClick={() => remove(row.id)}
                className="shrink-0 text-body text-[var(--color-danger)] hover:underline"
              >
                {t('settings.adminAllowedEmailsRemove')}
              </button>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function ProviderCard({
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
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setClientId(info.clientId);
    setEnabled(info.enabled);
    setEditingSecret(!info.hasSecret);
    setSecret('');
  }, [info]);

  const save = async () => {
    setSaving(true);
    try {
      await api.put(`/admin/oauth-providers/${info.provider}`, {
        clientId,
        clientSecret: editingSecret ? secret : '',
        enabled,
      });
      toast.success(t('panel.updated'));
      setSecret('');
      await onChange();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!confirm(t('settings.adminProviderRemoveConfirm', { provider: meta.label }))) return;
    try {
      await api.delete(`/admin/oauth-providers/${info.provider}`);
      toast.success(t('panel.updated'));
      await onChange();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    }
  };

  const sourceBadge =
    info.source === 'db' ? (
      <span className="rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-caption font-medium text-[var(--color-accent)]">
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
                  ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent)]'
                  : 'bg-[var(--color-surface-inset)] text-[var(--color-text-tertiary)]'
              }`}
            >
              {info.enabled
                ? t('settings.adminProviderEnabled')
                : t('settings.adminProviderDisabled')}
            </span>
          )}
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-body text-[var(--color-text-secondary)]">
          <span>{t('settings.adminProviderEnable')}</span>
          <span className="relative inline-flex h-5 w-9 items-center">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              className="peer sr-only"
            />
            <span
              aria-hidden
              className="absolute inset-0 rounded-full bg-[var(--color-border)] transition peer-checked:bg-[var(--color-accent)]"
            />
            <span
              aria-hidden
              className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow transition peer-checked:translate-x-4"
            />
          </span>
        </label>
      </div>

      <FieldRow label={t('settings.adminProviderClientId')}>
        <input
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
        ) : (
          <div className="flex items-center gap-3">
            <span className="font-mono text-body text-[var(--color-text-secondary)]">
              ••••••••••••
            </span>
            <button
              type="button"
              onClick={() => setEditingSecret(true)}
              className="text-footnote font-medium text-[var(--color-accent)] hover:underline"
            >
              {t('settings.adminProviderReplaceSecret')}
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
          className="btn-primary px-5 text-default disabled:opacity-50"
        >
          {saving ? t('common.saving') : t('common.save')}
        </button>
        {info.source === 'db' && (
          <button
            type="button"
            onClick={remove}
            className="text-body font-medium text-[var(--color-danger)] hover:underline"
          >
            {t('settings.adminProviderClear')}
          </button>
        )}
      </div>
    </Section>
  );
}
