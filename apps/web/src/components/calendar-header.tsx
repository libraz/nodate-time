import { useT } from '@/i18n';
import { ApiError, api } from '@/lib/api';
import { formatMonthYear } from '@/lib/date-utils';
import { useAuthStore } from '@/stores/auth-store';
import { useUiStore } from '@/stores/ui-store';
import { MEMBER_COLORS } from '@/types/calendar';
import { DateTime } from 'luxon';
import { useCallback, useEffect, useRef, useState } from 'react';

export function CalendarHeader() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const navigateMonth = useUiStore((s) => s.navigateMonth);
  const calendarView = useUiStore((s) => s.calendarView);
  const setCalendarView = useUiStore((s) => s.setCalendarView);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const toggleSearch = useUiStore((s) => s.toggleSearch);
  const toggleSettings = useUiStore((s) => s.toggleSettings);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const updateProfile = useAuthStore((s) => s.updateProfile);
  const [showProfileMenu, setShowProfileMenu] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [editIcon, setEditIcon] = useState('');
  const [editColor, setEditColor] = useState('');
  const [saving, setSaving] = useState(false);
  const [isChangingPassword, setIsChangingPassword] = useState(false);
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [passwordError, setPasswordError] = useState('');
  const [passwordSuccess, setPasswordSuccess] = useState(false);
  const [savingPassword, setSavingPassword] = useState(false);
  const profileMenuRef = useRef<HTMLDivElement>(null);

  const ICON_OPTIONS = [
    '\u{1F464}',
    '\u{1F60A}',
    '\u{1F60E}',
    '\u{1F31F}',
    '\u{1F680}',
    '\u{1F3B5}',
    '\u{1F4DA}',
    '\u{2615}',
    '\u{1F33F}',
    '\u{1F525}',
    '\u{1F496}',
    '\u{1F308}',
  ];

  const startEditing = useCallback(() => {
    if (user) {
      setEditName(user.name);
      setEditIcon(user.icon);
      setEditColor(user.color);
      setIsEditing(true);
    }
  }, [user]);

  const handleSaveProfile = useCallback(async () => {
    if (!editName.trim()) return;
    setSaving(true);
    try {
      await updateProfile({ name: editName.trim(), icon: editIcon, color: editColor });
      setIsEditing(false);
    } catch {
      // ignore
    } finally {
      setSaving(false);
    }
  }, [editName, editIcon, editColor, updateProfile]);

  const startChangingPassword = useCallback(() => {
    setCurrentPassword('');
    setNewPassword('');
    setConfirmPassword('');
    setPasswordError('');
    setPasswordSuccess(false);
    setIsChangingPassword(true);
  }, []);

  const handleChangePassword = useCallback(async () => {
    setPasswordError('');
    if (newPassword.length < 8) {
      setPasswordError(t('profile.passwordMinLength'));
      return;
    }
    if (newPassword !== confirmPassword) {
      setPasswordError(t('profile.passwordMismatch'));
      return;
    }
    setSavingPassword(true);
    try {
      await api.put('/user/password', { currentPassword, newPassword });
      setPasswordSuccess(true);
      setTimeout(() => {
        setIsChangingPassword(false);
        setPasswordSuccess(false);
      }, 1500);
    } catch (e) {
      if (e instanceof ApiError && e.status === 400) {
        setPasswordError(t('profile.wrongPassword'));
      } else {
        setPasswordError(t('profile.passwordChangeFailed'));
      }
    } finally {
      setSavingPassword(false);
    }
  }, [currentPassword, newPassword, confirmPassword, t]);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (profileMenuRef.current && !profileMenuRef.current.contains(e.target as Node)) {
        setShowProfileMenu(false);
      }
    };
    if (showProfileMenu) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showProfileMenu]);

  const handleGoToToday = () => {
    const today = DateTime.now();
    setCurrentMonth(today.startOf('month'));
    setSelectedDate(today);
  };

  const ChevronLeft = () => (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-text-primary)"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M15 18l-6-6 6-6" />
    </svg>
  );

  const ChevronRight = () => (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-text-primary)"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M9 18l6-6-6-6" />
    </svg>
  );

  const SearchIcon = () => (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-text-primary)"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="sm:h-[22px] sm:w-[22px]"
    >
      <circle cx="11" cy="11" r="7" />
      <line x1="16.65" y1="16.65" x2="21" y2="21" />
    </svg>
  );

  const PlusIcon = ({ className }: { className?: string }) => (
    <svg
      width="22"
      height="22"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-text-primary)"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className ?? 'sm:h-6 sm:w-6'}
    >
      <line x1="12" y1="5" x2="12" y2="19" />
      <line x1="5" y1="12" x2="19" y2="12" />
    </svg>
  );

  const AvatarButton = ({ size = 'default' }: { size?: 'default' | 'sm' }) => (
    <button
      type="button"
      onClick={() => setShowProfileMenu(!showProfileMenu)}
      className={
        size === 'sm'
          ? 'flex h-8 w-8 items-center justify-center rounded-lg hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]'
          : 'flex h-10 w-10 items-center justify-center rounded-xl hover:bg-[var(--color-hover)] active:bg-[var(--color-active)] sm:h-11 sm:w-11'
      }
      aria-label={t('profile.profile')}
    >
      {user?.icon ? (
        <span className={size === 'sm' ? 'text-base' : 'text-lg'}>{user.icon}</span>
      ) : (
        <svg
          width={size === 'sm' ? '20' : '22'}
          height={size === 'sm' ? '20' : '22'}
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--color-text-primary)"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={size === 'sm' ? undefined : 'sm:h-6 sm:w-6'}
        >
          <circle cx="12" cy="8" r="4" />
          <path d="M20 21a8 8 0 1 0-16 0" />
        </svg>
      )}
    </button>
  );

  const profileDropdown = showProfileMenu && (
    <div className="glass-surface-heavy absolute right-0 top-full z-50 mt-1 w-64 rounded-2xl py-2 ring-1 ring-[var(--color-border)]">
      {user && !isEditing && (
        <div className="border-b border-[var(--color-separator)] px-4 py-3">
          <div className="flex items-center gap-3">
            <span
              className="flex h-9 w-9 items-center justify-center rounded-full text-lg text-white"
              style={{ backgroundColor: user.color }}
            >
              {user.icon}
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[14px] font-bold text-[var(--color-text-primary)]">
                {user.name}
              </p>
              <p className="truncate text-[12px] text-[var(--color-text-secondary)]">
                {user.email}
              </p>
            </div>
          </div>
        </div>
      )}
      {isEditing ? (
        <div className="border-b border-[var(--color-separator)] px-4 py-3">
          <div className="mb-3">
            <span className="mb-1 block text-[12px] text-[var(--color-text-secondary)]">
              {t('profile.name')}
            </span>
            <input
              type="text"
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
              className="input-modern w-full text-[13px]"
              aria-label={t('profile.name')}
            />
          </div>
          <div className="mb-3">
            <span className="mb-1 block text-[12px] text-[var(--color-text-secondary)]">
              {t('profile.icon')}
            </span>
            <div className="flex flex-wrap gap-1.5">
              {ICON_OPTIONS.map((icon) => (
                <button
                  key={icon}
                  type="button"
                  onClick={() => setEditIcon(icon)}
                  className="flex h-9 w-9 items-center justify-center rounded-xl text-[18px] transition-colors"
                  style={{
                    backgroundColor:
                      editIcon === icon ? 'var(--color-accent-subtle)' : 'transparent',
                    border:
                      editIcon === icon
                        ? '2px solid var(--color-accent)'
                        : '1px solid var(--color-border)',
                  }}
                >
                  {icon}
                </button>
              ))}
            </div>
          </div>
          <div className="mb-3">
            <span className="mb-1 block text-[12px] text-[var(--color-text-secondary)]">
              {t('profile.color')}
            </span>
            <div className="flex flex-wrap gap-1.5">
              {MEMBER_COLORS.map((color) => (
                <button
                  key={color}
                  type="button"
                  onClick={() => setEditColor(color)}
                  className="flex h-7 w-7 items-center justify-center rounded-full transition-shadow"
                  style={{
                    backgroundColor: color,
                    boxShadow:
                      editColor === color
                        ? '0 0 0 2px var(--color-surface), 0 0 0 4px var(--color-accent)'
                        : 'none',
                  }}
                />
              ))}
            </div>
          </div>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setIsEditing(false)}
              className="btn-secondary h-9 flex-1 px-4 text-[13px]"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              onClick={handleSaveProfile}
              disabled={saving || !editName.trim()}
              className="btn-primary h-9 flex-1 px-4 text-[13px] disabled:opacity-50"
            >
              {saving ? t('profile.saving') : t('common.save')}
            </button>
          </div>
        </div>
      ) : isChangingPassword ? (
        <div className="px-4 py-3">
          {passwordSuccess ? (
            <div
              className="py-4 text-center text-[13px] font-medium"
              style={{ color: 'var(--color-accent)' }}
            >
              {t('profile.passwordChanged')}
            </div>
          ) : (
            <>
              <div className="mb-3">
                <span className="mb-1 block text-[12px] text-[var(--color-text-secondary)]">
                  {t('profile.currentPassword')}
                </span>
                <input
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  className="input-modern w-full text-[13px]"
                  aria-label={t('profile.currentPassword')}
                />
              </div>
              <div className="mb-3">
                <span className="mb-1 block text-[12px] text-[var(--color-text-secondary)]">
                  {t('profile.newPassword')}
                </span>
                <input
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  className="input-modern w-full text-[13px]"
                  aria-label={t('profile.newPassword')}
                />
              </div>
              <div className="mb-3">
                <span className="mb-1 block text-[12px] text-[var(--color-text-secondary)]">
                  {t('profile.confirmPassword')}
                </span>
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  className="input-modern w-full text-[13px]"
                  aria-label={t('profile.confirmPassword')}
                />
              </div>
              {passwordError && (
                <p className="mb-3 text-[12px] text-[var(--color-danger)]">{passwordError}</p>
              )}
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setIsChangingPassword(false)}
                  className="btn-secondary h-9 flex-1 px-4 text-[13px]"
                >
                  {t('common.cancel')}
                </button>
                <button
                  type="button"
                  onClick={handleChangePassword}
                  disabled={savingPassword || !currentPassword || !newPassword || !confirmPassword}
                  className="btn-primary h-9 flex-1 px-4 text-[13px] disabled:opacity-50"
                >
                  {savingPassword ? t('profile.changing') : t('profile.changePassword')}
                </button>
              </div>
            </>
          )}
        </div>
      ) : (
        <>
          <button
            type="button"
            onClick={startEditing}
            className="flex w-full items-center gap-3 px-4 py-2.5 text-[14px] text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-text-secondary)"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
            </svg>
            {t('profile.edit')}
          </button>
          <button
            type="button"
            onClick={startChangingPassword}
            className="flex w-full items-center gap-3 px-4 py-2.5 text-[14px] text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-text-secondary)"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
              <path d="M7 11V7a5 5 0 0 1 10 0v4" />
            </svg>
            {t('profile.changePassword')}
          </button>
          <button
            type="button"
            onClick={() => {
              setShowProfileMenu(false);
              toggleSettings();
            }}
            className="flex w-full items-center gap-3 px-4 py-2.5 text-[14px] text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-text-secondary)"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <circle cx="12" cy="12" r="3" />
              <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
            </svg>
            {t('tabs.settings')}
          </button>
          <button
            type="button"
            onClick={() => {
              setShowProfileMenu(false);
              logout();
            }}
            className="flex w-full items-center gap-3 px-4 py-2.5 text-[14px] text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-text-secondary)"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
              <polyline points="16 17 21 12 16 7" />
              <line x1="21" y1="12" x2="9" y2="12" />
            </svg>
            {t('auth.logout')}
          </button>
        </>
      )}
    </div>
  );

  return (
    <div className="glass-surface-heavy sticky top-0 z-30">
      {/* Mobile header (< sm) */}
      <div className="flex h-[48px] items-center px-2 sm:hidden">
        {/* Left: nav arrows + month label */}
        <div className="flex items-center gap-0.5">
          <button
            type="button"
            onClick={() => navigateMonth(-1)}
            className="flex h-8 w-8 items-center justify-center rounded-full hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('calendar.prevMonth')}
          >
            <ChevronLeft />
          </button>
          <button
            type="button"
            onClick={() => navigateMonth(1)}
            className="flex h-8 w-8 items-center justify-center rounded-full hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('calendar.nextMonth')}
          >
            <ChevronRight />
          </button>
          <span className="ml-0.5 text-[16px] font-semibold text-[var(--color-text-primary)]">
            {formatMonthYear(currentMonth, locale)}
          </span>
        </div>

        {/* Center: spacer */}
        <div className="flex-1" />

        {/* Right: Today + create + avatar */}
        <div className="flex items-center gap-0">
          <button
            type="button"
            onClick={handleGoToToday}
            className="flex h-8 items-center rounded-full bg-[var(--color-accent-bg)] px-2.5 text-[12px] font-medium text-[var(--color-accent)]"
          >
            <span className="leading-none">{t('calendar.today')}</span>
          </button>

          <button
            type="button"
            onClick={() => openEventModal()}
            className="flex h-8 w-8 items-center justify-center rounded-xl hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('event.createEvent')}
          >
            <PlusIcon className="h-5 w-5" />
          </button>

          <div className="relative" ref={profileMenuRef}>
            <AvatarButton size="sm" />
            {profileDropdown}
          </div>
        </div>
      </div>

      {/* PC header (sm+) */}
      <div className="hidden h-[56px] items-center px-3 sm:flex">
        {/* Left: month label + nav arrows + today */}
        <div className="flex items-center gap-1">
          <span className="mr-1 text-[16px] font-semibold text-[var(--color-text-primary)]">
            {formatMonthYear(currentMonth, locale)}
          </span>

          <button
            type="button"
            onClick={() => navigateMonth(-1)}
            className="flex h-8 w-8 items-center justify-center rounded-full hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('calendar.prevMonth')}
          >
            <ChevronLeft />
          </button>

          <button
            type="button"
            onClick={() => navigateMonth(1)}
            className="flex h-8 w-8 items-center justify-center rounded-full hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('calendar.nextMonth')}
          >
            <ChevronRight />
          </button>

          <button
            type="button"
            onClick={handleGoToToday}
            className="flex h-9 items-center rounded-full bg-[var(--color-accent-bg)] px-3 text-sm font-medium text-[var(--color-accent)]"
          >
            <span className="leading-none">{t('calendar.today')}</span>
          </button>
        </div>

        {/* Center: spacer */}
        <div className="flex-1" />

        {/* Right: view toggle + search + create + avatar */}
        <div className="flex items-center gap-0.5">
          <div className="segmented-control inline-flex">
            <button
              data-active={calendarView === 'month'}
              type="button"
              onClick={() => setCalendarView('month')}
            >
              {t('calendar.monthly')}
            </button>
            <button
              data-active={calendarView === 'week'}
              type="button"
              onClick={() => setCalendarView('week')}
            >
              {t('calendar.weekly')}
            </button>
          </div>

          <button
            type="button"
            onClick={toggleSearch}
            className="flex h-11 w-11 items-center justify-center rounded-xl hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('search.searchEvents')}
          >
            <SearchIcon />
          </button>

          <button
            type="button"
            onClick={() => openEventModal()}
            className="flex h-11 w-11 items-center justify-center rounded-xl hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('event.createEvent')}
          >
            <PlusIcon />
          </button>

          <div className="relative" ref={profileMenuRef}>
            <AvatarButton />
            {profileDropdown}
          </div>
        </div>
      </div>
    </div>
  );
}
