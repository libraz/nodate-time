import { cleanup, render, waitFor } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';

const uiState = {
  locale: 'ja',
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

const calendarState = { events: [], activeCalendarIds: [] };

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
