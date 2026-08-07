import { act, cleanup, render, screen, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
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
  return {
    id,
    calendarId: 'cal-1',
    title,
    allDay: true,
    startAt: `${from}T00:00:00+09:00`,
    endAt: `${throughExclusive}T00:00:00+09:00`,
    color: '#47B2F7',
    ownerId: 'u1',
    location: '',
    memo: '',
    url: '',
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    notificationOffset: null,
    participants: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '2026-08-01T00:00:00+09:00',
    updatedAt: '2026-08-01T00:00:00+09:00',
  } as CalendarEvent;
}

function timedEvent(id: string, title: string, day: string, hour: number) {
  return {
    ...spanEvent(id, title, day, day),
    allDay: false,
    startAt: `${day}T0${hour}:00:00+09:00`,
    endAt: `${day}T0${hour}:30:00+09:00`,
  } as CalendarEvent;
}

const calendarState = {
  events: [] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
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

// Sunday 2026-08-02 through Saturday 2026-08-08 is one row on both surfaces.
// These are the desktop grid's own cases: the extraction is only worth having
// while a fix made once holds on the surface it was not written for.
describe('MonthScroll multi-day bars past the visible tracks', () => {
  const shortBars = ['s1', 's2', 's3'].map((id) =>
    spanEvent(id, `Short ${id}`, '2026-08-02', '2026-08-06'),
  );

  it('keeps a week-long bar visible when shorter ones crowd its first days', async () => {
    await renderScroll([...shortBars, spanEvent('long', 'Long trip', '2026-08-02', '2026-08-09')]);

    expect(screen.getByTitle('Long trip').style.top).toBe('0px');
  });

  it('offers a +N on a day whose only event sits past the visible tracks', async () => {
    await renderScroll([
      ...['a', 'b', 'c'].map((id) => spanEvent(id, `Crowd ${id}`, '2026-08-02', '2026-08-07')),
      spanEvent('late', 'Late trip', '2026-08-06', '2026-08-09'),
    ]);

    expect(screen.queryByTitle('Late trip')).toBeNull();
    expect(within(cellOf('2026-08-07')).getByText('+1')).toBeInTheDocument();
  });
});

describe('MonthScroll cell body', () => {
  it('reserves the +N row instead of squeezing the tracks under it', async () => {
    await renderScroll([1, 2, 3, 4].map((n) => timedEvent(`c${n}`, `Chip ${n}`, '2026-08-05', n)));

    const crowded = cellOf('2026-08-05');
    const body = crowded.querySelector<HTMLElement>('.flex.w-full.flex-col[style*="height"]');
    if (!body) throw new Error('no slot body');

    const children = [...body.children] as HTMLElement[];
    let stacked = 0;
    for (const child of children) {
      expect(child.style.height).not.toBe('');
      stacked += px(child.style.height) + (px(child.style.marginBottom) || 0);
    }
    expect(stacked).toBe(px(body.style.height));
  });

  it('draws every week at the same height whatever its events are', async () => {
    await renderScroll([1, 2, 3, 4].map((n) => timedEvent(`c${n}`, `Chip ${n}`, '2026-08-05', n)));

    const bodyHeight = (isoDate: string) =>
      cellOf(isoDate).querySelector<HTMLElement>('.flex.w-full.flex-col[style*="height"]')?.style
        .height;

    expect(bodyHeight('2026-08-04')).toBe(bodyHeight('2026-08-05'));
    expect(bodyHeight('2026-09-15')).toBe(bodyHeight('2026-08-05'));
  });
});
