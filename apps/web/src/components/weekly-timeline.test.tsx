import {
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { WeekStart } from '@/lib/date-utils';
import { EMPTY_FILTER, type EventFilter } from '@/lib/event-filter';
import { makeEvent } from '@/test/make-event';
import type { CalendarEvent } from '@/types/calendar';

// New York springs forward at 02:00 on 2026-03-08, so that day runs 23 elapsed
// hours against the 24 the grid draws. 05:00 is four elapsed hours after
// midnight and belongs on the fifth hour line; in a fixed-offset zone the two
// figures agree and neither the grid nor the click can be told apart.
const ZONE = 'America/New_York';
const DAY = '2026-03-08';
const HOUR_HEIGHT = 48;

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (e: unknown) => String(e),
}));

const openEventModal = vi.fn();

const uiState = {
  locale: 'en',
  selectedDate: DateTime.fromISO(DAY, { zone: ZONE }),
  currentMonth: DateTime.fromISO(DAY, { zone: ZONE }).startOf('month'),
  timezone: ZONE,
  weekStart: 'sun' as WeekStart,
  openEventModal,
  setSelectedDate: vi.fn(),
  setCurrentMonth: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const probe: CalendarEvent = makeEvent({
  id: 'e1',
  title: 'Morning run',
  timezone: ZONE,
  startAt: `${DAY}T05:00:00-04:00`,
  endAt: `${DAY}T06:00:00-04:00`,
  ownerId: 'u1',
  createdAt: '2026-03-01T00:00:00-05:00',
  updatedAt: '2026-03-01T00:00:00-05:00',
});

const calendarState = {
  events: [probe],
  activeCalendarIds: ['cal-1'],
  eventFilter: EMPTY_FILTER as EventFilter,
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

import { WeeklyTimeline } from './weekly-timeline';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function dayColumn(): HTMLElement {
  const col = document.querySelector<HTMLElement>(`[data-daycol="${DAY}"]`);
  if (!col) throw new Error(`no column for ${DAY}`);
  return col;
}

/** Top of the hour rule the gutter labels `hour` against. */
function hourRuleTop(hour: number): number {
  const rule = dayColumn().querySelectorAll<HTMLElement>('div.border-t')[hour];
  if (!rule) throw new Error(`no rule for hour ${hour}`);
  return Number.parseFloat(rule.style.top);
}

describe('WeeklyTimeline across a daylight-saving transition', () => {
  it('draws an event on the hour rule its wall-clock time is labelled with', () => {
    render(<WeeklyTimeline />);

    const block = screen.getByText(probe.title).closest('button');
    expect(block).not.toBeNull();
    // Positioning by elapsed time puts this an hour high, on the fourth rule.
    expect(Number.parseFloat((block as HTMLElement).style.top)).toBe(hourRuleTop(5));
    expect(Number.parseFloat((block as HTMLElement).style.height)).toBe(
      hourRuleTop(6) - hourRuleTop(5),
    );
  });

  it('creates an event at the wall-clock time of the slot that was clicked', () => {
    render(<WeeklyTimeline />);

    const slot = screen.getByLabelText(`${DAY} event.createEvent`);
    // jsdom's MouseEvent has no layout, so the offset the handler reads has to
    // be supplied; 5 * HOUR_HEIGHT is the top of the block asserted above.
    const click = createEvent.click(slot);
    Object.defineProperty(click, 'offsetY', { value: 5 * HOUR_HEIGHT });
    fireEvent(slot, click);

    expect(openEventModal).toHaveBeenCalledTimes(1);
    const start = openEventModal.mock.calls[0]?.[1] as DateTime;
    // Adding the minutes to midnight as elapsed time stores 06:00 instead.
    expect(start.toFormat('yyyy-MM-dd HH:mm')).toBe(`${DAY} 05:00`);
  });
});

describe('WeeklyTimeline member filter', () => {
  it('draws only the events that pass the filter', () => {
    const other = makeEvent({
      id: 'e2',
      title: 'Evening walk',
      timezone: ZONE,
      startAt: `${DAY}T18:00:00-04:00`,
      endAt: `${DAY}T19:00:00-04:00`,
      ownerId: 'b',
    });
    const mine = { ...probe, ownerId: 'a' };
    const saved = { events: calendarState.events, eventFilter: calendarState.eventFilter };
    calendarState.events = [mine, other];
    calendarState.eventFilter = {
      match: 'all',
      conditions: [{ id: 'c', field: 'owner', exclude: false, people: [{ id: 'a', name: 'A' }] }],
    };
    try {
      render(<WeeklyTimeline />);
      expect(screen.queryByText(probe.title)).not.toBeNull();
      expect(screen.queryByText(other.title)).toBeNull();
    } finally {
      Object.assign(calendarState, saved);
    }
  });
});

describe('WeeklyTimeline range create', () => {
  // jsdom has no pointer capture; the browser keeps the drag on the slot.
  const proto = HTMLElement.prototype as { setPointerCapture?: ((id: number) => void) | undefined };
  const original = proto.setPointerCapture;
  afterEach(() => {
    proto.setPointerCapture = original;
  });

  it('opens a new event over the wall-clock range dragged across empty time', () => {
    proto.setPointerCapture = () => {};
    render(<WeeklyTimeline />);
    const slot = screen.getByLabelText(`${DAY} event.createEvent`);

    // From inside 01:00-01:30 to inside 03:30-04:00, on the day New York skips 02:00.
    fireEvent.pointerDown(slot, { ...mouse(50, HOUR_HEIGHT + 5), pointerId: 1 });
    fireEvent.pointerMove(slot, mouse(50, 3 * HOUR_HEIGHT + 40));
    fireEvent.pointerUp(slot, mouse(50, 3 * HOUR_HEIGHT + 40));
    // The click that ends the gesture must not open a second, one-hour event.
    fireEvent.click(slot);

    expect(openEventModal).toHaveBeenCalledTimes(1);
    const [, start, end] = openEventModal.mock.calls[0] as [unknown, DateTime, DateTime];
    expect(start.toFormat('yyyy-MM-dd HH:mm')).toBe(`${DAY} 01:00`);
    expect(end.toFormat('yyyy-MM-dd HH:mm')).toBe(`${DAY} 04:00`);
  });

  it('leaves a press that stays inside one slot to the click', () => {
    proto.setPointerCapture = () => {};
    render(<WeeklyTimeline />);
    const slot = screen.getByLabelText(`${DAY} event.createEvent`);

    fireEvent.pointerDown(slot, { ...mouse(50, HOUR_HEIGHT + 5), pointerId: 1 });
    fireEvent.pointerMove(slot, mouse(50, HOUR_HEIGHT + 9));
    fireEvent.pointerUp(slot, mouse(50, HOUR_HEIGHT + 9));

    expect(openEventModal).not.toHaveBeenCalled();
  });
});

/**
 * An event whose end equals its start is a marker at a moment. The API returns
 * one and attributes it to the day it opens; this view discarded it before
 * layout, so none of that reached anybody.
 *
 * Surviving the filter is not enough to be seen: a block with no duration is a
 * block of no height, so these assert what is drawn rather than what exists.
 */
describe('WeeklyTimeline markers', () => {
  const marker: CalendarEvent = {
    ...probe,
    id: 'm1',
    title: 'Deploy',
    startAt: `${DAY}T05:00:00-04:00`,
    endAt: `${DAY}T05:00:00-04:00`,
  };

  afterEach(() => {
    calendarState.events = [probe];
  });

  it('draws a marker tall enough to see, at the time it marks', () => {
    calendarState.events = [marker];

    render(<WeeklyTimeline />);

    const block = screen.getByText('Deploy').closest('button') as HTMLElement;
    expect(block).not.toBeNull();
    expect(Number.parseFloat(block.style.top)).toBe(hourRuleTop(5));
    expect(Number.parseFloat(block.style.height)).toBeGreaterThanOrEqual(20);
  });

  it('gives a marker one time rather than a range from itself to itself', () => {
    calendarState.events = [marker];

    render(<WeeklyTimeline />);

    // Scoped to the block: the hour gutter carries a "5:00" label of its own.
    const block = within(screen.getByText('Deploy').closest('button') as HTMLElement);
    expect(block.getByText('5:00')).toBeInTheDocument();
    expect(block.queryByText('5:00 - 5:00')).toBeNull();
  });

  /**
   * The marker is drawn across the event that follows it, so the two are packed
   * side by side. Laid out by the nothing it occupies in time, it would sit at
   * full width on top of it.
   */
  it('shares the width with the event it is drawn across', () => {
    calendarState.events = [
      marker,
      {
        ...probe,
        id: 'e2',
        title: 'Standup',
        startAt: `${DAY}T05:10:00-04:00`,
        endAt: `${DAY}T06:00:00-04:00`,
      },
    ];

    render(<WeeklyTimeline />);

    const markerBlock = screen.getByText('Deploy').closest('button') as HTMLElement;
    const eventBlock = screen.getByText('Standup').closest('button') as HTMLElement;
    expect(markerBlock.style.width).toBe('calc(50% - 4px)');
    expect(eventBlock.style.width).toBe('calc(50% - 4px)');
  });
});

// A resize registers three window listeners and drops them on pointerup. Switch
// view mid-drag and that pointerup arrives at a component that is gone, so the
// listeners — and the event they close over — stayed on window for the life of
// the page.
describe('WeeklyTimeline resize listeners', () => {
  function resizeHandle(): HTMLElement {
    const handle = document.querySelector<HTMLElement>('.cursor-ns-resize');
    if (!handle) throw new Error('no resize handle');
    return handle;
  }

  it('ends a resize the pointer never finished when it unmounts', () => {
    const addListener = vi.spyOn(window, 'addEventListener');
    const { unmount } = render(<WeeklyTimeline />);

    fireEvent.pointerDown(resizeHandle(), { clientY: 240 });

    const signals = addListener.mock.calls
      .filter(([type]) => type === 'pointermove' || type === 'pointerup')
      .map(([, , options]) => (options as AddEventListenerOptions | undefined)?.signal);
    expect(signals.length).toBeGreaterThan(0);
    expect(signals.every((s) => s?.aborted === false)).toBe(true);

    unmount();

    expect(signals.every((s) => s?.aborted === true)).toBe(true);
  });

  it('leaves a resize that ended on its own alone', async () => {
    render(<WeeklyTimeline />);

    fireEvent.pointerDown(resizeHandle(), { clientY: 240 });
    fireEvent.pointerMove(window, { clientY: 300 });
    fireEvent.pointerUp(window, { clientY: 300 });

    // The gesture committed, so the event was asked to change.
    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
  });
});

describe('WeeklyTimeline week model', () => {
  afterEach(() => {
    uiState.selectedDate = DateTime.fromISO(DAY, { zone: ZONE });
    uiState.currentMonth = DateTime.fromISO(DAY, { zone: ZONE }).startOf('month');
    uiState.weekStart = 'sun';
    calendarState.events = [probe];
  });

  const headerLabels = () =>
    screen.getAllByText(/^(Sun|Mon|Tue|Wed|Thu|Fri|Sat)$/).map((el) => el.textContent);

  // 2026-03-08 is a Sunday: it closes a Monday week and opens a Sunday one.
  it('draws the week from the chosen first day', () => {
    uiState.weekStart = 'mon';
    render(<WeeklyTimeline />);

    expect(headerLabels()).toEqual(['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']);
  });

  it('draws a Sunday week by default', () => {
    render(<WeeklyTimeline />);

    expect(headerLabels()).toEqual(['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']);
  });

  // The events fetched follow currentMonth. Arriving at this week from a month
  // the reader paged to, nothing had moved it to the week drawn.
  it('re-anchors the fetched month to the week it draws', () => {
    uiState.currentMonth = DateTime.fromISO('2026-01-01', { zone: ZONE });

    render(<WeeklyTimeline />);

    const anchored = uiState.setCurrentMonth.mock.calls[0]?.[0] as DateTime | undefined;
    expect(anchored?.toISODate()).toBe('2026-03-01');
  });

  it('leaves the month alone when it already holds the week', () => {
    render(<WeeklyTimeline />);

    expect(uiState.setCurrentMonth).not.toHaveBeenCalled();
  });

  // An all-day event is a date in its own zone. Measured as instants in New
  // York, a Tokyo all-day 5 August runs 4 Aug 11:00 to 5 Aug 11:00 and was
  // drawn in two columns.
  it('puts an all-day event on its own date for a viewer in another zone', () => {
    uiState.selectedDate = DateTime.fromISO('2026-08-05', { zone: ZONE });
    uiState.currentMonth = DateTime.fromISO('2026-08-01', { zone: ZONE });
    calendarState.events = [
      {
        ...probe,
        id: 'a1',
        title: 'Birthday',
        allDay: true,
        startAt: '2026-08-05T00:00:00Z',
        endAt: '2026-08-06T00:00:00Z',
        startDate: '2026-08-05',
        endDate: '2026-08-06',
        timezone: 'Asia/Tokyo',
      },
    ];

    render(<WeeklyTimeline />);

    const chips = screen.getAllByText('Birthday');
    expect(chips).toHaveLength(1);
    // Wednesday is the fourth column of the all-day grid, one day wide.
    expect(chips[0]?.closest('button')?.style.gridColumn).toBe('4 / span 1');
  });

  it('draws a multi-day all-day event as one bar across its days', () => {
    uiState.selectedDate = DateTime.fromISO('2026-08-05', { zone: ZONE });
    uiState.currentMonth = DateTime.fromISO('2026-08-01', { zone: ZONE });
    calendarState.events = [
      {
        ...probe,
        id: 'a2',
        title: 'Trip',
        allDay: true,
        startAt: '2026-08-04T00:00:00Z',
        endAt: '2026-08-07T00:00:00Z',
        startDate: '2026-08-04',
        endDate: '2026-08-07',
        timezone: 'Asia/Tokyo',
      },
    ];

    render(<WeeklyTimeline />);

    const bars = screen.getAllByText('Trip');
    expect(bars).toHaveLength(1);
    expect(bars[0]?.closest('button')?.style.gridColumn).toBe('3 / span 3');
  });

  // A marker opening the day belongs to it. A pre-filter asking that the span
  // reach past midnight dropped it before the layout saw it.
  it('draws a marker at midnight on the day it opens', () => {
    calendarState.events = [
      {
        ...probe,
        id: 'm0',
        title: 'Midnight',
        startAt: `${DAY}T00:00:00-05:00`,
        endAt: `${DAY}T00:00:00-05:00`,
      },
    ];

    render(<WeeklyTimeline />);

    const blocks = screen.getAllByText('Midnight');
    expect(blocks).toHaveLength(1);
    const block = blocks[0]?.closest('button') as HTMLElement;
    expect(block.closest('[data-daycol]')?.getAttribute('data-daycol')).toBe(DAY);
    expect(Number.parseFloat(block.style.top)).toBe(0);
  });
});

/** Points `document.elementsFromPoint` at the day column whose index is x / 100. */
function stubColumnsAtX() {
  const original = document.elementsFromPoint;
  document.elementsFromPoint = (x: number) => {
    const cols = document.querySelectorAll<HTMLElement>('[data-daycol]');
    const col = cols[Math.floor(x / 100)];
    return col ? [col] : [];
  };
  return () => {
    document.elementsFromPoint = original;
  };
}

function mouse(x: number, y: number) {
  return { pointerType: 'mouse', button: 0, clientX: x, clientY: y };
}

function blockIn(dayKey: string, title: string): HTMLElement {
  const col = document.querySelector<HTMLElement>(`[data-daycol="${dayKey}"]`);
  const text =
    col && Array.from(col.querySelectorAll('p')).find((p) => p.textContent?.includes(title));
  if (!text) throw new Error(`no ${title} in ${dayKey}`);
  return text;
}

async function movedPayload(): Promise<{ startAt: string; endAt: string }> {
  await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
  return calendarState.updateEvent.mock.calls[0]?.[2] as { startAt: string; endAt: string };
}

describe('WeeklyTimeline move drop', () => {
  let restore = () => {};
  afterEach(() => {
    restore();
    calendarState.events = [probe];
  });

  // The grab offset was read from whatever element was pressed, so pressing
  // the title instead of the block's padding moved the event by the title's
  // own offset inside it.
  it('moves by the pointer travel when the title inside the block is pressed', async () => {
    render(<WeeklyTimeline />);
    restore = stubColumnsAtX();
    // 2026-03-08 is the Sunday, column 0.
    const title = blockIn(DAY, probe.title);

    fireEvent.pointerDown(title, mouse(50, 300));
    fireEvent.pointerMove(window, mouse(50, 320));
    fireEvent.pointerUp(window, mouse(50, 300 + HOUR_HEIGHT));

    const payload = await movedPayload();
    expect(payload.startAt).toBe('2026-03-08T06:00:00.000-04:00');
    expect(payload.endAt).toBe('2026-03-08T07:00:00.000-04:00');
  });

  // A 22:00-02:00 event draws its Tuesday segment from midnight. Placing the
  // start at the drop position moved it a whole day plus an hour.
  it('moves an overnight event by the travel of whichever segment was grabbed', async () => {
    calendarState.events = [
      {
        ...probe,
        id: 'n1',
        title: 'Night shift',
        startAt: '2026-03-09T22:00:00-04:00',
        endAt: '2026-03-10T02:00:00-04:00',
      },
    ];
    render(<WeeklyTimeline />);
    restore = stubColumnsAtX();
    const tuesday = blockIn('2026-03-10', 'Night shift');

    fireEvent.pointerDown(tuesday, mouse(250, 10));
    fireEvent.pointerMove(window, mouse(250, 30));
    fireEvent.pointerUp(window, mouse(250, 10 + HOUR_HEIGHT));

    const payload = await movedPayload();
    expect(payload.startAt).toBe('2026-03-09T23:00:00.000-04:00');
    expect(payload.endAt).toBe('2026-03-10T03:00:00.000-04:00');
  });
});

describe('WeeklyTimeline resize gesture', () => {
  afterEach(() => {
    calendarState.events = [probe];
  });

  it('does not touch the event when the handle is only tapped', () => {
    render(<WeeklyTimeline />);
    const handle = document.querySelector<HTMLElement>('.cursor-ns-resize');
    if (!handle) throw new Error('no resize handle');

    fireEvent.pointerDown(handle, mouse(50, 288));
    fireEvent.pointerMove(window, mouse(50, 290));
    fireEvent.pointerUp(window, mouse(50, 290));

    expect(calendarState.updateEvent).not.toHaveBeenCalled();
  });

  // The first day's segment of an overnight event ends at midnight on screen,
  // and resizing it there rewrote the real next-day end.
  it('offers the handle only on the segment holding the real end', () => {
    calendarState.events = [
      {
        ...probe,
        id: 'n1',
        title: 'Night shift',
        startAt: '2026-03-09T22:00:00-04:00',
        endAt: '2026-03-10T02:00:00-04:00',
      },
    ];
    render(<WeeklyTimeline />);

    const handles = document.querySelectorAll('.cursor-ns-resize');
    expect(handles).toHaveLength(1);
    expect(handles[0]?.closest('[data-daycol]')?.getAttribute('data-daycol')).toBe('2026-03-10');
  });
});

describe('WeeklyTimeline availability states', () => {
  it('puts the modifier on the stacked block and names the state', () => {
    calendarState.events = [{ ...probe, showAs: 'oof' }];

    render(<WeeklyTimeline />);

    const block = within(dayColumn()).getByRole('button', { name: /Morning run/ });
    expect(block).toHaveClass('event-block-stacked', 'evt-oof');
    expect(
      within(block).getByText('event.showAsOof', { selector: '.sr-only' }),
    ).toBeInTheDocument();
    expect(block.querySelector('.evt-marker')).not.toBeNull();
    calendarState.events = [probe];
  });

  it('leaves a busy block unmodified', () => {
    calendarState.events = [probe];

    render(<WeeklyTimeline />);

    const block = within(dayColumn()).getByRole('button', { name: /Morning run/ });
    expect(block.className).not.toContain('evt-');
    expect(block.querySelector('.sr-only')).toBeNull();
  });
});
