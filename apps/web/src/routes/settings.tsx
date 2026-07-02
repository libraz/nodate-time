import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CustomSelect } from '@/components/pickers';
import { useT } from '@/i18n';
import { ApiError, api, errorMessage } from '@/lib/api';
import { HOLIDAY_COUNTRIES } from '@/lib/holidays';
import {
  DEFAULT_INVITE_ROLE,
  isAdmin as isAdminRole,
  ROLE_OPTIONS,
  roleLabelKey,
} from '@/lib/permissions';
import { THEME_OPTIONS } from '@/lib/theme';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { Member } from '@/types/calendar';

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

const PROFILE_COLORS = [
  '#42A5F5',
  '#5C6BC0',
  '#26A69A',
  '#66BB6A',
  '#FFCA28',
  '#FF7043',
  '#EC407A',
  '#AB47BC',
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
  const [name, setName] = useState(user?.name ?? '');
  const [icon, setIcon] = useState(user?.icon ?? '');
  const [color, setColor] = useState(user?.color ?? '#42A5F5');
  const [saving, setSaving] = useState(false);
  const [avatarBusy, setAvatarBusy] = useState(false);
  const avatarInputRef = useRef<HTMLInputElement>(null);

  const [currentPw, setCurrentPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [pwSaving, setPwSaving] = useState(false);

  useEffect(() => {
    if (user) {
      setName(user.name);
      setIcon(user.icon);
      setColor(user.color);
    }
  }, [user]);

  const dirty = !!user && (name !== user.name || icon !== user.icon || color !== user.color);

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
      await updateProfile({ name, icon, color });
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
      await api.put('/user/password', { currentPassword: currentPw, newPassword: newPw });
      toast.success(t('profile.passwordChanged'));
      setCurrentPw('');
      setNewPw('');
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : t('profile.passwordChangeFailed'));
    } finally {
      setPwSaving(false);
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
            className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-2xl text-hero font-bold text-white shadow-sm transition hover:opacity-90 disabled:opacity-50"
            style={{ backgroundColor: color }}
            aria-label={t('profile.avatar')}
          >
            {user?.avatarUrl ? (
              <img src={user.avatarUrl} alt="" className="h-full w-full object-cover" />
            ) : (
              <span>{icon || (name ? name.slice(0, 1) : '👤')}</span>
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

        <FieldRow label={t('profile.name')} hint={t('profile.icon')}>
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={icon}
              maxLength={4}
              onChange={(e) => setIcon(e.target.value)}
              aria-label={t('profile.icon')}
              placeholder="🙂"
              className="input-modern shrink-0 text-center text-title"
              style={{ width: '3rem', paddingLeft: 0, paddingRight: 0 }}
            />
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="input-modern w-full"
              autoComplete="name"
            />
          </div>
        </FieldRow>
        <FieldRow label={t('profile.color')}>
          <div className="flex flex-wrap items-center gap-2">
            {PROFILE_COLORS.map((c) => (
              <button
                key={c}
                type="button"
                onClick={() => setColor(c)}
                aria-label={c}
                aria-pressed={color === c}
                className="relative h-9 w-9 rounded-full transition hover:scale-110"
                style={{
                  backgroundColor: c,
                  boxShadow: color === c ? `0 0 0 3px ${c}55` : undefined,
                }}
              />
            ))}
            <input
              type="color"
              value={color}
              onChange={(e) => setColor(e.target.value)}
              aria-label={t('profile.color')}
              className="h-9 w-9 cursor-pointer rounded-full border-2 border-[var(--color-border)] bg-transparent"
            />
          </div>
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
    </>
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

function AppearanceSection() {
  const t = useT();
  const theme = useUiStore((s) => s.theme);
  const colorMode = useUiStore((s) => s.colorMode);
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const setTheme = useUiStore((s) => s.setTheme);
  const setColorMode = useUiStore((s) => s.setColorMode);
  const setLocale = useUiStore((s) => s.setLocale);
  const setTimezone = useUiStore((s) => s.setTimezone);
  const setHolidaysCountry = useUiStore((s) => s.setHolidaysCountry);

  const detectedTz = useMemo(() => {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
    } catch {
      return 'UTC';
    }
  }, []);

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
            onChange={setLocale}
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
            onChange={setTimezone}
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
              onChange={(e) => setHolidaysCountry(e.target.checked ? 'JP' : null)}
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
              options={HOLIDAY_COUNTRIES.map((c) => ({
                value: c.code,
                label: locale === 'ja' ? c.nameJa : c.nameEn,
              }))}
            />
          </FieldRow>
        )}
      </Section>
    </>
  );
}

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

function CalendarsSection() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const me = useAuthStore((s) => s.user);

  const [selectedId, setSelectedId] = useState<string>(calendars[0]?.id ?? '');
  const [invites, setInvites] = useState<InviteData[]>([]);
  const [loadingInvites, setLoadingInvites] = useState(false);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [inviteRole, setInviteRole] = useState(DEFAULT_INVITE_ROLE);

  useEffect(() => {
    if (!selectedId && calendars.length > 0) {
      const first = calendars[0];
      if (first) setSelectedId(first.id);
    }
  }, [calendars, selectedId]);

  const loadInvites = useCallback(async (calId: string) => {
    setLoadingInvites(true);
    try {
      const list = await api.get<InviteData[]>(`/calendars/${calId}/invites`);
      setInvites(list);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    } finally {
      setLoadingInvites(false);
    }
  }, []);

  useEffect(() => {
    if (selectedId) {
      fetchMembers(selectedId);
      loadInvites(selectedId);
    }
  }, [selectedId, fetchMembers, loadInvites]);

  const members = (selectedId && membersMap[selectedId]) || [];
  const myMembership = members.find((m) => m.email === me?.email);
  const isAdmin = isAdminRole(myMembership?.role);
  const adminCount = members.filter((m) => m.role === 'admin').length;

  const handleRoleChange = async (member: Member, role: string) => {
    if (member.role === 'admin' && role !== 'admin' && adminCount <= 1) {
      toast.error(t('members.lastAdmin'));
      return;
    }
    try {
      await api.put(`/calendars/${selectedId}/members/${member.id}/role`, { role });
      await fetchMembers(selectedId);
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const handleRemoveMember = async (member: Member) => {
    if (!confirm(t('members.removeConfirm'))) return;
    try {
      await api.delete(`/calendars/${selectedId}/members/${member.id}`);
      await fetchMembers(selectedId);
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const handleCreateInvite = async () => {
    setCreatingInvite(true);
    try {
      const inv = await api.post<InviteData>(`/calendars/${selectedId}/invites`, {
        role: inviteRole,
        maxUses: 1,
      });
      setInvites((cur) => [inv, ...cur]);
      toast.success(t('invites.create'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setCreatingInvite(false);
    }
  };

  const handleRevokeInvite = async (id: number) => {
    try {
      await api.delete(`/calendars/${selectedId}/invites/${id}`);
      setInvites((cur) => cur.filter((i) => i.id !== id));
      toast.success(t('invites.revoke'));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const copyInvite = (token: string) => {
    const url = `${window.location.origin}/share/${token}`;
    void navigator.clipboard?.writeText(url);
    toast.success(t('common.copied'));
  };

  // Public, read-only embed links are managed in the Share panel with their own
  // /embed URL; only show joinable invites here.
  const joinInvites = invites.filter((i) => !i.isPublic);

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
              const isMe = m.email === me?.email;
              const cannotChange = m.role === 'admin' && adminCount <= 1;
              return (
                <li key={m.id} className="flex items-center gap-3 py-3">
                  <span
                    aria-hidden
                    className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-default font-bold text-white"
                    style={{ backgroundColor: m.color }}
                  >
                    {m.icon || m.name.slice(0, 1)}
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
                  {isAdmin && !cannotChange ? (
                    <CustomSelect
                      value={m.role}
                      onChange={(v) => handleRoleChange(m, v)}
                      className="shrink-0"
                      triggerClassName="rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
                      options={ROLE_OPTIONS.map((r) => ({ value: r, label: t(roleLabelKey(r)) }))}
                    />
                  ) : (
                    <span className="shrink-0 rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)]">
                      {t(roleLabelKey(m.role))}
                    </span>
                  )}
                  {(isAdmin || isMe) && !(cannotChange && isMe) && (
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

      <Section title={t('settings.invites')}>
        <p className="mb-3 text-footnote text-[var(--color-text-secondary)]">
          {t('share.inviteSingleUseNote')}
        </p>
        <CustomSelect
          value={inviteRole}
          onChange={(role) => setInviteRole(role as typeof inviteRole)}
          options={ROLE_OPTIONS.filter((role) => role !== 'admin').map((role) => ({
            value: role,
            label: t(roleLabelKey(role)),
          }))}
          className="mb-3 max-w-xs"
          triggerClassName="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
        />
        <button
          type="button"
          onClick={handleCreateInvite}
          disabled={creatingInvite || !isAdmin}
          className="btn-primary mb-4 px-5 text-default disabled:opacity-50"
        >
          {creatingInvite ? t('share.creating') : t('invites.create')}
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
                  /share/{inv.token}
                </code>
                <span className="shrink-0 text-footnote text-[var(--color-text-tertiary)]">
                  {inv.useCount}/{inv.maxUses ?? t('invites.unlimited')}
                </span>
                <button
                  type="button"
                  onClick={() => copyInvite(inv.token)}
                  className="shrink-0 rounded-lg px-3 py-1.5 text-footnote font-medium text-[var(--color-accent)] transition hover:bg-[var(--color-accent-bg)]"
                >
                  {t('invites.copy')}
                </button>
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
    </>
  );
}

function ExportSection() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const fetchEvents = useCalendarStore((s) => s.fetchEvents);
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
      const blob = await api.getBlob(`/calendars/${selectedId}/export?format=${format}`);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `calendar.${format}`;
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
      const res = await api.post<{ imported: number; skipped: number; failed: number }>(
        `/calendars/${selectedId}/import`,
        { ics: icsText },
      );
      toast.success(t('settings.imported', { count: String(res.imported) }));
      setIcsText('');
      const now = new Date();
      const start = new Date(now.getFullYear(), now.getMonth() - 1, 1).toISOString().slice(0, 10);
      const end = new Date(now.getFullYear(), now.getMonth() + 2, 1).toISOString().slice(0, 10);
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

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.get<{ providers: OAuthProviderInfo[] }>('/admin/oauth-providers');
      setProviders(res.providers);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
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
  id: number;
  email: string;
  note: string;
  createdAt: string;
}

interface AllowedEmailsResponse {
  allowedDomains: string[];
  restricted: boolean;
  emails: AllowedEmail[];
}

function AllowedEmailsSection() {
  const t = useT();
  const [data, setData] = useState<AllowedEmailsResponse | null>(null);
  const [email, setEmail] = useState('');
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setData(await api.get<AllowedEmailsResponse>('/admin/allowed-emails'));
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim()) return;
    setSaving(true);
    try {
      await api.post('/admin/allowed-emails', { email: email.trim(), note: note.trim() });
      setEmail('');
      setNote('');
      await refresh();
      toast.success(t('panel.updated'));
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail : 'Error');
    } finally {
      setSaving(false);
    }
  };

  const remove = async (id: number) => {
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
          value={note}
          onChange={(e) => setNote(e.target.value)}
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
                {row.note && (
                  <p className="truncate text-footnote text-[var(--color-text-tertiary)]">
                    {row.note}
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
