import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  locale: 'en',
  timezone: 'Asia/Tokyo',
  showSearch: true,
  searchQuery: '',
  setSearchQuery: vi.fn(),
  toggleSearch: vi.fn(),
  setCurrentMonth: vi.fn(),
  setSelectedDate: vi.fn(),
  openEventModal: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState = {
  events: [] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { SearchPanel } from './search-panel';

function event(id: string, calendarId: string, title: string): CalendarEvent {
  return {
    id,
    calendarId,
    title,
    allDay: false,
    startAt: '2026-08-05T10:00:00+09:00',
    endAt: '2026-08-05T11:00:00+09:00',
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
  };
}

afterEach(() => {
  cleanup();
  uiState.showSearch = true;
  uiState.searchQuery = '';
  calendarState.events = [];
  calendarState.activeCalendarIds = ['cal-1'];
});

describe('SearchPanel', () => {
  it('shows nothing until it is opened', () => {
    uiState.showSearch = false;

    const { container } = render(<SearchPanel />);

    expect(container).toBeEmptyDOMElement();
  });

  it('opens onto a search box and lists what the query matches', () => {
    calendarState.events = [
      event('e1', 'cal-1', 'Morning Rehearsal'),
      event('e2', 'cal-1', 'Lunch'),
    ];
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);

    // The panel draws twice: a fullscreen overlay for narrow widths and a
    // dropdown for wide ones. Both are in the document; CSS picks one.
    expect(screen.getAllByPlaceholderText('search.placeholder')).toHaveLength(2);
    expect(screen.getAllByText('Morning Rehearsal')).toHaveLength(2);
    expect(screen.queryAllByText('Lunch')).toHaveLength(0);
  });

  it('leaves out events from a calendar the reader has switched off', () => {
    calendarState.events = [
      event('e1', 'cal-1', 'Rehearsal at home'),
      event('e2', 'cal-2', 'Rehearsal at work'),
    ];
    calendarState.activeCalendarIds = ['cal-1'];
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);

    expect(screen.getAllByText('Rehearsal at home')).toHaveLength(2);
    expect(screen.queryAllByText('Rehearsal at work')).toHaveLength(0);
  });
});
