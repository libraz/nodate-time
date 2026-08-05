import { DateTime } from 'luxon';
import { create } from 'zustand';
import { api, errorMessage } from '@/lib/api';
import { loadJson, saveJson } from '@/lib/storage';
import { toast } from '@/lib/toast';
import { useUiStore } from '@/stores/ui-store';
import type {
  Calendar,
  CalendarEvent,
  Flexibility,
  Label,
  Member,
  Memo,
  RecurrenceRule,
  ShowAs,
  Visibility,
} from '@/types/calendar';

/** Request body shared by event create and update. */
export interface EventInput {
  title: string;
  allDay: boolean;
  startAt: string;
  endAt: string;
  timezone?: string | undefined;
  location?: string | undefined;
  memo?: string | undefined;
  url?: string | undefined;
  notificationOffset?: number | null | undefined;
  participants?: string[] | undefined;
  ownerId?: string | null | undefined;
  recurrenceRule?: RecurrenceRule | null | undefined;
  // showAs, flexibility and visibility are omitted-means-unchanged on the
  // wire. They have to be listed here all the same: a caller that carries
  // them forward -- a drag, say -- must be able to send them back, or the
  // server's defaults would silently overwrite what the user chose.
  showAs?: ShowAs | undefined;
  flexibility?: Flexibility | undefined;
  visibility?: Visibility | undefined;
}

interface CalendarState {
  calendars: Calendar[];
  events: CalendarEvent[];
  memos: Memo[];
  membersMap: Record<string, Member[]>;
  labels: Label[];
  activeCalendarIds: string[];
  isLoading: boolean;

  fetchCalendars: () => Promise<void>;
  fetchEvents: (start: string, end: string) => Promise<void>;
  fetchMemos: () => Promise<void>;
  fetchMembers: (calendarId: string) => Promise<void>;
  fetchLabels: (calendarId: string) => Promise<void>;

  addCalendar: (cal: { name: string; color: string }) => Promise<void>;
  updateCalendar: (
    id: string,
    patch: { name?: string; color?: string; coverUrl?: string },
  ) => Promise<void>;
  deleteCalendar: (id: string) => Promise<void>;

  addEvent: (calendarId: string, evt: EventInput) => Promise<void>;
  updateEvent: (
    calendarId: string,
    eventId: string,
    evt: EventInput,
    scope?: 'this' | 'all',
  ) => Promise<void>;
  deleteEvent: (calendarId: string, eventId: string, scope?: 'this' | 'all') => Promise<void>;

  addMemo: (calendarId: string, memo: { title: string; body: string }) => Promise<void>;
  updateMemo: (
    calendarId: string,
    memoId: string,
    patch: { title: string; body: string; done: boolean },
  ) => Promise<void>;
  toggleMemo: (calendarId: string, memoId: string, done: boolean, title: string) => Promise<void>;
  deleteMemo: (calendarId: string, memoId: string) => Promise<void>;

  toggleCalendarFilter: (calId: string) => void;
  setActiveCalendarIds: (ids: string[]) => void;
  resetSessionData: () => void;

  /** Returns the currently visible event window as ISO date strings. */
  visibleRange: () => { start: string; end: string };
}

let accountGeneration = 0;
let calendarRequestGeneration = 0;
let eventRequestGeneration = 0;
let memoRequestGeneration = 0;

export const useCalendarStore = create<CalendarState>((set, get) => ({
  calendars: [],
  events: [],
  memos: [],
  membersMap: {},
  labels: [],
  activeCalendarIds: loadJson<string[]>('activeCalendarIds', []),
  isLoading: false,

  async fetchCalendars() {
    const requestGeneration = ++calendarRequestGeneration;
    const currentAccountGeneration = accountGeneration;
    set({ isLoading: true });
    try {
      const cals = await api.get<Calendar[]>('/calendars');
      if (
        requestGeneration !== calendarRequestGeneration ||
        currentAccountGeneration !== accountGeneration
      )
        return;
      const saved = loadJson<string[]>('activeCalendarIds', []);
      const calendarIDs = cals.map((c) => c.id);
      const savedActive = saved.filter((id) => calendarIDs.includes(id));
      const newIDs = calendarIDs.filter((id) => !saved.includes(id));
      const ids = saved.length > 0 ? [...savedActive, ...newIDs] : calendarIDs;
      set({ calendars: cals, activeCalendarIds: ids });
      saveJson('activeCalendarIds', ids);

      const memberResults = await Promise.allSettled(cals.map((c) => get().fetchMembers(c.id)));
      if (
        requestGeneration !== calendarRequestGeneration ||
        currentAccountGeneration !== accountGeneration
      )
        return;
      for (const result of memberResults) {
        if (result.status === 'rejected') toast.error(errorMessage(result.reason));
      }
      const first = cals[0];
      if (first && get().labels.length === 0) {
        try {
          await get().fetchLabels(first.id);
        } catch (e) {
          toast.error(errorMessage(e));
        }
      }
    } finally {
      if (
        requestGeneration === calendarRequestGeneration &&
        currentAccountGeneration === accountGeneration
      ) {
        set({ isLoading: false });
      }
    }
  },

  async fetchEvents(start, end) {
    const requestGeneration = ++eventRequestGeneration;
    const currentAccountGeneration = accountGeneration;
    const { calendars } = get();
    const allEvents: CalendarEvent[] = [];
    const results = await Promise.allSettled(
      calendars.map(async (cal) => {
        // The dates are days, and which instants a day spans depends on where
        // it is read. Saying so keeps the grid and the server agreeing on
        // which month an early-morning event belongs to.
        const tz = encodeURIComponent(useUiStore.getState().timezone);
        const evts = await api.get<CalendarEvent[]>(
          `/calendars/${cal.id}/events?start=${start}&end=${end}&tz=${tz}`,
        );
        for (const evt of evts) {
          allEvents.push({ ...evt, calendarId: cal.id });
        }
      }),
    );
    if (
      requestGeneration !== eventRequestGeneration ||
      currentAccountGeneration !== accountGeneration
    )
      return;
    for (const result of results) {
      if (result.status === 'rejected') toast.error(errorMessage(result.reason));
    }
    if (
      requestGeneration === eventRequestGeneration &&
      currentAccountGeneration === accountGeneration
    ) {
      set({ events: allEvents });
    }
  },

  async fetchMemos() {
    const requestGeneration = ++memoRequestGeneration;
    const currentAccountGeneration = accountGeneration;
    const { calendars } = get();
    const allMemos: Memo[] = [];
    const results = await Promise.allSettled(
      calendars.map(async (cal) => {
        const ms = await api.get<Memo[]>(`/calendars/${cal.id}/memos`);
        for (const m of ms) {
          allMemos.push({ ...m, calendarId: cal.id });
        }
      }),
    );
    if (
      requestGeneration !== memoRequestGeneration ||
      currentAccountGeneration !== accountGeneration
    )
      return;
    for (const result of results) {
      if (result.status === 'rejected') toast.error(errorMessage(result.reason));
    }
    if (
      requestGeneration === memoRequestGeneration &&
      currentAccountGeneration === accountGeneration
    ) {
      set({ memos: allMemos });
    }
  },

  async fetchMembers(calendarId) {
    const currentAccountGeneration = accountGeneration;
    const members = await api.get<Member[]>(`/calendars/${calendarId}/members`);
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      membersMap: { ...s.membersMap, [calendarId]: members },
    }));
  },

  async fetchLabels(calendarId) {
    const currentAccountGeneration = accountGeneration;
    const labels = await api.get<Label[]>(`/calendars/${calendarId}/labels`);
    if (currentAccountGeneration !== accountGeneration) return;
    set({ labels });
  },

  async addCalendar(cal) {
    const currentAccountGeneration = accountGeneration;
    const newCal = await api.post<Calendar>('/calendars', cal);
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => {
      const ids = [...s.activeCalendarIds, newCal.id];
      saveJson('activeCalendarIds', ids);
      return {
        calendars: [...s.calendars, newCal],
        activeCalendarIds: ids,
      };
    });
    await get().fetchMembers(newCal.id);
  },

  async updateCalendar(id, patch) {
    const updated = await api.put<Calendar>(`/calendars/${id}`, patch);
    set((s) => ({
      calendars: s.calendars.map((c) => (c.id === id ? { ...c, ...updated } : c)),
    }));
  },

  async deleteCalendar(id) {
    await api.delete(`/calendars/${id}`);
    set((s) => {
      const ids = s.activeCalendarIds.filter((cid) => cid !== id);
      saveJson('activeCalendarIds', ids);
      const nextMap = { ...s.membersMap };
      delete nextMap[id];
      return {
        calendars: s.calendars.filter((c) => c.id !== id),
        events: s.events.filter((e) => e.calendarId !== id),
        memos: s.memos.filter((m) => m.calendarId !== id),
        activeCalendarIds: ids,
        membersMap: nextMap,
      };
    });
  },

  async addEvent(calendarId, evt) {
    const currentAccountGeneration = accountGeneration;
    const newEvt = await api.post<CalendarEvent>(`/calendars/${calendarId}/events`, evt);
    if (currentAccountGeneration !== accountGeneration) return;
    if (newEvt.recurrenceRule) {
      // The POST returns a single un-expanded master row, so appending it would
      // show only one occurrence. Re-fetch the visible range to pull the fully
      // expanded instances instead.
      const { start, end } = get().visibleRange();
      await get().fetchEvents(start, end);
      return;
    }
    set((s) => ({
      events: [...s.events, { ...newEvt, calendarId }],
    }));
  },

  async updateEvent(calendarId, eventId, evt, scope) {
    const query = scope ? `?scope=${scope}` : '';
    const updated = await api.put<CalendarEvent>(
      `/calendars/${calendarId}/events/${eventId}${query}`,
      evt,
    );
    const wasRecurring = eventId.includes('_') || !!evt.recurrenceRule;
    if (updated.recurrenceRule || wasRecurring) {
      // Recurring event: expanded instances change, so re-fetch the visible range
      // to replace the stale (or removed) occurrences with fresh ones.
      const parentId = eventId.includes('_') ? eventId.substring(0, 36) : eventId;
      set((s) => ({
        events: s.events.filter((e) => !e.id.startsWith(parentId)),
      }));
      const { start, end } = get().visibleRange();
      await get().fetchEvents(start, end);
    } else {
      set((s) => ({
        events: s.events.map((e) => (e.id === eventId ? { ...updated, calendarId } : e)),
      }));
    }
  },

  async deleteEvent(calendarId, eventId, scope) {
    const query = scope ? `?scope=${scope}` : '';
    await api.delete(`/calendars/${calendarId}/events/${eventId}${query}`);
    if (eventId.includes('_')) {
      // Recurring instance: drop the affected occurrences, then re-fetch the
      // visible range so a single-occurrence delete leaves the rest of the
      // series intact rather than clearing every instance.
      const parentId = eventId.substring(0, 36);
      set((s) => ({
        events: s.events.filter((e) => !e.id.startsWith(parentId)),
      }));
      const { start, end } = get().visibleRange();
      await get().fetchEvents(start, end);
    } else {
      set((s) => ({
        events: s.events.filter((e) => e.id !== eventId),
      }));
    }
  },

  async addMemo(calendarId, memo) {
    const currentAccountGeneration = accountGeneration;
    const existing = get().memos.filter((m) => m.calendarId === calendarId);
    const newMemo = await api.post<Memo>(`/calendars/${calendarId}/memos`, {
      title: memo.title,
      body: memo.body,
      sortOrder: existing.length,
    });
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      memos: [...s.memos, { ...newMemo, calendarId }],
    }));
  },

  async updateMemo(calendarId, memoId, patch) {
    const memo = get().memos.find((m) => m.id === memoId);
    const updated = await api.put<Memo>(`/calendars/${calendarId}/memos/${memoId}`, {
      title: patch.title,
      body: patch.body,
      done: patch.done,
      sortOrder: memo?.sortOrder ?? 0,
    });
    set((s) => ({
      memos: s.memos.map((m) => (m.id === memoId ? { ...updated, calendarId } : m)),
    }));
  },

  async toggleMemo(calendarId, memoId, done, title) {
    const memo = get().memos.find((m) => m.id === memoId);
    const updated = await api.put<Memo>(`/calendars/${calendarId}/memos/${memoId}`, {
      title,
      body: memo?.body ?? '',
      done,
      sortOrder: memo?.sortOrder ?? 0,
    });
    set((s) => ({
      memos: s.memos.map((m) => (m.id === memoId ? { ...updated, calendarId } : m)),
    }));
  },

  async deleteMemo(calendarId, memoId) {
    await api.delete(`/calendars/${calendarId}/memos/${memoId}`);
    set((s) => ({
      memos: s.memos.filter((m) => m.id !== memoId),
    }));
  },

  toggleCalendarFilter(calId) {
    set((s) => {
      const ids = s.activeCalendarIds.includes(calId)
        ? s.activeCalendarIds.filter((id) => id !== calId)
        : [...s.activeCalendarIds, calId];
      saveJson('activeCalendarIds', ids);
      return { activeCalendarIds: ids };
    });
  },

  setActiveCalendarIds(ids) {
    saveJson('activeCalendarIds', ids);
    set({ activeCalendarIds: ids });
  },

  resetSessionData() {
    accountGeneration++;
    calendarRequestGeneration++;
    eventRequestGeneration++;
    memoRequestGeneration++;
    localStorage.removeItem('tt_activeCalendarIds');
    set({
      calendars: [],
      events: [],
      memos: [],
      membersMap: {},
      labels: [],
      activeCalendarIds: [],
      isLoading: false,
    });
  },

  visibleRange() {
    const { currentMonth } = useUiStore.getState();
    const start = currentMonth.minus({ months: 1 }).startOf('month');
    const end = currentMonth.plus({ months: 2 }).startOf('month');
    return {
      start: (start.toISODate() ?? DateTime.now().toISODate()) as string,
      end: (end.toISODate() ?? DateTime.now().toISODate()) as string,
    };
  },
}));
