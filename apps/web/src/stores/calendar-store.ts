import { create } from 'zustand';
import { errorMessage, isAbortError } from '@/lib/api';
import type { components } from '@/lib/api-schema';
import { client } from '@/lib/client';
import { fetchWindow } from '@/lib/date-utils';
import { type EventScope, scopeEtag, seriesId } from '@/lib/event-move';
import { offerOccurrenceRestore, offerRestore } from '@/lib/event-restore';
import { resizeImageForCover } from '@/lib/image-resize';
import { loadJson, saveJson } from '@/lib/storage';
import { toast } from '@/lib/toast';
import { present, uploadCalendarCover as uploadCover } from '@/lib/upload';
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

/**
 * Request body for an event create. An all-day event carries `startDate` and
 * `endDate` (`YYYY-MM-DD`, end exclusive) and a timed one `startAt` and
 * `endAt`; sending the other pair is refused.
 */
export interface EventInput {
  title: string;
  allDay: boolean;
  startAt?: string | undefined;
  endAt?: string | undefined;
  startDate?: string | undefined;
  endDate?: string | undefined;
  timezone?: string | undefined;
  location?: string | undefined;
  memo?: string | undefined;
  url?: string | undefined;
  notificationOffset?: number | null | undefined;
  participantIds?: string[] | undefined;
  ownerId?: string | null | undefined;
  recurrenceRule?: RecurrenceRule | null | undefined;
  showAs?: ShowAs | undefined;
  flexibility?: Flexibility | undefined;
  visibility?: Visibility | undefined;
}

/**
 * A partial update. An omitted field keeps its stored value, so a caller sends
 * only what it changes; a drag sends the time fields and nothing else.
 */
export type EventPatch = Partial<EventInput>;

/** A memo as the store holds it: the listing it came from is part of its identity. */
export type StoredMemo = Memo & { calendarId: string };

type Schemas = components['schemas'];

/** The create body: the generated schema has no null for these, an omission says the same. */
function createBody(evt: EventInput): Schemas['CreateEventInputBody'] {
  const { ownerId, notificationOffset, recurrenceRule, ...rest } = evt;
  return {
    ...rest,
    ...(ownerId ? { ownerId } : {}),
    ...(notificationOffset != null ? { notificationOffset } : {}),
    // The schema types the rule as an opaque object.
    ...(recurrenceRule ? { recurrenceRule } : {}),
  } as Schemas['CreateEventInputBody'];
}

/** The patch body, with the rule typed as the opaque object the schema declares. */
function patchBody(evt: EventPatch): Schemas['PatchEventInputBody'] {
  return evt as unknown as Schemas['PatchEventInputBody'];
}

/** A listing item as the store holds it. */
function toEvent(item: Schemas['EventResponse'], calendarId: string): CalendarEvent {
  return { ...(item as unknown as CalendarEvent), calendarId };
}

interface CalendarState {
  calendars: Calendar[];
  events: CalendarEvent[];
  memos: StoredMemo[];
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
  /**
   * Calendars whose last event listing had more to give than it returned. The
   * grid would otherwise read the missing tail of the window as free time.
   */
  truncatedCalendarIds: string[];
  /** True while an event listing is in flight and no newer one has replaced it. */
  eventsLoading: boolean;
  /** True while a memo listing is in flight and no newer one has replaced it. */
  memosLoading: boolean;
  /**
   * Why the last event listing came back incomplete, when it did. One message
   * for the whole listing: a month change fans out one request per calendar,
   * and one notice per failed calendar buried the screen in repeats.
   */
  eventsError: string | null;
  /** As `eventsError`, for the memo listing. */
  memosError: string | null;

  fetchCalendars: () => Promise<void>;
  /**
   * Loads the events for a date range. A caller that can supersede its own
   * request -- the month view, whose month changes as it scrolls -- passes a
   * signal so the requests it has already moved past stop costing anything.
   */
  fetchEvents: (start: string, end: string, signal?: AbortSignal) => Promise<void>;
  /** Loads every calendar's memos; a caller that can supersede the load passes a signal. */
  fetchMemos: (signal?: AbortSignal) => Promise<void>;
  fetchMembers: (calendarId: string) => Promise<void>;
  fetchLabels: (calendarId: string) => Promise<void>;
  /** Re-runs whichever of the startup loads did not come back. */
  retryFailedLoads: () => Promise<void>;

  /** Resolves to the created calendar, or null when the account changed meanwhile. */
  addCalendar: (cal: { name: string; color: string }) => Promise<Calendar | null>;
  updateCalendar: (id: string, patch: { name?: string; color?: string }) => Promise<void>;
  uploadCalendarCover: (id: string, file: File) => Promise<void>;
  deleteCalendarCover: (id: string) => Promise<void>;
  deleteCalendar: (id: string) => Promise<void>;
  /**
   * Gives up the caller's own membership. Distinct from removing someone
   * else: the calendar leaves with the membership rather than staying behind
   * to be refetched.
   */
  leaveCalendar: (calendarId: string, memberId: string) => Promise<void>;
  /**
   * Changes the colour a member's layer is drawn in. An event's colour is its
   * owner's, so the events already loaded take the new one with the member.
   */
  updateMemberColor: (calendarId: string, memberId: string, color: string) => Promise<void>;

  addEvent: (calendarId: string, evt: EventInput) => Promise<void>;
  /**
   * Applies a partial update. `etag` is the version of the copy the edit
   * started from -- the listing's `etag`, or `seriesEtag` for a scope over the
   * whole series -- so a save built on a copy someone else has since replaced
   * is refused (a stale error) instead of overwriting them.
   */
  updateEvent: (
    calendarId: string,
    eventId: string,
    evt: EventPatch,
    scope?: EventScope,
    etag?: string,
  ) => Promise<void>;
  /**
   * Deletes the event, or one occurrence, or the occurrences from this one on.
   * The version checked is the one the listing carried. A delete that can be
   * undone offers the undo in a toast; `following` cannot.
   */
  deleteEvent: (event: CalendarEvent, scope?: EventScope) => Promise<void>;

  addMemo: (calendarId: string, memo: { title: string; body: string }) => Promise<void>;
  updateMemo: (
    calendarId: string,
    memoId: string,
    patch: { title: string; body: string; done: boolean },
  ) => Promise<void>;
  toggleMemo: (calendarId: string, memoId: string, done: boolean) => Promise<void>;
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
    truncatedCalendarIds: s.truncatedCalendarIds.filter((cid) => cid !== id),
  };
}

/**
 * How many memo pages one calendar is walked for. A backstop, not a limit
 * anyone should reach: at the server's default page size this is far more
 * memos than a calendar accumulates, and it only exists so a cursor that
 * somehow stopped advancing cannot spin here forever.
 */
const maxMemoPages = 20;

let accountGeneration = 0;
let calendarRequestGeneration = 0;
let eventRequestGeneration = 0;
let memoRequestGeneration = 0;

function mergeCalendarResponse(current: Calendar, updated: Calendar): Calendar {
  const merged = { ...current, ...updated };
  if (!updated.coverImageUrl) delete merged.coverImageUrl;
  return merged;
}

/** Patches a memo against the version the store holds for it. */
async function patchMemo(
  memos: StoredMemo[],
  calendarId: string,
  memoId: string,
  body: Schemas['UpdateMemoInputBody'],
): Promise<Memo> {
  const etag = memos.find((m) => m.id === memoId)?.etag ?? '';
  const { data } = await client.PATCH('/calendars/{calendarId}/memos/{memoId}', {
    params: { path: { calendarId, memoId }, header: { 'If-Match': etag } },
    body,
  });
  return present(data);
}

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
  truncatedCalendarIds: [],
  eventsLoading: false,
  memosLoading: false,
  eventsError: null,
  memosError: null,

  async fetchCalendars() {
    const requestGeneration = ++calendarRequestGeneration;
    const currentAccountGeneration = accountGeneration;
    set({ isLoading: true });
    try {
      let cals: Calendar[];
      try {
        cals = present((await client.GET('/calendars')).data) ?? [];
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
      // Which calendars are shown is the user's choice, and "none of them" is
      // one of the choices. It is told apart from "has not chosen yet" by the
      // key being absent rather than by the selection being empty.
      //
      // Which calendars are new is a separate question, kept separately: a
      // calendar the user has hidden is not new, so deciding newness from the
      // shown ones is what turned hiding everything into showing everything
      // again on the next load.
      const saved = loadJson<string[] | null>('activeCalendarIds', null);
      const calendarIDs = cals.map((c) => c.id);
      let ids: string[];
      if (saved === null) {
        ids = calendarIDs;
      } else {
        // A session that predates the seen list has only its selection to go
        // on, which is what the old behaviour assumed anyway.
        const seen = loadJson<string[] | null>('seenCalendarIds', null) ?? saved;
        const fresh = calendarIDs.filter((id) => !seen.includes(id));
        ids = [...saved.filter((id) => calendarIDs.includes(id)), ...fresh];
      }
      set({ calendars: cals, activeCalendarIds: ids });
      saveJson('activeCalendarIds', ids);
      saveJson('seenCalendarIds', calendarIDs);

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

  async fetchEvents(start, end, signal) {
    const requestGeneration = ++eventRequestGeneration;
    const currentAccountGeneration = accountGeneration;
    const { calendars } = get();
    set({ eventsLoading: true });
    const allEvents: CalendarEvent[] = [];
    // Calendars that answered. Only their events are replaced: a calendar that
    // did not answer is one we have no news about, and treating silence as an
    // empty month wiped the grid over a single failed request -- most visibly
    // right after editing a recurring event, where the series disappeared and
    // looked deleted.
    const answered = new Set<string>();
    const truncated = new Set<string>();
    const results = await Promise.allSettled(
      calendars.map(async (cal) => {
        // The dates are days, and which instants a day spans depends on where
        // it is read. Saying so keeps the grid and the server agreeing on
        // which month an early-morning event belongs to.
        const { data } = await client.GET('/calendars/{calendarId}/events', {
          params: {
            path: { calendarId: cal.id },
            query: { start, end, tz: useUiStore.getState().timezone },
          },
          ...(signal ? { signal } : {}),
        });
        const page = present(data);
        answered.add(cal.id);
        if (page.hasMore) truncated.add(cal.id);
        for (const evt of page.items ?? []) allEvents.push(toEvent(evt, cal.id));
      }),
    );
    if (
      requestGeneration !== eventRequestGeneration ||
      currentAccountGeneration !== accountGeneration
    )
      return;
    // A request the caller superseded is not a failure anyone should be
    // told about. It stays out of `answered` either way, so the calendar
    // keeps showing what it had rather than being blanked.
    const failures = results.filter(
      (r): r is PromiseRejectedResult => r.status === 'rejected' && !isAbortError(r.reason),
    );
    const aborted = results.some((r) => r.status === 'rejected' && isAbortError(r.reason));
    const known = new Set(calendars.map((c) => c.id));
    set((s) => ({
      eventsLoading: false,
      eventsError: failures[0] ? errorMessage(failures[0].reason) : aborted ? s.eventsError : null,
      events: [
        ...s.events.filter((e) => known.has(e.calendarId) && !answered.has(e.calendarId)),
        ...allEvents,
      ],
      truncatedCalendarIds: [
        ...s.truncatedCalendarIds.filter((id) => known.has(id) && !answered.has(id)),
        ...truncated,
      ],
    }));
  },

  async fetchMemos(signal) {
    const requestGeneration = ++memoRequestGeneration;
    const currentAccountGeneration = accountGeneration;
    const { calendars } = get();
    set({ memosLoading: true });
    const allMemos: StoredMemo[] = [];
    // As in fetchEvents: a calendar that did not answer keeps what it had.
    const answered = new Set<string>();
    const results = await Promise.allSettled(
      calendars.map(async (cal) => {
        // The list is paged. The panel shows a calendar's memos as one
        // arranged list, so the pages are walked to the end rather than
        // stopping at the first -- the bound is there to keep one response
        // from being unbounded, not to hide the rest.
        const collected: StoredMemo[] = [];
        let cursor = '';
        for (let page = 0; page < maxMemoPages; page++) {
          const { data } = await client.GET('/calendars/{calendarId}/memos', {
            params: { path: { calendarId: cal.id }, ...(cursor ? { query: { cursor } } : {}) },
            ...(signal ? { signal } : {}),
          });
          const res = present(data);
          for (const m of res.items ?? []) {
            collected.push({ ...m, calendarId: cal.id });
          }
          cursor = res.nextCursor ?? '';
          if (!cursor) break;
        }
        answered.add(cal.id);
        allMemos.push(...collected);
      }),
    );
    if (
      requestGeneration !== memoRequestGeneration ||
      currentAccountGeneration !== accountGeneration
    )
      return;
    const failures = results.filter(
      (r): r is PromiseRejectedResult => r.status === 'rejected' && !isAbortError(r.reason),
    );
    const aborted = results.some((r) => r.status === 'rejected' && isAbortError(r.reason));
    const known = new Set(calendars.map((c) => c.id));
    set((s) => ({
      memosLoading: false,
      memosError: failures[0] ? errorMessage(failures[0].reason) : aborted ? s.memosError : null,
      memos: [
        ...s.memos.filter((m) => known.has(m.calendarId) && !answered.has(m.calendarId)),
        ...allMemos,
      ],
    }));
  },

  async fetchMembers(calendarId) {
    const currentAccountGeneration = accountGeneration;
    let members: Member[];
    try {
      const { data } = await client.GET('/calendars/{calendarId}/members', {
        params: { path: { calendarId } },
      });
      members = present(data) ?? [];
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
        // The roster just read is newer than the count the calendar came with.
        calendars: s.calendars.map((c) =>
          c.id === calendarId && c.memberCount !== members.length
            ? { ...c, memberCount: members.length }
            : c,
        ),
      };
    });
  },

  async fetchLabels(calendarId) {
    const currentAccountGeneration = accountGeneration;
    const { data } = await client.GET('/calendars/{calendarId}/labels', {
      params: { path: { calendarId } },
    });
    const labels: Label[] = present(data) ?? [];
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
    const { start, end } = get().visibleRange();
    await Promise.all([
      ...failed.map((id) => get().fetchMembers(id)),
      ...(get().eventsError ? [get().fetchEvents(start, end)] : []),
      ...(get().memosError ? [get().fetchMemos()] : []),
    ]);
  },

  async addCalendar(cal) {
    const currentAccountGeneration = accountGeneration;
    const newCal = present((await client.POST('/calendars', { body: cal })).data);
    if (currentAccountGeneration !== accountGeneration) return null;
    set((s) => {
      const ids = [...s.activeCalendarIds, newCal.id];
      saveJson('activeCalendarIds', ids);
      return {
        calendars: [...s.calendars, newCal],
        activeCalendarIds: ids,
      };
    });
    await get().fetchMembers(newCal.id);
    return newCal;
  },

  async updateCalendar(id, patch) {
    const currentAccountGeneration = accountGeneration;
    const etag = get().calendars.find((c) => c.id === id)?.etag ?? '';
    const { data } = await client.PATCH('/calendars/{calendarId}', {
      params: { path: { calendarId: id }, header: { 'If-Match': etag } },
      body: patch,
    });
    const updated = present(data);
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      calendars: s.calendars.map((c) => (c.id === id ? mergeCalendarResponse(c, updated) : c)),
    }));
  },

  async uploadCalendarCover(id, file) {
    const currentAccountGeneration = accountGeneration;
    const resized = await resizeImageForCover(file);
    const updated = await uploadCover(id, resized.bytes, resized.contentType);
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      calendars: s.calendars.map((c) => (c.id === id ? mergeCalendarResponse(c, updated) : c)),
    }));
  },

  async deleteCalendarCover(id) {
    const currentAccountGeneration = accountGeneration;
    await client.DELETE('/calendars/{calendarId}/cover', {
      params: { path: { calendarId: id } },
    });
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      calendars: s.calendars.map((c) => {
        if (c.id !== id) return c;
        const { coverImageUrl: _removed, ...rest } = c;
        return rest;
      }),
    }));
  },

  async deleteCalendar(id) {
    const currentAccountGeneration = accountGeneration;
    const etag = get().calendars.find((c) => c.id === id)?.etag ?? '';
    await client.DELETE('/calendars/{calendarId}', {
      params: { path: { calendarId: id }, header: { 'If-Match': etag } },
    });
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => forgetCalendar(s, id));
  },

  async leaveCalendar(calendarId, memberId) {
    const currentAccountGeneration = accountGeneration;
    await client.DELETE('/calendars/{calendarId}/members/{userId}', {
      params: { path: { calendarId, userId: memberId } },
    });
    if (currentAccountGeneration !== accountGeneration) return;
    // Everything this calendar contributed goes with the membership. Refetching
    // its members instead -- which is what the generic remove path does -- asks
    // a calendar the caller has just left, and the 403 that comes back reads as
    // a failure to leave.
    set((s) => forgetCalendar(s, calendarId));
  },

  async updateMemberColor(calendarId, memberId, color) {
    const currentAccountGeneration = accountGeneration;
    const updated = present(
      (
        await client.PUT('/calendars/{calendarId}/members/{userId}/color', {
          params: { path: { calendarId, userId: memberId } },
          body: { color },
        })
      ).data,
    );
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      events: s.events.map((e) =>
        e.calendarId === calendarId && e.ownerId === memberId ? { ...e, color: updated.color } : e,
      ),
      membersMap: {
        ...s.membersMap,
        [calendarId]: (s.membersMap[calendarId] ?? []).map((m) =>
          m.id === memberId ? { ...m, color: updated.color } : m,
        ),
      },
    }));
  },

  async addEvent(calendarId, evt) {
    const currentAccountGeneration = accountGeneration;
    const { data } = await client.POST('/calendars/{calendarId}/events', {
      params: { path: { calendarId } },
      body: createBody(evt),
    });
    const newEvt = toEvent(present(data), calendarId);
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

  async updateEvent(calendarId, eventId, evt, scope, etag) {
    const currentAccountGeneration = accountGeneration;
    // Naming the copy being replaced turns a save built on a copy someone
    // else has since changed into a refusal the user can see.
    const { data } = await client.PATCH('/calendars/{calendarId}/events/{eventId}', {
      params: {
        path: { calendarId, eventId },
        header: { 'If-Match': etag ?? '' },
        ...(scope ? { query: { scope } } : {}),
      },
      body: patchBody(evt),
    });
    const updated = toEvent(present(data), calendarId);
    if (currentAccountGeneration !== accountGeneration) return;
    const wasRecurring = eventId.includes('_') || !!evt.recurrenceRule;
    if (updated.recurrenceRule || wasRecurring) {
      // Recurring event: expanded instances change, so re-fetch the visible
      // range. Only a calendar that answers has its events replaced; clearing
      // the series first left it blank whenever the refetch failed.
      const { start, end } = get().visibleRange();
      await get().fetchEvents(start, end);
    } else {
      set((s) => ({
        events: s.events.map((e) => (e.id === eventId ? updated : e)),
      }));
    }
  },

  async deleteEvent(event, scope) {
    const currentAccountGeneration = accountGeneration;
    const { calendarId, id: eventId } = event;
    await client.DELETE('/calendars/{calendarId}/events/{eventId}', {
      params: {
        path: { calendarId, eventId },
        header: { 'If-Match': scopeEtag(event, scope) },
        ...(scope ? { query: { scope } } : {}),
      },
    });
    if (currentAccountGeneration !== accountGeneration) return;
    const reload = () => {
      const { start, end } = get().visibleRange();
      void get().fetchEvents(start, end);
    };
    const date = event.recurrenceDate ?? eventId.split('_')[1];
    if (scope === 'this' && date?.length === 8) {
      offerOccurrenceRestore({
        calendarId,
        title: event.title,
        seriesId: seriesId(eventId),
        occurrenceDate: `${date.slice(0, 4)}-${date.slice(4, 6)}-${date.slice(6, 8)}`,
        seriesEtag: event.seriesEtag ?? event.etag,
        onRestored: reload,
      });
    } else if (scope !== 'following') {
      offerRestore({
        calendarId,
        eventId: seriesId(eventId),
        title: event.title,
        onRestored: reload,
      });
    }
    if (eventId.includes('_')) {
      // Recurring instance: re-fetch the visible range so a single-occurrence
      // delete leaves the rest of the series intact. The answer replaces the
      // calendar's events; a failed refetch leaves the series on screen.
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
    const { data } = await client.POST('/calendars/{calendarId}/memos', {
      params: { path: { calendarId } },
      body: { title: memo.title, body: memo.body, sortOrder: existing.length },
    });
    const newMemo = present(data);
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      memos: [...s.memos, { ...newMemo, calendarId }],
    }));
  },

  // Position is left out of both calls below: an omitted field keeps its
  // stored value, and sending a remembered one would let a stale copy of the
  // list reorder a memo that someone else had already moved.
  async updateMemo(calendarId, memoId, patch) {
    const currentAccountGeneration = accountGeneration;
    const updated = await patchMemo(get().memos, calendarId, memoId, {
      title: patch.title,
      body: patch.body,
      done: patch.done,
    });
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      memos: s.memos.map((m) => (m.id === memoId ? { ...updated, calendarId } : m)),
    }));
  },

  async toggleMemo(calendarId, memoId, done) {
    const currentAccountGeneration = accountGeneration;
    const updated = await patchMemo(get().memos, calendarId, memoId, { done });
    if (currentAccountGeneration !== accountGeneration) return;
    set((s) => ({
      memos: s.memos.map((m) => (m.id === memoId ? { ...updated, calendarId } : m)),
    }));
  },

  async deleteMemo(calendarId, memoId) {
    const currentAccountGeneration = accountGeneration;
    const etag = get().memos.find((m) => m.id === memoId)?.etag ?? '';
    await client.DELETE('/calendars/{calendarId}/memos/{memoId}', {
      params: { path: { calendarId, memoId }, header: { 'If-Match': etag } },
    });
    if (currentAccountGeneration !== accountGeneration) return;
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
    localStorage.removeItem('tt_seenCalendarIds');
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
      truncatedCalendarIds: [],
      eventsLoading: false,
      memosLoading: false,
      eventsError: null,
      memosError: null,
    });
  },

  visibleRange() {
    const { currentMonth, calendarView } = useUiStore.getState();
    return fetchWindow(calendarView, currentMonth);
  },
}));
