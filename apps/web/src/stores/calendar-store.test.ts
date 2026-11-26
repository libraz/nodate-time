import { beforeEach, describe, expect, it, vi } from 'vitest';
import { makeCalendar, makeEvent, makeMemo } from '@/test/make-event';
import type { Calendar, CalendarEvent, Memo } from '@/types/calendar';

// The typed client is replaced by an adapter over five stubs. They see the
// request as a path with its parameters filled in (query string last), and
// the init the client was called with, so assertions read as the requests made.
const raw = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
}));

type Init = {
  params?: { path?: Record<string, string>; query?: Record<string, string>; header?: unknown };
  body?: unknown;
  signal?: AbortSignal;
};

function resolved(path: string, init?: Init): string {
  const filled = path.replace(/\{(\w+)\}/g, (_, key: string) => init?.params?.path?.[key] ?? '');
  const query = new URLSearchParams(init?.params?.query).toString();
  return query ? `${filled}?${query}` : filled;
}

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
  isAbortError: (e: unknown) => e instanceof DOMException && e.name === 'AbortError',

  client: {
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    GET: async (path: string, init?: Init) => ({ data: await raw.get(resolved(path, init), init) }),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    POST: async (path: string, init?: Init) => ({
      data: await raw.post(resolved(path, init), init),
    }),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    PUT: async (path: string, init?: Init) => ({ data: await raw.put(resolved(path, init), init) }),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    PATCH: async (path: string, init?: Init) => ({
      data: await raw.patch(resolved(path, init), init),
    }),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    DELETE: async (path: string, init?: Init) => ({
      data: await raw.delete(resolved(path, init), init),
    }),
  },
}));

vi.mock('@/lib/toast', () => ({
  toast: {
    error: vi.fn(),
  },
}));

const upload = vi.hoisted(() => ({ uploadCalendarCover: vi.fn() }));
vi.mock('@/lib/upload', () => ({
  present: <T>(data: T | undefined): T => {
    if (data === undefined) throw new Error('empty response');
    return data;
  },
  uploadCalendarCover: upload.uploadCalendarCover,
}));

const restore = vi.hoisted(() => ({ offerRestore: vi.fn(), offerOccurrenceRestore: vi.fn() }));
vi.mock('@/lib/event-restore', () => restore);

const filterNotice = vi.hoisted(() => ({ offerFilterReveal: vi.fn() }));
vi.mock('@/lib/filter-notice', () => filterNotice);

vi.mock('@/lib/image-resize', () => ({
  resizeImageForCover: vi.fn(async () => ({
    bytes: new TextEncoder().encode('resized bytes').buffer,
    contentType: 'image/webp',
    width: 1600,
    height: 900,
  })),
}));

import { toast } from '@/lib/toast';
import { useCalendarStore } from './calendar-store';

const mockApi = raw;
const mockToast = vi.mocked(toast);

const cal = makeCalendar;
const memo = makeMemo;

/** A listing answer: the envelope the events endpoint wraps its items in. */
function page(items: CalendarEvent[], hasMore = false) {
  return { items, hasMore };
}

function evt(
  id: string,
  calendarId: string,
  overrides: Partial<CalendarEvent> = {},
): CalendarEvent {
  return makeEvent({ id, calendarId, title: `Event ${id}`, ...overrides });
}

describe('calendar cover upload', () => {
  it('clears a signed URL omitted from a later calendar update', async () => {
    const existing = cal('cal-1', { coverImageUrl: 'https://storage.example/stale-signed-cover' });
    mockApi.patch.mockResolvedValue(cal('cal-1', { name: 'Renamed' }));
    useCalendarStore.setState({ calendars: [existing] });

    await useCalendarStore.getState().updateCalendar('cal-1', { name: 'Renamed' });

    expect(useCalendarStore.getState().calendars[0]?.name).toBe('Renamed');
    expect(useCalendarStore.getState().calendars[0]?.coverImageUrl).toBeUndefined();
  });

  it('names the calendar version it read when it updates', async () => {
    mockApi.patch.mockResolvedValue(cal('cal-1', { name: 'Renamed' }));
    useCalendarStore.setState({ calendars: [cal('cal-1', { etag: '"c1"' })] });

    await useCalendarStore.getState().updateCalendar('cal-1', { name: 'Renamed' });

    expect(mockApi.patch).toHaveBeenCalledWith(
      '/calendars/cal-1',
      expect.objectContaining({
        params: expect.objectContaining({ header: { 'If-Match': '"c1"' } }),
        body: { name: 'Renamed' },
      }),
    );
  });

  it('uploads the resized bytes and stores the signed image URL', async () => {
    const file = new Blob(['cover bytes'], { type: 'image/png' }) as File;
    const updated = cal('cal-1', {
      coverImageUrl: 'https://storage.example/signed-cover',
      coverUploadAllowed: true,
    });
    upload.uploadCalendarCover.mockResolvedValue(updated);
    useCalendarStore.setState({ calendars: [cal('cal-1')] });

    await useCalendarStore.getState().uploadCalendarCover('cal-1', file);

    expect(upload.uploadCalendarCover).toHaveBeenCalledWith(
      'cal-1',
      expect.anything(),
      'image/webp',
    );
    expect(useCalendarStore.getState().calendars[0]).toMatchObject({
      coverImageUrl: 'https://storage.example/signed-cover',
    });
  });

  it('clears a prior signed URL after removing the uploaded image', async () => {
    const existing = cal('cal-1', { coverImageUrl: 'https://storage.example/old-signed-cover' });
    mockApi.delete.mockResolvedValue(undefined);
    useCalendarStore.setState({ calendars: [existing] });

    await useCalendarStore.getState().deleteCalendarCover('cal-1');

    expect(mockApi.delete).toHaveBeenCalledWith('/calendars/cal-1/cover', expect.anything());
    expect(useCalendarStore.getState().calendars[0]?.coverImageUrl).toBeUndefined();
  });

  it('does not apply an upload response after the signed-in account changes', async () => {
    const file = new Blob(['cover bytes'], { type: 'image/png' }) as File;
    let completeUpload: (calendar: Calendar) => void = () => {};
    upload.uploadCalendarCover.mockImplementation(
      () => new Promise((resolve) => (completeUpload = resolve)),
    );
    useCalendarStore.setState({ calendars: [cal('cal-1')] });

    const pending = useCalendarStore.getState().uploadCalendarCover('cal-1', file);
    useCalendarStore.getState().resetSessionData();
    useCalendarStore.setState({ calendars: [cal('other-account-calendar')] });
    await vi.waitFor(() => expect(upload.uploadCalendarCover).toHaveBeenCalled());
    completeUpload(cal('cal-1', { coverImageUrl: 'https://storage.example/signed-cover' }));
    await pending;

    expect(useCalendarStore.getState().calendars).toEqual([cal('other-account-calendar')]);
  });
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useCalendarStore.setState({
    calendars: [],
    events: [],
    memos: [],
    membersMap: {},
    labelsMap: {},
    labelErrors: {},
    activeCalendarIds: [],
    isLoading: false,
  });
});

describe('fetchEvents truncation', () => {
  // A listing that had more to give looks like a complete one, and the
  // missing tail of the window reads as free time.
  it('records which calendars had more, and clears it when they no longer do', async () => {
    useCalendarStore.setState({
      calendars: [cal('cal-1'), cal('cal-2')],
      events: [],
      truncatedCalendarIds: [],
    });
    const hasMoreFor = (cutId: string | null) => async (path: string) =>
      page([], cutId !== null && path.includes(`/calendars/${cutId}/`));
    mockApi.get
      .mockImplementationOnce(hasMoreFor('cal-1'))
      .mockImplementationOnce(hasMoreFor('cal-1'));

    await useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');
    expect(useCalendarStore.getState().truncatedCalendarIds).toEqual(['cal-1']);

    mockApi.get.mockImplementationOnce(hasMoreFor(null)).mockImplementationOnce(hasMoreFor(null));
    await useCalendarStore.getState().fetchEvents('2026-05-01', '2026-05-31');
    expect(useCalendarStore.getState().truncatedCalendarIds).toEqual([]);
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
      if (url.includes('/calendars/cal-1/events')) return page([evt('e1', 'cal-1')]);
      if (url.includes('/calendars/cal-2/events')) return page([evt('e2', 'cal-2')]);
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
      if (url.includes('/calendars/cal-1/events')) return page([evt('e1', 'cal-1')]);
      if (url.includes('/calendars/cal-2/events')) throw new Error('cal-2 failed');
      return [] as never;
    });

    await useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');

    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual(['e1']);
    expect(useCalendarStore.getState().eventsError).toBe('cal-2 failed');
  });

  it('leaves a calendar that did not answer showing what it had', async () => {
    useCalendarStore.setState({
      calendars: [cal('cal-1'), cal('cal-2')],
      events: [evt('e1', 'cal-1'), evt('e2', 'cal-2')],
    });
    mockApi.get.mockImplementation(async (url: string) => {
      if (url.includes('/calendars/cal-1/events')) return page([evt('e1b', 'cal-1')]);
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
    mockApi.get.mockImplementation(async () => page([]));

    await useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');

    expect(useCalendarStore.getState().events).toEqual([]);
  });

  it('ignores an older range response that finishes after the latest request', async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1')] });
    let resolveOlder: ((answer: ReturnType<typeof page>) => void) | undefined;
    let resolveLatest: ((answer: ReturnType<typeof page>) => void) | undefined;
    mockApi.get.mockImplementation((url: string) => {
      if (url.includes('start=2026-04-01')) {
        return new Promise<ReturnType<typeof page>>((resolve) => {
          resolveOlder = resolve;
        });
      }
      return new Promise<ReturnType<typeof page>>((resolve) => {
        resolveLatest = resolve;
      });
    });

    const older = useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');
    const latest = useCalendarStore.getState().fetchEvents('2026-05-01', '2026-05-31');
    resolveLatest?.(page([evt('latest', 'cal-1')]));
    await latest;
    resolveOlder?.(page([evt('older', 'cal-1')]));
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
      (_url: string, init?: Init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          );
        }),
    );
    const controller = new AbortController();

    const pending = useCalendarStore
      .getState()
      .fetchEvents('2026-04-01', '2026-04-30', controller.signal);
    controller.abort();
    await pending;

    expect(mockApi.get).toHaveBeenCalledWith(
      expect.stringContaining('/calendars/cal-1/events'),
      expect.objectContaining({ signal: controller.signal }),
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
    let resolveRequest: ((answer: ReturnType<typeof page>) => void) | undefined;
    mockApi.get.mockReturnValue(
      new Promise<ReturnType<typeof page>>((resolve) => {
        resolveRequest = resolve;
      }),
    );

    const request = useCalendarStore.getState().fetchEvents('2026-04-01', '2026-04-30');
    useCalendarStore.getState().resetSessionData();
    resolveRequest?.(page([evt('stale', 'cal-1')]));
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

describe('fetchMembers', () => {
  it('brings the calendar member count up to the roster it just read', async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1', { memberCount: 1 }), cal('cal-2')] });
    mockApi.get.mockResolvedValueOnce([
      { id: 'u1', name: 'A', email: 'a@example.com', role: 'owner', color: '#000000' },
      { id: 'u2', name: 'B', email: 'b@example.com', role: 'editor', color: '#000000' },
    ] as never);

    await useCalendarStore.getState().fetchMembers('cal-1');

    const counts = useCalendarStore.getState().calendars.map((c) => [c.id, c.memberCount]);
    expect(counts).toEqual([
      ['cal-1', 2],
      ['cal-2', 1],
    ]);
  });
});

describe('fetchMemos', () => {
  it("passes the caller's signal on and stays quiet when the load is cancelled", async () => {
    useCalendarStore.setState({ calendars: [cal('cal-1')], memos: [] });
    mockToast.error.mockClear();
    const controller = new AbortController();
    mockApi.get.mockRejectedValue(new DOMException('aborted', 'AbortError'));

    await useCalendarStore.getState().fetchMemos(controller.signal);

    expect(mockApi.get).toHaveBeenCalledWith(
      '/calendars/cal-1/memos',
      expect.objectContaining({ signal: controller.signal }),
    );
    expect(mockToast.error).not.toHaveBeenCalled();
  });

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

    expect(mockApi.delete).toHaveBeenCalledWith(
      '/calendars/cal-1/members/member-me',
      expect.anything(),
    );
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

describe('updateMemberColor', () => {
  // An event is drawn in its owner's colour, so the events already on screen
  // kept the old one until the next navigation.
  it("recolours the loaded events on that member's layer, and only those", async () => {
    mockApi.put.mockResolvedValue({ id: 'u1', color: '#123456' } as never);
    useCalendarStore.setState({
      events: [
        evt('e1', 'cal-1', { ownerId: 'u1' }),
        evt('e2', 'cal-1', { ownerId: 'u2' }),
        evt('e3', 'cal-2', { ownerId: 'u1' }),
      ],
      membersMap: {
        'cal-1': [{ id: 'u1', name: 'A', email: '', role: 'writer', color: '#47B2F7' }],
      },
    });

    await useCalendarStore.getState().updateMemberColor('cal-1', 'u1', '#123456');

    expect(mockApi.put).toHaveBeenCalledTimes(1);
    expect(mockApi.put).toHaveBeenCalledWith(
      '/calendars/cal-1/members/u1/color',
      expect.objectContaining({ body: { color: '#123456' } }),
    );
    const s = useCalendarStore.getState();
    expect(s.events.map((e) => e.color)).toEqual(['#123456', '#47B2F7', '#47B2F7']);
    expect(s.membersMap['cal-1']?.[0]?.color).toBe('#123456');
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

    expect(mockApi.post).toHaveBeenCalledWith(
      '/calendars/cal-1/memos',
      expect.objectContaining({ body: { title: 'third', body: '', sortOrder: 2 } }),
    );
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
    mockApi.get.mockResolvedValue(
      page([evt(`${master}_20260420`, 'cal-1'), evt(`${master}_20260427`, 'cal-1')]),
    );

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
  it('removes a single non-recurring event, checked against the listing etag', async () => {
    mockApi.delete.mockResolvedValue(undefined);
    const drop = evt('drop', 'cal-1', { etag: '"listed"' });
    useCalendarStore.setState({ events: [evt('keep', 'cal-1'), drop] });

    await useCalendarStore.getState().deleteEvent(drop);

    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual(['keep']);
    expect(mockApi.delete).toHaveBeenCalledWith(
      '/calendars/cal-1/events/drop',
      expect.objectContaining({
        params: expect.objectContaining({ header: { 'If-Match': '"listed"' } }),
      }),
    );
  });

  it('offers to put a deleted event back', async () => {
    mockApi.delete.mockResolvedValue(undefined);
    const drop = evt('drop', 'cal-1', { title: 'Lunch' });
    useCalendarStore.setState({ events: [drop] });

    await useCalendarStore.getState().deleteEvent(drop);

    expect(restore.offerRestore).toHaveBeenCalledWith(
      expect.objectContaining({ calendarId: 'cal-1', eventId: 'drop', title: 'Lunch' }),
    );
    expect(restore.offerOccurrenceRestore).not.toHaveBeenCalled();
  });

  it('offers nothing when the delete fails', async () => {
    mockApi.delete.mockRejectedValue(new Error('refused'));
    const drop = evt('drop', 'cal-1');
    useCalendarStore.setState({ events: [drop] });

    await expect(useCalendarStore.getState().deleteEvent(drop)).rejects.toThrow('refused');

    expect(restore.offerRestore).not.toHaveBeenCalled();
  });

  it('re-syncs from the server after deleting a recurring instance', async () => {
    mockApi.delete.mockResolvedValue(undefined);
    const parent = 'a'.repeat(32);
    const other = 'b'.repeat(32);
    // A single-occurrence delete preserves the rest of the series, so the store
    // re-fetches the visible range; the server returns the surviving instances.
    mockApi.get.mockResolvedValue(page([evt(`${parent}_20260410`, 'cal-1'), evt(other, 'cal-1')]));
    const gone = evt(`${parent}_20260403`, 'cal-1', {
      isRecurrence: true,
      recurrenceDate: '20260403',
      etag: '"occurrence"',
      seriesEtag: '"master"',
    });
    useCalendarStore.setState({
      calendars: [cal('cal-1')],
      events: [gone, evt(`${parent}_20260410`, 'cal-1'), evt(other, 'cal-1')],
    });

    await useCalendarStore.getState().deleteEvent(gone, 'this');

    expect(mockApi.delete).toHaveBeenCalledWith(
      `/calendars/cal-1/events/${parent}_20260403?scope=this`,
      expect.objectContaining({
        params: expect.objectContaining({ header: { 'If-Match': '"occurrence"' } }),
      }),
    );
    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual([
      `${parent}_20260410`,
      other,
    ]);
  });

  it('undoes one cancelled occurrence through the exception, against the master', async () => {
    mockApi.delete.mockResolvedValue(undefined);
    mockApi.get.mockResolvedValue(page([]));
    const parent = 'a'.repeat(32);
    const gone = evt(`${parent}_20260403`, 'cal-1', {
      isRecurrence: true,
      recurrenceDate: '20260403',
      seriesEtag: '"master"',
      title: 'Standup',
    });
    useCalendarStore.setState({ calendars: [cal('cal-1')], events: [gone] });

    await useCalendarStore.getState().deleteEvent(gone, 'this');

    expect(restore.offerOccurrenceRestore).toHaveBeenCalledWith(
      expect.objectContaining({
        calendarId: 'cal-1',
        seriesId: parent,
        occurrenceDate: '2026-04-03',
        seriesEtag: '"master"',
        title: 'Standup',
      }),
    );
    expect(restore.offerRestore).not.toHaveBeenCalled();
  });

  it('checks a series delete against the master and offers to restore the series', async () => {
    mockApi.delete.mockResolvedValue(undefined);
    mockApi.get.mockResolvedValue(page([]));
    const parent = 'a'.repeat(32);
    const occurrence = evt(`${parent}_20260403`, 'cal-1', {
      isRecurrence: true,
      etag: '"occurrence"',
      seriesEtag: '"master"',
    });
    useCalendarStore.setState({ calendars: [cal('cal-1')], events: [occurrence] });

    await useCalendarStore.getState().deleteEvent(occurrence, 'all');

    expect(mockApi.delete).toHaveBeenCalledWith(
      `/calendars/cal-1/events/${parent}_20260403?scope=all`,
      expect.objectContaining({
        params: expect.objectContaining({ header: { 'If-Match': '"master"' } }),
      }),
    );
    expect(restore.offerRestore).toHaveBeenCalledWith(expect.objectContaining({ eventId: parent }));
  });

  it('cannot be undone from this occurrence onward, and says nothing about it', async () => {
    mockApi.delete.mockResolvedValue(undefined);
    mockApi.get.mockResolvedValue(page([]));
    const parent = 'a'.repeat(32);
    const occurrence = evt(`${parent}_20260403`, 'cal-1', {
      isRecurrence: true,
      seriesEtag: '"master"',
    });
    useCalendarStore.setState({ calendars: [cal('cal-1')], events: [occurrence] });

    await useCalendarStore.getState().deleteEvent(occurrence, 'following');

    expect(mockApi.delete).toHaveBeenCalledWith(
      `/calendars/cal-1/events/${parent}_20260403?scope=following`,
      expect.objectContaining({
        params: expect.objectContaining({ header: { 'If-Match': '"master"' } }),
      }),
    );
    expect(restore.offerRestore).not.toHaveBeenCalled();
    expect(restore.offerOccurrenceRestore).not.toHaveBeenCalled();
  });
});

describe('recurring writes when the refetch fails', () => {
  const parent = 'c'.repeat(32);
  const series = () => [evt(`${parent}_20260403`, 'cal-1'), evt(`${parent}_20260410`, 'cal-1')];

  beforeEach(() => {
    vi.clearAllMocks();
    useCalendarStore.setState({ calendars: [{ id: 'cal-1' } as never], events: series() });
    mockApi.get.mockRejectedValue(new Error('offline'));
  });

  // The series used to be cleared before the refetch, so a refetch that did
  // not come back left it blank and looking deleted.
  it('keeps the series on screen after an edit', async () => {
    mockApi.patch.mockResolvedValue(evt(`${parent}_20260403`, 'cal-1'));

    await useCalendarStore
      .getState()
      .updateEvent('cal-1', `${parent}_20260403`, { title: 'x' }, 'all', '"master"');

    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual(series().map((e) => e.id));
  });

  it('keeps the series on screen after deleting one occurrence', async () => {
    mockApi.delete.mockResolvedValue(undefined);

    await useCalendarStore.getState().deleteEvent(series()[0] as CalendarEvent, 'this');

    expect(useCalendarStore.getState().events.map((e) => e.id)).toEqual(series().map((e) => e.id));
  });
});

describe('updateEvent etags', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useCalendarStore.setState({ calendars: [cal('cal-1')], events: [] });
  });

  it('names the copy being replaced', async () => {
    // A partial update merges into whatever is stored, so a save built on a
    // copy someone else has since changed is refused rather than layered on.
    mockApi.patch.mockResolvedValue(evt('e1', 'cal-1'));
    useCalendarStore.setState({ events: [evt('e1', 'cal-1')] });

    await useCalendarStore
      .getState()
      .updateEvent('cal-1', 'e1', { title: 'x' }, undefined, '"20260910T040000.000"');

    expect(mockApi.patch).toHaveBeenCalledWith(
      '/calendars/cal-1/events/e1',
      expect.objectContaining({
        params: expect.objectContaining({ header: { 'If-Match': '"20260910T040000.000"' } }),
        body: { title: 'x' },
      }),
    );
  });

  it('sends the scope as a query parameter', async () => {
    mockApi.patch.mockResolvedValue(evt('e1', 'cal-1'));
    mockApi.get.mockResolvedValue(page([]));

    await useCalendarStore
      .getState()
      .updateEvent('cal-1', 'e1_20260403', { title: 'x' }, 'following', '"master"');

    expect(mockApi.patch.mock.calls[0]?.[0]).toBe(
      '/calendars/cal-1/events/e1_20260403?scope=following',
    );
  });

  it('replaces the event with the answer, which carries the new etag', async () => {
    mockApi.patch.mockResolvedValue(evt('e1', 'cal-1', { etag: '"after"' }));
    useCalendarStore.setState({ events: [evt('e1', 'cal-1', { etag: '"before"' })] });

    await useCalendarStore
      .getState()
      .updateEvent('cal-1', 'e1', { title: 'x' }, undefined, '"before"');

    expect(useCalendarStore.getState().events[0]?.etag).toBe('"after"');
  });

  it('lets a refusal through and leaves the copy on screen as it was', async () => {
    mockApi.patch.mockRejectedValue(Object.assign(new Error('stale'), { status: 412 }));
    useCalendarStore.setState({ events: [evt('e1', 'cal-1', { etag: '"before"' })] });

    await expect(
      useCalendarStore.getState().updateEvent('cal-1', 'e1', { title: 'x' }, undefined, '"before"'),
    ).rejects.toThrow('stale');

    expect(useCalendarStore.getState().events[0]?.etag).toBe('"before"');
  });
});

describe('labels', () => {
  const label = (id: string, name: string, etag = `"${id}"`) => ({
    id,
    name,
    color: '#47B2F7',
    etag,
  });

  /** Answers a labels listing and the event listing a label change reads again. */
  function answer(labels: ReturnType<typeof label>[]) {
    mockApi.get.mockImplementation(async (url: string) =>
      url.endsWith('/labels') ? labels : page([]),
    );
  }

  it('keeps each calendar own list', async () => {
    answer([label('l1', 'Live')]);
    await useCalendarStore.getState().fetchLabels('cal-1');
    answer([label('l2', 'Lesson')]);
    await useCalendarStore.getState().fetchLabels('cal-2');

    const { labelsMap } = useCalendarStore.getState();
    expect(labelsMap['cal-1']?.map((l) => l.name)).toEqual(['Live']);
    expect(labelsMap['cal-2']?.map((l) => l.name)).toEqual(['Lesson']);
  });

  it('records a failed listing instead of throwing', async () => {
    mockApi.get.mockRejectedValue(new Error('down'));

    await useCalendarStore.getState().fetchLabels('cal-1');

    expect(useCalendarStore.getState().labelErrors['cal-1']).toBe('down');
  });

  it('creates a label, then reads the labels and events again', async () => {
    const fetchEvents = vi.fn(async () => {});
    useCalendarStore.setState({ fetchEvents });
    mockApi.post.mockResolvedValue(label('l1', 'Live'));
    answer([label('l1', 'Live')]);

    await useCalendarStore.getState().addLabel('cal-1', { name: 'Live', color: '#47B2F7' });

    expect(mockApi.post.mock.calls[0]?.[0]).toBe('/calendars/cal-1/labels');
    expect(mockApi.post.mock.calls[0]?.[1]).toMatchObject({
      body: { name: 'Live', color: '#47B2F7' },
    });
    expect(useCalendarStore.getState().labelsMap['cal-1']).toHaveLength(1);
    expect(fetchEvents).toHaveBeenCalled();
  });

  it('sends the version the list carries, read again after each change', async () => {
    useCalendarStore.setState({ labelsMap: { 'cal-1': [label('l1', 'Live', '"v1"')] } });
    mockApi.patch.mockResolvedValue(label('l1', 'Gig'));
    mockApi.delete.mockResolvedValue(undefined);
    answer([label('l1', 'Gig', '"v2"')]);

    await useCalendarStore.getState().updateLabel('cal-1', 'l1', { name: 'Gig' });
    await useCalendarStore.getState().deleteLabel('cal-1', 'l1');

    expect(mockApi.patch.mock.calls[0]?.[0]).toBe('/calendars/cal-1/labels/l1');
    expect(mockApi.patch.mock.calls[0]?.[1]).toMatchObject({
      params: { header: { 'If-Match': '"v1"' } },
      body: { name: 'Gig' },
    });
    expect(mockApi.delete.mock.calls[0]?.[1]).toMatchObject({
      params: { header: { 'If-Match': '"v2"' } },
    });
  });

  it('omits a null label from a create body and keeps it on a patch', async () => {
    mockApi.post.mockResolvedValue(evt('e1', 'cal-1'));
    mockApi.patch.mockResolvedValue(evt('e1', 'cal-1'));

    await useCalendarStore
      .getState()
      .addEvent('cal-1', { title: 'x', allDay: true, labelId: null });
    await useCalendarStore
      .getState()
      .updateEvent('cal-1', 'e1', { labelId: null }, undefined, '"e"');

    expect(mockApi.post.mock.calls[0]?.[1]).toMatchObject({ body: { title: 'x' } });
    expect(mockApi.post.mock.calls[0]?.[1]?.body).not.toHaveProperty('labelId');
    expect(mockApi.patch.mock.calls[0]?.[1]).toMatchObject({ body: { labelId: null } });
  });
});

describe('event filter', () => {
  const onlyA = {
    match: 'all' as const,
    conditions: [
      { id: 'c1', field: 'owner' as const, exclude: false, people: [{ id: 'a', name: 'A' }] },
    ],
  };
  const input = {
    title: 'x',
    allDay: false,
    startAt: '2026-04-20T10:00:00+09:00',
    endAt: '2026-04-20T11:00:00+09:00',
  };

  beforeEach(() => {
    filterNotice.offerFilterReveal.mockClear();
  });

  it('persists the filter and clears it on reset', () => {
    useCalendarStore.getState().setEventFilter(onlyA);
    expect(JSON.parse(localStorage.getItem('tt_eventFilter') ?? 'null')).toEqual(onlyA);

    useCalendarStore.getState().resetSessionData();
    expect(useCalendarStore.getState().eventFilter).toEqual({ match: 'all', conditions: [] });
    expect(localStorage.getItem('tt_eventFilter')).toBeNull();
  });

  it('says so when a saved event is hidden by the filter, and the offer clears it', async () => {
    useCalendarStore.setState({ eventFilter: onlyA });
    mockApi.post.mockResolvedValue(evt('e-mine', 'cal-1', { ownerId: 'me' }) as never);

    await useCalendarStore.getState().addEvent('cal-1', input);

    expect(filterNotice.offerFilterReveal).toHaveBeenCalledTimes(1);
    filterNotice.offerFilterReveal.mock.calls[0]?.[0]();
    expect(useCalendarStore.getState().eventFilter.conditions).toEqual([]);
  });

  it('stays quiet when the saved event passes the filter, or no filter is set', async () => {
    useCalendarStore.setState({ eventFilter: onlyA });
    mockApi.post.mockResolvedValue(evt('e-a', 'cal-1', { ownerId: 'a' }) as never);
    await useCalendarStore.getState().addEvent('cal-1', input);

    useCalendarStore.setState({ eventFilter: { match: 'all', conditions: [] } });
    mockApi.post.mockResolvedValue(evt('e-b', 'cal-1', { ownerId: 'b' }) as never);
    await useCalendarStore.getState().addEvent('cal-1', input);

    expect(filterNotice.offerFilterReveal).not.toHaveBeenCalled();
  });

  it('says so when an edit moves an event out of the filter', async () => {
    useCalendarStore.setState({
      eventFilter: onlyA,
      events: [evt('e1', 'cal-1', { ownerId: 'a' })],
    });
    mockApi.patch.mockResolvedValue(evt('e1', 'cal-1', { ownerId: 'b' }) as never);

    await useCalendarStore
      .getState()
      .updateEvent('cal-1', 'e1', { ownerId: 'b' }, undefined, '"e1"');

    expect(filterNotice.offerFilterReveal).toHaveBeenCalledTimes(1);
  });
});
