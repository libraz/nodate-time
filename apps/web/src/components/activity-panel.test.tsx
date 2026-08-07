import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ja, type TranslationKey } from '@/i18n/ja';
import type { Calendar } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string }) => unknown) => selector({ locale: 'en' }),
}));

const calendarState = {
  calendars: [] as Calendar[],
  activeCalendarIds: [] as string[],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { ActivityPanel } from './activity-panel';

const mockApi = vi.mocked(api);
const mockToast = vi.mocked(toast);

function calendar(id: string, name: string): Calendar {
  return {
    id,
    name,
    color: '#3b82f6',
    coverUrl: '',
    createdAt: '2026-01-01T00:00:00+09:00',
    publicShared: false,
    role: 'owner',
    memberColor: '#3b82f6',
  };
}

/**
 * One row as the server serialises it: `audit.FeedItem` in the Go API, wrapped
 * in the `{items, nextCursor}` envelope the activity endpoint answers with.
 *
 * Declared in the server's own terms rather than as the component's private
 * `FeedItem`, so a client type drifting away from the response cannot make
 * these tests agree with it.
 */
interface ServerFeedItem {
  id: string;
  action: string;
  summary: string;
  createdAt: string;
  actor: { id: string; name: string; avatarUrl?: string } | null;
  entityType: string;
  entityId: string;
}

function feedItem(overrides: Partial<ServerFeedItem> = {}): ServerFeedItem {
  return {
    id: '00000000-0000-7000-8000-000000000001',
    action: 'calendar.event.created',
    summary: 'Dinner',
    createdAt: '2026-04-20T10:00:00+09:00',
    actor: { id: '00000000-0000-7000-8000-0000000000a1', name: 'Rin' },
    entityType: 'event',
    entityId: '00000000-0000-7000-8000-0000000000e1',
    ...overrides,
  };
}

/**
 * The entity the API derives from each dotted action, and the badge a reader
 * is meant to see for it. Taken from the Go event-type constants, not from
 * the client's own table.
 */
const ENTITIES: { action: string; entityType: string; label: TranslationKey }[] = [
  { action: 'calendar.event.created', entityType: 'event', label: 'activity.entityEvent' },
  { action: 'calendar.memo.updated', entityType: 'memo', label: 'activity.entityMemo' },
  { action: 'calendar.member.joined', entityType: 'member', label: 'activity.entityMember' },
  { action: 'calendar.invite.revoked', entityType: 'invite', label: 'activity.entityInvite' },
  { action: 'calendar.comment.added', entityType: 'comment', label: 'activity.entityComment' },
  {
    action: 'calendar.checklist.updated',
    entityType: 'checklist',
    label: 'activity.entityChecklist',
  },
  {
    action: 'calendar.attachment.added',
    entityType: 'attachment',
    label: 'activity.entityAttachment',
  },
  { action: 'calendar.photo.uploaded', entityType: 'photo', label: 'activity.entityPhoto' },
  { action: 'calendar.updated', entityType: 'calendar', label: 'activity.entityCalendar' },
];

beforeEach(() => {
  mockApi.get.mockReset();
  mockToast.error.mockClear();
  calendarState.calendars = [calendar('cal-1', 'Family')];
  calendarState.activeCalendarIds = ['cal-1'];
});

afterEach(() => {
  cleanup();
  document.body.style.overflow = '';
});

/** jsdom reports no layout, so focusable elements have to look painted. */
function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

describe('ActivityPanel', () => {
  it('says it is loading until the first page arrives', () => {
    mockApi.get.mockReturnValue(new Promise(() => {}));

    render(<ActivityPanel onClose={() => {}} />);

    expect(screen.getByText('history.loading')).toBeInTheDocument();
    expect(mockApi.get).toHaveBeenCalledWith('/calendars/cal-1/activity?limit=50');
  });

  it('renders each entry with who did it, to what, and what changed', async () => {
    mockApi.get.mockResolvedValue({
      items: [feedItem({ summary: 'Dinner with Rin' })],
      nextCursor: undefined,
    });

    render(<ActivityPanel onClose={() => {}} />);

    expect(await screen.findByText('Rin')).toBeInTheDocument();
    expect(screen.getByText('activity.entityEvent')).toBeInTheDocument();
    expect(screen.getByText('history.created')).toBeInTheDocument();
    expect(screen.getByText('Dinner with Rin')).toBeInTheDocument();
    // The dotted name is an internal identifier and must never reach the page.
    expect(screen.queryByText('calendar.event.created')).not.toBeInTheDocument();
  });

  it.each(ENTITIES)('badges a $action row as $label', async ({ action, entityType, label }) => {
    mockApi.get.mockResolvedValue({ items: [feedItem({ action, entityType })] });

    render(<ActivityPanel onClose={() => {}} />);

    expect(await screen.findByText(label)).toBeInTheDocument();
    expect(screen.queryByText(entityType)).not.toBeInTheDocument();
  });

  it('has a translation for every badge the feed can produce', () => {
    for (const { entityType, label } of ENTITIES) {
      expect(ja[label], entityType).toBeTruthy();
    }
  });

  // The feed reads the entity out of the action name, so a kind added on the
  // server reaches this panel before the client knows a word for it. Naming it
  // as itself keeps the row readable instead of dropping the badge.
  it('names an entity it has no word for rather than leaving the row bare', async () => {
    mockApi.get.mockResolvedValue({
      items: [feedItem({ action: 'calendar.reminder.created', entityType: 'reminder' })],
    });

    render(<ActivityPanel onClose={() => {}} />);

    expect(await screen.findByText('reminder')).toBeInTheDocument();
    expect(screen.getByText('history.created')).toBeInTheDocument();
  });

  it('shows the empty state for a calendar with no activity', async () => {
    mockApi.get.mockResolvedValue({ items: [] });

    render(<ActivityPanel onClose={() => {}} />);

    expect(await screen.findByText('activity.empty')).toBeInTheDocument();
  });

  it('asks for nothing when no calendar is in view', () => {
    calendarState.activeCalendarIds = [];

    render(<ActivityPanel onClose={() => {}} />);

    expect(screen.getByText('activity.empty')).toBeInTheDocument();
    expect(mockApi.get).not.toHaveBeenCalled();
  });

  it('reports a failed load instead of leaving the panel spinning', async () => {
    mockApi.get.mockRejectedValue(new Error('feed unavailable'));

    render(<ActivityPanel onClose={() => {}} />);

    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith('feed unavailable'));
    expect(screen.queryByText('history.loading')).not.toBeInTheDocument();
    expect(screen.getByText('activity.empty')).toBeInTheDocument();
  });

  it('pages with the cursor the server handed back and appends the next page', async () => {
    const cursor = '00000000-0000-7000-8000-0000000000c1';
    mockApi.get
      .mockResolvedValueOnce({
        items: [feedItem({ id: 'row-1', summary: 'first page' })],
        nextCursor: cursor,
      })
      .mockResolvedValueOnce({
        items: [feedItem({ id: 'row-2', summary: 'second page' })],
      });

    render(<ActivityPanel onClose={() => {}} />);

    fireEvent.click(await screen.findByText('activity.loadMore'));

    expect(await screen.findByText('second page')).toBeInTheDocument();
    // The first page stays: paging adds to the feed rather than replacing it.
    expect(screen.getByText('first page')).toBeInTheDocument();
    expect(mockApi.get).toHaveBeenLastCalledWith(
      `/calendars/cal-1/activity?limit=50&cursor=${cursor}`,
    );
    // The last page carries no cursor, so there is nothing left to ask for.
    await waitFor(() => expect(screen.queryByText('activity.loadMore')).not.toBeInTheDocument());
  });

  it('keeps the loaded feed on screen when paging fails', async () => {
    mockApi.get
      .mockResolvedValueOnce({
        items: [feedItem({ summary: 'first page' })],
        nextCursor: '00000000-0000-7000-8000-0000000000c1',
      })
      .mockRejectedValueOnce(new Error('paging failed'));

    render(<ActivityPanel onClose={() => {}} />);

    fireEvent.click(await screen.findByText('activity.loadMore'));

    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith('paging failed'));
    expect(screen.getByText('first page')).toBeInTheDocument();
    expect(screen.getByText('activity.loadMore')).toBeInTheDocument();
  });

  it('offers only the calendars in view, and refetches when one is picked', async () => {
    calendarState.calendars = [
      calendar('cal-1', 'Family'),
      calendar('cal-2', 'Work'),
      calendar('cal-3', 'Hidden'),
    ];
    calendarState.activeCalendarIds = ['cal-1', 'cal-2'];
    mockApi.get.mockResolvedValue({ items: [] });

    render(<ActivityPanel onClose={() => {}} />);

    const picker = await screen.findByRole('combobox');
    expect(screen.getByRole('option', { name: 'Family' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Work' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Hidden' })).not.toBeInTheDocument();

    fireEvent.change(picker, { target: { value: 'cal-2' } });

    await waitFor(() =>
      expect(mockApi.get).toHaveBeenCalledWith('/calendars/cal-2/activity?limit=50'),
    );
  });

  it('names the single calendar in view instead of offering a choice', async () => {
    mockApi.get.mockResolvedValue({ items: [] });

    render(<ActivityPanel onClose={() => {}} />);

    await waitFor(() => expect(mockApi.get).toHaveBeenCalled());
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    expect(screen.getByText('Family')).toBeInTheDocument();
  });

  it('closes from the header button and from the backdrop', async () => {
    mockApi.get.mockResolvedValue({ items: [] });
    const onClose = vi.fn();

    render(<ActivityPanel onClose={onClose} />);

    const closers = screen.getAllByLabelText('common.close');
    expect(closers).toHaveLength(2);
    for (const closer of closers) fireEvent.click(closer);
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});

// The feed covers the page but had none of what that owes the keyboard: Tab
// walked into the calendar behind it and Escape did nothing.
describe('ActivityPanel keyboard', () => {
  it('announces itself as a modal dialog', async () => {
    mockApi.get.mockResolvedValue({ items: [] });

    render(<ActivityPanel onClose={() => {}} />);

    const panel = await screen.findByRole('dialog');
    expect(panel).toHaveAttribute('aria-modal', 'true');
    expect(panel).toHaveAccessibleName('activity.title');
  });

  it('locks the page behind it and closes on Escape', () => {
    mockApi.get.mockResolvedValue({ items: [] });
    const onClose = vi.fn();

    render(<ActivityPanel onClose={onClose} />);

    expect(document.body.style.overflow).toBe('hidden');

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('keeps Tab inside the panel', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    mockApi.get.mockResolvedValue({
      items: [feedItem()],
      nextCursor: '00000000-0000-7000-8000-0000000000c1',
    });

    render(<ActivityPanel onClose={() => {}} />);

    const panel = await screen.findByRole('dialog');
    await screen.findByText('activity.loadMore');
    const focusable = Array.from(panel.querySelectorAll<HTMLElement>('button,input,select'));
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

    vi.restoreAllMocks();
  });

  it('returns focus to whatever opened it', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    mockApi.get.mockResolvedValue({ items: [] });

    function Harness({ open }: { open: boolean }) {
      return (
        <>
          <button type="button" data-testid="trigger">
            open
          </button>
          {open && <ActivityPanel onClose={() => {}} />}
        </>
      );
    }
    const { rerender } = render(<Harness open={false} />);
    const trigger = screen.getByTestId('trigger');
    trigger.focus();

    rerender(<Harness open={true} />);
    await waitFor(() => expect(document.activeElement).not.toBe(trigger));

    rerender(<Harness open={false} />);

    expect(document.activeElement).toBe(trigger);
    vi.restoreAllMocks();
  });
});
