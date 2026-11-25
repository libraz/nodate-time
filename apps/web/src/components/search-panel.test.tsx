import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useT: () => (key: string, params?: Record<string, string>) =>
    params ? `${key} ${JSON.stringify(params)}` : key,
}));

const uiState = {
  locale: 'en',
  timezone: 'Asia/Tokyo',
  calendarView: 'month',
  currentMonth: DateTime.fromISO('2026-08-01', { zone: 'Asia/Tokyo' }),
  showSearch: true,
  searchQuery: '',
  setSearchQuery: vi.fn(),
  toggleSearch: vi.fn(),
  setCurrentMonth: vi.fn(),
  setSelectedDate: vi.fn(),
  openEventModal: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: Object.assign((selector: (s: typeof uiState) => unknown) => selector(uiState), {
    getState: () => uiState,
  }),
}));

const get = vi.fn();
vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  client: {
    // biome-ignore lint/style/useNamingConvention: mirrors the HTTP method name of the client
    GET: (...args: unknown[]) => get(...args),
  },
}));

import { ApiError } from '@/lib/client';
import { SearchPanel } from './search-panel';

/** Matches a result line by its whole text, which the match highlight splits across nodes. */
function fullText(text: string) {
  return (_: string, el: Element | null) => el?.tagName === 'P' && el.textContent === text;
}

/** Answers the next search with `events`. */
function serve(events: CalendarEvent[], hasMore = false) {
  get.mockResolvedValue({ data: { items: events, hasMore } });
}

function event(id: string, calendarId: string, title: string): CalendarEvent {
  return {
    id,
    calendarId,
    title,
    allDay: false,
    startAt: '2026-08-05T10:00:00+09:00',
    endAt: '2026-08-05T11:00:00+09:00',
    color: '#47B2F7',
    ownerId: 'u1',
    labelId: null,
    ownerName: 'Owner',
    creatorId: 'u1',
    creatorName: 'Owner',
    detailsHidden: false,
    etag: 'W/"1"',
    kind: 'event',
    taskOwned: false,
    timezone: 'Asia/Tokyo',
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
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  uiState.showSearch = true;
  uiState.searchQuery = '';
  get.mockReset();
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

  it('opens onto a search box and lists what the server matched', async () => {
    serve([event('e1', 'cal-1', 'Morning Rehearsal')]);
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);

    // The panel draws twice: a fullscreen overlay for narrow widths and a
    // dropdown for wide ones. Both are in the document; CSS picks one.
    expect(screen.getAllByPlaceholderText('search.placeholder')).toHaveLength(2);
    expect(await screen.findAllByText(fullText('Morning Rehearsal'))).toHaveLength(2);
    expect(get).toHaveBeenCalledWith('/search', {
      params: { query: { q: 'rehearsal' } },
      signal: expect.any(AbortSignal),
    });
  });

  it('does not ask the server for a blank query', () => {
    uiState.searchQuery = '   ';

    render(<SearchPanel />);

    expect(screen.getAllByText('search.searchEvents').length).toBeGreaterThan(0);
    expect(get).not.toHaveBeenCalled();
  });

  it('says so when nothing matches', async () => {
    serve([]);
    uiState.searchQuery = 'dentist';

    render(<SearchPanel />);

    expect(await screen.findAllByText('search.noResults')).toHaveLength(2);
  });

  it('opens the matched event on its day and closes the panel', async () => {
    serve([event('e1', 'cal-1', 'Morning Rehearsal')]);
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);
    const [first] = await screen.findAllByText(fullText('Morning Rehearsal'));
    if (!first) throw new Error('no result');
    fireEvent.click(first);

    expect(uiState.setSelectedDate.mock.calls[0]?.[0]?.toISODate()).toBe('2026-08-05');
    expect(uiState.setCurrentMonth.mock.calls[0]?.[0]?.toISODate()).toBe('2026-08-01');
    expect(uiState.toggleSearch).toHaveBeenCalledTimes(1);
    expect(uiState.openEventModal).toHaveBeenCalledWith('e1');
  });

  // A series is found as its next occurrence, whose id names the occurrence.
  it('opens the occurrence of a series by its own id', async () => {
    serve([
      {
        ...event('master_2026-08-05', 'cal-1', 'Standup'),
        isRecurrence: true,
        recurrenceDate: '2026-08-05',
      },
    ]);
    uiState.searchQuery = 'standup';

    render(<SearchPanel />);
    const [first] = await screen.findAllByText(fullText('Standup'));
    if (!first) throw new Error('no result');
    fireEvent.click(first);

    expect(uiState.openEventModal).toHaveBeenCalledWith('master_2026-08-05');
  });

  it('says when there are more matches than were returned', async () => {
    serve([event('e1', 'cal-1', 'Rehearsal')], true);
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);

    expect(await screen.findAllByText('search.hasMore')).toHaveLength(2);
  });

  it('does not claim more matches when the server says there are none', async () => {
    serve([event('e1', 'cal-1', 'Rehearsal')], false);
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);
    await screen.findAllByText(fullText('Rehearsal'));

    expect(screen.queryAllByText('search.hasMore')).toHaveLength(0);
  });

  it('shows the too-broad message for a query that matches too much', async () => {
    get.mockRejectedValue(new ApiError(422, 'too broad', 'SEARCH.TOO_BROAD'));
    uiState.searchQuery = 'a';

    render(<SearchPanel />);

    expect(await screen.findAllByRole('alert')).toHaveLength(2);
    expect(screen.getAllByText('Too many matches. Add a keyword to narrow it down')).toHaveLength(
      2,
    );
    expect(screen.queryAllByText('search.noResults')).toHaveLength(0);
  });

  it('shows the server reason when a query is refused for its length', async () => {
    get.mockRejectedValue(new ApiError(422, 'q must be 1 to 100 characters', 'REQUEST.INVALID'));
    uiState.searchQuery = 'x'.repeat(101);

    render(<SearchPanel />);

    expect(await screen.findAllByRole('alert')).toHaveLength(2);
    expect(screen.queryAllByText('search.noResults')).toHaveLength(0);
  });
});

describe('SearchPanel results', () => {
  it('marks the part of a title the query matched', async () => {
    serve([event('e1', 'cal-1', 'Morning Rehearsal')]);
    uiState.searchQuery = 'rehe';

    render(<SearchPanel />);

    const marks = await screen.findAllByText('Rehe', { selector: 'mark' });
    expect(marks).toHaveLength(2);
  });

  it('walks the results with the arrow keys and opens the top one on Enter', async () => {
    serve([event('e1', 'cal-1', 'Rehearsal A'), event('e2', 'cal-1', 'Rehearsal B')]);
    uiState.searchQuery = 'rehearsal';
    const { container } = render(<SearchPanel />);
    await screen.findAllByText(fullText('Rehearsal A'));
    const dropdown = container.querySelector('.modal-panel') as HTMLElement;
    const box = dropdown.querySelector('input') as HTMLInputElement;
    const rows = dropdown.querySelectorAll<HTMLElement>('[data-search-result]');

    box.focus();
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(rows[0]);
    fireEvent.keyDown(rows[0] as HTMLElement, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(rows[1]);
    fireEvent.keyDown(rows[1] as HTMLElement, { key: 'ArrowUp' });
    fireEvent.keyDown(rows[0] as HTMLElement, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(box);

    fireEvent.keyDown(box, { key: 'Enter' });
    expect(uiState.openEventModal).toHaveBeenCalledWith('e1');
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

// An all-day event is a date, not an instant: read in the viewer's zone, a
// Tokyo birthday on 5 August starts on the 4th in New York.
describe('SearchPanel dates', () => {
  afterEach(() => {
    uiState.timezone = 'Asia/Tokyo';
  });

  it('names and jumps to an all-day event on its own date in any viewer zone', async () => {
    uiState.timezone = 'America/New_York';
    serve([
      {
        ...event('e1', 'cal-1', 'Birthday'),
        allDay: true,
        startAt: '2026-08-05T00:00:00Z',
        endAt: '2026-08-06T00:00:00Z',
        startDate: '2026-08-05',
        endDate: '2026-08-06',
        timezone: 'Asia/Tokyo',
      },
    ]);
    uiState.searchQuery = 'birthday';

    render(<SearchPanel />);

    expect(await screen.findAllByText('2026/08/05 (Wed)')).toHaveLength(2);

    const [first] = screen.getAllByText('Birthday');
    if (!first) throw new Error('no result');
    fireEvent.click(first);

    const selected = uiState.setSelectedDate.mock.calls[0]?.[0];
    const month = uiState.setCurrentMonth.mock.calls[0]?.[0];
    expect(selected?.toISODate()).toBe('2026-08-05');
    expect(month?.toISODate()).toBe('2026-08-01');
    expect(uiState.openEventModal).toHaveBeenCalledWith('e1');
  });

  it('keeps a timed event on the viewer clock', async () => {
    uiState.timezone = 'America/New_York';
    serve([event('e1', 'cal-1', 'Call')]);
    uiState.searchQuery = 'call';

    render(<SearchPanel />);

    // 10:00 in Tokyo is 21:00 the previous evening in New York.
    expect(await screen.findAllByText('2026/08/04 (Tue) 21:00 - 22:00')).toHaveLength(2);
  });
});

describe('SearchPanel availability states', () => {
  it('shows the showAs and flexibility labels on a result', async () => {
    serve([{ ...event('e1', 'cal-1', 'Rehearsal'), showAs: 'free', flexibility: 'negotiable' }]);
    uiState.searchQuery = 'rehearsal';

    render(<SearchPanel />);

    expect(
      await screen.findAllByText('event.showAsFree · event.flexibilityShort.negotiable'),
    ).toHaveLength(2);
  });
});
