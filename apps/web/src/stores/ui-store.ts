import type { DateTime } from 'luxon';
import { create } from 'zustand';
import type { Locale } from '@/i18n';
import { isWeekStart, nowInZone, type WeekStart } from '@/lib/date-utils';
import { detectHolidayCountry } from '@/lib/holidays';
import type { AccountPreferences } from '@/lib/preferences';
import { detectTimezone } from '@/lib/preferences';
import { loadJson, saveJson } from '@/lib/storage';
import type { ColorMode, ThemeStyle } from '@/lib/theme';
import type { CalendarView } from '@/types/calendar';

export type RightPanelId = 'memo' | 'album' | 'members' | 'labels' | 'public' | null;

export type MobileTab = 'calendar' | 'memo' | 'search' | 'settings';

/** An open asked for while the editor was already open, waiting for the editor's answer. */
export interface PendingEventOpen {
  eventId: string | null;
  draftStart: DateTime | null;
  draftEnd: DateTime | null;
  /** The selected day when the open was asked for; a new draft without a start lands on it. */
  draftDay: DateTime;
}

interface UiState {
  calendarView: CalendarView;
  selectedDate: DateTime;
  currentMonth: DateTime;
  showEventModal: boolean;
  editingEventId: string | null;
  /** Preselected start for a new timed event (e.g. a clicked weekly slot). */
  eventDraftStart: DateTime | null;
  /** End of a range picked on the timeline; absent, a draft runs one hour. */
  eventDraftEnd: DateTime | null;
  /** Day a new all-day draft opens on, fixed when the open happened. */
  eventDraftDay: DateTime | null;
  /** Counts editor openings, so the form rebuilds even when the id is unchanged. */
  eventOpenSeq: number;
  pendingEventOpen: PendingEventOpen | null;
  showDayDetail: boolean;
  rightPanel: RightPanelId;
  /**
   * The calendar the last openPanelFor named. A fresh object per call, so a
   * panel adopts it even when the same calendar is asked for twice.
   * `addMembers` opens the members panel straight on its picker.
   */
  panelCalendarRequest: { calendarId: string; addMembers?: boolean } | null;
  showSearch: boolean;
  searchQuery: string;
  mobileTab: MobileTab;
  /** Mobile-only slide-in drawer holding the calendar list and panel triggers. */
  showMobileMenu: boolean;
  /** Activity (history) feed overlay; reachable from both desktop and mobile. */
  showActivity: boolean;
  scrollToTodaySignal: number;

  theme: ThemeStyle;
  colorMode: ColorMode;
  locale: Locale;
  timezone: string;
  weekStart: WeekStart;
  holidaysCountry: string | null;

  setCalendarView: (view: CalendarView) => void;
  setSelectedDate: (date: DateTime) => void;
  setCurrentMonth: (date: DateTime) => void;
  /**
   * Opens the editor, or, while it is open, queues the request for the editor
   * to apply or to confirm discarding unsaved edits first.
   */
  openEventModal: (eventId?: string, draftStart?: DateTime, draftEnd?: DateTime) => void;
  applyPendingEventOpen: () => void;
  clearPendingEventOpen: () => void;
  closeEventModal: () => void;
  openDayDetail: (date: DateTime) => void;
  closeDayDetail: () => void;
  navigateMonth: (delta: number) => void;
  /** Steps by the unit the active view shows: a week, a year, otherwise a month. */
  stepView: (dir: number) => void;
  /** Selects today and brings it on screen, including in the mobile month scroller. */
  goToToday: () => void;
  toggleRightPanel: (panel: RightPanelId) => void;
  /** Opens a side panel pointed at one calendar, closing the mobile menu it may come from. */
  openPanelFor: (
    panel: Exclude<RightPanelId, null>,
    calendarId: string,
    opts?: { addMembers?: boolean },
  ) => void;
  toggleSearch: () => void;
  setSearchQuery: (query: string) => void;
  setMobileTab: (tab: MobileTab) => void;
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

/** The day and month the calendar opens on: today in `zone`, never the device's date. */
function openingDates(zone: string): { selectedDate: DateTime; currentMonth: DateTime } {
  const today = nowInZone(zone);
  return { selectedDate: today, currentMonth: today.startOf('month') };
}

const storedWeekStart = loadJson<unknown>('weekStart', 'sun');
const initialWeekStart: WeekStart = isWeekStart(storedWeekStart) ? storedWeekStart : 'sun';
const initialTimezone = loadJson<string>('timezone', detectTimezone());

export const useUiStore = create<UiState>((set, get) => ({
  calendarView: 'month',
  ...openingDates(initialTimezone),
  showEventModal: false,
  editingEventId: null,
  eventDraftStart: null,
  eventDraftEnd: null,
  eventDraftDay: null,
  eventOpenSeq: 0,
  pendingEventOpen: null,
  showDayDetail: false,
  rightPanel: null,
  panelCalendarRequest: null,
  showSearch: false,
  searchQuery: '',
  mobileTab: 'calendar' as MobileTab,
  showMobileMenu: false,
  showActivity: false,
  scrollToTodaySignal: 0,

  theme: loadJson<ThemeStyle>('theme', 'glass'),
  colorMode: loadJson<ColorMode>('colorMode', 'system'),
  locale: loadJson<Locale>('locale', 'ja'),
  timezone: initialTimezone,
  weekStart: initialWeekStart,
  holidaysCountry: loadJson<string | null>('holidaysCountry', detectHolidayCountry()),

  setCalendarView: (view) => set({ calendarView: view }),
  setSelectedDate: (date) => set({ selectedDate: date }),
  setCurrentMonth: (date) => set({ currentMonth: date }),

  openEventModal: (eventId, draftStart, draftEnd) =>
    set((s) => {
      if (s.showEventModal) {
        return {
          pendingEventOpen: {
            eventId: eventId ?? null,
            draftStart: draftStart ?? null,
            draftEnd: draftEnd ?? null,
            draftDay: s.selectedDate,
          },
        };
      }
      return {
        showEventModal: true,
        editingEventId: eventId ?? null,
        eventDraftStart: draftStart ?? null,
        eventDraftEnd: draftEnd ?? null,
        eventDraftDay: draftStart ? null : s.selectedDate,
        eventOpenSeq: s.eventOpenSeq + 1,
      };
    }),

  applyPendingEventOpen: () =>
    set((s) => {
      const p = s.pendingEventOpen;
      if (!p) return {};
      return {
        editingEventId: p.eventId,
        eventDraftStart: p.draftStart,
        eventDraftEnd: p.draftEnd,
        eventDraftDay: p.draftStart ? null : p.draftDay,
        eventOpenSeq: s.eventOpenSeq + 1,
        pendingEventOpen: null,
      };
    }),

  clearPendingEventOpen: () => set({ pendingEventOpen: null }),

  closeEventModal: () =>
    set({
      showEventModal: false,
      editingEventId: null,
      eventDraftStart: null,
      eventDraftEnd: null,
      eventDraftDay: null,
      pendingEventOpen: null,
    }),

  openDayDetail: (date) => set({ selectedDate: date, showDayDetail: true }),

  closeDayDetail: () => set({ showDayDetail: false }),

  navigateMonth: (delta) =>
    set((s) => ({
      currentMonth: s.currentMonth.plus({ months: delta }),
    })),

  stepView: (dir) => {
    const { calendarView, selectedDate, currentMonth } = get();
    if (calendarView === 'week') {
      const next = selectedDate.plus({ weeks: dir });
      set({ selectedDate: next, currentMonth: next.startOf('month') });
    } else if (calendarView === 'year') {
      set({ currentMonth: currentMonth.plus({ years: dir }) });
    } else {
      set({ currentMonth: currentMonth.plus({ months: dir }) });
    }
  },

  goToToday: () =>
    set((s) => ({
      ...openingDates(s.timezone),
      scrollToTodaySignal: s.scrollToTodaySignal + 1,
    })),

  toggleRightPanel: (panel) =>
    set((s) => ({
      rightPanel: s.rightPanel === panel ? null : panel,
    })),

  openPanelFor: (panel, calendarId, opts) =>
    set({
      rightPanel: panel,
      panelCalendarRequest: { calendarId, ...(opts?.addMembers ? { addMembers: true } : {}) },
      showMobileMenu: false,
    }),

  toggleSearch: () => set((s) => ({ showSearch: !s.showSearch, searchQuery: '' })),
  setSearchQuery: (query) => set({ searchQuery: query }),
  setMobileTab: (tab) => set({ mobileTab: tab }),
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
    if (prefs.weekStart) {
      saveJson('weekStart', prefs.weekStart);
      set({ weekStart: prefs.weekStart });
    }
    if (prefs.timezone) {
      saveJson('timezone', prefs.timezone);
      const { timezone: previous, selectedDate, currentMonth } = get();
      // Still on the opening day and month of the old zone means the user has not
      // navigated: reopen on today in the account's zone.
      const before = openingDates(previous);
      const untouched =
        selectedDate.hasSame(before.selectedDate, 'day') &&
        currentMonth.hasSame(before.currentMonth, 'month');
      set(
        untouched && previous !== prefs.timezone
          ? { timezone: prefs.timezone, ...openingDates(prefs.timezone) }
          : { timezone: prefs.timezone },
      );
    }
  },
  resetSessionUi: () =>
    set((s) => ({
      ...openingDates(s.timezone),
      showEventModal: false,
      editingEventId: null,
      eventDraftStart: null,
      eventDraftEnd: null,
      eventDraftDay: null,
      pendingEventOpen: null,
      showDayDetail: false,
      rightPanel: null,
      panelCalendarRequest: null,
      showSearch: false,
      searchQuery: '',
      mobileTab: 'calendar',
      showMobileMenu: false,
      showActivity: false,
    })),
}));
