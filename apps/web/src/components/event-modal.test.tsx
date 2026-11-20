import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent, Member } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => {
  class ApiError extends Error {
    constructor(
      public status: number,
      public code: string,
      message: string,
    ) {
      super(message);
    }
  }
  return {
    // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
    ApiError,
    api: {
      get: vi.fn(),
      post: vi.fn(),
      put: vi.fn(),
      delete: vi.fn(),
      getWithRevision: vi.fn(),
    },
    errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
  };
});

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

// Only the relative-time formatter is stood in for; the rest of the module is
// what the modal actually calls for its weekday labels.
const relativeTime = vi.hoisted(() => vi.fn());

vi.mock('@/lib/date-utils', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/date-utils')>()),
  formatRelativeTime: relativeTime,
}));

const event: CalendarEvent = {
  id: 'ev-1',
  calendarId: 'cal-1',
  title: 'Standup',
  allDay: false,
  startAt: '2026-08-07T00:00:00Z',
  endAt: '2026-08-07T01:00:00Z',
  timezone: 'Asia/Tokyo',
  color: '#000000',
  ownerId: 'u1',
  location: '',
  memo: '',
  url: '',
  showAs: 'busy',
  flexibility: 'fixed',
  visibility: 'default',
  notificationOffset: null,
  participantIds: [],
  recurrenceRule: null,
  isRecurrence: false,
  recurrenceDate: null,
  creatorId: 'u1',
  createdAt: '2026-08-01T00:00:00Z',
  updatedAt: '2026-08-01T00:00:00Z',
};

const uiState = {
  locale: 'ja' as 'ja' | 'en',
  timezone: 'Asia/Tokyo',
  showEventModal: true,
  editingEventId: 'ev-1' as string | null,
  closeEventModal: vi.fn(),
  selectedDate: DateTime.fromISO('2026-08-07T00:00:00', { zone: 'Asia/Tokyo' }),
  eventDraftStart: null as DateTime | null,
  eventDraftEnd: null as DateTime | null,
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const teamCalendar = {
  id: 'cal-1',
  name: 'Team',
  color: '#000',
  createdAt: '',
  publicShared: false,
  role: 'owner',
  memberColor: '#000',
};

const calendarState = {
  calendars: [teamCalendar],
  activeCalendarIds: ['cal-1'],
  events: [event],
  addEvent: vi.fn(),
  updateEvent: vi.fn(),
  deleteEvent: vi.fn(),
  membersMap: { 'cal-1': [] } as Record<string, Member[]>,
  fetchMembers: vi.fn(),
  fetchEvents: vi.fn(),
  visibleRange: () => ({ start: uiState.selectedDate, end: uiState.selectedDate }),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

const authState = { user: { id: 'u1', name: 'Me' } };

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { EventModal } from './event-modal';

const mockApi = vi.mocked(api);
const mockToast = vi.mocked(toast);

/** Paths the modal's body loads, one request each. */
const bodyPaths = ['/checklist', '/attachments', '/activities', '/history'];

function callsFor(suffix: string): number {
  return mockApi.get.mock.calls.filter(([path]) => String(path).endsWith(suffix)).length;
}

/** jsdom reports no layout, so focusable elements have to look painted. */
function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

function titleField(): HTMLElement {
  return screen.getByPlaceholderText('event.titlePlaceholder');
}

beforeEach(() => {
  uiState.showEventModal = true;
  uiState.editingEventId = 'ev-1';
  uiState.eventDraftStart = null;
  uiState.eventDraftEnd = null;
  uiState.locale = 'ja';
  uiState.timezone = 'Asia/Tokyo';
  calendarState.events = [event];
  calendarState.calendars = [teamCalendar];
  calendarState.activeCalendarIds = ['cal-1'];
  teamCalendar.role = 'owner';
  relativeTime.mockReturnValue('a while ago');
  mockApi.get.mockResolvedValue([]);
  mockApi.getWithRevision.mockResolvedValue({ data: event, revision: 'rev-1' });
  mockApi.post.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  document.body.style.overflow = '';
});

describe('EventModal body', () => {
  // Mounting the body for each layout doubled every request the sections make
  // and let the desktop copy claim the title ref, which is why the mobile
  // keyboard never came up.
  it('mounts once, so each section asks the server for its contents once', async () => {
    render(<EventModal />);

    await waitFor(() => expect(callsFor('/history')).toBe(1));
    for (const path of bodyPaths) {
      expect(callsFor(path)).toBe(1);
    }
    expect(mockApi.getWithRevision).toHaveBeenCalledTimes(1);
    expect(screen.getAllByPlaceholderText('event.titlePlaceholder')).toHaveLength(1);
  });

  // The thread is paged, so the answer is an envelope. Reading it as a bare
  // array fails silently: there is no type error and the section renders as a
  // thread with nothing in it.
  it('reads the thread out of the paged answer', async () => {
    mockApi.get.mockImplementation((path: string) =>
      path.endsWith('/activities')
        ? Promise.resolve({
            items: [
              {
                id: 'c1',
                body: 'first thing',
                authorName: 'Someone',
                authorId: 'u2',
                createdAt: '2026-04-20T10:00:00Z',
              },
            ],
          })
        : Promise.resolve([]),
    );

    render(<EventModal />);

    expect(await screen.findByText('first thing')).toBeInTheDocument();
  });

  it('offers the older comments the first page did not carry', async () => {
    mockApi.get.mockImplementation((path: string) => {
      if (!path.endsWith('/activities') && !path.includes('/activities?')) {
        return Promise.resolve([]);
      }
      const comment = (id: string, body: string) => ({
        id,
        body,
        authorName: 'Someone',
        authorId: 'u2',
        createdAt: '2026-04-20T10:00:00Z',
      });
      return path.includes('cursor=')
        ? Promise.resolve({ items: [comment('c0', 'older thing')] })
        : Promise.resolve({ items: [comment('c1', 'newer thing')], nextCursor: 'older' });
    });

    render(<EventModal />);
    const earlier = await screen.findByText('event.loadEarlierComments');
    fireEvent.click(earlier);

    expect(await screen.findByText('older thing')).toBeInTheDocument();
    // Older comments belong in front of what is on screen, not after it.
    const rendered = screen.getAllByText(/thing$/).map((n) => n.textContent);
    expect(rendered).toEqual(['older thing', 'newer thing']);
  });

  // authorAvatarUrl is a URL, and rendering it as the contents of the avatar box
  // put the signed link on screen as text where the face should be.
  it('draws a comment author as their picture, not as the URL of it', async () => {
    const avatar = 'https://storage.example/avatars/u2?signature=abc';
    mockApi.get.mockImplementation((path: string) =>
      path.endsWith('/activities')
        ? Promise.resolve({
            items: [
              {
                id: 'c1',
                body: 'first thing',
                authorName: 'Someone',
                authorId: 'u2',
                authorAvatarUrl: avatar,
                createdAt: '2026-04-20T10:00:00Z',
              },
            ],
          })
        : Promise.resolve([]),
    );

    const { container } = render(<EventModal />);
    await screen.findByText('first thing');

    expect(container.querySelector(`img[src="${avatar}"]`)).not.toBeNull();
    expect(screen.queryByText(avatar)).toBeNull();
  });

  // The modal carried its own copy of the relative-time formatter, so the same
  // timestamp could read one way in a comment and another in the history beside
  // it, and a fix to one of them would reach only half the screen.
  it('times a comment with the shared formatter, not a copy of its own', async () => {
    mockApi.get.mockImplementation((path: string) =>
      path.endsWith('/activities')
        ? Promise.resolve({
            items: [
              {
                id: 'c1',
                body: 'first thing',
                authorName: 'Someone',
                authorId: 'u2',
                createdAt: '2026-04-20T10:00:00Z',
              },
            ],
          })
        : Promise.resolve([]),
    );

    render(<EventModal />);
    await screen.findByText('first thing');

    expect(relativeTime).toHaveBeenCalledWith('2026-04-20T10:00:00Z', 'ja');
    expect(screen.getAllByText('a while ago')).toHaveLength(1);
  });

  it('focuses the title field on open', async () => {
    render(<EventModal />);

    await waitFor(() => expect(document.activeElement).toBe(titleField()));
  });
});

// The custom-repeat editor named its weekdays from a Japanese table, so an
// English account picked its repeat days off buttons labelled 日 to 土.
describe('EventModal custom recurrence weekdays', () => {
  /** An interval of 2 is no preset, which is what opens the custom editor. */
  function openWith(rule: CalendarEvent['recurrenceRule']) {
    calendarState.events = [{ ...event, recurrenceRule: rule }];
  }

  const weekly = { freq: 'weekly', interval: 2, byDay: ['MO', 'WE'] } as const;
  const monthlyNth = { freq: 'monthly', interval: 2, bySetPos: [3], byDay: ['TU'] } as const;

  it('labels the day toggles in the reader’s language', async () => {
    openWith(weekly);
    uiState.locale = 'en';

    render(<EventModal />);
    await screen.findByPlaceholderText('event.titlePlaceholder');

    for (const day of ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']) {
      expect(screen.getByText(day)).toBeInTheDocument();
    }
    expect(screen.queryByText('日')).toBeNull();
  });

  it('names the nth-weekday choice in the reader’s language', async () => {
    openWith(monthlyNth);
    uiState.locale = 'en';

    render(<EventModal />);
    await screen.findByPlaceholderText('event.titlePlaceholder');

    expect(screen.getByText('Tuesday')).toBeInTheDocument();
    expect(screen.queryByText('火')).toBeNull();
  });

  it('keeps the single-character labels a Japanese account reads', async () => {
    openWith(weekly);

    render(<EventModal />);
    await screen.findByPlaceholderText('event.titlePlaceholder');

    for (const day of ['日', '月', '火', '水', '木', '金', '土']) {
      expect(screen.getByText(day)).toBeInTheDocument();
    }
  });
});

// A dialog inside a dialog. The chooser's buttons sit outside the modal's
// container, so with only the modal's trap running they were unreachable by
// keyboard and Tab left the page entirely.
describe('EventModal scope chooser', () => {
  async function openChooser() {
    // The trap only considers painted elements, and jsdom paints nothing.
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    calendarState.events = [{ ...event, isRecurrence: true }];
    render(<EventModal />);
    await screen.findByPlaceholderText('event.titlePlaceholder');
    fireEvent.click(screen.getByText('common.save'));
    return screen.findByRole('dialog', { name: 'event.scopeEditTitle' });
  }

  it('takes the keyboard when it opens', async () => {
    const chooser = await openChooser();

    await waitFor(() => expect(chooser.contains(document.activeElement)).toBe(true));
  });

  it('keeps Tab inside the question it is asking', async () => {
    const chooser = await openChooser();

    const choices = Array.from(chooser.querySelectorAll<HTMLElement>('button'));
    const first = choices[0];
    const last = choices[choices.length - 1];
    expect(first).toBeDefined();
    expect(last).toBeDefined();
    if (!first || !last) return;

    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  // Escape belongs to the innermost surface: it answers the chooser, and the
  // editor underneath stays open with the edit still in it.
  it('is what Escape closes, leaving the editor open', async () => {
    const chooser = await openChooser();

    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => expect(chooser).not.toBeInTheDocument());
    expect(screen.getByPlaceholderText('event.titlePlaceholder')).toBeInTheDocument();
    expect(uiState.closeEventModal).not.toHaveBeenCalled();
  });
});

describe('EventModal accessibility', () => {
  it('locks the background while it is open', () => {
    render(<EventModal />);

    expect(document.body.style.overflow).toBe('hidden');
  });

  it('closes on Escape', async () => {
    render(<EventModal />);

    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => expect(uiState.closeEventModal).toHaveBeenCalledTimes(1));
  });

  it('keeps Tab inside the modal', async () => {
    render(<EventModal />);
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    await waitFor(() => expect(document.activeElement).toBe(titleField()));

    const dialog = screen.getByRole('dialog');
    const focusable = Array.from(
      dialog.querySelectorAll<HTMLElement>('button:not([disabled]),textarea,input,select'),
    );
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    expect(first).toBeDefined();
    expect(last).toBeDefined();
    if (!first || !last) return;

    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it('returns focus to whatever opened it', async () => {
    uiState.showEventModal = false;
    function Harness() {
      return (
        <>
          <button type="button" data-testid="trigger">
            open
          </button>
          <EventModal />
        </>
      );
    }
    const { rerender } = render(<Harness />);
    const trigger = screen.getByTestId('trigger');
    trigger.focus();

    uiState.showEventModal = true;
    rerender(<Harness />);
    await waitFor(() => expect(document.activeElement).toBe(titleField()));

    uiState.showEventModal = false;
    rerender(<Harness />);

    expect(document.activeElement).toBe(trigger);
  });
});

describe('EventModal failures', () => {
  it('reports a comment it could not post', async () => {
    render(<EventModal />);
    await waitFor(() => expect(callsFor('/activities')).toBe(1));
    mockApi.post.mockRejectedValueOnce(new Error('comment refused'));

    const input = screen.getByPlaceholderText('event.commentPlaceholder');
    fireEvent.change(input, { target: { value: 'hello' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith('comment refused'));
  });

  it('reports a checklist it could not read where the list would be', async () => {
    mockApi.get.mockImplementation((path: string) =>
      path.endsWith('/checklist')
        ? Promise.reject(new Error('checklist offline'))
        : Promise.resolve([]),
    );

    render(<EventModal />);

    expect(await screen.findByText('checklist offline')).toBeInTheDocument();
  });

  it('ticks a checklist item by sending only done', async () => {
    mockApi.get.mockImplementation((path: string) =>
      path.endsWith('/checklist')
        ? Promise.resolve([
            {
              id: 'item-1',
              title: 'Tent',
              done: false,
              sortOrder: 3,
              createdAt: '2026-04-20T10:00:00Z',
            },
          ])
        : Promise.resolve([]),
    );
    mockApi.put.mockResolvedValue(undefined);

    render(<EventModal />);

    const title = await screen.findByText('Tent');
    const toggle = title.closest('.group')?.querySelector('button');
    expect(toggle).toBeTruthy();
    fireEvent.click(toggle as HTMLButtonElement);

    await waitFor(() =>
      expect(mockApi.put).toHaveBeenCalledWith(expect.stringMatching(/\/checklist\/item-1$/), {
        done: true,
      }),
    );
  });
});

/**
 * What the modal actually sends.
 *
 * The rest of this file covers what the modal draws. Nothing covered what it
 * puts on the wire, and both of the release blockers that reached production
 * through this component were payload bugs: a `color` key the body schema
 * forbids, which failed every save with 422, and the viewer's timezone sent
 * for an event that had one of its own, which moved the whole series.
 *
 * The API rejects unknown properties, so the field set is a contract and is
 * asserted as one. `apps/api/tests/e2e/event_payload_test.go` states the same
 * set on the server side, but it states it by hand -- adding a field here
 * cannot fail it. This is the assertion that can.
 */
describe('EventModal save payload', () => {
  /** Every key the events API accepts from this modal. */
  const wireFields = [
    'allDay',
    'endAt',
    'flexibility',
    'location',
    'memo',
    'notificationOffset',
    'ownerId',
    'participantIds',
    'recurrenceRule',
    'showAs',
    'startAt',
    'timezone',
    'title',
    'url',
    'visibility',
  ];

  async function saveExisting() {
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    return calendarState.updateEvent.mock.calls[0]?.[2] as Record<string, unknown>;
  }

  it('sends the fields the API accepts and no others', async () => {
    const payload = await saveExisting();

    expect(Object.keys(payload).sort()).toEqual(wireFields);
  });

  it('saves how movable the event is independently of how its time is shown', async () => {
    calendarState.events = [{ ...event, flexibility: 'conditional' }];
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));
    const flexibility = screen.getByRole('button', { name: 'event.flexibility' });
    expect(flexibility).toHaveTextContent('event.flexibilityConditional');
    fireEvent.click(flexibility);
    fireEvent.click(await screen.findByRole('option', { name: 'event.flexibilityNegotiable' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    expect(calendarState.updateEvent.mock.calls[0]?.[2]).toMatchObject({
      flexibility: 'negotiable',
      showAs: 'busy',
    });
  });

  // The colour of an event is derived from the owner's membership; there is no
  // input field for it. Sending one is not a value the server ignores, it is a
  // 422 on every save, which is how create, edit, drag and resize were all
  // broken from the shipped UI at once.
  it('sends no colour, which the body schema has no field for', async () => {
    const payload = await saveExisting();

    expect(payload).not.toHaveProperty('color');
  });

  // An existing event keeps the zone it was created in. Its all-day dates and
  // its recurrence grid are both measured there, so sending the viewer's zone
  // re-anchors the series and moves every later occurrence across a DST
  // boundary -- from a save that changed nothing.
  it('sends an existing event back in its own timezone, not the viewer’s', async () => {
    uiState.timezone = 'America/New_York';

    const payload = await saveExisting();

    expect(payload.timezone).toBe('Asia/Tokyo');
  });

  // The other half of the same rule: a new event has no zone of its own, so it
  // takes the one its author is working in. A fix that always sent the event's
  // zone would leave creation with nothing to send.
  // A range dragged out on the timeline is the event's span, not just its start.
  it('creates a new event over the range it was opened on', async () => {
    uiState.editingEventId = null;
    uiState.eventDraftStart = DateTime.fromISO('2026-08-07T10:00', { zone: 'Asia/Tokyo' });
    uiState.eventDraftEnd = DateTime.fromISO('2026-08-07T12:30', { zone: 'Asia/Tokyo' });

    render(<EventModal />);
    fireEvent.change(titleField(), { target: { value: 'Workshop' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(calendarState.addEvent).toHaveBeenCalled());
    const payload = calendarState.addEvent.mock.calls[0]?.[1] as Record<string, string>;
    expect(DateTime.fromISO(payload.startAt as string).toISO()).toBe(
      DateTime.fromISO('2026-08-07T10:00:00+09:00').toISO(),
    );
    expect(DateTime.fromISO(payload.endAt as string).toISO()).toBe(
      DateTime.fromISO('2026-08-07T12:30:00+09:00').toISO(),
    );
  });

  it('sends a new event in the viewer’s timezone', async () => {
    uiState.editingEventId = null;
    uiState.timezone = 'America/New_York';

    render(<EventModal />);
    fireEvent.change(titleField(), { target: { value: 'Kickoff' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(calendarState.addEvent).toHaveBeenCalled());
    const payload = calendarState.addEvent.mock.calls[0]?.[1] as Record<string, unknown>;
    expect(payload.timezone).toBe('America/New_York');
    expect(Object.keys(payload).sort()).toEqual(wireFields);
  });
});

describe('EventModal recurrence end', () => {
  const weeklyWithCount: CalendarEvent = {
    ...event,
    recurrenceRule: { freq: 'weekly', interval: 1, byDay: ['FR'], count: 5 },
  };

  function openWith(evt: CalendarEvent) {
    calendarState.events = [evt];
    mockApi.getWithRevision.mockResolvedValue({ data: evt, revision: 'rev-1' });
    render(<EventModal />);
  }

  // A weekly rule with a count reads as the weekly preset, and the end
  // controls lived only in the custom editor, so the count was invisible.
  it('shows the end condition of a rule that reads as a preset', async () => {
    openWith(weeklyWithCount);

    await waitFor(() => expect(titleField()).toHaveValue('Standup'));
    expect(screen.getByDisplayValue('5')).toBeInTheDocument();
  });

  // Moving the start rebuilt the preset's rule from scratch, so a series of
  // five became endless on the next save.
  it('keeps the end condition when the start date moves', async () => {
    openWith(weeklyWithCount);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    const [startDate] = screen.getAllByRole('button', { name: '2026年8月7日(金)' });
    if (!startDate) throw new Error('no start date field');
    fireEvent.click(startDate);
    fireEvent.click(screen.getByRole('button', { name: '2026-08-13' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    const payload = calendarState.updateEvent.mock.calls[0]?.[2] as Record<string, unknown>;
    expect(payload.recurrenceRule).toMatchObject({ freq: 'weekly', byDay: ['TH'], count: 5 });
  });

  // The end date is stored as the end of that day in the event's zone; read in
  // a zone further east it fell on the next day.
  it('shows the end date in the event’s zone, not the viewer’s', async () => {
    uiState.timezone = 'Pacific/Auckland';
    openWith({
      ...event,
      recurrenceRule: {
        freq: 'weekly',
        interval: 1,
        byDay: ['FR'],
        until: '2026-08-31T23:59:59.999+09:00',
      },
    });

    await waitFor(() => expect(titleField()).toHaveValue('Standup'));
    expect(screen.getByRole('button', { name: '2026年8月31日(月)' })).toBeInTheDocument();
  });
});

describe('EventModal RSVP', () => {
  const invited: CalendarEvent = {
    ...event,
    participantIds: ['u1'],
    attendees: [{ userId: 'u1', rsvp: 'pending', canEdit: false }],
  };

  // The answer was kept only in the modal, so reopening read the store's old
  // copy and showed the previous answer again.
  it('reads the events back once the answer is stored', async () => {
    calendarState.events = [invited];
    mockApi.put.mockResolvedValue(undefined);
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.click(screen.getByRole('button', { name: 'event.rsvpAccepted' }));

    await waitFor(() => expect(calendarState.fetchEvents).toHaveBeenCalledTimes(1));
    expect(mockApi.put).toHaveBeenCalledWith('/calendars/cal-1/events/ev-1/rsvp', {
      rsvp: 'accepted',
    });
  });

  it('leaves the store alone when the answer is refused', async () => {
    calendarState.events = [invited];
    mockApi.put.mockRejectedValue(new Error('nope'));
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.click(screen.getByRole('button', { name: 'event.rsvpAccepted' }));

    await waitFor(() => expect(mockToast.error).toHaveBeenCalled());
    expect(calendarState.fetchEvents).not.toHaveBeenCalled();
  });
});

describe('EventModal monthly weekday position', () => {
  // A start on the 29th-31st is the fifth weekday of its month. The picker
  // stopped at the fourth and offered a "last" the server refuses, so the
  // rule showed nothing selected and the one extra choice could not be saved.
  it('shows the fifth weekday and offers only positions the server accepts', async () => {
    const fifthFriday: CalendarEvent = {
      ...event,
      startAt: '2026-07-31T00:00:00Z',
      endAt: '2026-07-31T01:00:00Z',
      recurrenceRule: { freq: 'monthly', interval: 2, bySetPos: [5], byDay: ['FR'] },
    };
    calendarState.events = [fifthFriday];
    mockApi.getWithRevision.mockResolvedValue({ data: fifthFriday, revision: 'rev-1' });
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.click(screen.getByRole('button', { name: 'event.recurrenceMonthlyByWeekday' }));

    expect(screen.queryByRole('option', { name: 'event.recurrenceNthLast' })).toBeNull();
    expect(screen.getByRole('option', { name: 'event.recurrenceNthFifth' })).toBeInTheDocument();
  });
});

describe('EventModal named controls', () => {
  it('names the editor close button apart from the backdrop', async () => {
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(screen.getAllByRole('button', { name: 'common.close' }).length).toBeGreaterThanOrEqual(
      2,
    );
  });

  it('exposes the all-day toggle with its state and flips it with the form', async () => {
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    const toggle = screen.getByRole('button', { name: 'calendar.allDay' });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
  });
});

describe('EventModal recurrence presets across zones', () => {
  // Monday 20:00 in New York is Tuesday 09:00 for a viewer in Tokyo; the server
  // expands the rule in New York, so "weekly" has to mean Monday.
  it('saves a weekly preset on the weekday of the event’s own zone', async () => {
    const newYorkMonday: CalendarEvent = {
      ...event,
      startAt: '2026-08-04T00:00:00Z',
      endAt: '2026-08-04T01:00:00Z',
      timezone: 'America/New_York',
    };
    calendarState.events = [newYorkMonday];
    mockApi.getWithRevision.mockResolvedValue({ data: newYorkMonday, revision: 'rev-1' });
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.click(screen.getByRole('button', { name: 'event.recurrence' }));
    fireEvent.click(screen.getByRole('option', { name: 'event.recurrenceWeekly' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    const payload = calendarState.updateEvent.mock.calls[0]?.[2] as Record<string, unknown>;
    expect(payload.recurrenceRule).toMatchObject({ freq: 'weekly', byDay: ['MO'] });
  });
});

describe('EventModal canonical recurrence rules', () => {
  async function openAndPick(label: string, evt: CalendarEvent = event) {
    calendarState.events = [evt];
    mockApi.getWithRevision.mockResolvedValue({ data: evt, revision: 'rev-1' });
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));
    fireEvent.click(screen.getByRole('button', { name: 'event.recurrence' }));
    fireEvent.click(screen.getByRole('option', { name: label }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    return calendarState.updateEvent.mock.calls[0]?.[2] as Record<string, unknown>;
  }

  it('saves a monthly date as a byMonthDay array', async () => {
    const payload = await openAndPick('event.recurrenceMonthlyDate');
    expect(payload.recurrenceRule).toEqual({ freq: 'monthly', interval: 1, byMonthDay: [7] });
  });

  it('saves a weekly preset with the Sunday week start', async () => {
    const payload = await openAndPick('event.recurrenceWeekly');
    expect(payload.recurrenceRule).toEqual({
      freq: 'weekly',
      interval: 1,
      byDay: ['FR'],
      wkst: 'SU',
    });
  });

  it('saves the nth weekday with a bySetPos array', async () => {
    const payload = await openAndPick('event.recurrenceMonthlyNth');
    expect(payload.recurrenceRule).toEqual({
      freq: 'monthly',
      interval: 1,
      bySetPos: [1],
      byDay: ['FR'],
    });
  });

  it('shows and keeps the day of a stored byMonthDay array', async () => {
    const stored: CalendarEvent = {
      ...event,
      recurrenceRule: { freq: 'monthly', interval: 2, byMonthDay: [15] },
    };
    calendarState.events = [stored];
    mockApi.getWithRevision.mockResolvedValue({ data: stored, revision: 'rev-1' });
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(screen.getByDisplayValue('15')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    const payload = calendarState.updateEvent.mock.calls[0]?.[2] as Record<string, unknown>;
    expect(payload.recurrenceRule).toMatchObject({ byMonthDay: [15] });
  });

  it('keeps the week start a stored weekly rule carries', async () => {
    const stored: CalendarEvent = {
      ...event,
      recurrenceRule: { freq: 'weekly', interval: 2, byDay: ['MO', 'WE'], wkst: 'MO' },
    };
    calendarState.events = [stored];
    mockApi.getWithRevision.mockResolvedValue({ data: stored, revision: 'rev-1' });
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    const payload = calendarState.updateEvent.mock.calls[0]?.[2] as Record<string, unknown>;
    expect(payload.recurrenceRule).toMatchObject({ byDay: ['MO', 'WE'], wkst: 'MO' });
  });
});

describe('EventModal task-derived event', () => {
  const taskEvent: CalendarEvent = { ...event, taskOwned: true };

  async function openEvent(evt: CalendarEvent) {
    calendarState.events = [evt];
    mockApi.getWithRevision.mockResolvedValue({ data: evt, revision: 'rev-1' });
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));
  }

  it('explains why the synced fields cannot be changed here', async () => {
    await openEvent(taskEvent);

    expect(screen.getByText('event.taskOwnedNote')).toBeInTheDocument();
  });

  it('locks the title, the dates, the all-day toggle and the recurrence', async () => {
    await openEvent(taskEvent);

    expect(titleField()).toHaveAttribute('readonly');
    expect(screen.getByRole('button', { name: 'calendar.allDay' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'event.recurrence' })).toBeDisabled();
    for (const date of screen.getAllByRole('button', { name: '2026年8月7日(金)' })) {
      expect(date).toBeDisabled();
    }
  });

  it('offers no delete, since the event goes with its task', async () => {
    await openEvent(taskEvent);

    expect(screen.queryByRole('button', { name: 'event.deleteEvent' })).toBeNull();
  });

  it('still saves the fields that have no task-side counterpart', async () => {
    await openEvent(taskEvent);

    expect(screen.getByRole('button', { name: 'common.save' })).toBeEnabled();
  });

  it('leaves an ordinary event fully editable', async () => {
    await openEvent(event);

    expect(screen.queryByText('event.taskOwnedNote')).toBeNull();
    expect(titleField()).not.toHaveAttribute('readonly');
    expect(screen.getByRole('button', { name: 'calendar.allDay' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'event.deleteEvent' })).toBeInTheDocument();
  });
});

// On a shared calendar whose plan it is, who takes part and whether the time
// is taken are what the plan says to everyone else, so they are answered on
// the form itself rather than behind the optional fields.
describe('EventModal who and availability', () => {
  const members: Member[] = [
    { id: 'u1', name: 'Me', email: '', role: 'owner', color: '#111' },
    { id: 'u2', name: 'Bob', email: '', role: 'editor', color: '#222' },
  ];

  afterEach(() => {
    calendarState.membersMap = { 'cal-1': [] };
  });

  it('files a new plan under the person picked, with participants and availability', async () => {
    uiState.editingEventId = null;
    calendarState.membersMap = { 'cal-1': members };

    render(<EventModal />);
    fireEvent.change(titleField(), { target: { value: 'Dentist' } });
    const whose = screen.getByRole('button', { name: 'event.whose' });
    expect(whose).toHaveTextContent('event.memberMe');
    fireEvent.click(whose);
    fireEvent.click(await screen.findByRole('option', { name: 'Bob' }));
    fireEvent.click(screen.getByRole('button', { name: 'event.addParticipant' }));
    fireEvent.click(await screen.findByRole('option', { name: 'Bob' }));
    // Only the people taking part are drawn, each with a way to take them off.
    const people = screen.getByRole('group', { name: 'event.participants' });
    expect(within(people).getByText('Bob')).toBeInTheDocument();
    expect(within(people).queryByText('event.memberMe')).toBeNull();
    expect(within(people).getByRole('button', { name: 'event.removeParticipant' })).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'event.showAsTentative' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(calendarState.addEvent).toHaveBeenCalled());
    expect(calendarState.addEvent.mock.calls[0]?.[1]).toMatchObject({
      ownerId: 'u2',
      participantIds: ['u2'],
      showAs: 'tentative',
    });
  });

  it('leaves out the people on a calendar nobody else is on', () => {
    uiState.editingEventId = null;
    calendarState.membersMap = { 'cal-1': [members[0] as Member] };

    render(<EventModal />);

    expect(screen.queryByRole('button', { name: 'event.whose' })).toBeNull();
    expect(screen.getByRole('group', { name: 'event.showAs' })).toBeInTheDocument();
  });
});

describe('EventModal unsaved edits', () => {
  async function editTitle() {
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));
    fireEvent.change(titleField(), { target: { value: 'Standup moved' } });
  }

  function discardSheet() {
    return screen.queryByRole('alertdialog', { name: 'common.discardTitle' });
  }

  const closePaths: [string, () => void][] = [
    [
      'the header close button',
      () =>
        fireEvent.click(
          within(screen.getByRole('dialog')).getByRole('button', { name: 'common.close' }),
        ),
    ],
    [
      'cancel',
      () =>
        fireEvent.click(
          within(screen.getByRole('dialog')).getByRole('button', { name: 'common.cancel' }),
        ),
    ],
    [
      'the backdrop',
      () => fireEvent.click(document.querySelector('.modal-backdrop') as HTMLElement),
    ],
    ['Escape', () => fireEvent.keyDown(document, { key: 'Escape' })],
    [
      'a swipe down',
      () => {
        const handle = document.querySelector('.touch-none') as HTMLElement;
        fireEvent(handle, new MouseEvent('pointerdown', { bubbles: true, clientY: 0 }));
        fireEvent(window, new MouseEvent('pointermove', { clientY: 300 }));
        fireEvent(window, new MouseEvent('pointerup', { clientY: 300 }));
      },
    ],
  ];

  it.each(closePaths)('asks before %s throws edits away', async (_, close) => {
    await editTitle();

    close();

    expect(await screen.findByRole('alertdialog', { name: 'common.discardTitle' })).toBeVisible();
    expect(uiState.closeEventModal).not.toHaveBeenCalled();
  });

  it('closes once the discard is confirmed, and stays open on keep editing', async () => {
    await editTitle();
    fireEvent.keyDown(document, { key: 'Escape' });
    fireEvent.click(await screen.findByRole('button', { name: 'common.keepEditing' }));
    await waitFor(() => expect(discardSheet()).toBeNull());
    expect(titleField()).toHaveValue('Standup moved');

    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'common.cancel' }),
    );
    fireEvent.click(await screen.findByRole('button', { name: 'common.discard' }));

    await waitFor(() => expect(uiState.closeEventModal).toHaveBeenCalledTimes(1));
  });

  it('closes without asking after a successful save', async () => {
    calendarState.updateEvent.mockResolvedValue(undefined);
    await editTitle();

    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(uiState.closeEventModal).toHaveBeenCalledTimes(1));
    expect(discardSheet()).toBeNull();
  });
});

describe('EventModal for a reader who may not edit', () => {
  const invited: CalendarEvent = {
    ...event,
    ownerId: 'u2',
    participantIds: ['u1'],
    attendees: [{ userId: 'u1', rsvp: 'pending', canEdit: false }],
  };

  it('viewer can RSVP and expand more options; inputs are readOnly', async () => {
    teamCalendar.role = 'viewer';
    calendarState.events = [invited];
    mockApi.put.mockResolvedValue(undefined);
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(titleField()).toHaveAttribute('readonly');
    expect(screen.queryByRole('button', { name: 'common.save' })).toBeNull();

    const more = screen.getByRole('button', { name: 'event.moreOptions' });
    fireEvent.click(more);
    expect(more).toHaveAttribute('aria-expanded', 'true');
    for (const field of ['event.location', 'event.urlPlaceholder', 'event.memo']) {
      expect(screen.getByPlaceholderText(field)).toHaveAttribute('readonly');
    }
    for (const select of ['event.notification', 'event.visibility', 'event.flexibility']) {
      expect(screen.getByRole('button', { name: select })).toBeDisabled();
    }
    expect(screen.getByRole('button', { name: 'event.recurrence' })).toBeDisabled();

    fireEvent.click(screen.getByRole('button', { name: 'event.rsvpAccepted' }));
    await waitFor(() =>
      expect(mockApi.put).toHaveBeenCalledWith('/calendars/cal-1/events/ev-1/rsvp', {
        rsvp: 'accepted',
      }),
    );
  });

  it('closes without asking, since nothing it shows can be an edit', async () => {
    teamCalendar.role = 'viewer';
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => expect(uiState.closeEventModal).toHaveBeenCalledTimes(1));
  });
});

describe('EventModal task-derived edits', () => {
  it('keeps location and memo editable, and an edit there counts as unsaved', async () => {
    calendarState.events = [{ ...event, taskOwned: true }];
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.click(screen.getByRole('button', { name: 'event.moreOptions' }));
    const location = screen.getByPlaceholderText('event.location');
    const memo = screen.getByPlaceholderText('event.memo');
    expect(location).not.toHaveAttribute('readonly');
    expect(memo).not.toHaveAttribute('readonly');
    fireEvent.change(location, { target: { value: 'Room 4' } });

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(await screen.findByRole('alertdialog', { name: 'common.discardTitle' })).toBeVisible();
  });
});

describe('EventModal owner picker', () => {
  const members: Member[] = [
    { id: 'u1', name: 'Me', email: '', role: 'editor', color: '#111' },
    { id: 'u2', name: 'Bob', email: '', role: 'editor', color: '#222' },
    { id: 'u3', name: 'Cat', email: '', role: 'editor', color: '#333' },
  ];

  beforeEach(() => {
    calendarState.membersMap = { 'cal-1': members };
  });

  afterEach(() => {
    calendarState.membersMap = { 'cal-1': [] };
  });

  async function pickOwner(name: string) {
    fireEvent.click(screen.getByRole('button', { name: 'event.whose' }));
    fireEvent.click(await screen.findByRole('option', { name }));
  }

  it('lets an editor file a new plan under anyone, and says what that gives away', async () => {
    teamCalendar.role = 'editor';
    uiState.editingEventId = null;
    render(<EventModal />);

    expect(screen.queryByText('event.ownerOtherHint')).toBeNull();
    await pickOwner('Bob');

    expect(screen.getByText('event.ownerOtherHint')).toBeInTheDocument();
  });

  it('gives a manager no hint, since they keep the right to edit', async () => {
    uiState.editingEventId = null;
    render(<EventModal />);

    await pickOwner('Bob');

    expect(screen.queryByText('event.ownerOtherHint')).toBeNull();
  });

  it('lets an editor move a plan they own', async () => {
    teamCalendar.role = 'editor';
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(screen.getByRole('button', { name: 'event.whose' })).toBeEnabled();
  });

  it('shows the owner as a row to an editor trusted on somebody else’s plan', async () => {
    teamCalendar.role = 'editor';
    calendarState.events = [
      {
        ...event,
        ownerId: 'u2',
        participantIds: ['u1'],
        attendees: [{ userId: 'u1', rsvp: 'accepted', canEdit: true }],
      },
    ];
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(screen.queryByRole('button', { name: 'event.whose' })).toBeNull();
    expect(screen.getByText('Bob')).toBeInTheDocument();
  });

  it('forgets the chosen owner when the calendar changes', async () => {
    const home = { ...teamCalendar, id: 'cal-2', name: 'Home' };
    calendarState.calendars = [teamCalendar, home];
    calendarState.activeCalendarIds = ['cal-1', 'cal-2'];
    calendarState.membersMap = { 'cal-1': members, 'cal-2': members };
    uiState.editingEventId = null;
    render(<EventModal />);
    fireEvent.change(titleField(), { target: { value: 'Dentist' } });

    await pickOwner('Bob');
    fireEvent.click(screen.getByRole('button', { name: 'event.calendar' }));
    fireEvent.click(await screen.findByRole('option', { name: 'Home' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(calendarState.addEvent).toHaveBeenCalled());
    expect(calendarState.addEvent.mock.calls[0]?.[0]).toBe('cal-2');
    expect(calendarState.addEvent.mock.calls[0]?.[1]).toMatchObject({ ownerId: null });
  });
});

describe('EventModal participants', () => {
  const members: Member[] = [
    { id: 'u1', name: 'Me', email: '', role: 'editor', color: '#111' },
    { id: 'u2', name: 'Bob', email: '', role: 'editor', color: '#222' },
    { id: 'u3', name: 'Cat', email: '', role: 'editor', color: '#333' },
    { id: 'u4', name: 'Dan', email: '', role: 'editor', color: '#444' },
    { id: 'u5', name: 'Eve', email: '', role: 'editor', color: '#555' },
  ];

  beforeEach(() => {
    calendarState.membersMap = { 'cal-1': members };
  });

  afterEach(() => {
    calendarState.membersMap = { 'cal-1': [] };
  });

  const delegated: CalendarEvent = {
    ...event,
    ownerId: 'u2',
    participantIds: ['u1', 'u3'],
    attendees: [
      { userId: 'u1', rsvp: 'pending', canEdit: true },
      { userId: 'u3', rsvp: 'pending', canEdit: false },
    ],
  };

  function removeButtons() {
    const people = screen.getByRole('group', { name: 'event.participants' });
    return within(people).queryAllByRole('button', { name: 'event.removeParticipant' });
  }

  it('offers a delegate no way to take off a participant trusted to edit', async () => {
    teamCalendar.role = 'editor';
    calendarState.events = [delegated];
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(removeButtons()).toHaveLength(1);
  });

  it('lets a manager take off anyone', async () => {
    calendarState.events = [delegated];
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(removeButtons()).toHaveLength(2);
  });

  it('marks each participant with their answer', async () => {
    calendarState.events = [
      {
        ...event,
        participantIds: ['u2', 'u3', 'u4', 'u5'],
        attendees: [
          { userId: 'u2', rsvp: 'accepted', canEdit: false },
          { userId: 'u3', rsvp: 'declined', canEdit: false },
          { userId: 'u4', rsvp: 'tentative', canEdit: false },
          { userId: 'u5', rsvp: 'pending', canEdit: false },
        ],
      },
    ];
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    const people = screen.getByRole('group', { name: 'event.participants' });
    for (const answer of ['event.rsvpAccepted', 'event.rsvpDeclined', 'event.rsvpTentative']) {
      const mark = within(people).getByRole('img', { name: answer });
      expect(mark).toHaveAttribute('title', answer);
    }
    expect(within(people).getAllByRole('img')).toHaveLength(3);
    expect(within(people).getByText('Cat')).toHaveClass('line-through');
    expect(within(people).getByText('Bob')).not.toHaveClass('line-through');
  });
});

describe('EventModal end before start', () => {
  async function typeEnd(time: string) {
    fireEvent.click(screen.getByRole('button', { name: '10:00' }));
    const input = await screen.findByRole('textbox', { name: 'picker.typeTime' });
    fireEvent.change(input, { target: { value: time } });
    fireEvent.keyDown(input, { key: 'Enter' });
  }

  it('reports the error and refuses to save', async () => {
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    await typeEnd('8:00');

    expect(await screen.findByText('event.endBeforeStart')).toBeInTheDocument();
    const save = screen.getByRole('button', { name: 'common.save' });
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(calendarState.updateEvent).not.toHaveBeenCalled();
  });

  it('allows an end equal to the start', async () => {
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    await typeEnd('9:00');

    expect(screen.queryByText('event.endBeforeStart')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    await waitFor(() => expect(calendarState.updateEvent).toHaveBeenCalled());
    const payload = calendarState.updateEvent.mock.calls[0]?.[2] as Record<string, string>;
    expect(DateTime.fromISO(payload.endAt as string).toMillis()).toBe(
      DateTime.fromISO(payload.startAt as string).toMillis(),
    );
  });
});

describe('EventModal immediate sections', () => {
  it('says the sections below save immediately when editing', async () => {
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    expect(screen.getByRole('heading', { name: 'event.savedImmediately' })).toBeInTheDocument();
  });

  it('has no such heading on create, where those sections are absent', () => {
    uiState.editingEventId = null;
    render(<EventModal />);

    expect(screen.queryByRole('heading', { name: 'event.savedImmediately' })).toBeNull();
  });
});

describe('EventModal delete', () => {
  it('asks before deleting a single event', async () => {
    calendarState.deleteEvent.mockResolvedValue(undefined);
    render(<EventModal />);
    await waitFor(() => expect(titleField()).toHaveValue('Standup'));

    fireEvent.click(screen.getByRole('button', { name: 'event.deleteEvent' }));
    const sheet = await screen.findByRole('alertdialog', { name: 'event.deleteConfirmTitle' });
    expect(calendarState.deleteEvent).not.toHaveBeenCalled();

    fireEvent.click(within(sheet).getByRole('button', { name: 'common.delete' }));

    await waitFor(() =>
      expect(calendarState.deleteEvent).toHaveBeenCalledWith('cal-1', 'ev-1', undefined),
    );
  });
});
