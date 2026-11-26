import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EMPTY_FILTER, type EventFilter } from '@/lib/event-filter';
import { makeEvent } from '@/test/make-event';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (e: unknown) => String(e),
}));

const openDayDetail = vi.fn();
const openEventModal = vi.fn();

const uiState = {
  locale: 'en',
  currentMonth: DateTime.fromISO('2026-08-01', { zone: 'Asia/Tokyo' }),
  selectedDate: DateTime.fromISO('2026-08-01', { zone: 'Asia/Tokyo' }),
  calendarView: 'month' as const,
  // No holiday country: the grid then needs no holiday data to render.
  holidaysCountry: null,
  timezone: 'Asia/Tokyo',
  openEventModal,
  openDayDetail,
  setSelectedDate: vi.fn(),
  setCalendarView: vi.fn(),
  navigateMonth: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

function event(id: string, hour: number): CalendarEvent {
  return makeEvent({
    id,
    title: `Event ${id}`,
    startAt: `2026-08-05T0${hour}:00:00+09:00`,
    endAt: `2026-08-05T0${hour}:30:00+09:00`,
    ownerId: 'u1',
    createdAt: '2026-08-01T00:00:00+09:00',
    updatedAt: '2026-08-01T00:00:00+09:00',
  });
}

/** A bar running from the start of one day through the end of another (exclusive end). */
function spanEvent(
  id: string,
  title: string,
  from: string,
  throughExclusive: string,
): CalendarEvent {
  return {
    ...event(id, 1),
    title,
    allDay: true,
    startAt: `${from}T00:00:00Z`,
    endAt: `${throughExclusive}T00:00:00Z`,
    startDate: from,
    endDate: throughExclusive,
  };
}

// Five events on one day: more than an empty week's three tracks.
const defaultEvents = [event('a', 1), event('b', 2), event('c', 3), event('d', 4), event('e', 5)];

const calendarState = {
  events: defaultEvents as CalendarEvent[],
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

import { CalendarGrid } from './calendar-grid';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  calendarState.events = defaultEvents;
});

/** The grid re-renders once the (empty) holiday load settles. */
async function renderGrid() {
  await act(async () => {
    render(<CalendarGrid />);
  });
}

function cellOf(isoDate: string): HTMLElement {
  const cell = document.querySelector<HTMLElement>(`[data-day="${isoDate}"]`);
  if (!cell) throw new Error(`no cell for ${isoDate}`);
  return cell;
}

describe('CalendarGrid day number', () => {
  it('opens the day detail on its own day', async () => {
    const user = userEvent.setup();
    await renderGrid();

    const date = within(cellOf('2026-08-05')).getByRole('button', {
      name: 'calendar.openDayDetail',
    });
    await user.click(date);

    expect(openDayDetail).toHaveBeenCalledTimes(1);
    const opened = openDayDetail.mock.calls[0]?.[0] as DateTime;
    expect(opened.toFormat('yyyy-MM-dd')).toBe('2026-08-05');
  });

  // The number sits in a container that hands its clicks to the day button
  // behind it, so a handler alone is not enough -- it has to take pointer
  // events back. The stylesheet is not loaded here, so the class is the
  // evidence available.
  it('takes pointer events back from its container', async () => {
    await renderGrid();

    const date = within(cellOf('2026-08-05')).getByRole('button', {
      name: 'calendar.openDayDetail',
    });
    expect(date.className).toContain('pointer-events-auto');
    expect(date.closest('.pointer-events-none')).not.toBeNull();
  });
});

describe('CalendarGrid every event', () => {
  it('draws all five events of a crowded day and no "+N"', async () => {
    await renderGrid();

    const cell = cellOf('2026-08-05');
    for (const id of ['a', 'b', 'c', 'd', 'e']) {
      expect(within(cell).getByRole('button', { name: new RegExp(`Event ${id}`) })).toBeTruthy();
    }
    expect(screen.queryByText(/^\+\d/)).toBeNull();
  });
});

/** The scrolling container that holds the week rows. */
function rowsOf(): HTMLElement {
  const rows = document.querySelector<HTMLElement>('[data-dir]');
  if (!rows) throw new Error('no rows container');
  return rows;
}

function mockScroll(
  el: HTMLElement,
  m: { scrollHeight: number; clientHeight: number; scrollTop: number },
) {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, value: m.scrollHeight });
  Object.defineProperty(el, 'clientHeight', { configurable: true, value: m.clientHeight });
  Object.defineProperty(el, 'scrollTop', {
    configurable: true,
    writable: true,
    value: m.scrollTop,
  });
}

// A row grows with its events, so the month can be taller than the grid.
describe('CalendarGrid rows layout', () => {
  it('lets a row grow past its share and scrolls the rows, not the weekday header', async () => {
    await renderGrid();

    const rows = rowsOf();
    expect(rows.style.gridTemplateRows).toContain('minmax(min-content, 1fr)');
    expect(rows.className).toContain('overflow-y-auto');
    expect(rows.textContent).not.toContain('Sun');
    expect(rows.parentElement?.textContent).toContain('Sun');
  });
});

describe('CalendarGrid wheel', () => {
  it('turns the month when the rows fit', async () => {
    await renderGrid();
    mockScroll(rowsOf(), { scrollHeight: 600, clientHeight: 600, scrollTop: 0 });

    fireEvent.wheel(rowsOf(), { deltaY: 100 });
    expect(uiState.navigateMonth).toHaveBeenCalledWith(1);
  });

  it('scrolls an overflowing month instead of turning it', async () => {
    await renderGrid();
    mockScroll(rowsOf(), { scrollHeight: 900, clientHeight: 600, scrollTop: 100 });

    fireEvent.wheel(rowsOf(), { deltaY: 100 });
    fireEvent.wheel(rowsOf(), { deltaY: -100 });
    expect(uiState.navigateMonth).not.toHaveBeenCalled();
  });

  it('turns to the next month at the bottom edge', async () => {
    await renderGrid();
    mockScroll(rowsOf(), { scrollHeight: 900, clientHeight: 600, scrollTop: 300 });

    fireEvent.wheel(rowsOf(), { deltaY: 100 });
    expect(uiState.navigateMonth).toHaveBeenCalledWith(1);
  });

  it('turns to the previous month at the top edge', async () => {
    await renderGrid();
    mockScroll(rowsOf(), { scrollHeight: 900, clientHeight: 600, scrollTop: 0 });

    fireEvent.wheel(rowsOf(), { deltaY: -100 });
    expect(uiState.navigateMonth).toHaveBeenCalledWith(-1);
  });

  it('does not turn at the top edge when wheeling down', async () => {
    await renderGrid();
    mockScroll(rowsOf(), { scrollHeight: 900, clientHeight: 600, scrollTop: 0 });

    fireEvent.wheel(rowsOf(), { deltaY: 100 });
    expect(uiState.navigateMonth).not.toHaveBeenCalled();
  });
});

describe('CalendarGrid vertical swipe', () => {
  function swipeUp(target: HTMLElement) {
    fireEvent.touchStart(target, { touches: [{ clientX: 100, clientY: 400 }] });
    fireEvent.touchEnd(target, { changedTouches: [{ clientX: 100, clientY: 200 }] });
  }

  it('switches to the week when the rows fit', async () => {
    await renderGrid();
    mockScroll(rowsOf(), { scrollHeight: 600, clientHeight: 600, scrollTop: 0 });

    swipeUp(cellOf('2026-08-05'));
    expect(uiState.setCalendarView).toHaveBeenCalledWith('week');
  });

  it('leaves the swipe to scroll an overflowing month', async () => {
    await renderGrid();
    mockScroll(rowsOf(), { scrollHeight: 900, clientHeight: 600, scrollTop: 100 });

    swipeUp(cellOf('2026-08-05'));
    expect(uiState.setCalendarView).not.toHaveBeenCalled();
  });
});

// The desktop grid only ever renders above the one breakpoint this app has, so
// a `max-sm:` class here is a mobile rule nothing can apply -- and the two
// month surfaces drifting apart is exactly how they got written.
describe('CalendarGrid responsive rules', () => {
  it('carries no styles for a width it never renders at', async () => {
    await renderGrid();

    const styled = [...document.querySelectorAll<HTMLElement>('[class]')].filter((el) =>
      el.className.includes('max-sm:'),
    );
    expect(styled.map((el) => el.className)).toEqual([]);
  });
});

// Chips are stacked inside the day cell while multi-day bars are drawn in an
// overlay above it, and they only line up while both are measured from the same
// track pitch.
describe('CalendarGrid track metrics', () => {
  it('gives the bars the pitch the chip slots use', async () => {
    calendarState.events = [
      spanEvent('b0', 'Bar zero', '2026-08-02', '2026-08-06'),
      spanEvent('b1', 'Bar one', '2026-08-03', '2026-08-07'),
      event('chip', 8),
    ];
    await renderGrid();

    const top0 = Number.parseFloat(screen.getByTitle('Bar zero').style.top);
    const top1 = Number.parseFloat(screen.getByTitle('Bar one').style.top);
    const chip = screen.getByRole('button', { name: /Event chip/ });
    const slotPitch =
      Number.parseFloat(chip.style.height) + Number.parseFloat(chip.style.marginBottom);

    expect(top1 - top0).toBe(slotPitch);
  });
});

// Tracks are held for a bar's whole span, so a bar can end up on a high track
// because of crowding on days it shares with nothing else.
describe('CalendarGrid multi-day bars on deep tracks', () => {
  // Sunday 2026-08-02 through Saturday 2026-08-08 is one grid row.
  const shortBars = ['s1', 's2', 's3'].map((id) =>
    spanEvent(id, `Short ${id}`, '2026-08-02', '2026-08-06'),
  );

  it('keeps a week-long bar visible when shorter ones crowd its first days', async () => {
    calendarState.events = [
      ...shortBars,
      spanEvent('long', 'Long trip', '2026-08-02', '2026-08-09'),
    ];
    await renderGrid();

    const bar = screen.getByTitle('Long trip');
    expect(bar.style.top).toBe('0px');
  });

  it('draws a bar on the fourth track on a day whose crowd has ended', async () => {
    // Three bars run Sunday to Thursday; a fourth starts on that Thursday, so it
    // takes the fourth track -- and Friday holds nothing else.
    calendarState.events = [
      ...['a', 'b', 'c'].map((id) => spanEvent(id, `Crowd ${id}`, '2026-08-02', '2026-08-07')),
      spanEvent('late', 'Late trip', '2026-08-06', '2026-08-09'),
    ];
    await renderGrid();

    // Comfortable track pitch is 21px.
    expect(screen.getByTitle('Late trip').style.top).toBe('63px');
    expect(screen.queryByText(/^\+\d/)).toBeNull();
  });
});

/** Resolves points to day cells by x: the key at index x / 100 of `keys`. */
function stubCellsAtX(keys: string[]) {
  const original = document.elementsFromPoint;
  document.elementsFromPoint = (x: number) => {
    const cell = document.querySelector(`[data-day="${keys[Math.floor(x / 100)]}"]`);
    return cell ? [cell] : [];
  };
  return () => {
    document.elementsFromPoint = original;
  };
}

function press(type: string, x: number, y = 10) {
  return { pointerType: type, button: 0, clientX: x, clientY: y };
}

// A one-day Tokyo all-day event read from New York, dragged four cells across
// the 2026-03-08 DST change there.
const birthday: CalendarEvent = {
  ...event('bd', 1),
  title: 'Birthday',
  allDay: true,
  startAt: '2026-03-06T00:00:00Z',
  endAt: '2026-03-07T00:00:00Z',
  startDate: '2026-03-06',
  endDate: '2026-03-07',
};

describe('CalendarGrid drag to move', () => {
  let restore = () => {};
  afterEach(() => {
    restore();
    vi.useRealTimers();
    uiState.timezone = 'Asia/Tokyo';
    uiState.currentMonth = DateTime.fromISO('2026-08-01', { zone: 'Asia/Tokyo' });
  });

  it('moves an all-day event by whole dates across a DST change', async () => {
    uiState.timezone = 'America/New_York';
    uiState.currentMonth = DateTime.fromISO('2026-03-01', { zone: 'America/New_York' });
    calendarState.events = [birthday];
    await renderGrid();
    restore = stubCellsAtX(['2026-03-06', '2026-03-10']);

    const chip = screen.getByRole('button', { name: /Birthday/ });
    fireEvent.pointerDown(chip, press('mouse', 50));
    fireEvent.pointerMove(window, press('mouse', 150));
    fireEvent.pointerUp(window, press('mouse', 150));

    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    // Dates carry no zone: nothing the viewer's DST change does can move them.
    expect(calendarState.updateEvent.mock.calls[0]?.[2]).toEqual({
      startDate: '2026-03-10',
      endDate: '2026-03-11',
    });
  });

  // The touchend that ends a long-press drag carries the drag's whole travel,
  // and the grid read it as a swipe.
  it('does not page the month at the end of a touch drag', async () => {
    calendarState.events = [event('t', 1)];
    await renderGrid();
    restore = stubCellsAtX(['2026-08-05', '2026-08-06', '2026-08-07']);
    vi.useFakeTimers();

    const chip = screen.getByRole('button', { name: /Event t/ });
    fireEvent.touchStart(chip, { touches: [{ clientX: 50, clientY: 10 }] });
    fireEvent.pointerDown(chip, press('touch', 50));
    act(() => {
      vi.advanceTimersByTime(400);
    });
    fireEvent.pointerMove(window, press('touch', 250));
    fireEvent.pointerUp(window, press('touch', 250));
    fireEvent.touchEnd(chip, { changedTouches: [{ clientX: 250, clientY: 10 }] });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(calendarState.updateEvent).toHaveBeenCalledTimes(1);
    expect(uiState.navigateMonth).not.toHaveBeenCalled();
  });

  it('still pages the month on a swipe after a drag', async () => {
    calendarState.events = [event('t', 1)];
    await renderGrid();
    restore = stubCellsAtX(['2026-08-05', '2026-08-06', '2026-08-07']);

    const chip = screen.getByRole('button', { name: /Event t/ });
    fireEvent.pointerDown(chip, press('mouse', 50));
    fireEvent.pointerMove(window, press('mouse', 150));
    fireEvent.pointerUp(window, press('mouse', 150));

    const grid = document.querySelector('.select-none') as HTMLElement;
    fireEvent.touchStart(grid, { touches: [{ clientX: 250, clientY: 10 }] });
    fireEvent.touchEnd(grid, { changedTouches: [{ clientX: 50, clientY: 10 }] });

    expect(uiState.navigateMonth).toHaveBeenCalledWith(1);
  });
});

describe('CalendarGrid member filter', () => {
  afterEach(() => {
    calendarState.eventFilter = EMPTY_FILTER;
  });

  it('draws only the events the filter keeps', async () => {
    calendarState.eventFilter = {
      match: 'all',
      conditions: [{ id: 'c', field: 'owner', exclude: true, people: [{ id: 'u2', name: 'B' }] }],
    };
    calendarState.events = [event('a', 1), { ...event('b', 2), ownerId: 'u2' }];

    await renderGrid();

    const cell = cellOf('2026-08-05');
    expect(within(cell).getByRole('button', { name: /Event a/ })).toBeTruthy();
    expect(within(cell).queryByRole('button', { name: /Event b/ })).toBeNull();
  });
});
