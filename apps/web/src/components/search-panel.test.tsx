import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  locale: 'en',
  timezone: 'Asia/Tokyo',
  showSearch: true,
  searchQuery: '',
  setSearchQuery: vi.fn(),
  toggleSearch: vi.fn(),
  setCurrentMonth: vi.fn(),
  setSelectedDate: vi.fn(),
  openEventModal: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState = {
  events: [] as CalendarEvent[],
  activeCalendarIds: ['cal-1'],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { SearchPanel } from './search-panel';

function event(id: string, calendarId: string, title: string): CalendarEvent {
  return {
    id,
    calendarId,
    title,
    allDay: false,
    startAt: '2026-08-05T10:00:00+09:00',
    endAt: '2026-08-05T11:00:00+09:00',
    color: '#47B2F7',
    ownerId: null,
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
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  uiState.showSearch = true;
  uiState.searchQuery = '';
  calendarState.events = [];
  calendarState.activeCalendarIds = ['cal-1'];
  document.body.style.overflow = '';
});

/** jsdom reports no layout, so focusable elements have to look painted. */
function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

function emptyRectList(): DOMRectList {
  const rects = [] as unknown as DOMRectList;
  rects.item = () => null;
  return rects;
}

/** Paints only the given subtree, the way one of the two layouts is at a time. */
function paintOnly(surface: Element | null) {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(function (
    this: HTMLElement,
  ) {
    return surface?.contains(this) ? rectList() : emptyRectList();
  });
}

describe('SearchPanel', () => {
  it('shows nothing until it is opened', () => {
    uiState.showSearch = false;

    const { container } = render(<SearchPanel />);

    expect(container).toBeEmptyDOMElement();
  });

  it('opens onto a search box and lists what the query matches', () => {
    calendarState.events = [
      event('e1', 'cal-1', 'Morning Rehearsal'),
      event('e2', 'cal-1', 'Lunch'),
    ];
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);

    // The panel draws twice: a fullscreen overlay for narrow widths and a
    // dropdown for wide ones. Both are in the document; CSS picks one.
    expect(screen.getAllByPlaceholderText('search.placeholder')).toHaveLength(2);
    expect(screen.getAllByText('Morning Rehearsal')).toHaveLength(2);
    expect(screen.queryAllByText('Lunch')).toHaveLength(0);
  });

  it('leaves out events from a calendar the reader has switched off', () => {
    calendarState.events = [
      event('e1', 'cal-1', 'Rehearsal at home'),
      event('e2', 'cal-2', 'Rehearsal at work'),
    ];
    calendarState.activeCalendarIds = ['cal-1'];
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);

    expect(screen.getAllByText('Rehearsal at home')).toHaveLength(2);
    expect(screen.queryAllByText('Rehearsal at work')).toHaveLength(0);
  });
});

// Search covers the page but had only a hand-rolled Escape: the page behind it
// scrolled, Tab walked into the calendar, and the focus it took never came back.
describe('SearchPanel keyboard', () => {
  it('announces itself as a modal dialog', () => {
    render(<SearchPanel />);

    const panel = screen.getByRole('dialog');
    expect(panel).toHaveAttribute('aria-modal', 'true');
    expect(panel).toHaveAccessibleName('search.searchEvents');
  });

  it('locks the page behind it and closes on Escape', () => {
    render(<SearchPanel />);

    expect(document.body.style.overflow).toBe('hidden');

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(uiState.toggleSearch).toHaveBeenCalledTimes(1);
  });

  // Both layouts are in the document at once and CSS picks one, so the trap has
  // to walk the surface that is painted rather than the one that renders last.
  it('keeps Tab inside the layout that is on screen', () => {
    const { container } = render(<SearchPanel />);
    paintOnly(container.querySelector('.sm\\:hidden'));

    const overlay = container.querySelector('.sm\\:hidden');
    expect(overlay).not.toBeNull();
    if (!overlay) return;
    const focusable = Array.from(overlay.querySelectorAll<HTMLElement>('button,input'));
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    expect(focusable.length).toBeGreaterThan(1);
    if (!first || !last) return;

    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  // The search box carried one ref across both layouts, so the element it
  // pointed at was whichever rendered last: on a phone the focus went to the
  // desktop dropdown's box, which is not on screen.
  it('focuses the search box of the layout that is on screen', async () => {
    const { container } = render(<SearchPanel />);
    paintOnly(container.querySelector('.sm\\:hidden'));

    const boxes = screen.getAllByPlaceholderText('search.placeholder');
    await waitFor(() => expect(document.activeElement).toBe(boxes[0]));
  });

  it('returns focus to whatever opened it', async () => {
    uiState.showSearch = false;
    function Harness() {
      return (
        <>
          <button type="button" data-testid="trigger">
            open
          </button>
          <SearchPanel />
        </>
      );
    }
    const { rerender, container } = render(<Harness />);
    const trigger = screen.getByTestId('trigger');
    trigger.focus();

    uiState.showSearch = true;
    rerender(<Harness />);
    paintOnly(container.querySelector('.sm\\:hidden'));
    await waitFor(() => expect(document.activeElement).not.toBe(trigger));

    uiState.showSearch = false;
    rerender(<Harness />);

    expect(document.activeElement).toBe(trigger);
  });
});
