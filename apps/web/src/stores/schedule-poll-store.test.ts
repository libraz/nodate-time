import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { SchedulePollAction } from '@/lib/schedule-polls';

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
  errorMessage: (error: unknown) => String(error),
  isAbortError: (error: unknown) => error instanceof Error && error.name === 'AbortError',
}));

import { api } from '@/lib/api';
import { useSchedulePollStore } from './schedule-poll-store';

const action = (title: string): SchedulePollAction => ({
  calendarId: 'cal-1',
  calendarName: 'Calendar',
  pollId: title,
  title,
  kind: 'answer',
});

beforeEach(() => {
  vi.clearAllMocks();
  useSchedulePollStore.getState().reset();
});

describe('schedule poll action cache', () => {
  it('does not publish a late response from the previous account', async () => {
    let finishAlice: ((value: { actions: SchedulePollAction[] }) => void) | undefined;
    vi.mocked(api.get)
      .mockReturnValueOnce(
        new Promise((resolve) => {
          finishAlice = resolve;
        }) as never,
      )
      .mockResolvedValueOnce({ actions: [action('Bob action')] } as never);

    const aliceRequest = useSchedulePollStore.getState().fetchActions('alice');
    const bobRequest = useSchedulePollStore.getState().fetchActions('bob');
    await bobRequest;
    finishAlice?.({ actions: [action('Alice action')] });
    await aliceRequest;

    expect(useSchedulePollStore.getState()).toMatchObject({
      accountId: 'bob',
      actions: [action('Bob action')],
      isLoading: false,
    });
  });

  it('keeps a reset clear when a request completes after logout', async () => {
    let finishRequest: ((value: { actions: SchedulePollAction[] }) => void) | undefined;
    vi.mocked(api.get).mockReturnValueOnce(
      new Promise((resolve) => {
        finishRequest = resolve;
      }) as never,
    );

    const request = useSchedulePollStore.getState().fetchActions('alice');
    useSchedulePollStore.getState().reset();
    finishRequest?.({ actions: [action('Old action')] });
    await request;

    expect(useSchedulePollStore.getState()).toMatchObject({ accountId: null, actions: [] });
  });
});
