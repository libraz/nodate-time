import type { Locale } from '@/i18n';
import { loadJson, saveJson } from '@/lib/storage';
import type { ColorMode, ThemeStyle } from '@/lib/theme';
import type { CalendarView } from '@/types/calendar';
import { DateTime } from 'luxon';
import { create } from 'zustand';

export type RightPanelId = 'memo' | 'album' | 'members' | 'notifications' | 'share' | null;

export type MobileTab = 'calendar' | 'memo' | 'search' | 'settings';

interface UiState {
  calendarView: CalendarView;
  selectedDate: DateTime;
  currentMonth: DateTime;
  showEventModal: boolean;
  editingEventId: string | null;
  showDayDetail: boolean;
  rightPanel: RightPanelId;
  leftSidebarExpanded: boolean;
  showSearch: boolean;
  searchQuery: string;
  mobileTab: MobileTab;
  showSettings: boolean;

  theme: ThemeStyle;
  colorMode: ColorMode;
  locale: Locale;
  timezone: string;
  holidaysCountry: string | null;

  setCalendarView: (view: CalendarView) => void;
  setSelectedDate: (date: DateTime) => void;
  setCurrentMonth: (date: DateTime) => void;
  openEventModal: (eventId?: string) => void;
  closeEventModal: () => void;
  openDayDetail: (date: DateTime) => void;
  closeDayDetail: () => void;
  navigateMonth: (delta: number) => void;
  toggleRightPanel: (panel: RightPanelId) => void;
  toggleLeftSidebar: () => void;
  toggleSearch: () => void;
  setSearchQuery: (query: string) => void;
  setMobileTab: (tab: MobileTab) => void;
  toggleSettings: () => void;
  setTheme: (theme: ThemeStyle) => void;
  setColorMode: (mode: ColorMode) => void;
  setLocale: (locale: Locale) => void;
  setTimezone: (tz: string) => void;
  setHolidaysCountry: (country: string | null) => void;
}

export const useUiStore = create<UiState>((set) => ({
  calendarView: 'month',
  selectedDate: DateTime.now(),
  currentMonth: DateTime.now().startOf('month'),
  showEventModal: false,
  editingEventId: null,
  showDayDetail: false,
  rightPanel: null,
  leftSidebarExpanded: false,
  showSearch: false,
  searchQuery: '',
  mobileTab: 'calendar' as MobileTab,
  showSettings: false,

  theme: loadJson<ThemeStyle>('theme', 'glass'),
  colorMode: loadJson<ColorMode>('colorMode', 'system'),
  locale: loadJson<Locale>('locale', 'ja'),
  timezone: loadJson<string>('timezone', Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'),
  holidaysCountry: loadJson<string | null>('holidaysCountry', 'JP'),

  setCalendarView: (view) => set({ calendarView: view }),
  setSelectedDate: (date) => set({ selectedDate: date }),
  setCurrentMonth: (date) => set({ currentMonth: date }),

  openEventModal: (eventId) => set({ showEventModal: true, editingEventId: eventId ?? null }),

  closeEventModal: () => set({ showEventModal: false, editingEventId: null }),

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

  toggleLeftSidebar: () => set((s) => ({ leftSidebarExpanded: !s.leftSidebarExpanded })),

  toggleSearch: () => set((s) => ({ showSearch: !s.showSearch, searchQuery: '' })),
  setSearchQuery: (query) => set({ searchQuery: query }),
  setMobileTab: (tab) => set({ mobileTab: tab }),
  toggleSettings: () => set((s) => ({ showSettings: !s.showSettings })),

  setTheme: (theme) => {
    saveJson('theme', theme);
    set({ theme });
  },
  setColorMode: (mode) => {
    saveJson('colorMode', mode);
    set({ colorMode: mode });
  },
  setLocale: (locale) => {
    saveJson('locale', locale);
    set({ locale });
  },
  setTimezone: (tz) => {
    saveJson('timezone', tz);
    set({ timezone: tz });
  },
  setHolidaysCountry: (country) => {
    saveJson('holidaysCountry', country);
    set({ holidaysCountry: country });
  },
}));
