import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';

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
  participants: [],
  recurrenceRule: null,
  isRecurrence: false,
  recurrenceDate: null,
  createdBy: 'u1',
  createdAt: '2026-08-01T00:00:00Z',
  updatedAt: '2026-08-01T00:00:00Z',
};

const uiState = {
  locale: 'ja' as const,
  timezone: 'Asia/Tokyo',
  showEventModal: true,
  editingEventId: 'ev-1' as string | null,
  closeEventModal: vi.fn(),
  selectedDate: DateTime.fromISO('2026-08-07T00:00:00', { zone: 'Asia/Tokyo' }),
  eventDraftStart: null,
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState = {
  calendars: [
    {
      id: 'cal-1',
      name: 'Team',
      color: '#000',
      coverUrl: '',
      createdAt: '',
      publicShared: false,
      role: 'owner',
      memberColor: '#000',
    },
  ],
  activeCalendarIds: ['cal-1'],
  events: [event],
  addEvent: vi.fn(),
  updateEvent: vi.fn(),
  deleteEvent: vi.fn(),
  membersMap: { 'cal-1': [] },
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
                userName: 'Someone',
                userPublicId: 'u2',
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
        userName: 'Someone',
        userPublicId: 'u2',
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

  // userAvatar is a URL, and rendering it as the contents of the avatar box
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
                userName: 'Someone',
                userPublicId: 'u2',
                userAvatar: avatar,
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

  it('focuses the title field on open', async () => {
    render(<EventModal />);

    await waitFor(() => expect(document.activeElement).toBe(titleField()));
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
});
