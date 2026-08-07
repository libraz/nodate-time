import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Calendar, CalendarEvent, Memo } from '@/types/calendar';

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
  isAbortError: (e: unknown) => e instanceof DOMException && e.name === 'AbortError',
}));

vi.mock('@/lib/toast', () => ({
  toast: {
    error: vi.fn(),
  },
}));

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { useCalendarStore } from './calendar-store';

const mockApi = vi.mocked(api);
const mockToast = vi.mocked(toast);

function cal(id: string, overrides: Partial<Calendar> = {}): Calendar {
  return {
    id,
    name: `Calendar ${id}`,
    color: '#47B2F7',
    coverUrl: '',
    createdAt: '',
    publicShared: false,
    role: 'owner',
    memberColor: '#47B2F7',
    ...overrides,
  };
}

function evt(
  id: string,
  calendarId: string,
  overrides: Partial<CalendarEvent> = {},
): CalendarEvent {
  return {
    id,
    calendarId,
    title: `Event ${id}`,
    allDay: false,
    startAt: '2026-04-20T10:00:00+09:00',
    endAt: '2026-04-20T11:00:00+09:00',
    color: '#47B2F7',
    ownerId: null,
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participants: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '',
    updatedAt: '',
    ...overrides,
  };
}

function memo(id: string, calendarId: string): Memo {
  return {
    id,
    calendarId,
    title: `Memo ${id}`,
    body: '',
    done: false,
    sortOrder: 0,
    createdAt: '',
    updatedAt: '',
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useCalendarStore.setState({
    calendars: [],
    events: [],
    memos: [],
    membersMap: {},
    labels: [],
    activeCalendarIds: [],
    isLoading: false,
  });
});

describe('fetchEvents', () => {
  it('aggregates events from every calendar and stamps the calendarId', async () => {
    useCalendarStore.setState({
      calendars: [
        cal('cal-1', { name: 'A', color: '#000' }),
        cal('cal-2', { name: 'B', color: '#111' }),
      ],
    });
    mockApi.get.mockImplementation(async (url: string) => {
      if (url.includes('/calendars/cal-1/events')) return [evt('e1', 'cal-1')] as never;
      if (url.includes('/calendars/cal-2/events')) return [evt('e2', 'cal-2')] as never;
      return [] as never;
    });

    await useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');

    const { events } = useCalendarStore.getState();
    expect(events).toHaveLength(2);
    expect(events.map((e) => e.calendarId).sort()).toEqual(['cal-1', 'cal-2']);
  });

  it('keeps successful calendars when one calendar fails', async () => {
    useCalendarStore.setState({
      calendars: [cal('cal-1'), cal('cal-2')],
    });
    mockApi.get.mockImplementation(async (url: string) => {
      if (url.includes('/calendars/cal-1/events')) return [evt('e1', 'cal-1')] as never;
      if (url.includes('/calendars/cal-2/events')) throw new Error('cal-2 failed');
      return [] as never;
    });

    await useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');

    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual(['e1']);
    expect(mockToast.error).toHaveBeenCalledWith('cal-2 failed');
  });

  it('leaves a calendar that did not answer showing what it had', async () => {
    useCalendarStore.setState({
      calendars: [cal('cal-1'), cal('cal-2')],
      events: [evt('e1', 'cal-1'), evt('e2', 'cal-2')],
    });
    mockApi.get.mockImplementation(async (url: string) => {
      if (url.includes('/calendars/cal-1/events')) return [evt('e1b', 'cal-1')] as never;
      if (url.includes('/calendars/cal-2/events')) throw new Error('cal-2 failed');
      return [] as never;
    });

    await useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');

    // cal-1 answered, so its events are replaced; cal-2 did not, and blanking
    // it would read as a series someone had just deleted.
    expect(
      useCalendarStore
        .getState()
        .events.map((e) => e.id)
        .sort(),
    ).toEqual(['e1b', 'e2']);
  });

  it('drops what belonged to a calendar that is no longer in the list', async () => {
    useCalendarStore.setState({
      calendars: [cal('cal-1')],
      events: [evt('e1', 'cal-1'), evt('gone', 'cal-removed')],
    });
    mockApi.get.mockImplementation(async () => [] as never);

    await useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');

    expect(useCalendarStore.getState().events).toEqual([]);
  });

  it('ignores an older range response that finishes after the latest request', async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1')] });
    let resolveOlder: ((events: CalendarEvent[]) => void) | undefined;
    let resolveLatest: ((events: CalendarEvent[]) => void) | undefined;
    mockApi.get.mockImplementation((url: string) => {
      if (url.includes('start=2026-04-01')) {
        return new Promise<CalendarEvent[]>((resolve) => {
          resolveOlder = resolve;
        }) as never;
      }
      return new Promise<CalendarEvent[]>((resolve) => {
        resolveLatest = resolve;
      }) as never;
    });

    const older = useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');
    const latest = useCalendarStore.getState().fetchEvents('2026-05-01', '2026-05-31');
    resolveLatest?.([evt('latest', 'cal-1')]);
    await latest;
    resolveOlder?.([evt('older', 'cal-1')]);
    await older;

    expect(useCalendarStore.getState().events.map((event) => event.id)).toEqual(['latest']);
  });

  it('says nothing and keeps the grid when the caller cancels', async () => {
    // The month view cancels the request it has scrolled past. Reporting that
    // as a failure would put an error in front of the user for something they
    // did not do, and blanking the cancelled calendars would empty the month
    // mid-flick.
    useCalendarStore.setState({
      calendars: [cal('cal-1'), cal('cal-2')],
      events: [evt('e1', 'cal-1'), evt('e2', 'cal-2')],
    });
    mockApi.get.mockImplementation(
      (_url: string, _skipAuthRedirect?: boolean, signal?: AbortSignal) =>
        new Promise((_resolve, reject) => {
          signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          );
        }) as never,
    );
    const controller = new AbortController();

    const pending = useCalendarStore
      .getState()
      .fetchEvents('2026-04-01', '2026-04-30', controller.signal);
    controller.abort();
    await pending;

    expect(mockApi.get).toHaveBeenCalledWith(
      expect.stringContaining('/calendars/cal-1/events'),
      false,
      controller.signal,
    );
    expect(mockToast.error).not.toHaveBeenCalled();
    expect(
      useCalendarStore
        .getState()
        .events.map((e) => e.id)
        .sort(),
    ).toEqual(['e1', 'e2']);
  });

  it('does not repopulate events after session data is reset', async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1')] });
    let resolveRequest: ((events: CalendarEvent[]) => void) | undefined;
    mockApi.get.mockReturnValue(
      new Promise<CalendarEvent[]>((resolve) => {
        resolveRequest = resolve;
      }) as never,
    );

    const request = useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');
    useCalendarStore.getState().resetSessionData();
    resolveRequest?.([evt('stale', 'cal-1')]);
    await request;

    expect(useCalendarStore.getState().events).toEqual([]);
  });
});

describe('fetchCalendars', () => {
  it('prunes stale active calendar ids and enables newly visible calendars', async () => {
    localStorage.setItem('tt_activeCalendarIds', JSON.stringify(['stale', 'cal-1']));
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') return [cal('cal-1'), cal('cal-2')] as never;
      if (url.endsWith('/members')) return [] as never;
      if (url.endsWith('/labels')) return [] as never;
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();

    expect(useCalendarStore.getState().activeCalendarIds).toEqual(['cal-1', 'cal-2']);
    expect(localStorage.getItem('tt_activeCalendarIds')).toBe('["cal-1","cal-2"]');
  });

  it('keeps every calendar hidden when that is what the user chose', async () => {
    localStorage.setItem('tt_activeCalendarIds', JSON.stringify([]));
    localStorage.setItem('tt_seenCalendarIds', JSON.stringify(['cal-1', 'cal-2']));
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') return [cal('cal-1'), cal('cal-2')] as never;
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();

    // Hiding everything is a choice, not an absence of one.
    expect(useCalendarStore.getState().activeCalendarIds).toEqual([]);
  });

  it('shows a calendar that appeared since the last visit, even with the rest hidden', async () => {
    localStorage.setItem('tt_activeCalendarIds', JSON.stringify([]));
    localStorage.setItem('tt_seenCalendarIds', JSON.stringify(['cal-1']));
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') return [cal('cal-1'), cal('cal-2')] as never;
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();

    expect(useCalendarStore.getState().activeCalendarIds).toEqual(['cal-2']);
  });

  it('shows everything on a first visit, which has made no choice yet', async () => {
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') return [cal('cal-1'), cal('cal-2')] as never;
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();

    expect(useCalendarStore.getState().activeCalendarIds).toEqual(['cal-1', 'cal-2']);
  });

  it('does not ask for a member list per calendar', async () => {
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') return [cal('cal-1'), cal('cal-2')] as never;
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();

    const s = useCalendarStore.getState();
    expect(s.calendars.map((c) => c.id)).toEqual(['cal-1', 'cal-2']);
    // The caller's role arrives with the calendar, so startup costs the same
    // whether the account has two calendars or twenty.
    expect(mockApi.get.mock.calls.map((c) => c[0])).not.toContain('/calendars/cal-1/members');
    expect(mockApi.get.mock.calls.map((c) => c[0])).not.toContain('/calendars/cal-2/members');
  });

  it('records why the calendar list is missing instead of leaving an empty grid', async () => {
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') throw new Error('network down');
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();

    const s = useCalendarStore.getState();
    expect(s.calendars).toEqual([]);
    expect(s.loadError).toBe('network down');
    expect(s.isLoading).toBe(false);
  });
});

describe('retryFailedLoads', () => {
  it('recovers the calendar list after a failed first attempt', async () => {
    let listAttempts = 0;
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') {
        listAttempts++;
        if (listAttempts === 1) throw new Error('network down');
        return [cal('cal-1')] as never;
      }
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();
    expect(useCalendarStore.getState().loadError).toBe('network down');

    await useCalendarStore.getState().retryFailedLoads();

    const s = useCalendarStore.getState();
    expect(s.loadError).toBeNull();
    expect(s.calendars.map((c) => c.id)).toEqual(['cal-1']);
  });

  it('recovers only the member lists that did not arrive', async () => {
    let cal2Attempts = 0;
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') return [cal('cal-1'), cal('cal-2')] as never;
      if (url === '/calendars/cal-2/members') {
        cal2Attempts++;
        if (cal2Attempts === 1) throw new Error('members failed');
        return [{ id: 'u1', name: 'A', email: 'a@example.com', role: 'editor' }] as never;
      }
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();
    await useCalendarStore.getState().fetchMembers('cal-1');
    await useCalendarStore.getState().fetchMembers('cal-2');
    expect(useCalendarStore.getState().memberErrors['cal-2']).toBe('members failed');

    mockApi.get.mockClear();
    await useCalendarStore.getState().retryFailedLoads();

    const s = useCalendarStore.getState();
    expect(s.memberErrors).toEqual({});
    // The calendar that answered the first time is not asked again: the retry
    // is for what is missing, not a second run of the whole startup.
    expect(mockApi.get.mock.calls.map((c) => c[0])).toEqual(['/calendars/cal-2/members']);
    expect(s.membersMap['cal-2']?.[0]?.role).toBe('editor');
  });
});

describe('fetchMemos', () => {
  it('reads the paged answer and walks it to the end', async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1')] });
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars/cal-1/memos') {
        return { items: [memo('m1', 'cal-1')], nextCursor: 'next' } as never;
      }
      if (url === '/calendars/cal-1/memos?cursor=next') {
        return { items: [memo('m2', 'cal-1')] } as never;
      }
      return { items: [] } as never;
    });

    await useCalendarStore.getState().fetchMemos();

    // The panel shows one arranged list, so stopping at the first page would
    // silently drop the rest of it -- and nothing else would say so.
    expect(useCalendarStore.getState().memos.map((m) => m.id)).toEqual(['m1', 'm2']);
  });

  it('stamps the calendarId on every memo it collects', async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1'), cal('cal-2')] });
    mockApi.get.mockImplementation(async (url: string) => {
      if (url.startsWith('/calendars/cal-1/memos')) {
        return { items: [memo('m1', 'cal-1')] } as never;
      }
      if (url.startsWith('/calendars/cal-2/memos')) {
        return { items: [memo('m2', 'cal-2')] } as never;
      }
      return { items: [] } as never;
    });

    await useCalendarStore.getState().fetchMemos();

    const byCalendar = useCalendarStore
      .getState()
      .memos.map((m) => m.calendarId)
      .sort();
    expect(byCalendar).toEqual(['cal-1', 'cal-2']);
  });

  it('leaves a calendar that did not answer showing what it had', async () => {
    useCalendarStore.setState({
      calendars: [cal('cal-1'), cal('cal-2')],
      memos: [memo('m1', 'cal-1'), memo('m2', 'cal-2')],
    });
    mockApi.get.mockImplementation(async (url: string) => {
      if (url.startsWith('/calendars/cal-1/memos')) {
        return { items: [memo('m1b', 'cal-1')] } as never;
      }
      throw new Error('cal-2 failed');
    });

    await useCalendarStore.getState().fetchMemos();

    expect(
      useCalendarStore
        .getState()
        .memos.map((m) => m.id)
        .sort(),
    ).toEqual(['m1b', 'm2']);
  });
});

describe('deleteCalendar', () => {
  it('removes the calendar and cascades its events, memos, and members', async () => {
    mockApi.delete.mockResolvedValue(undefined as never);
    useCalendarStore.setState({
      calendars: [
        cal('cal-1', { name: 'A', color: '#000' }),
        cal('cal-2', { name: 'B', color: '#111' }),
      ],
      events: [evt('e1', 'cal-1'), evt('e2', 'cal-2')],
      memos: [memo('m1', 'cal-1'), memo('m2', 'cal-2')],
      membersMap: { 'cal-1': [], 'cal-2': [] },
      activeCalendarIds: ['cal-1', 'cal-2'],
    });

    await useCalendarStore.getState().deleteCalendar('cal-1');

    const s = useCalendarStore.getState();
    expect(s.calendars.map((c) => c.id)).toEqual(['cal-2']);
    expect(s.events.map((e) => e.id)).toEqual(['e2']);
    expect(s.memos.map((m) => m.id)).toEqual(['m2']);
    expect(s.membersMap['cal-1']).toBeUndefined();
    expect(s.activeCalendarIds).toEqual(['cal-2']);
    expect(localStorage.getItem('tt_activeCalendarIds')).toBe('["cal-2"]');
  });
});

describe('leaveCalendar', () => {
  it('takes the calendar with the membership and asks nothing more of it', async () => {
    mockApi.delete.mockResolvedValue(undefined as never);
    useCalendarStore.setState({
      calendars: [
        cal('cal-1', { name: 'A', color: '#000' }),
        cal('cal-2', { name: 'B', color: '#111' }),
      ],
      events: [evt('e1', 'cal-1'), evt('e2', 'cal-2')],
      memos: [memo('m1', 'cal-1'), memo('m2', 'cal-2')],
      membersMap: { 'cal-1': [], 'cal-2': [] },
      activeCalendarIds: ['cal-1', 'cal-2'],
    });

    await useCalendarStore.getState().leaveCalendar('cal-1', 'member-me');

    expect(mockApi.delete).toHaveBeenCalledWith('/calendars/cal-1/members/member-me');
    // Refetching the members of a calendar the caller has just left answers
    // 403, which is what used to make a successful departure look failed.
    expect(mockApi.get).not.toHaveBeenCalled();

    const s = useCalendarStore.getState();
    expect(s.calendars.map((c) => c.id)).toEqual(['cal-2']);
    expect(s.events.map((e) => e.id)).toEqual(['e2']);
    expect(s.memos.map((m) => m.id)).toEqual(['m2']);
    expect(s.membersMap['cal-1']).toBeUndefined();
    expect(s.activeCalendarIds).toEqual(['cal-2']);
  });

  it('keeps the calendar when the departure itself fails', async () => {
    mockApi.delete.mockRejectedValue(new Error('nope'));
    useCalendarStore.setState({
      calendars: [cal('cal-1', { name: 'A', color: '#000' })],
      activeCalendarIds: ['cal-1'],
    });

    await expect(useCalendarStore.getState().leaveCalendar('cal-1', 'member-me')).rejects.toThrow(
      'nope',
    );
    expect(useCalendarStore.getState().calendars.map((c) => c.id)).toEqual(['cal-1']);
  });
});

describe('toggleCalendarFilter', () => {
  it('removes an active id and persists the change', () => {
    useCalendarStore.setState({ activeCalendarIds: ['cal-1', 'cal-2'] });
    useCalendarStore.getState().toggleCalendarFilter('cal-1');
    expect(useCalendarStore.getState().activeCalendarIds).toEqual(['cal-2']);
    expect(localStorage.getItem('tt_activeCalendarIds')).toBe('["cal-2"]');
  });

  it('adds an inactive id back', () => {
    useCalendarStore.setState({ activeCalendarIds: ['cal-2'] });
    useCalendarStore.getState().toggleCalendarFilter('cal-1');
    expect(useCalendarStore.getState().activeCalendarIds).toEqual(['cal-2', 'cal-1']);
  });
});

describe('addMemo', () => {
  it('sets sortOrder to the count of existing memos for that calendar', async () => {
    useCalendarStore.setState({ memos: [memo('m1', 'cal-1'), memo('m2', 'cal-1')] });
    mockApi.post.mockResolvedValue(memo('m3', 'cal-1') as never);

    await useCalendarStore.getState().addMemo('cal-1', { title: 'third', body: '' });

    expect(mockApi.post).toHaveBeenCalledWith('/calendars/cal-1/memos', {
      title: 'third',
      body: '',
      sortOrder: 2,
    });
    expect(useCalendarStore.getState().memos.map((m) => m.id)).toEqual(['m1', 'm2', 'm3']);
  });

  it('does not inject the memo after session data is reset', async () => {
    let resolvePost: ((m: Memo) => void) | undefined;
    mockApi.post.mockReturnValue(
      new Promise<Memo>((resolve) => {
        resolvePost = resolve;
      }) as never,
    );

    const request = useCalendarStore.getState().addMemo('cal-1', { title: 'stale', body: '' });
    useCalendarStore.getState().resetSessionData();
    resolvePost?.(memo('m-stale', 'cal-1'));
    await request;

    expect(useCalendarStore.getState().memos).toEqual([]);
  });
});

describe('addCalendar', () => {
  it('does not inject the calendar or persist ids after session data is reset', async () => {
    let resolvePost: ((c: Calendar) => void) | undefined;
    mockApi.post.mockReturnValue(
      new Promise<Calendar>((resolve) => {
        resolvePost = resolve;
      }) as never,
    );

    const request = useCalendarStore.getState().addCalendar({ name: 'stale', color: '#000' });
    useCalendarStore.getState().resetSessionData();
    resolvePost?.(cal('cal-stale'));
    await request;

    expect(useCalendarStore.getState().calendars).toEqual([]);
    expect(useCalendarStore.getState().activeCalendarIds).toEqual([]);
    expect(localStorage.getItem('tt_activeCalendarIds')).toBeNull();
  });
});

describe('addEvent', () => {
  it('appends a non-recurring event to the store', async () => {
    mockApi.post.mockResolvedValue(evt('e-new', 'cal-1') as never);

    await useCalendarStore.getState().addEvent('cal-1', {
      title: 'One-off',
      allDay: false,
      startAt: '2026-04-20T10:00:00+09:00',
      endAt: '2026-04-20T11:00:00+09:00',
    });

    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual(['e-new']);
  });

  it('does not inject the event after session data is reset', async () => {
    let resolvePost: ((e: CalendarEvent) => void) | undefined;
    mockApi.post.mockReturnValue(
      new Promise<CalendarEvent>((resolve) => {
        resolvePost = resolve;
      }) as never,
    );

    const request = useCalendarStore.getState().addEvent('cal-1', {
      title: 'stale',
      allDay: false,
      startAt: '2026-04-20T10:00:00+09:00',
      endAt: '2026-04-20T11:00:00+09:00',
    });
    useCalendarStore.getState().resetSessionData();
    resolvePost?.(evt('e-stale', 'cal-1'));
    await request;

    expect(useCalendarStore.getState().events).toEqual([]);
  });

  it('re-fetches the visible range for a recurring event instead of appending the master row', async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1')] });
    const master = 'c'.repeat(32);
    mockApi.post.mockResolvedValue(
      evt(master, 'cal-1', {
        recurrenceRule: { freq: 'weekly', interval: 1 },
        isRecurrence: false,
      }) as never,
    );
    mockApi.get.mockResolvedValue([
      evt(`${master}_20260420`, 'cal-1'),
      evt(`${master}_20260427`, 'cal-1'),
    ] as never);

    await useCalendarStore.getState().addEvent('cal-1', {
      title: 'Weekly',
      allDay: false,
      startAt: '2026-04-20T10:00:00+09:00',
      endAt: '2026-04-20T11:00:00+09:00',
      recurrenceRule: { freq: 'weekly', interval: 1 },
    });

    expect(mockApi.get).toHaveBeenCalled();
    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual([
      `${master}_20260420`,
      `${master}_20260427`,
    ]);
  });
});

describe('deleteEvent', () => {
  it('removes a single non-recurring event', async () => {
    mockApi.delete.mockResolvedValue(undefined as never);
    useCalendarStore.setState({ events: [evt('keep', 'cal-1'), evt('drop', 'cal-1')] });

    await useCalendarStore.getState().deleteEvent('cal-1', 'drop');

    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual(['keep']);
  });

  it('re-syncs from the server after deleting a recurring instance', async () => {
    mockApi.delete.mockResolvedValue(undefined as never);
    const parent = 'a'.repeat(32);
    const other = 'b'.repeat(32);
    // A single-occurrence delete preserves the rest of the series, so the store
    // re-fetches the visible range; the server returns the surviving instances.
    mockApi.get.mockResolvedValue([
      evt(`${parent}_20260410`, 'cal-1'),
      evt(other, 'cal-1'),
    ] as never);
    useCalendarStore.setState({
      calendars: [{ id: 'cal-1' } as never],
      events: [
        evt(`${parent}_20260403`, 'cal-1'),
        evt(`${parent}_20260410`, 'cal-1'),
        evt(other, 'cal-1'),
      ],
    });

    await useCalendarStore.getState().deleteEvent('cal-1', `${parent}_20260403`, 'this');

    expect(mockApi.delete).toHaveBeenCalledWith(
      `/calendars/cal-1/events/${parent}_20260403?scope=this`,
    );
    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual([
      `${parent}_20260410`,
      other,
    ]);
  });
});

describe('updateEvent revisions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useCalendarStore.setState({ calendars: [{ id: 'cal-1' } as never], events: [] });
  });

  it('names the copy being replaced when the editor knows it', async () => {
    // The update carries every field, so a save built on a copy someone else
    // has since replaced erases their work. If-Match turns that into a refusal.
    mockApi.put.mockResolvedValue(evt('e1', 'cal-1') as never);
    useCalendarStore.setState({ events: [evt('e1', 'cal-1')] });

    await useCalendarStore
      .getState()
      .updateEvent('cal-1', 'e1', { title: 'x' } as never, undefined, '"20260910T040000.000"');

    expect(mockApi.put).toHaveBeenCalledWith(
      '/calendars/cal-1/events/e1',
      { title: 'x' },
      {
        'If-Match': '"20260910T040000.000"',
      },
    );
  });

  it('saves unconditionally when there is no revision to stand on', async () => {
    // A drag applies the gesture the user just made, and a revision that could
    // not be read must not turn every save into a refusal.
    mockApi.put.mockResolvedValue(evt('e1', 'cal-1') as never);
    useCalendarStore.setState({ events: [evt('e1', 'cal-1')] });

    await useCalendarStore.getState().updateEvent('cal-1', 'e1', { title: 'x' } as never);

    expect(mockApi.put).toHaveBeenCalledWith(
      '/calendars/cal-1/events/e1',
      { title: 'x' },
      undefined,
    );
  });
});
