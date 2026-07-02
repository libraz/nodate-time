import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { filterEventsForSearch } from './search';

function event(id: string, overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id,
    calendarId: 'cal-1',
    title: `Event ${id}`,
    allDay: false,
    startAt: '2026-07-02T10:00:00+09:00',
    endAt: '2026-07-02T11:00:00+09:00',
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

describe('filterEventsForSearch', () => {
  it('matches title, location, and memo case-insensitively', () => {
    const events = [
      event('title', { title: 'Morning Rehearsal' }),
      event('location', { location: 'Studio Alpha' }),
      event('memo', { memo: 'bring script draft' }),
      event('miss', { title: 'Lunch' }),
    ];

    expect(filterEventsForSearch(events, 'rehearsal').map((e) => e.id)).toEqual(['title']);
    expect(filterEventsForSearch(events, 'STUDIO').map((e) => e.id)).toEqual(['location']);
    expect(filterEventsForSearch(events, 'script').map((e) => e.id)).toEqual(['memo']);
  });

  it('trims queries and returns no results for blank input', () => {
    const events = [event('one', { title: 'Meeting' })];

    expect(filterEventsForSearch(events, '  meet  ').map((e) => e.id)).toEqual(['one']);
    expect(filterEventsForSearch(events, '   ')).toEqual([]);
  });
});
