import { cleanup, render, waitFor } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { WeekStart } from '@/lib/date-utils';
import { EMPTY_FILTER, type EventFilter } from '@/lib/event-filter';
import { makeEvent } from '@/test/make-event';
import type { CalendarEvent } from '@/types/calendar';

const uiState = {
  locale: 'ja',
  weekStart: 'sun' as WeekStart,
  // A year that is not the current one: the fixture stays valid, and the view
  // has to load holidays for the years it draws rather than for "now".
  currentMonth: DateTime.local(2025, 6, 1),
  timezone: 'Asia/Tokyo',
  holidaysCountry: 'JP',
  setCurrentMonth: vi.fn(),
  setCalendarView: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState = {
  events: [] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
  eventFilter: EMPTY_FILTER as EventFilter,
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { YearView } from './year-view';

afterEach(cleanup);

/** The inline colour a day cell paints itself, e.g. "var(--color-sunday)". */
function colorOf(cell: Element | undefined): string {
  const style = cell?.getAttribute('style') ?? '';
  return style.match(/(?:^|;)\s*color:\s*([^;]+)/)?.[1]?.trim() ?? '';
}

/** The 35 day cells of the nth month card, in grid order. */
function dayCells(container: HTMLElement, monthIndex: number): Element[] {
  const card = container.querySelectorAll('.year-card')[monthIndex];
  return Array.from(card?.lastElementChild?.children ?? []);
}

describe('YearView holidays', () => {
  it('marks the next year, which December already shows', async () => {
    const { container } = render(<YearView />);

    // December 2025 fills its last week with 2026-01-01..03.
    const december = () => dayCells(container, 11);
    expect(december()).toHaveLength(35);

    // New Year's Day 2026 falls on a Thursday, so nothing but the holiday can
    // colour it.
    await waitFor(() => expect(colorOf(december()[32])).toBe('var(--color-sunday)'));
  });

  it('leaves the bank days around it alone', async () => {
    const { container } = render(<YearView />);
    const december = () => dayCells(container, 11);

    await waitFor(() => expect(colorOf(december()[32])).toBe('var(--color-sunday)'));

    // 2025-12-31 and 2026-01-02 are working days in Japan: an observance and a
    // bank day, neither of them a day off.
    expect(colorOf(december()[31])).toBe('var(--color-text-primary)');
    expect(colorOf(december()[33])).toBe('var(--color-text-primary)');
  });

  it('marks a holiday inside the year on show', async () => {
    const { container } = render(<YearView />);

    // 2025-05-06, the substitute for 憲法記念日, is a Tuesday and the 10th
    // cell of May (May 1 is a Thursday, so the grid starts on Apr 27).
    const may = () => dayCells(container, 4);
    await waitFor(() => expect(colorOf(may()[9])).toBe('var(--color-sunday)'));
  });
});

describe('YearView week start', () => {
  afterEach(() => {
    uiState.weekStart = 'sun';
  });

  /** Header letters and colours of the nth month card. */
  function header(container: HTMLElement, monthIndex: number) {
    const row = container.querySelectorAll('.year-card')[monthIndex]?.children[1];
    return Array.from(row?.children ?? []).map((el) => ({
      text: el.textContent,
      color: colorOf(el),
    }));
  }

  it('opens the header on Monday and keeps Sunday red in the last column', () => {
    uiState.weekStart = 'mon';
    const { container } = render(<YearView />);

    const cols = header(container, 0);
    expect(cols.map((c) => c.text)).toEqual(['月', '火', '水', '木', '金', '土', '日']);
    expect(cols[5]?.color).toBe('var(--color-saturday)');
    expect(cols[6]?.color).toBe('var(--color-sunday)');
    expect(cols[0]?.color).toBe('');
  });

  it('opens the header on Saturday', () => {
    uiState.weekStart = 'sat';
    const { container } = render(<YearView />);

    expect(header(container, 0).map((c) => c.text)).toEqual([
      '土',
      '日',
      '月',
      '火',
      '水',
      '木',
      '金',
    ]);
  });

  it('starts the first row of a month on the chosen day', () => {
    uiState.weekStart = 'mon';
    const { container } = render(<YearView />);

    // June 2025 starts on a Sunday, so a Monday grid opens on 2025-05-26.
    const cells = dayCells(container, 5);
    expect(cells[0]?.textContent).toBe('26');
    expect(cells[6]?.textContent).toBe('1');
  });
});

describe('YearView member filter', () => {
  it('marks only the days of events that pass the filter', () => {
    const at = (id: string, ownerId: string, day: string) =>
      makeEvent({
        id,
        ownerId,
        timezone: 'Asia/Tokyo',
        startAt: `2025-06-${day}T10:00:00+09:00`,
        endAt: `2025-06-${day}T11:00:00+09:00`,
      });
    calendarState.events = [at('mine', 'a', '10'), at('theirs', 'b', '12')];
    calendarState.eventFilter = {
      match: 'all',
      conditions: [{ id: 'c', field: 'owner', exclude: false, people: [{ id: 'a', name: 'A' }] }],
    };
    try {
      const { container } = render(<YearView />);
      // June 2025 starts on a Sunday, so the 10th and 12th are cells 9 and 11.
      const june = dayCells(container, 5);
      expect(june[9]?.querySelector('[aria-hidden].absolute.flex')).not.toBeNull();
      expect(june[11]?.querySelector('[aria-hidden].absolute.flex')).toBeNull();
    } finally {
      calendarState.events = [];
      calendarState.eventFilter = EMPTY_FILTER;
    }
  });
});
