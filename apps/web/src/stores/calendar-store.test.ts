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
    assignedTo: null,
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
        { id: 'cal-1', name: 'A', color: '#000', coverUrl: '', createdAt: '', publicShared: false },
        { id: 'cal-2', name: 'B', color: '#111', coverUrl: '', createdAt: '', publicShared: false },
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

  it('keeps calendar loading successful when a member request fails', async () => {
    mockApi.get.mockImplementation(async (url: string) => {
      if (url === '/calendars') return [cal('cal-1'), cal('cal-2')] as never;
      if (url === '/calendars/cal-1/members') return [] as never;
      if (url === '/calendars/cal-2/members') throw new Error('members failed');
      if (url === '/calendars/cal-1/labels') return [] as never;
      return [] as never;
    });

    await useCalendarStore.getState().fetchCalendars();

    expect(useCalendarStore.getState().calendars.map((c) => c.id)).toEqual(['cal-1', 'cal-2']);
    expect(mockToast.error).toHaveBeenCalledWith('members failed');
  });
});

describe('deleteCalendar', () => {
  it('removes the calendar and cascades its events, memos, and members', async () => {
    mockApi.delete.mockResolvedValue(undefined as never);
    useCalendarStore.setState({
      calendars: [
        { id: 'cal-1', name: 'A', color: '#000', coverUrl: '', createdAt: '', publicShared: false },
        { id: 'cal-2', name: 'B', color: '#111', coverUrl: '', createdAt: '', publicShared: false },
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
