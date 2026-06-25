import { useNavigate } from '@tanstack/react-router';
import { DateTime } from 'luxon';
import { useEffect, useRef, useState } from 'react';
import { useT } from '@/i18n';
import { formatMonthYear } from '@/lib/date-utils';
import { useAuthStore } from '@/stores/auth-store';
import { useUiStore } from '@/stores/ui-store';

export function CalendarHeader() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const navigateMonth = useUiStore((s) => s.navigateMonth);
  const calendarView = useUiStore((s) => s.calendarView);
  const setCalendarView = useUiStore((s) => s.setCalendarView);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setShowMobileMenu = useUiStore((s) => s.setShowMobileMenu);
  const triggerScrollToToday = useUiStore((s) => s.triggerScrollToToday);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const toggleSearch = useUiStore((s) => s.toggleSearch);
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const [showProfileMenu, setShowProfileMenu] = useState(false);
  const profileMenuRef = useRef<HTMLDivElement>(null);
  const [showViewMenu, setShowViewMenu] = useState(false);
  const viewMenuRef = useRef<HTMLDivElement>(null);

  const VIEWS = [
    { value: 'month', label: t('calendar.monthly') },
    { value: 'week', label: t('calendar.weekly') },
    { value: 'list', label: t('calendar.list') },
    { value: 'year', label: t('calendar.year') },
  ] as const;
  const currentViewLabel = VIEWS.find((v) => v.value === calendarView)?.label ?? '';

  const goToProfile = () => {
    setShowProfileMenu(false);
    navigate({ to: '/settings', search: { tab: 'profile' } });
  };

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

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (viewMenuRef.current && !viewMenuRef.current.contains(e.target as Node)) {
        setShowViewMenu(false);
      }
    };
    if (showViewMenu) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showViewMenu]);

  const handleGoToToday = () => {
    const today = DateTime.now();
    setCurrentMonth(today.startOf('month'));
    setSelectedDate(today);
    // Mobile month view is an infinite scroll; signal it to scroll back to today.
    triggerScrollToToday();
  };

  // Prev/next navigation steps by the unit the active view shows: a week for the
  // weekly timeline, a year for the year grid, otherwise a month.
  const navigateByView = (dir: number) => {
    if (calendarView === 'week') {
      const next = selectedDate.plus({ weeks: dir });
      setSelectedDate(next);
      setCurrentMonth(next.startOf('month'));
    } else if (calendarView === 'year') {
      setCurrentMonth(currentMonth.plus({ years: dir }));
    } else {
      navigateMonth(dir);
    }
  };
  // The list view is a flat agenda with no date window, so it has no paging.
  const showDateNav = calendarView !== 'list';

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

  const ChevronDown = () => (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M6 9l6 6 6-6" />
    </svg>
  );

  const CheckIcon = () => (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-accent)"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M20 6L9 17l-5-5" />
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

  const AvatarButton = ({ size = 'default' }: { size?: 'default' | 'sm' }) => {
    const dimensions =
      size === 'sm' ? 'h-8 w-8 rounded-lg' : 'h-10 w-10 rounded-xl sm:h-11 sm:w-11';
    return (
      <button
        type="button"
        onClick={() => setShowProfileMenu(!showProfileMenu)}
        className={`flex items-center justify-center overflow-hidden hover:bg-[var(--color-hover)] active:bg-[var(--color-active)] ${dimensions}`}
        aria-label={t('profile.profile')}
      >
        {user?.avatarUrl ? (
          <img
            src={user.avatarUrl}
            alt=""
            className="h-full w-full object-cover"
            draggable={false}
          />
        ) : user?.icon ? (
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
  };

  const profileDropdown = showProfileMenu && (
    <div className="glass-surface-heavy absolute right-0 top-full z-50 mt-1 w-64 rounded-2xl py-2 ring-1 ring-[var(--color-border)]">
      {user && (
        <div className="border-b border-[var(--color-separator)] px-4 py-3">
          <div className="flex items-center gap-3">
            <div
              className="flex h-10 w-10 items-center justify-center overflow-hidden rounded-full text-lg text-white"
              style={{ backgroundColor: user.color }}
            >
              {user.avatarUrl ? (
                <img src={user.avatarUrl} alt="" className="h-full w-full object-cover" />
              ) : (
                <span>{user.icon}</span>
              )}
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-default font-bold text-[var(--color-text-primary)]">
                {user.name}
              </p>
              <p className="truncate text-footnote text-[var(--color-text-secondary)]">
                {user.email}
              </p>
            </div>
          </div>
        </div>
      )}
      <button
        type="button"
        onClick={goToProfile}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
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
        onClick={() => {
          setShowProfileMenu(false);
          navigate({ to: '/settings' });
        }}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
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
        className="flex w-full items-center gap-3 px-4 py-2.5 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
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
    </div>
  );

  return (
    <div className="glass-surface-heavy sticky top-0 z-30">
      {/* Mobile header (< sm) */}
      <div className="flex h-[48px] items-center gap-1 px-3 sm:hidden">
        {/* Left: drawer toggle + month label (follows scroll) + view switcher */}
        <button
          type="button"
          onClick={() => setShowMobileMenu(true)}
          aria-label={t('calendar.calendarList')}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-[var(--color-text-primary)] hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
          >
            <line x1="3" y1="6" x2="21" y2="6" />
            <line x1="3" y1="12" x2="21" y2="12" />
            <line x1="3" y1="18" x2="21" y2="18" />
          </svg>
        </button>
        <div className="flex min-w-0 items-center gap-1">
          <span className="truncate text-title font-bold tabular-nums text-[var(--color-text-primary)]">
            {formatMonthYear(currentMonth, locale)}
          </span>

          <div className="relative" ref={viewMenuRef}>
            <button
              type="button"
              onClick={() => setShowViewMenu((s) => !s)}
              className="flex h-7 items-center gap-0.5 rounded-full px-2 text-footnote font-semibold text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
              aria-haspopup="menu"
              aria-expanded={showViewMenu}
            >
              {currentViewLabel}
              <ChevronDown />
            </button>
            {showViewMenu && (
              <div className="glass-surface-heavy absolute left-0 top-full z-50 mt-1 w-40 rounded-2xl py-1.5 ring-1 ring-[var(--color-border)]">
                {VIEWS.map((v) => (
                  <button
                    key={v.value}
                    type="button"
                    onClick={() => {
                      setCalendarView(v.value);
                      setShowViewMenu(false);
                    }}
                    className="flex w-full items-center justify-between px-4 py-2.5 text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
                  >
                    {v.label}
                    {calendarView === v.value && <CheckIcon />}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Week/year paging (month uses infinite scroll; list is a flat agenda) */}
        {showDateNav && calendarView !== 'month' && (
          <div className="flex shrink-0 items-center">
            <button
              type="button"
              onClick={() => navigateByView(-1)}
              className="flex h-8 w-7 items-center justify-center rounded-lg hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
              aria-label={t('calendar.prevMonth')}
            >
              <ChevronLeft />
            </button>
            <button
              type="button"
              onClick={() => navigateByView(1)}
              className="flex h-8 w-7 items-center justify-center rounded-lg hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
              aria-label={t('calendar.nextMonth')}
            >
              <ChevronRight />
            </button>
          </div>
        )}

        {/* Center: spacer */}
        <div className="flex-1" />

        {/* Right: Today + create + avatar */}
        <div className="flex items-center gap-0">
          <button
            type="button"
            onClick={handleGoToToday}
            className="flex h-8 items-center rounded-full bg-[var(--color-accent-bg)] px-2.5 text-footnote font-medium text-[var(--color-accent)]"
          >
            <span className="leading-none">{t('calendar.today')}</span>
          </button>

          <button
            type="button"
            onClick={() => openEventModal()}
            className="flex h-8 w-8 items-center justify-center hover:opacity-80 active:scale-95 transition-all"
            style={{
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-accent)',
            }}
            aria-label={t('event.createEvent')}
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="white"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <line x1="12" y1="5" x2="12" y2="19" />
              <line x1="5" y1="12" x2="19" y2="12" />
            </svg>
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
          <span className="mr-1 text-subhead font-semibold tabular-nums text-[var(--color-text-primary)]">
            {formatMonthYear(currentMonth, locale)}
          </span>

          <button
            type="button"
            onClick={() => navigateByView(-1)}
            className="flex h-8 w-8 items-center justify-center rounded-full hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
            aria-label={t('calendar.prevMonth')}
          >
            <ChevronLeft />
          </button>

          <button
            type="button"
            onClick={() => navigateByView(1)}
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
            <button
              data-active={calendarView === 'list'}
              type="button"
              onClick={() => setCalendarView('list')}
            >
              {t('calendar.list')}
            </button>
            <button
              data-active={calendarView === 'year'}
              type="button"
              onClick={() => setCalendarView('year')}
            >
              {t('calendar.year')}
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
            className="flex h-9 items-center gap-1.5 px-3 font-medium text-default hover:opacity-85 active:scale-[0.97] transition-all"
            style={{
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-accent)',
              color: 'var(--color-text-on-accent, #fff)',
            }}
            aria-label={t('event.createEvent')}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <line x1="12" y1="5" x2="12" y2="19" />
              <line x1="5" y1="12" x2="19" y2="12" />
            </svg>
            <span className="hidden lg:inline">{t('event.createEvent')}</span>
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
