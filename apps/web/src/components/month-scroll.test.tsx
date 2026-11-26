import { act, cleanup, render, screen, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { WeekStart } from '@/lib/date-utils';
import { EMPTY_FILTER, type EventFilter } from '@/lib/event-filter';
import { makeEvent } from '@/test/make-event';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (e: unknown) => String(e),
}));

// jsdom has no scrolling, and the view scrolls itself to today on mount.
Element.prototype.scrollTo = () => {};

const ZONE = 'Asia/Tokyo';

const uiState = {
  locale: 'en',
  selectedDate: DateTime.fromISO('2026-08-01', { zone: ZONE }),
  // No holiday country: the rows then need no holiday data to render.
  holidaysCountry: null,
  timezone: ZONE,
  weekStart: 'sun' as WeekStart,
  scrollToTodaySignal: 0,
  openDayDetail: vi.fn(),
  openEventModal: vi.fn(),
  setSelectedDate: vi.fn(),
  setCurrentMonth: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

/** A bar running from the start of one day through the end of another (exclusive end). */
function spanEvent(id: string, title: string, from: string, throughExclusive: string) {
  return makeEvent({
    id,
    title,
    allDay: true,
    startAt: `${from}T00:00:00Z`,
    endAt: `${throughExclusive}T00:00:00Z`,
    startDate: from,
    endDate: throughExclusive,
    ownerId: 'u1',
    createdAt: '2026-08-01T00:00:00+09:00',
    updatedAt: '2026-08-01T00:00:00+09:00',
  });
}

function timedEvent(id: string, title: string, day: string, hour: number) {
  const { startDate: _s, endDate: _e, ...span } = spanEvent(id, title, day, day);
  return {
    ...span,
    allDay: false,
    startAt: `${day}T0${hour}:00:00+09:00`,
    endAt: `${day}T0${hour}:30:00+09:00`,
  };
}

const calendarState = {
  events: [] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
  eventFilter: EMPTY_FILTER as EventFilter,
  setEventFilter: vi.fn(),
  calendars: [{ id: 'cal-1', name: 'Family', color: '#47B2F7', role: 'owner' }],
  updateEvent: vi.fn().mockResolvedValue(undefined),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

const authState = { user: { id: 'u1', name: 'Taro', email: 'taro@example.com' } };

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { MonthScroll } from './month-scroll';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  calendarState.events = [];
});

/** The rows re-render once the (empty) holiday load settles. */
async function renderScroll(events: CalendarEvent[]) {
  calendarState.events = events;
  await act(async () => {
    render(<MonthScroll />);
  });
}

function cellOf(isoDate: string): HTMLElement {
  const cell = document.querySelector<HTMLElement>(`[data-day="${isoDate}"]`);
  if (!cell) throw new Error(`no cell for ${isoDate}`);
  return cell;
}

function px(value: string): number {
  return Number.parseFloat(value);
}

describe('MonthScroll week start', () => {
  afterEach(() => {
    uiState.weekStart = 'sun';
  });

  const headerLabels = () =>
    Array.from(document.querySelector('.grid.shrink-0.grid-cols-7')?.children ?? []).map(
      (el) => el.textContent,
    );
  const weekDays = () =>
    Array.from(document.querySelectorAll<HTMLElement>('[data-week]')).map(
      (el) => DateTime.fromISO(el.dataset.week ?? '').weekday,
    );

  it('opens every row on Sunday by default', async () => {
    await renderScroll([]);

    expect(headerLabels()).toEqual(['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']);
    expect(new Set(weekDays())).toEqual(new Set([7]));
  });

  it('opens every row on Monday and heads the columns to match', async () => {
    uiState.weekStart = 'mon';
    await renderScroll([]);

    expect(headerLabels()).toEqual(['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']);
    expect(new Set(weekDays())).toEqual(new Set([1]));
  });

  it('opens every row on Saturday', async () => {
    uiState.weekStart = 'sat';
    await renderScroll([]);

    expect(headerLabels()[0]).toBe('Sat');
    expect(new Set(weekDays())).toEqual(new Set([6]));
  });
});

/**
 * Reaching the edge of the mounted range re-anchors it: the anchor moves by a
 * year, the whole list is rebuilt, and the month the reader was on has to
 * survive that and be scrolled back to. Nothing covered this, and it is the
 * path that decides how far the range can be shortened -- a shorter range
 * means reaching the edge more often, which is only safe if arriving there
 * costs the reader nothing.
 */
describe('MonthScroll re-anchoring at the edge of the range', () => {
  /** The scrolling element, given the size jsdom will not give it. */
  function scroller(scrollTop: number): HTMLElement {
    const el = document.querySelector<HTMLElement>('.overflow-y-auto');
    if (!el) throw new Error('no scroll container');
    Object.defineProperty(el, 'scrollTop', { value: scrollTop, configurable: true });
    Object.defineProperty(el, 'clientHeight', { value: 800, configurable: true });
    Object.defineProperty(el, 'scrollHeight', { value: 100000, configurable: true });
    return el;
  }

  function monthsShown(): string[] {
    return Array.from(document.querySelectorAll<HTMLElement>('[data-month]')).map(
      (el) => el.dataset.month ?? '',
    );
  }

  /**
   * Puts a month at the top of the viewport. jsdom lays nothing out, so every
   * header reports the same position and the view would call the last one
   * current -- an artefact that would have this assert the opposite of what a
   * reader experiences. The headers above the chosen one sit at the top edge,
   * the rest below the fold, which is what the view reads to decide.
   */
  function readerIsOn(index: number): string {
    const headers = Array.from(document.querySelectorAll<HTMLElement>('[data-month]'));
    headers.forEach((header, i) => {
      Object.defineProperty(header, 'getBoundingClientRect', {
        value: () => ({ top: i <= index ? 0 : 1000 }) as DOMRect,
        configurable: true,
      });
    });
    return headers[index]?.dataset.month ?? '';
  }

  it('grows the range backwards and keeps the reader where they were', async () => {
    await renderScroll([]);
    const before = monthsShown();
    const earliest = before[0] as string;
    const el = scroller(0);
    const current = readerIsOn(Math.floor(before.length / 2));
    const scrollTo = vi.fn();
    Object.defineProperty(el, 'scrollTo', { value: scrollTo, configurable: true });

    await act(async () => {
      el.dispatchEvent(new Event('scroll'));
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    });

    const after = monthsShown();
    expect(after[0] as string).not.toBe(earliest);
    expect((after[0] as string) < earliest).toBe(true);
    // The month being read is still mounted, and was scrolled back to.
    expect(after).toContain(current);
    expect(scrollTo).toHaveBeenCalled();
  });

  it('grows the range forwards at the other edge', async () => {
    await renderScroll([]);
    const before = monthsShown();
    const latest = before[before.length - 1] as string;
    // Nothing left below: scrollTop within a screen of the bottom.
    const el = scroller(99500);
    Object.defineProperty(el, 'scrollTo', { value: vi.fn(), configurable: true });

    await act(async () => {
      el.dispatchEvent(new Event('scroll'));
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    });

    const after = monthsShown();
    expect((after[after.length - 1] as string) > latest).toBe(true);
  });
});

// Sunday 2026-08-02 through Saturday 2026-08-08 is one row on both surfaces.
// These are the desktop grid's own cases: the extraction is only worth having
// while a fix made once holds on the surface it was not written for.
describe('MonthScroll multi-day bars on deep tracks', () => {
  const shortBars = ['s1', 's2', 's3'].map((id) =>
    spanEvent(id, `Short ${id}`, '2026-08-02', '2026-08-06'),
  );

  it('keeps a week-long bar visible when shorter ones crowd its first days', async () => {
    await renderScroll([...shortBars, spanEvent('long', 'Long trip', '2026-08-02', '2026-08-09')]);

    expect(screen.getByTitle('Long trip').style.top).toBe('0px');
  });

  it('draws a bar on the fourth track on a day whose crowd has ended', async () => {
    await renderScroll([
      ...['a', 'b', 'c'].map((id) => spanEvent(id, `Crowd ${id}`, '2026-08-02', '2026-08-07')),
      spanEvent('late', 'Late trip', '2026-08-06', '2026-08-09'),
    ]);

    // Compact track pitch is 15px.
    expect(screen.getByTitle('Late trip').style.top).toBe('45px');
    expect(within(cellOf('2026-08-07')).queryByText(/^\+\d/)).toBeNull();
  });
});

function bodyOf(isoDate: string): HTMLElement {
  const body = cellOf(isoDate).querySelector<HTMLElement>('.flex.w-full.flex-col[style*="height"]');
  if (!body) throw new Error('no slot body');
  return body;
}

describe('MonthScroll cell body', () => {
  const crowded = [1, 2, 3, 4, 5].map((n) => timedEvent(`c${n}`, `Chip ${n}`, '2026-08-05', n));

  it('draws every chip of a crowded day and no "+N"', async () => {
    await renderScroll(crowded);

    const cell = cellOf('2026-08-05');
    for (const n of [1, 2, 3, 4, 5]) {
      expect(within(cell).getByRole('button', { name: new RegExp(`Chip ${n}`) })).toBeTruthy();
    }
    expect(within(cell).queryByText(/^\+\d/)).toBeNull();
  });

  it('stacks the slots to exactly the body height', async () => {
    await renderScroll(crowded);

    const body = bodyOf('2026-08-05');
    let stacked = 0;
    for (const child of [...body.children] as HTMLElement[]) {
      expect(child.style.height).not.toBe('');
      stacked += px(child.style.height) + (px(child.style.marginBottom) || 0);
    }
    expect(stacked).toBe(px(body.style.height));
  });

  it('sizes each week to its own events', async () => {
    await renderScroll(crowded);

    expect(px(bodyOf('2026-08-05').style.height)).toBe(5 * 15);
    expect(bodyOf('2026-08-04').style.height).toBe(bodyOf('2026-08-05').style.height);
    expect(px(bodyOf('2026-09-15').style.height)).toBe(3 * 15);
  });
});

/**
 * Rows above the reader grow as their events load. Without a correction every
 * row below moves down and the week being read slides off screen.
 */
describe('MonthScroll keeps the week being read in place', () => {
  const ROW_H = 100;
  let restoreLayout = () => {};

  afterEach(() => {
    restoreLayout();
    restoreLayout = () => {};
    uiState.scrollToTodaySignal = 0;
  });

  async function mount() {
    let view: ReturnType<typeof render> | undefined;
    await act(async () => {
      view = render(<MonthScroll />);
    });
    const container = document.querySelector<HTMLElement>('.overflow-y-auto');
    if (!view || !container) throw new Error('not mounted');
    Object.defineProperty(container, 'clientHeight', { value: 800, configurable: true });
    Object.defineProperty(container, 'scrollHeight', { value: 100000, configurable: true });
    Object.defineProperty(container, 'scrollTo', { value: vi.fn(), configurable: true });
    const rerender = async () => {
      await act(async () => {
        view?.rerender(<MonthScroll />);
      });
    };
    return { container, rerender };
  }

  function setScrollTop(container: HTMLElement, top: number) {
    Object.defineProperty(container, 'scrollTop', {
      value: top,
      writable: true,
      configurable: true,
    });
  }

  /** Lays the week rows out top to bottom at their given heights, scrolled by scrollTop. */
  function layoutRows(container: HTMLElement, heights: Map<string, number>) {
    const original = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = function (this: Element) {
      if (this === container) return { top: 0, bottom: 800, height: 800 } as DOMRect;
      const week = this instanceof HTMLElement ? this.dataset.week : undefined;
      if (!week) return original.call(this);
      let top = -container.scrollTop;
      for (const row of container.querySelectorAll<HTMLElement>('[data-week]')) {
        const h = heights.get(row.dataset.week ?? '') ?? ROW_H;
        if (row === this) return { top, bottom: top + h, height: h } as DOMRect;
        top += h;
      }
      return original.call(this);
    };
    restoreLayout = () => {
      Element.prototype.getBoundingClientRect = original;
    };
  }

  function weekKeys(container: HTMLElement): string[] {
    return [...container.querySelectorAll<HTMLElement>('[data-week]')].map(
      (row) => row.dataset.week ?? '',
    );
  }

  function topOf(container: HTMLElement, week: string): number {
    const row = container.querySelector<HTMLElement>(`[data-week="${week}"]`);
    if (!row) throw new Error(`no row ${week}`);
    return row.getBoundingClientRect().top;
  }

  async function scrollAndSettle(container: HTMLElement) {
    await act(async () => {
      container.dispatchEvent(new Event('scroll'));
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    });
  }

  it('shifts the scroll by what a row above the reader grew', async () => {
    calendarState.events = [];
    const { container, rerender } = await mount();
    const heights = new Map<string, number>();
    layoutRows(container, heights);
    // Row 30 starts at the top edge: it is the week being read.
    setScrollTop(container, 30 * ROW_H);
    await scrollAndSettle(container);

    const weeks = weekKeys(container);
    const read = weeks[30] as string;
    const above = weeks[10] as string;
    heights.set(above, ROW_H + 40);
    calendarState.events = [timedEvent('n', 'New', above, 9)];
    await rerender();

    expect(container.scrollTop).toBe(30 * ROW_H + 40);
    expect(topOf(container, read)).toBe(0);
  });

  it('leaves the scroll alone while a rebuilt range is being scrolled back', async () => {
    calendarState.events = [];
    const { container } = await mount();
    layoutRows(container, new Map());
    const firstBefore = weekKeys(container)[0] as string;
    // Near the top edge: the range grows backwards in the same commit the events land.
    setScrollTop(container, 600);
    calendarState.events = [timedEvent('n', 'New', '2026-08-05', 9)];
    await scrollAndSettle(container);

    // The range keeps its width, so the rebuild shows as an earlier first week.
    expect((weekKeys(container)[0] as string) < firstBefore).toBe(true);
    expect(container.scrollTop).toBe(600);
  });

  it('waits for a smooth scroll to end before correcting', async () => {
    calendarState.events = [];
    const { container, rerender } = await mount();
    const heights = new Map<string, number>();
    layoutRows(container, heights);
    setScrollTop(container, 30 * ROW_H);
    await scrollAndSettle(container);
    const weeks = weekKeys(container);

    // The Today button starts a smooth scroll.
    uiState.scrollToTodaySignal = 1;
    await rerender();
    heights.set(weeks[10] as string, ROW_H + 40);
    calendarState.events = [timedEvent('n', 'New', weeks[10] as string, 9)];
    await rerender();
    expect(container.scrollTop).toBe(30 * ROW_H);

    await act(async () => {
      container.dispatchEvent(new Event('scrollend'));
    });
    await scrollAndSettle(container);
    heights.set(weeks[11] as string, ROW_H + 25);
    calendarState.events = [
      timedEvent('n', 'New', weeks[10] as string, 9),
      timedEvent('m', 'More', weeks[11] as string, 9),
    ];
    await rerender();
    expect(container.scrollTop).toBe(30 * ROW_H + 25);
  });
});

describe('MonthScroll member filter', () => {
  afterEach(() => {
    calendarState.eventFilter = EMPTY_FILTER;
  });

  it('draws only the events the filter keeps', async () => {
    calendarState.eventFilter = {
      match: 'all',
      conditions: [{ id: 'c', field: 'owner', exclude: true, people: [{ id: 'u2', name: 'B' }] }],
    };
    await renderScroll([
      timedEvent('a', 'Kept', '2026-08-05', 1),
      { ...timedEvent('b', 'Hidden', '2026-08-05', 2), ownerId: 'u2' },
    ]);

    const cell = cellOf('2026-08-05');
    expect(within(cell).getByRole('button', { name: /Kept/ })).toBeTruthy();
    expect(within(cell).queryByRole('button', { name: /Hidden/ })).toBeNull();
  });
});
