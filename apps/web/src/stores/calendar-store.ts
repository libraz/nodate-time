import { DateTime } from 'luxon';
import { create } from 'zustand';
import { api } from '@/lib/api';
import { loadJson, saveJson } from '@/lib/storage';
import { useUiStore } from '@/stores/ui-store';
import type {
  Calendar,
  CalendarEvent,
  Label,
  Member,
  Memo,
  RecurrenceRule,
} from '@/types/calendar';

/** Request body shared by event create and update. */
export interface EventInput {
  title: string;
  allDay: boolean;
  startAt: string;
  endAt: string;
  timezone?: string | undefined;
  color?: string | undefined;
  location?: string | undefined;
  memo?: string | undefined;
  url?: string | undefined;
  notificationOffset?: number | null | undefined;
  participants?: string[] | undefined;
  assignedTo?: string | null | undefined;
  recurrenceRule?: RecurrenceRule | null | undefined;
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
  updateEvent: (calendarId: string, eventId: string, evt: EventInput) => Promise<void>;
  deleteEvent: (calendarId: string, eventId: string) => Promise<void>;

  addMemo: (calendarId: string, memo: { title: string }) => Promise<void>;
  toggleMemo: (calendarId: string, memoId: string, done: boolean, title: string) => Promise<void>;
  deleteMemo: (calendarId: string, memoId: string) => Promise<void>;

  toggleCalendarFilter: (calId: string) => void;
  setActiveCalendarIds: (ids: string[]) => void;

  /** Returns the currently visible event window as ISO date strings. */
  visibleRange: () => { start: string; end: string };
}

export const useCalendarStore = create<CalendarState>((set, get) => ({
  calendars: [],
  events: [],
  memos: [],
  membersMap: {},
  labels: [],
  activeCalendarIds: loadJson<string[]>('activeCalendarIds', []),
  isLoading: false,

  async fetchCalendars() {
    set({ isLoading: true });
    try {
      const cals = await api.get<Calendar[]>('/calendars');
      const saved = loadJson<string[]>('activeCalendarIds', []);
      const ids = saved.length > 0 ? saved : cals.map((c) => c.id);
      set({ calendars: cals, activeCalendarIds: ids });
      saveJson('activeCalendarIds', ids);

      await Promise.all(cals.map((c) => get().fetchMembers(c.id)));
      const first = cals[0];
      if (first && get().labels.length === 0) {
        await get().fetchLabels(first.id);
      }
    } finally {
      set({ isLoading: false });
    }
  },

  async fetchEvents(start, end) {
    const { calendars } = get();
    const allEvents: CalendarEvent[] = [];
    await Promise.all(
      calendars.map(async (cal) => {
        const evts = await api.get<CalendarEvent[]>(
          `/calendars/${cal.id}/events?start=${start}&end=${end}`,
        );
        for (const evt of evts) {
          allEvents.push({ ...evt, calendarId: cal.id });
        }
      }),
    );
    set({ events: allEvents });
  },

  async fetchMemos() {
    const { calendars } = get();
    const allMemos: Memo[] = [];
    await Promise.all(
      calendars.map(async (cal) => {
        const ms = await api.get<Memo[]>(`/calendars/${cal.id}/memos`);
        for (const m of ms) {
          allMemos.push({ ...m, calendarId: cal.id });
        }
      }),
    );
    set({ memos: allMemos });
  },

  async fetchMembers(calendarId) {
    const members = await api.get<Member[]>(`/calendars/${calendarId}/members`);
    set((s) => ({
      membersMap: { ...s.membersMap, [calendarId]: members },
    }));
  },

  async fetchLabels(calendarId) {
    const labels = await api.get<Label[]>(`/calendars/${calendarId}/labels`);
    set({ labels });
  },

  async addCalendar(cal) {
    const newCal = await api.post<Calendar>('/calendars', cal);
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
    const newEvt = await api.post<CalendarEvent>(`/calendars/${calendarId}/events`, evt);
    set((s) => ({
      events: [...s.events, { ...newEvt, calendarId }],
    }));
  },

  async updateEvent(calendarId, eventId, evt) {
    const updated = await api.put<CalendarEvent>(`/calendars/${calendarId}/events/${eventId}`, evt);
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

  async deleteEvent(calendarId, eventId) {
    await api.delete(`/calendars/${calendarId}/events/${eventId}`);
    if (eventId.includes('_')) {
      // Recurring event: remove all instances from same parent
      const parentId = eventId.substring(0, 36);
      set((s) => ({
        events: s.events.filter((e) => !e.id.startsWith(parentId)),
      }));
    } else {
      set((s) => ({
        events: s.events.filter((e) => e.id !== eventId),
      }));
    }
  },

  async addMemo(calendarId, memo) {
    const existing = get().memos.filter((m) => m.calendarId === calendarId);
    const newMemo = await api.post<Memo>(`/calendars/${calendarId}/memos`, {
      title: memo.title,
      sortOrder: existing.length,
    });
    set((s) => ({
      memos: [...s.memos, { ...newMemo, calendarId }],
    }));
  },

  async toggleMemo(calendarId, memoId, done, title) {
    const memo = get().memos.find((m) => m.id === memoId);
    const updated = await api.put<Memo>(`/calendars/${calendarId}/memos/${memoId}`, {
      title,
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
