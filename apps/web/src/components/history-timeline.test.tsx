import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ja, type TranslationKey } from '@/i18n/ja';

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string }) => string) => selector({ locale: 'en' }),
}));

import { api } from '@/lib/api';
import { HistoryTimeline } from './history-timeline';

const mockApi = vi.mocked(api);

/**
 * One row as the server serialises it: `audit.HistoryItem` in the Go API, and
 * a bare array rather than an `{items}` envelope for the history endpoints.
 *
 * The fixture is declared here in the server's own terms instead of being
 * typed as the component's `HistoryItem`, so a client type that drifts away
 * from the response cannot quietly make these tests agree with it.
 */
interface ServerHistoryItem {
  id: string;
  action: string;
  summary: string;
  createdAt: string;
  actor: { id: string; name: string; avatarUrl?: string } | null;
}

function item(overrides: Partial<ServerHistoryItem> = {}): ServerHistoryItem {
  return {
    id: '00000000-0000-7000-8000-000000000001',
    action: 'calendar.event.updated',
    summary: 'Dinner',
    createdAt: '2026-04-20T10:00:00+09:00',
    actor: { id: '00000000-0000-7000-8000-0000000000a1', name: 'Rin' },
    ...overrides,
  };
}

/**
 * Every event type the log writer can append, paired with the line a reader
 * is meant to see for it. Written out by hand from the Go constants rather
 * than derived from the client's own mapping: a table computed by the code
 * under test agrees with that code by construction and proves nothing.
 */
const ACTIONS: { action: string; label: TranslationKey }[] = [
  { action: 'calendar.event.created', label: 'history.created' },
  { action: 'calendar.event.updated', label: 'history.updated' },
  { action: 'calendar.event.deleted', label: 'history.deleted' },
  { action: 'calendar.memo.created', label: 'history.created' },
  { action: 'calendar.memo.updated', label: 'history.updated' },
  { action: 'calendar.memo.deleted', label: 'history.deleted' },
  { action: 'calendar.member.joined', label: 'activity.joined' },
  { action: 'calendar.member.left', label: 'activity.left' },
  { action: 'calendar.member.removed', label: 'history.deleted' },
  { action: 'calendar.member.role_changed', label: 'activity.roleChanged' },
  { action: 'calendar.invite.created', label: 'history.created' },
  { action: 'calendar.invite.revoked', label: 'activity.revoked' },
  { action: 'calendar.photo.uploaded', label: 'history.created' },
  { action: 'calendar.photo.updated', label: 'history.updated' },
  { action: 'calendar.photo.deleted', label: 'history.deleted' },
  { action: 'calendar.comment.added', label: 'history.created' },
  { action: 'calendar.comment.edited', label: 'history.updated' },
  { action: 'calendar.comment.removed', label: 'history.deleted' },
  { action: 'calendar.checklist.added', label: 'history.created' },
  { action: 'calendar.checklist.updated', label: 'history.updated' },
  { action: 'calendar.checklist.removed', label: 'history.deleted' },
  { action: 'calendar.attachment.added', label: 'history.created' },
  { action: 'calendar.attachment.removed', label: 'history.deleted' },
  { action: 'calendar.created', label: 'history.created' },
  { action: 'calendar.updated', label: 'history.updated' },
  { action: 'calendar.deleted', label: 'history.deleted' },
];

beforeEach(() => {
  mockApi.get.mockReset();
});

afterEach(cleanup);

describe('HistoryTimeline', () => {
  it('reads the event history endpoint for an event and the memo one for a memo', async () => {
    mockApi.get.mockResolvedValue([]);

    render(<HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />);
    await waitFor(() =>
      expect(mockApi.get).toHaveBeenCalledWith('/calendars/cal-1/events/evt-1/history'),
    );

    cleanup();
    mockApi.get.mockClear();

    render(<HistoryTimeline kind="memo" calendarId="cal-1" entityId="memo-1" />);
    await waitFor(() =>
      expect(mockApi.get).toHaveBeenCalledWith('/calendars/cal-1/memos/memo-1/history'),
    );
  });

  it('says it is loading until the history arrives', async () => {
    mockApi.get.mockReturnValue(new Promise(() => {}));

    render(<HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />);

    expect(screen.getByText('history.loading')).toBeInTheDocument();
  });

  it('renders a newest-first API response as an oldest-to-newest timeline', async () => {
    // The endpoint answers with a bare array, newest first.
    mockApi.get.mockResolvedValue([
      item({ id: 'id-3', summary: 'newest', createdAt: '2026-04-22T10:00:00+09:00' }),
      item({ id: 'id-2', summary: 'middle', createdAt: '2026-04-21T10:00:00+09:00' }),
      item({ id: 'id-1', summary: 'oldest', createdAt: '2026-04-20T10:00:00+09:00' }),
    ]);

    const { container } = render(
      <HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />,
    );

    await waitFor(() => expect(screen.getByText('oldest')).toBeInTheDocument());

    const text = container.textContent ?? '';
    expect(text.indexOf('oldest')).toBeLessThan(text.indexOf('middle'));
    expect(text.indexOf('middle')).toBeLessThan(text.indexOf('newest'));
  });

  // A row the client does not recognise renders with an empty label, which
  // reads as a blank gap between the name and the timestamp. Asserting the
  // label a person sees -- rather than that the row exists at all -- is what
  // makes that visible.
  it.each(ACTIONS)('shows $label for $action', async ({ action, label }) => {
    mockApi.get.mockResolvedValue([item({ action, summary: 'Dinner' })]);

    render(<HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />);

    expect(await screen.findByText(label)).toBeInTheDocument();
    // The dotted name is an internal identifier and must never reach the page.
    expect(screen.queryByText(action)).not.toBeInTheDocument();
  });

  it('has a translation for every label the log can produce', () => {
    for (const { action, label } of ACTIONS) {
      expect(ja[label], action).toBeTruthy();
    }
  });

  it('names the actor, and stands in for one the server no longer knows', async () => {
    mockApi.get.mockResolvedValue([
      item({ id: 'id-1', actor: { id: 'u1', name: 'Rin' } }),
      item({ id: 'id-2', actor: null }),
    ]);

    render(<HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />);

    expect(await screen.findByText('Rin')).toBeInTheDocument();
    // Both the avatar and the name fall back, so the label appears twice.
    expect(screen.getAllByText('history.deletedUser').length).toBeGreaterThan(0);
  });

  // Most log entries carry no summary at all -- the payload only has one when
  // the writer recorded a line for it -- so an entry without one still has to
  // read as a row rather than collapse to a bare bullet.
  it('still renders an entry whose log payload carries no summary', async () => {
    mockApi.get.mockResolvedValue([item({ summary: '' })]);

    render(<HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />);

    expect(await screen.findByText('Rin')).toBeInTheDocument();
    expect(screen.getByText('history.updated')).toBeInTheDocument();
  });

  it('shows the empty state for a history with no entries', async () => {
    mockApi.get.mockResolvedValue([]);

    render(<HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />);

    expect(await screen.findByText('history.empty')).toBeInTheDocument();
  });

  // A failed request used to fall through to the empty state, which answers
  // "nobody has touched it" to a question nobody asked. Somebody opening the
  // history is asking who changed their event; "there is no history" is a
  // different and wrong answer to that.
  it('says the history could not be read rather than that there is none', async () => {
    mockApi.get.mockRejectedValue(new Error('network down'));

    render(<HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />);

    expect(await screen.findByText('network down')).toBeInTheDocument();
    expect(screen.queryByText('history.empty')).not.toBeInTheDocument();
    expect(screen.queryByText('history.loading')).not.toBeInTheDocument();
  });
});
