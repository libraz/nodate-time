import { cleanup, render, screen } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  locale: 'en',
  selectedDate: DateTime.fromISO('2026-08-05', { zone: 'Asia/Tokyo' }),
  showDayDetail: true,
  timezone: 'Asia/Tokyo',
  closeDayDetail: vi.fn(),
  openEventModal: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState = {
  events: [],
  activeCalendarIds: ['cal-1'],
  calendars: [{ id: 'cal-1', name: 'Family', color: '#47B2F7', role: 'owner' }],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { DayDetail } from './day-detail';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('DayDetail', () => {
  // The desktop month view has no other way to reach an overflowed day, so a
  // sheet that only exists below the breakpoint leaves those events unread.
  // The stylesheet is not loaded here, so the class is the evidence available.
  it('is not hidden above the breakpoint', () => {
    render(<DayDetail />);

    const heading = screen.getByRole('heading');
    const hiddenAncestors: string[] = [];
    for (let node: HTMLElement | null = heading; node; node = node.parentElement) {
      if (node.classList.contains('sm:hidden')) hiddenAncestors.push(node.className);
    }

    expect(hiddenAncestors).toEqual([]);
  });

  // Centring the sheet on desktop puts a full-screen wrapper over the
  // backdrop; it has to pass its clicks through or the day cannot be closed.
  it('lets clicks through the wrapper around the sheet', () => {
    render(<DayDetail />);

    const sheet = screen.getByRole('heading').closest('.bottom-sheet');
    expect(sheet?.className).toContain('pointer-events-auto');
    expect(sheet?.parentElement?.className).toContain('pointer-events-none');
  });
});
