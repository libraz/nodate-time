import { act, cleanup, render, screen, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string, params?: Record<string, string | number>) =>
    params ? `${key}:${Object.values(params).join(',')}` : key,
}));

/** Stands in for the module cache the holiday chunk fills in after first paint. */
let loadedHoliday: string | null = null;

vi.mock('@/lib/holidays', () => ({
  getHoliday: (_country: string | null, isoDate: string) =>
    loadedHoliday && isoDate === '2026-08-02'
      ? { date: isoDate, name: loadedHoliday, type: 'public' }
      : null,
}));

import { MonthWeekRow, type MonthWeekRowProps, type WeekRowDensity } from './month-week-row';

const ZONE = 'Asia/Tokyo';
/** Sunday. */
const WEEK_START = DateTime.fromISO('2026-08-02T00:00:00', { zone: ZONE });

function makeEvent(overrides: Partial<CalendarEvent> & { startAt: string; endAt: string }) {
  return {
    id: 'e1',
    calendarId: 'cal-1',
    title: 'Event',
    allDay: false,
    color: '#47B2F7',
    ownerId: 'u1',
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participantIds: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '',
    updatedAt: '',
    ...overrides,
  } as CalendarEvent;
}

/** A bar running from the start of one day through the end of another (exclusive end). */
function spanEvent(id: string, title: string, from: string, throughExclusive: string) {
  return makeEvent({
    id,
    title,
    allDay: true,
    startAt: `${from}T00:00:00+09:00`,
    endAt: `${throughExclusive}T00:00:00+09:00`,
  });
}

function chip(id: string, title: string, day: string, hour: number) {
  return makeEvent({
    id,
    title,
    startAt: `${day}T${String(hour).padStart(2, '0')}:00:00+09:00`,
    endAt: `${day}T${String(hour).padStart(2, '0')}:30:00+09:00`,
  });
}

function props(overrides: Partial<MonthWeekRowProps> = {}): MonthWeekRowProps {
  return {
    weekStart: WEEK_START,
    events: [],
    zone: ZONE,
    holidaysCountry: null,
    selectedDate: WEEK_START,
    density: 'compact',
    onDayClick: vi.fn(),
    onEventClick: vi.fn(),
    onEventPointerDown: vi.fn(),
    canMoveEvent: () => true,
    ...overrides,
  };
}

/** The row re-renders once the (empty) holiday load settles. */
async function renderRow(overrides: Partial<MonthWeekRowProps> = {}) {
  await act(async () => {
    render(<MonthWeekRow {...props(overrides)} />);
  });
}

function cellOf(isoDate: string): HTMLElement {
  const cell = document.querySelector<HTMLElement>(`[data-day="${isoDate}"]`);
  if (!cell) throw new Error(`no cell for ${isoDate}`);
  return cell;
}

/** The fixed-height container holding a cell's track slots. */
function bodyOf(isoDate: string): HTMLElement {
  const body = cellOf(isoDate).querySelector<HTMLElement>('.flex.w-full.flex-col[style*="height"]');
  if (!body) throw new Error(`no slot body for ${isoDate}`);
  return body;
}

function px(value: string): number {
  return Number.parseFloat(value);
}

afterEach(cleanup);

// Chips are stacked in the cell while bars are absolutely positioned above it,
// so the two only meet while both are measured from the same track pitch.
describe.each<WeekRowDensity>(['compact', 'comfortable'])('%s track metrics', (density) => {
  const bars = [
    spanEvent('b0', 'Bar zero', '2026-08-02', '2026-08-06'),
    spanEvent('b1', 'Bar one', '2026-08-03', '2026-08-07'),
  ];

  it('gives bars the same pitch the chip slots use', async () => {
    // Saturday is clear of both bars, so its chip takes track 0.
    await renderRow({ density, events: [...bars, chip('c1', 'Chip', '2026-08-08', 9)] });

    const top0 = px(screen.getByTitle('Bar zero').style.top);
    const top1 = px(screen.getByTitle('Bar one').style.top);
    const slot = within(cellOf('2026-08-08')).getByRole('button', { name: /Chip/ });

    expect(top1 - top0).toBe(px(slot.style.height) + px(slot.style.marginBottom));
  });

  it('starts the bars where the cell stops drawing the date', async () => {
    await renderRow({ density, events: bars });

    const overlay = screen.getByTitle('Bar zero').parentElement as HTMLElement;
    const cell = cellOf('2026-08-02');
    const dateRow = cell.querySelector<HTMLElement>('[style*="height"]');

    expect(px(overlay.style.top)).toBe(px(cell.style.paddingTop) + px(dateRow?.style.height ?? ''));
  });
});

// A day draws every one of its events, so the row grows to the deepest track
// any of its days uses instead of folding the rest into a count.
describe.each<[WeekRowDensity, number]>([
  ['compact', 15],
  ['comfortable', 21],
])('%s track body', (density, slotH) => {
  const crowded = ['One', 'Two', 'Three', 'Four', 'Five'].map((title, i) =>
    chip(`c${i}`, title, '2026-08-05', i + 1),
  );

  it('draws every chip of a crowded day and no "+N"', async () => {
    await renderRow({ density, events: crowded });

    const cell = cellOf('2026-08-05');
    for (const title of ['One', 'Two', 'Three', 'Four', 'Five']) {
      expect(within(cell).getByRole('button', { name: new RegExp(title) })).toBeInTheDocument();
    }
    expect(within(cell).queryByText(/^\+/)).toBeNull();
  });

  it('sizes every day of the week to the deepest track', async () => {
    await renderRow({ density, events: crowded });

    const body = bodyOf('2026-08-05');
    expect(px(body.style.height)).toBe(5 * slotH);
    expect(bodyOf('2026-08-04').style.height).toBe(body.style.height);

    let stacked = 0;
    for (const child of [...body.children] as HTMLElement[]) {
      expect(child.style.height).not.toBe('');
      stacked += px(child.style.height) + (px(child.style.marginBottom) || 0);
    }
    expect(stacked).toBe(px(body.style.height));
  });

  it('keeps an empty week three tracks tall', async () => {
    await renderRow({ density });

    const body = bodyOf('2026-08-05');
    expect(px(body.style.height)).toBe(3 * slotH);
    expect(body.children).toHaveLength(3);
  });

  it('draws a bar on the fourth track', async () => {
    const bars = ['B0', 'B1', 'B2', 'B3'].map((title, i) =>
      spanEvent(`b${i}`, title, '2026-08-02', '2026-08-06'),
    );
    await renderRow({ density, events: bars });

    for (const title of ['B0', 'B1', 'B2', 'B3']) {
      expect(screen.getByTitle(title)).toBeInTheDocument();
    }
    expect(px(screen.getByTitle('B3').style.top)).toBe(3 * slotH);
    expect(px(bodyOf('2026-08-08').style.height)).toBe(4 * slotH);
  });
});

describe('day number', () => {
  it('is plain text where the surface offers no day detail', async () => {
    await renderRow();

    expect(
      within(cellOf('2026-08-05')).queryByRole('button', { name: /calendar\.openDayDetail/ }),
    ).toBeNull();
  });

  it('opens the day detail for its own day when the surface offers one', async () => {
    const onDateClick = vi.fn();
    const onDayClick = vi.fn();
    await renderRow({ onDateClick, onDayClick });

    const date = within(cellOf('2026-08-05')).getByRole('button', {
      name: 'calendar.openDayDetail:2026-08-05',
    });
    expect(date).toHaveTextContent('5');
    expect(date.className).toContain('pointer-events-auto');

    await act(async () => {
      date.click();
    });
    expect(onDateClick).toHaveBeenCalledTimes(1);
    expect(onDateClick.mock.calls[0]?.[0].toFormat('yyyy-MM-dd')).toBe('2026-08-05');
  });
});

// Columns come from the row's own day order, so a row that does not start on
// Sunday still lines its cells up with the bars and the drag ghost.
describe('columns', () => {
  const monday = DateTime.fromISO('2026-08-03T00:00:00', { zone: ZONE });

  it('reserves a bar track on the cell the bar is drawn over', async () => {
    await renderRow({
      weekStart: monday,
      selectedDate: monday,
      // Saturday and Sunday: the last two columns of a Monday row.
      events: [
        spanEvent('b0', 'Bar zero', '2026-08-08', '2026-08-10'),
        chip('c1', 'Chip', '2026-08-09', 9),
      ],
    });

    const body = bodyOf('2026-08-09');
    const first = body.children[0] as HTMLElement;
    expect(first.tagName).toBe('DIV');
    expect(within(body).getByRole('button', { name: /Chip/ })).toBe(body.children[1]);
  });

  it('puts the drag ghost in the landing day’s column', async () => {
    const evt = chip('c1', 'Dragged', '2026-08-03', 9);
    await renderRow({
      weekStart: monday,
      selectedDate: monday,
      // A bar starting on Monday marks column 0.
      events: [spanEvent('b0', 'Bar zero', '2026-08-03', '2026-08-05')],
      dragLanding: { event: evt, start: monday, end: monday },
    });

    const ghost = document.querySelector<HTMLElement>('.event-bar.z-20');
    expect(ghost).not.toBeNull();
    expect(ghost?.style.left).toBe(screen.getByTitle('Bar zero').style.left);
  });
});

// The mobile scroller keeps months of weeks mounted and a drag samples the
// pointer every frame, so a row that redraws on unrelated state cannot keep up.
describe('memoisation', () => {
  const canMoveEvent = vi.fn(() => true);
  const events = [spanEvent('b0', 'Bar zero', '2026-08-02', '2026-08-06')];
  const rowProps = props({ events, canMoveEvent });

  function Harness() {
    const [, setTick] = useState(0);
    return (
      <div>
        <button type="button" onClick={() => setTick((n) => n + 1)}>
          unrelated
        </button>
        <MonthWeekRow {...rowProps} />
      </div>
    );
  }

  it('does not redraw when state it does not read changes', async () => {
    await act(async () => {
      render(<Harness />);
    });
    canMoveEvent.mockClear();

    await act(async () => {
      screen.getByRole('button', { name: 'unrelated' }).click();
    });

    // canMoveEvent runs once per drawn bar, so a redraw would call it again.
    expect(canMoveEvent).not.toHaveBeenCalled();
  });

  // Holidays come from a module cache the row cannot subscribe to, so the
  // loader's revision is the only prop that reports the data arriving.
  it('redraws when the holiday load reports in', async () => {
    const withHolidays = props({ holidaysCountry: 'JP', showHolidayName: true });
    const { rerender } = render(<MonthWeekRow {...withHolidays} holidayRevision={0} />);
    expect(screen.queryByTitle('Test Day')).toBeNull();

    loadedHoliday = 'Test Day';
    await act(async () => {
      rerender(<MonthWeekRow {...withHolidays} holidayRevision={0} />);
    });
    expect(screen.queryByTitle('Test Day')).toBeNull();

    await act(async () => {
      rerender(<MonthWeekRow {...withHolidays} holidayRevision={1} />);
    });
    expect(screen.getByTitle('Test Day')).toBeInTheDocument();
    loadedHoliday = null;
  });

  it('redraws when its own events change', async () => {
    const { rerender } = render(<MonthWeekRow {...rowProps} />);
    canMoveEvent.mockClear();

    await act(async () => {
      rerender(
        <MonthWeekRow
          {...rowProps}
          events={[...events, spanEvent('b1', 'Bar one', '2026-08-03', '2026-08-07')]}
        />,
      );
    });

    expect(canMoveEvent).toHaveBeenCalled();
    expect(screen.getByTitle('Bar one')).toBeInTheDocument();
  });
});

describe('availability states', () => {
  it('puts the modifier on the chip and names the state', async () => {
    await renderRow({
      events: [{ ...chip('c1', 'Chip', '2026-08-08', 9), showAs: 'tentative' }],
    });

    const el = within(cellOf('2026-08-08')).getByRole('button', { name: /Chip/ });
    expect(el).toHaveClass('event-chip', 'evt-tentative');
    expect(within(el).getByText('event.showAsTentative')).toHaveClass('sr-only');
  });

  it('leaves a busy chip unmodified and unlabelled', async () => {
    await renderRow({ events: [chip('c1', 'Chip', '2026-08-08', 9)] });

    const el = within(cellOf('2026-08-08')).getByRole('button', { name: /Chip/ });
    expect(el.className).not.toContain('evt-');
    expect(within(el).queryByText('event.showAsTentative')).toBeNull();
  });

  it('shows the out-of-office marker only where the chip is wide enough', async () => {
    const events = [{ ...chip('c1', 'Chip', '2026-08-08', 9), showAs: 'oof' as const }];

    await renderRow({ density: 'comfortable', events });
    const wide = within(cellOf('2026-08-08')).getByRole('button', { name: /Chip/ });
    expect(wide).toHaveClass('evt-oof');
    expect(wide.querySelector('.evt-marker')).not.toBeNull();

    cleanup();
    await renderRow({ density: 'compact', events });
    const narrow = within(cellOf('2026-08-08')).getByRole('button', { name: /Chip/ });
    expect(narrow.querySelector('.evt-marker')).toBeNull();
  });

  it('paints a bar from custom properties and carries the modifier', async () => {
    await renderRow({
      events: [{ ...spanEvent('b0', 'Bar zero', '2026-08-02', '2026-08-06'), showAs: 'free' }],
    });

    const bar = screen.getByTitle('Bar zero');
    expect(bar).toHaveClass('event-bar', 'evt-free');
    expect(bar.style.getPropertyValue('--evt')).toBe('#47B2F7');
    expect(bar.style.backgroundColor).toBe('');
    expect(within(bar).getByText('event.showAsFree')).toHaveClass('sr-only');
  });
});
