import { cleanup, createEvent, fireEvent, render, screen } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
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

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => String(e),
}));

const openEventModal = vi.fn();

const uiState = {
  locale: 'en',
  selectedDate: DateTime.fromISO(DAY, { zone: ZONE }),
  timezone: ZONE,
  openEventModal,
  setSelectedDate: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const probe: CalendarEvent = {
  id: 'e1',
  calendarId: 'cal-1',
  title: 'Morning run',
  allDay: false,
  startAt: `${DAY}T05:00:00-04:00`,
  endAt: `${DAY}T06:00:00-04:00`,
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
  createdAt: '2026-03-01T00:00:00-05:00',
  updatedAt: '2026-03-01T00:00:00-05:00',
};

const calendarState = {
  events: [probe],
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
