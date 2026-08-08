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
