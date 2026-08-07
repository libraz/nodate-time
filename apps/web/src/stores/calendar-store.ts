import { create } from 'zustand';
import { api, errorMessage } from '@/lib/api';
import { fetchWindow } from '@/lib/date-utils';
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
  /**
   * Why the calendar list is missing, when it is. Set means the list was
   * asked for and did not arrive -- an empty grid with a reason behind it,
   * rather than an empty grid that looks like an empty account.
   */
  loadError: string | null;
  /**
   * Calendars whose member list failed to load, by id. The list is what names
   * the people an event can be assigned to, so its absence is reported rather
   * than shown as a calendar nobody is on.
   */
  memberErrors: Record<string, string>;

  fetchCalendars: () => Promise<void>;
  fetchEvents: (start: string, end: string) => Promise<void>;
  fetchMemos: () => Promise<void>;
  fetchMembers: (calendarId: string) => Promise<void>;
  fetchLabels: (calendarId: string) => Promise<void>;
  /** Re-runs whichever of the startup loads did not come back. */
  retryFailedLoads: () => Promise<void>;

  addCalendar: (cal: { name: string; color: string }) => Promise<void>;
  updateCalendar: (
    id: string,
    patch: { name?: string; color?: string; coverUrl?: string },
  ) => Promise<void>;
  deleteCalendar: (id: string) => Promise<void>;
  /**
   * Gives up the caller's own membership. Distinct from removing someone
   * else: the calendar leaves with the membership rather than staying behind
   * to be refetched.
   */
  leaveCalendar: (calendarId: string, memberId: string) => Promise<void>;

  addEvent: (calendarId: string, evt: EventInput) => Promise<void>;
  updateEvent: (
    calendarId: string,
    eventId: string,
    evt: EventInput,
    scope?: 'this' | 'all',
    /**
     * The revision this edit started from, as the server reported it. Sent as
     * If-Match so a save built on a copy someone else has since replaced is
     * refused instead of overwriting them. Omitted where there is nothing to
     * be stale about -- a drag applies the gesture the user just made.
     */
    revision?: string | null,
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

/**
 * Drops every trace of a calendar the session no longer has access to,
 * whether it was deleted or merely left. Anything kept behind would be
 * fetched again on the next month change and answered with a 403.
 */
function forgetCalendar(s: CalendarState, id: string): Partial<CalendarState> {
  const ids = s.activeCalendarIds.filter((cid) => cid !== id);
  saveJson('activeCalendarIds', ids);
  const nextMap = { ...s.membersMap };
  delete nextMap[id];
  const nextErrors = { ...s.memberErrors };
  delete nextErrors[id];
  return {
    calendars: s.calendars.filter((c) => c.id !== id),
    events: s.events.filter((e) => e.calendarId !== id),
    memos: s.memos.filter((m) => m.calendarId !== id),
    activeCalendarIds: ids,
    membersMap: nextMap,
    memberErrors: nextErrors,
  };
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
  loadError: null,
  memberErrors: {},

  async fetchCalendars() {
    const requestGeneration = ++calendarRequestGeneration;
    const currentAccountGeneration = accountGeneration;
    set({ isLoading: true });
    try {
      let cals: Calendar[];
      try {
        cals = await api.get<Calendar[]>('/calendars');
      } catch (e) {
        if (
          requestGeneration === calendarRequestGeneration &&
          currentAccountGeneration === accountGeneration
        ) {
          set({ loadError: errorMessage(e) });
        }
        return;
      }
      if (
        requestGeneration !== calendarRequestGeneration ||
        currentAccountGeneration !== accountGeneration
      )
        return;
      set({ loadError: null });
      const saved = loadJson<string[]>('activeCalendarIds', []);
      const calendarIDs = cals.map((c) => c.id);
      const savedActive = saved.filter((id) => calendarIDs.includes(id));
      const newIDs = calendarIDs.filter((id) => !saved.includes(id));
      const ids = saved.length > 0 ? [...savedActive, ...newIDs] : calendarIDs;
      set({ calendars: cals, activeCalendarIds: ids });
      saveJson('activeCalendarIds', ids);

      // Member lists are not fetched here. They are only needed where members
      // are shown -- the member panel and the participant picker -- and each
      // of those asks for the one calendar it is showing. Loading all of them
      // at startup cost one request per calendar for an answer that arrived
      // with the list itself.
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
    let members: Member[];
    try {
      members = await api.get<Member[]>(`/calendars/${calendarId}/members`);
    } catch (e) {
      if (currentAccountGeneration !== accountGeneration) return;
      set((s) => ({
        memberErrors: { ...s.memberErrors, [calendarId]: errorMessage(e) },
      }));
      return;
    }
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => {
      const nextErrors = { ...s.memberErrors };
      delete nextErrors[calendarId];
      return {
        membersMap: { ...s.membersMap, [calendarId]: members },
        memberErrors: nextErrors,
      };
    });
  },

  async fetchLabels(calendarId) {
    const currentAccountGeneration = accountGeneration;
    const labels = await api.get<Label[]>(`/calendars/${calendarId}/labels`);
    if (currentAccountGeneration !== accountGeneration) return;
    set({ labels });
  },

  async retryFailedLoads() {
    // A missing calendar list is the wider failure: refetching it re-runs the
    // member loads underneath it, so there is nothing left to retry
    // separately.
    if (get().loadError) {
      await get().fetchCalendars();
      return;
    }
    const failed = Object.keys(get().memberErrors);
    await Promise.all(failed.map((id) => get().fetchMembers(id)));
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
    set((s) => forgetCalendar(s, id));
  },

  async leaveCalendar(calendarId, memberId) {
    await api.delete(`/calendars/${calendarId}/members/${memberId}`);
    // Everything this calendar contributed goes with the membership. Refetching
    // its members instead -- which is what the generic remove path does -- asks
    // a calendar the caller has just left, and the 403 that comes back reads as
    // a failure to leave.
    set((s) => forgetCalendar(s, calendarId));
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

  async updateEvent(calendarId, eventId, evt, scope, revision) {
    const query = scope ? `?scope=${scope}` : '';
    // The update replaces every field, so a save from a copy read before
    // someone else's save erases theirs rather than merging with it. Naming
    // the copy being replaced turns that into a refusal the user can see.
    const updated = await api.put<CalendarEvent>(
      `/calendars/${calendarId}/events/${eventId}${query}`,
      evt,
      revision ? { 'If-Match': revision } : undefined,
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
      loadError: null,
      memberErrors: {},
    });
  },

  visibleRange() {
    const { currentMonth, calendarView } = useUiStore.getState();
    return fetchWindow(calendarView, currentMonth);
  },
}));
