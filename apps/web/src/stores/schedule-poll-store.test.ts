import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { SchedulePollAction } from '@/lib/schedule-polls';

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (error: unknown) => String(error),
  isAbortError: (error: unknown) => error instanceof Error && error.name === 'AbortError',
  // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
  client: { GET: vi.fn() },
}));

import { client } from '@/lib/client';
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
    let finishAlice: ((value: SchedulePollAction[]) => void) | undefined;
    vi.mocked(client.GET)
      .mockReturnValueOnce(
        new Promise((resolve) => {
          finishAlice = (items) => resolve({ data: items });
        }) as never,
      )
      .mockResolvedValueOnce({ data: [action('Bob action')] } as never);

    const aliceRequest = useSchedulePollStore.getState().fetchActions('alice');
    const bobRequest = useSchedulePollStore.getState().fetchActions('bob');
    await bobRequest;
    finishAlice?.([action('Alice action')]);
    await aliceRequest;

    expect(client.GET).toHaveBeenCalledWith('/user/schedule-poll-actions', {});
    expect(useSchedulePollStore.getState()).toMatchObject({
      accountId: 'bob',
      actions: [action('Bob action')],
      isLoading: false,
    });
  });

  it('keeps a reset clear when a request completes after logout', async () => {
    let finishRequest: ((value: SchedulePollAction[]) => void) | undefined;
    vi.mocked(client.GET).mockReturnValueOnce(
      new Promise((resolve) => {
        finishRequest = (items) => resolve({ data: items });
      }) as never,
    );

    const request = useSchedulePollStore.getState().fetchActions('alice');
    useSchedulePollStore.getState().reset();
    finishRequest?.([action('Old action')]);
    await request;

    expect(useSchedulePollStore.getState()).toMatchObject({ accountId: null, actions: [] });
  });
});
