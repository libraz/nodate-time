import type { Calendar, CalendarEvent, Memo } from '@/types/calendar';

/** A complete timed event as the listing returns it; override what a test cares about. */
export function makeEvent(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: 'a1b2c3d4-0000-0000-0000-000000000000',
    calendarId: 'cal-1',
    title: 'Meeting',
    kind: 'event',
    allDay: false,
    startAt: '2025-06-24T09:00:00+09:00',
    endAt: '2025-06-24T10:30:00+09:00',
    timezone: 'Asia/Tokyo',
    color: '#47B2F7',
    ownerId: 'user-1',
    ownerName: 'Owner',
    creatorId: 'user-1',
    creatorName: 'Owner',
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participantIds: [],
    attendees: [],
    recurrenceRule: null,
    isRecurrence: false,
    detailsHidden: false,
    taskOwned: false,
    etag: '"e1"',
    createdAt: '2025-06-01T00:00:00Z',
    updatedAt: '2025-06-01T00:00:00Z',
    ...overrides,
  };
}

/** A complete calendar as the listing returns it. */
export function makeCalendar(id: string, overrides: Partial<Calendar> = {}): Calendar {
  return {
    id,
    name: `Calendar ${id}`,
    color: '#47B2F7',
    createdAt: '',
    publicShared: false,
    memberCount: 1,
    role: 'owner',
    memberColor: '#47B2F7',
    coverUploadAllowed: false,
    etag: `"cal-${id}"`,
    ...overrides,
  };
}

/** A memo as the store holds it. */
export function makeMemo(id: string, calendarId: string, overrides: Partial<Memo> = {}) {
  return {
    id,
    calendarId,
    title: `Memo ${id}`,
    body: '',
    done: false,
    sortOrder: 0,
    createdAt: '',
    updatedAt: '',
    etag: `"memo-${id}"`,
    ...overrides,
  };
}
