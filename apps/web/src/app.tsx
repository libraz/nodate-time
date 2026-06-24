import { useEffect, useMemo, useRef, useState } from 'react';
import { AlbumPanel } from '@/components/album-panel';
import { CalendarGrid } from '@/components/calendar-grid';
import { CalendarHeader } from '@/components/calendar-header';
import { DayDetail } from '@/components/day-detail';
import { EventModal } from '@/components/event-modal';
import { FabButton } from '@/components/fab-button';
import { LeftSidebar } from '@/components/left-sidebar';
import { ListView } from '@/components/list-view';
import { MembersPanel } from '@/components/members-panel';
import { MonthScroll } from '@/components/month-scroll';
import { NotificationsPanel } from '@/components/notifications-panel';
import { MemoSection, SettingsModal } from '@/components/right-panel';
import { RightSidebar } from '@/components/right-sidebar';
import { SearchPanel } from '@/components/search-panel';
import { SharePanel } from '@/components/share-panel';
import { WeeklyTimeline } from '@/components/weekly-timeline';
import { YearView } from '@/components/year-view';
import { useT } from '@/i18n';
import { fromISOInZone } from '@/lib/date-utils';
import { useCalendarStore } from '@/stores/calendar-store';
import type { MobileTab } from '@/stores/ui-store';
import { useUiStore } from '@/stores/ui-store';

function MobileSearchView() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const events = useCalendarStore((s) => s.events);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const setMobileTab = useUiStore((s) => s.setMobileTab);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const [query, setQuery] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const q = query.trim().toLowerCase();
  const filtered = useMemo(() => {
    if (!q) return [];
    return events.filter(
      (evt) =>
        evt.title.toLowerCase().includes(q) ||
        evt.location.toLowerCase().includes(q) ||
        evt.memo.toLowerCase().includes(q),
    );
  }, [q, events]);

  const timezone = useUiStore((s) => s.timezone);
  const handleSelect = (eventId: string, startAt: string) => {
    const dt = fromISOInZone(startAt, timezone);
    setCurrentMonth(dt.startOf('month'));
    setSelectedDate(dt);
    setMobileTab('calendar');
    openEventModal(eventId);
  };

  return (
    <div className="flex h-full flex-col">
      <div className="px-4 py-3">
        <div className="flex items-center gap-3 rounded-xl bg-[var(--color-surface-inset)] px-4 py-2.5">
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
            <circle cx="11" cy="11" r="7" />
            <line x1="16.65" y1="16.65" x2="21" y2="21" />
          </svg>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('search.placeholder')}
            className="flex-1 bg-transparent text-callout outline-none placeholder:text-[var(--color-text-tertiary)]"
          />
        </div>
      </div>
      <div className="flex-1 overflow-y-auto">
        {q === '' ? (
          <div className="flex flex-col items-center justify-center gap-2 py-16 text-[var(--color-text-tertiary)]">
            <svg
              width="24"
              height="24"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              opacity="0.5"
            >
              <circle cx="11" cy="11" r="7" />
              <line x1="16.65" y1="16.65" x2="21" y2="21" />
            </svg>
            <span className="text-callout">{t('search.searchEvents')}</span>
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex items-center justify-center py-16 text-callout text-[var(--color-text-tertiary)]">
            {t('search.noResults')}
          </div>
        ) : (
          <div className="py-1">
            {filtered.map((evt) => {
              const dt = fromISOInZone(evt.startAt, timezone);
              const endDt = fromISOInZone(evt.endAt, timezone);
              const dateLabel = evt.allDay
                ? dt.toFormat('yyyy/MM/dd (EEE)', { locale })
                : `${dt.toFormat('yyyy/MM/dd (EEE) HH:mm', { locale })} - ${endDt.toFormat('HH:mm')}`;
              return (
                <button
                  key={evt.id}
                  type="button"
                  onClick={() => handleSelect(evt.id, evt.startAt)}
                  className="flex w-full items-start gap-3 px-4 py-3.5 text-left hover:bg-[var(--color-hover)]"
                >
                  <span
                    className="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-md"
                    style={{ backgroundColor: evt.color }}
                  />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-callout font-medium text-[var(--color-text-primary)]">
                      {evt.title}
                    </p>
                    <p className="text-body text-[var(--color-text-secondary)]">{dateLabel}</p>
                    {evt.location && (
                      <p className="truncate text-body text-[var(--color-text-tertiary)]">
                        {evt.location}
                      </p>
                    )}
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function MobileSettingsView() {
  const t = useT();
  const theme = useUiStore((s) => s.theme);
  const colorMode = useUiStore((s) => s.colorMode);
  const locale = useUiStore((s) => s.locale);
  const setTheme = useUiStore((s) => s.setTheme);
  const setColorMode = useUiStore((s) => s.setColorMode);
  const setLocale = useUiStore((s) => s.setLocale);

  return (
    <div className="flex h-full flex-col overflow-y-auto px-4 py-4">
      <h3 className="mb-3 text-footnote font-semibold uppercase tracking-wider text-[var(--color-text-secondary)]">
        {t('settings.appearance')}
      </h3>

      <div className="mb-4">
        <span className="mb-2 block text-body text-[var(--color-text-primary)]">
          {t('settings.theme')}
        </span>
        <div className="segmented-control w-full">
          {(['glass', 'classic', 'nothing'] as const).map((v) => (
            <button
              key={v}
              type="button"
              data-active={theme === v}
              onClick={() => setTheme(v)}
              className="flex-1"
            >
              {v === 'glass'
                ? t('settings.themeGlass')
                : v === 'classic'
                  ? t('settings.themeClassic')
                  : t('settings.themeNothing')}
            </button>
          ))}
        </div>
      </div>

      <div className="mb-4">
        <span className="mb-2 block text-body text-[var(--color-text-primary)]">
          {t('settings.colorMode')}
        </span>
        <div className="segmented-control w-full">
          {(['light', 'dark', 'system'] as const).map((v) => (
            <button
              key={v}
              type="button"
              data-active={colorMode === v}
              onClick={() => setColorMode(v)}
              className="flex-1"
            >
              {v === 'light'
                ? t('settings.modeLight')
                : v === 'dark'
                  ? t('settings.modeDark')
                  : t('settings.modeSystem')}
            </button>
          ))}
        </div>
      </div>

      <div className="mb-4">
        <span className="mb-2 block text-body text-[var(--color-text-primary)]">
          {t('settings.language')}
        </span>
        <div className="segmented-control w-full">
          {(['ja', 'en'] as const).map((v) => (
            <button
              key={v}
              type="button"
              data-active={locale === v}
              onClick={() => setLocale(v)}
              className="flex-1"
            >
              {v === 'ja' ? '\u65E5\u672C\u8A9E' : 'English'}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

const TAB_ICONS: Record<MobileTab, React.ReactNode> = {
  calendar: (
    <svg
      width="22"
      height="22"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="3" y="4" width="18" height="18" rx="2" />
      <line x1="16" y1="2" x2="16" y2="6" />
      <line x1="8" y1="2" x2="8" y2="6" />
      <line x1="3" y1="10" x2="21" y2="10" />
    </svg>
  ),
  memo: (
    <svg
      width="22"
      height="22"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
      <polyline points="14 2 14 8 20 8" />
      <line x1="16" y1="13" x2="8" y2="13" />
      <line x1="16" y1="17" x2="8" y2="17" />
    </svg>
  ),
  search: (
    <svg
      width="22"
      height="22"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="11" cy="11" r="7" />
      <line x1="16.65" y1="16.65" x2="21" y2="21" />
    </svg>
  ),
  settings: (
    <svg
      width="22"
      height="22"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z" />
    </svg>
  ),
};

export function App() {
  const t = useT();
  const calendarView = useUiStore((s) => s.calendarView);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const mobileTab = useUiStore((s) => s.mobileTab);
  const setMobileTab = useUiStore((s) => s.setMobileTab);
  const fetchCalendars = useCalendarStore((s) => s.fetchCalendars);
  const fetchEvents = useCalendarStore((s) => s.fetchEvents);
  const fetchMemos = useCalendarStore((s) => s.fetchMemos);
  const calendars = useCalendarStore((s) => s.calendars);
  const initDone = useRef(false);

  useEffect(() => {
    if (initDone.current) return;
    initDone.current = true;
    fetchCalendars();
  }, [fetchCalendars]);

  useEffect(() => {
    if (calendars.length === 0) return;
    const start = currentMonth.minus({ months: 1 }).toFormat('yyyy-MM-dd');
    const end = currentMonth.plus({ months: 2 }).toFormat('yyyy-MM-dd');
    fetchEvents(start, end);
    fetchMemos();
  }, [calendars, currentMonth, fetchEvents, fetchMemos]);

  const calendarContent = (
    <div className="relative flex-1 overflow-hidden">
      {calendarView === 'month' && (
        <div className="h-full">
          <CalendarGrid />
        </div>
      )}
      {calendarView === 'week' && <WeeklyTimeline />}
      {calendarView === 'list' && <ListView />}
      {calendarView === 'year' && <YearView />}
      <EventModal />
    </div>
  );

  // Mobile month view is a continuous vertical infinite scroll (no month paging).
  const mobileCalendarContent = (
    <div className="relative flex-1 overflow-hidden">
      {calendarView === 'month' ? (
        <MonthScroll />
      ) : calendarView === 'week' ? (
        <WeeklyTimeline />
      ) : calendarView === 'list' ? (
        <ListView />
      ) : (
        <YearView />
      )}
      <EventModal />
    </div>
  );

  return (
    <div className="app-bg relative flex h-full flex-col">
      <div className="relative z-[1] contents">
        <CalendarHeader />
      </div>

      {/* PC layout: sidebar + calendar */}
      <div className="relative z-[1] hidden flex-1 overflow-hidden sm:flex">
        <LeftSidebar />
        {calendarContent}
        <RightSidebar />
      </div>

      {/* SP layout: tab-switched content */}
      <div className="relative z-[1] flex flex-1 flex-col overflow-hidden pb-[calc(52px+env(safe-area-inset-bottom))] sm:hidden">
        {mobileTab === 'calendar' && mobileCalendarContent}
        {mobileTab === 'memo' && <MemoSection />}
        {mobileTab === 'search' && <MobileSearchView />}
        {mobileTab === 'settings' && <MobileSettingsView />}
      </div>

      {/* Mobile bottom tabs */}
      <div
        className="glass-surface-heavy fixed bottom-0 left-0 right-0 z-40 flex border-t border-[var(--color-border)] sm:hidden"
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        {(['calendar', 'memo', 'search', 'settings'] as const).map((tab) => (
          <button
            key={tab}
            type="button"
            onClick={() => setMobileTab(tab)}
            className="flex flex-1 flex-col items-center gap-0.5 py-2"
            style={{
              color: mobileTab === tab ? 'var(--color-accent)' : 'var(--color-text-tertiary)',
            }}
          >
            {TAB_ICONS[tab]}
            <span className="text-micro font-medium">{t(`tabs.${tab}`)}</span>
          </button>
        ))}
      </div>

      {/* Mobile FAB for creating events */}
      <FabButton />

      {/* Settings modal (PC) */}
      <SettingsModal />

      {/* Day detail overlay */}
      <DayDetail />

      {/* Search overlay / panel (PC only) */}
      <SearchPanel />

      {/* Album overlay */}
      <AlbumPanel />

      {/* Members overlay */}
      <MembersPanel />

      {/* Notifications overlay */}
      <NotificationsPanel />

      {/* Share overlay */}
      <SharePanel />
    </div>
  );
}
