import { act, cleanup, render, screen, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
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
    participants: [],
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

/** The fixed-height container holding a cell's track slots and its "+N" row. */
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

// The "+N" used to be squeezed into a body sized for the tracks alone, which
// let flex shrink every slot under it and walked the chips off their bars.
describe('overflow row', () => {
  const crowded = [
    chip('c1', 'One', '2026-08-05', 1),
    chip('c2', 'Two', '2026-08-05', 2),
    chip('c3', 'Three', '2026-08-05', 3),
    chip('c4', 'Four', '2026-08-05', 4),
  ];

  it('is reserved, so the tracks are never asked to shrink', async () => {
    await renderRow({ events: crowded });

    const body = bodyOf('2026-08-05');
    const children = [...body.children] as HTMLElement[];
    expect(children).toHaveLength(4);

    let stacked = 0;
    for (const child of children) {
      expect(child.style.height).not.toBe('');
      stacked += px(child.style.height) + (px(child.style.marginBottom) || 0);
    }
    expect(stacked).toBe(px(body.style.height));
  });

  it('keeps the same height on a day with nothing to hide', async () => {
    await renderRow({ events: crowded });

    expect(bodyOf('2026-08-04').style.height).toBe(bodyOf('2026-08-05').style.height);
  });

  it('counts what the cell hides', async () => {
    await renderRow({ events: crowded });

    expect(within(cellOf('2026-08-05')).getByText('+1')).toBeInTheDocument();
  });

  it('is a badge the cell tap covers where the surface offers no target', async () => {
    await renderRow({ events: crowded });
    expect(
      within(cellOf('2026-08-05')).queryByRole('button', { name: 'calendar.moreEvents' }),
    ).toBeNull();

    cleanup();
    await renderRow({ events: crowded, onOverflowClick: vi.fn() });
    const more = within(cellOf('2026-08-05')).getByRole('button', { name: 'calendar.moreEvents' });
    expect(more).toHaveTextContent('+1');
    expect(more.className).toContain('pointer-events-auto');
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
