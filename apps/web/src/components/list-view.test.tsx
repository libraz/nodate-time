import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EMPTY_FILTER, type EventFilter } from '@/lib/event-filter';
import { makeEvent } from '@/test/make-event';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  locale: 'ja',
  timezone: 'Asia/Tokyo',
  holidaysCountry: 'JP',
  openEventModal: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const newYear = makeEvent({
  id: 'e1',
  title: 'Shrine visit',
  startAt: '2026-01-01T01:00:00+09:00',
  endAt: '2026-01-01T03:00:00+09:00',
  ownerId: 'u1',
  createdAt: '',
  updatedAt: '',
});

const calendarState = {
  events: [newYear] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
  eventFilter: EMPTY_FILTER as EventFilter,
  setEventFilter: vi.fn(),
  calendars: [{ id: 'cal-1' }],
  eventsLoading: false,
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { ListView } from './list-view';

afterEach(cleanup);

describe('ListView holidays', () => {
  // The holiday data is an optional chunk that lands after the first paint, and
  // nothing on the row reports its arrival: the view has to redraw itself once
  // the load settles or the day is listed as an ordinary Thursday forever.
  it('names the holiday once its data has loaded', async () => {
    render(<ListView />);

    expect(screen.getByText('Shrine visit')).toBeInTheDocument();

    await waitFor(() => expect(screen.getByText('元日')).toBeInTheDocument());
  });
});

describe('ListView memo excerpt', () => {
  afterEach(() => {
    calendarState.events = [newYear];
  });

  it('shows the memo clamped to two lines and hidden from the accessible name', () => {
    calendarState.events = [{ ...newYear, memo: 'No shellfish\nBring the score' }];

    render(<ListView />);

    const memo = screen.getByText(/No shellfish/);
    expect(memo).toHaveClass('line-clamp-2', 'whitespace-pre-line', 'break-words');
    expect(memo).toHaveAttribute('aria-hidden', 'true');
    expect(screen.getByRole('button', { name: /Shrine visit/ })).toHaveClass('items-start');
  });

  it.each(['', '  \n\t '])('has no memo paragraph for %j', (memo) => {
    calendarState.events = [{ ...newYear, memo }];

    const { container } = render(<ListView />);

    expect(screen.getByText('Shrine visit')).toBeInTheDocument();
    expect(container.querySelector('.line-clamp-2')).toBeNull();
  });
});

describe('ListView availability states', () => {
  it('marks the row and states showAs and flexibility as secondary text', () => {
    calendarState.events = [
      { ...newYear, showAs: 'free', flexibility: 'negotiable' } as CalendarEvent,
    ];

    render(<ListView />);

    const row = screen.getByRole('button', { name: /Shrine visit/ });
    expect(row).toHaveClass('event-block', 'evt-free');
    expect(
      within(row).getByText('event.showAsFree · event.flexibilityShort.negotiable'),
    ).toBeInTheDocument();
    calendarState.events = [newYear];
  });

  it('adds nothing for a busy, fixed event', () => {
    render(<ListView />);

    const row = screen.getByRole('button', { name: /Shrine visit/ });
    expect(row.className).not.toContain('evt-');
    expect(within(row).queryByText(/event\./)).toBeNull();
  });
});

describe('ListView member filter', () => {
  const ownedByB: EventFilter = {
    match: 'all',
    conditions: [{ id: 'c', field: 'owner', exclude: false, people: [{ id: 'u2', name: 'B' }] }],
  };

  afterEach(() => {
    calendarState.eventFilter = EMPTY_FILTER;
    calendarState.events = [newYear];
  });

  it('lists only the events the filter keeps', () => {
    calendarState.eventFilter = ownedByB;
    calendarState.events = [newYear, { ...newYear, id: 'e2', title: 'Rehearsal', ownerId: 'u2' }];

    render(<ListView />);

    expect(screen.getByText('Rehearsal')).toBeInTheDocument();
    expect(screen.queryByText('Shrine visit')).toBeNull();
  });

  // An empty list under a filter reads as missing data unless it names the cause.
  it('says the filter hid everything and offers to clear it', () => {
    calendarState.eventFilter = ownedByB;

    render(<ListView />);

    expect(screen.getByText('filter.noMatch')).toBeInTheDocument();
    screen.getByRole('button', { name: 'filter.clear' }).click();
    expect(calendarState.setEventFilter).toHaveBeenCalledWith(EMPTY_FILTER);
  });
});
