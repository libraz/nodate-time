import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  getResponse: vi.fn(),
  refresh: vi.fn(),
  respond: vi.fn(),
  finalize: vi.fn(),
  cancel: vi.fn(),
  update: vi.fn(),
  navigate: vi.fn(),
  search: { view: 'manage' },
}));

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: unknown) => ({
    options,
    useParams: () => ({ calendarId: 'c1', pollId: 'p1' }),
    useSearch: () => mocks.search,
  }),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ children }: { children?: unknown }) => children,
  useNavigate: () => mocks.navigate,
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/schedule-polls', () => ({ schedulePollApi: mocks }));
vi.mock('@/lib/use-calendar-members', () => ({ useCalendarMembers: () => ({ members: [] }) }));
vi.mock('@/lib/use-unsaved-guard', () => ({
  useUnsavedGuard: () => ({ blocked: false, proceed: vi.fn(), stay: vi.fn() }),
}));
vi.mock('@/lib/toast', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: unknown) => unknown) => selector({ user: { id: 'u1' } }),
}));
vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: unknown) => unknown) => selector({ locale: 'en' }),
}));
vi.mock('@/stores/schedule-poll-store', () => ({
  useSchedulePollStore: (selector: (s: unknown) => unknown) => selector({ fetchActions: vi.fn() }),
}));
vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: { getState: () => ({ fetchMembers: vi.fn(), membersMap: {} }) },
}));
vi.mock('@/components/schedule-poll-candidate-finder', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  SchedulePollCandidateFinder: () => null,
}));

import { ApiError } from '@/lib/client';
import { Route } from './schedule-polls_.$calendarId.$pollId';

const Page = (Route as unknown as { options: { component: () => React.ReactNode } }).options
  .component;

function poll(over: Record<string, unknown> = {}) {
  return {
    id: 'p1',
    etag: 'v3.r1',
    calendarId: 'c1',
    title: 'Lunch',
    timezone: 'UTC',
    status: 'open',
    canManage: true,
    canRespond: false,
    slots: [
      {
        id: 's1',
        startAt: '2030-01-01T10:00:00Z',
        endAt: '2030-01-01T11:00:00Z',
        availability: 'open',
        active: true,
      },
    ],
    members: [
      { id: 'u1', name: 'Me' },
      { id: 'u2', name: 'You' },
    ],
    myAnswers: [],
    responses: [{ memberId: 'u1', answers: [{ slotId: 's1', value: 'yes' }] }],
    respondedCount: 1,
    memberCount: 2,
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.search.view = 'manage';
  mocks.get.mockResolvedValue(poll());
  mocks.refresh.mockImplementation(async () => poll());
  mocks.finalize.mockResolvedValue(poll({ status: 'finalized' }));
});

afterEach(cleanup);

const finalizeButton = () => screen.findByText('schedulePoll.finalizeAndCreateEvent');

describe('finalizing a poll', () => {
  it('asks before finalizing while recipients have not answered, then sends the poll etag', async () => {
    render(<Page />);
    fireEvent.click(await finalizeButton());
    expect(await screen.findByText('poll.finalizeWithoutPending')).toBeTruthy();
    expect(mocks.finalize).not.toHaveBeenCalled();

    const confirm = screen.getAllByText('schedulePoll.finalizeAndCreateEvent').at(-1);
    fireEvent.click(confirm as HTMLElement);
    await waitFor(() => expect(mocks.finalize).toHaveBeenCalledTimes(1));
    expect(mocks.finalize).toHaveBeenCalledWith(
      'c1',
      'p1',
      'v3.r1',
      expect.objectContaining({ slotId: 's1' }),
    );
  });

  it('does not finalize when the confirmation is dismissed', async () => {
    render(<Page />);
    fireEvent.click(await finalizeButton());
    fireEvent.click(await screen.findByText('common.cancel'));
    expect(mocks.finalize).not.toHaveBeenCalled();
  });

  it('finalizes without asking once everyone has answered', async () => {
    mocks.get.mockResolvedValue(poll({ respondedCount: 2 }));
    mocks.refresh.mockImplementation(async () => poll({ respondedCount: 2 }));
    render(<Page />);
    fireEvent.click(await finalizeButton());
    await waitFor(() => expect(mocks.finalize).toHaveBeenCalledTimes(1));
    expect(screen.queryByText('poll.finalizeWithoutPending')).toBeNull();
  });

  it('shows conflict.stale and refetches after a stale finalize', async () => {
    mocks.get.mockResolvedValue(poll({ respondedCount: 2 }));
    mocks.refresh.mockImplementation(async () => poll({ respondedCount: 2 }));
    mocks.finalize.mockRejectedValue(new ApiError(412, 'stale', 'SCHEDULE_POLL.STALE'));
    render(<Page />);
    await finalizeButton();
    const reads = mocks.get.mock.calls.length;
    fireEvent.click(await finalizeButton());
    expect(await screen.findByText('conflict.stale')).toBeTruthy();
    expect(mocks.get.mock.calls.length).toBeGreaterThan(reads);
  });
});

describe('answering a poll', () => {
  it('PUTs with the etag read from GET .../response and refetches the poll', async () => {
    const answerer = poll({ canManage: false, canRespond: true, respondedCount: 0 });
    mocks.get.mockResolvedValue(answerer);
    mocks.refresh.mockResolvedValue(answerer);
    mocks.search.view = 'respond';
    mocks.getResponse.mockResolvedValue({ etag: 'a7', answers: [] });
    mocks.respond.mockResolvedValue({ etag: 'a8', answers: [{ slotId: 's1', value: 'yes' }] });
    render(<Page />);
    const yes = await screen.findByRole('radio', { name: /schedulePoll\.answerYes$/ });
    fireEvent.click(yes);
    fireEvent.click(await screen.findByText('schedulePoll.saveAnswers'));
    await waitFor(() => expect(mocks.respond).toHaveBeenCalledTimes(1));
    expect(mocks.respond).toHaveBeenCalledWith('c1', 'p1', 'a7', [{ slotId: 's1', value: 'yes' }]);
    await waitFor(() => expect(mocks.get.mock.calls.length).toBeGreaterThan(1));
  });
});
