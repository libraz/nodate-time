import { DateTime } from 'luxon';
import { create } from 'zustand';
import type { Locale } from '@/i18n';
import { detectHolidayCountry } from '@/lib/holidays';
import type { AccountPreferences } from '@/lib/preferences';
import { detectTimezone } from '@/lib/preferences';
import { loadJson, saveJson } from '@/lib/storage';
import type { ColorMode, ThemeStyle } from '@/lib/theme';
import type { CalendarView } from '@/types/calendar';

export type RightPanelId = 'memo' | 'album' | 'members' | 'share' | null;

export type MobileTab = 'calendar' | 'memo' | 'search' | 'settings';

interface UiState {
  calendarView: CalendarView;
  selectedDate: DateTime;
  currentMonth: DateTime;
  showEventModal: boolean;
  editingEventId: string | null;
  /** Preselected start for a new timed event (e.g. a clicked weekly slot). */
  eventDraftStart: DateTime | null;
  showDayDetail: boolean;
  rightPanel: RightPanelId;
  showSearch: boolean;
  searchQuery: string;
  mobileTab: MobileTab;
  showSettings: boolean;
  /** Mobile-only slide-in drawer holding the calendar list and panel triggers. */
  showMobileMenu: boolean;
  /** Activity (history) feed overlay; reachable from both desktop and mobile. */
  showActivity: boolean;
  scrollToTodaySignal: number;

  theme: ThemeStyle;
  colorMode: ColorMode;
  locale: Locale;
  timezone: string;
  holidaysCountry: string | null;

  setCalendarView: (view: CalendarView) => void;
  setSelectedDate: (date: DateTime) => void;
  setCurrentMonth: (date: DateTime) => void;
  openEventModal: (eventId?: string, draftStart?: DateTime) => void;
  closeEventModal: () => void;
  openDayDetail: (date: DateTime) => void;
  closeDayDetail: () => void;
  navigateMonth: (delta: number) => void;
  toggleRightPanel: (panel: RightPanelId) => void;
  toggleSearch: () => void;
  setSearchQuery: (query: string) => void;
  setMobileTab: (tab: MobileTab) => void;
  toggleSettings: () => void;
  setShowMobileMenu: (show: boolean) => void;
  setShowActivity: (show: boolean) => void;
  triggerScrollToToday: () => void;
  setTheme: (theme: ThemeStyle) => void;
  setColorMode: (mode: ColorMode) => void;
  setHolidaysCountry: (country: string | null) => void;
  /**
   * Takes on the preferences stored against the signed-in account.
   *
   * The account decides, not the device: a timezone read from the browser
   * makes a calendar say different times on a laptop and a phone, and the
   * one thing a calendar cannot afford to be is device-dependent.
   */
  adoptAccountPreferences: (prefs: AccountPreferences) => void;
  resetSessionUi: () => void;
}

export const useUiStore = create<UiState>((set) => ({
  calendarView: 'month',
  selectedDate: DateTime.now(),
  currentMonth: DateTime.now().startOf('month'),
  showEventModal: false,
  editingEventId: null,
  eventDraftStart: null,
  showDayDetail: false,
  rightPanel: null,
  showSearch: false,
  searchQuery: '',
  mobileTab: 'calendar' as MobileTab,
  showSettings: false,
  showMobileMenu: false,
  showActivity: false,
  scrollToTodaySignal: 0,

  theme: loadJson<ThemeStyle>('theme', 'glass'),
  colorMode: loadJson<ColorMode>('colorMode', 'system'),
  locale: loadJson<Locale>('locale', 'ja'),
  timezone: loadJson<string>('timezone', detectTimezone()),
  holidaysCountry: loadJson<string | null>('holidaysCountry', detectHolidayCountry()),

  setCalendarView: (view) => set({ calendarView: view }),
  setSelectedDate: (date) => set({ selectedDate: date }),
  setCurrentMonth: (date) => set({ currentMonth: date }),

  openEventModal: (eventId, draftStart) =>
    set({
      showEventModal: true,
      editingEventId: eventId ?? null,
      eventDraftStart: draftStart ?? null,
    }),

  closeEventModal: () =>
    set({ showEventModal: false, editingEventId: null, eventDraftStart: null }),

  openDayDetail: (date) => set({ selectedDate: date, showDayDetail: true }),

  closeDayDetail: () => set({ showDayDetail: false }),

  navigateMonth: (delta) =>
    set((s) => ({
      currentMonth: s.currentMonth.plus({ months: delta }),
    })),

  toggleRightPanel: (panel) =>
    set((s) => ({
      rightPanel: s.rightPanel === panel ? null : panel,
    })),

  toggleSearch: () => set((s) => ({ showSearch: !s.showSearch, searchQuery: '' })),
  setSearchQuery: (query) => set({ searchQuery: query }),
  setMobileTab: (tab) => set({ mobileTab: tab }),
  toggleSettings: () => set((s) => ({ showSettings: !s.showSettings })),
  setShowMobileMenu: (show) => set({ showMobileMenu: show }),
  setShowActivity: (show) => set({ showActivity: show }),
  triggerScrollToToday: () => set((s) => ({ scrollToTodaySignal: s.scrollToTodaySignal + 1 })),

  setTheme: (theme) => {
    saveJson('theme', theme);
    set({ theme });
  },
  setColorMode: (mode) => {
    saveJson('colorMode', mode);
    set({ colorMode: mode });
  },
  setHolidaysCountry: (country) => {
    saveJson('holidaysCountry', country);
    set({ holidaysCountry: country });
  },
  adoptAccountPreferences: (prefs) => {
    // Persisted as well as applied: the next reload reads local storage
    // before the account has been fetched, and a flash of the wrong
    // timezone moves every event on screen.
    if (prefs.locale) {
      saveJson('locale', prefs.locale);
      set({ locale: prefs.locale });
    }
    if (prefs.timezone) {
      saveJson('timezone', prefs.timezone);
      set({ timezone: prefs.timezone });
    }
  },
  resetSessionUi: () =>
    set({
      showEventModal: false,
      editingEventId: null,
      eventDraftStart: null,
      showDayDetail: false,
      rightPanel: null,
      showSearch: false,
      searchQuery: '',
      mobileTab: 'calendar',
      showSettings: false,
      showMobileMenu: false,
      showActivity: false,
    }),
}));
