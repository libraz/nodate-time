import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
  ApiError: class ApiError extends Error {},
  setToken: vi.fn(),
  endSession: vi.fn(),
  hasToken: vi.fn(() => true),
  errorMessage: () => 'error',
  isSessionStorageEvent: (e: StorageEvent) =>
    e.key === null || e.key === 'tt_token' || e.key === 'tt_refresh',
  // biome-ignore lint/style/useNamingConvention: must mirror the real module export
  SESSION_EXPIRED_EVENT: 'nodate:session-expired',
}));

vi.mock('@/lib/client', () => ({
  // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
  client: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
  createApiClient: vi.fn(),
}));
vi.mock('@/i18n', () => ({ getT: () => (key: string) => key }));
vi.mock('@/lib/image-resize', () => ({ resizeImageForAvatar: vi.fn() }));

import { hasToken } from '@/lib/api';
import { client } from '@/lib/client';
import { useCalendarStore } from '@/stores/calendar-store';
import { useSchedulePollStore } from '@/stores/schedule-poll-store';
import { useUiStore } from '@/stores/ui-store';
import { useAuthStore } from './auth-store';

const alice = {
  id: 'u1',
  name: 'Alice',
  email: 'a@x.test',
  locale: 'ja',
  timezone: 'Asia/Tokyo',
  hasPassword: true,
};
const bob = {
  id: 'u2',
  name: 'Bob',
  email: 'b@x.test',
  locale: 'ja',
  timezone: 'Asia/Tokyo',
  hasPassword: true,
};

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(hasToken).mockReturnValue(true);
  useAuthStore.setState({ user: alice, isAuthenticated: true, isInitializing: false });
  useSchedulePollStore.getState().reset();
});

describe('another tab changing the stored session', () => {
  it('logs this tab out when the token is removed elsewhere', () => {
    vi.mocked(hasToken).mockReturnValue(false);
    const resetData = vi.spyOn(useCalendarStore.getState(), 'resetSessionData');

    window.dispatchEvent(new StorageEvent('storage', { key: 'tt_token', newValue: null }));

    const s = useAuthStore.getState();
    expect(s.user).toBeNull();
    expect(s.isAuthenticated).toBe(false);
    expect(resetData).toHaveBeenCalled();
  });

  it('drops the previous account data when the token now belongs to someone else', async () => {
    vi.mocked(client.GET).mockResolvedValue({ data: bob } as never);
    const resetData = vi.spyOn(useCalendarStore.getState(), 'resetSessionData');
    const resetUi = vi.spyOn(useUiStore.getState(), 'resetSessionUi');
    useSchedulePollStore.setState({
      accountId: alice.id,
      actions: [
        {
          calendarId: 'cal-1',
          calendarName: 'Calendar',
          pollId: 'poll-1',
          title: 'Meeting',
          kind: 'answer',
        },
      ],
    });

    window.dispatchEvent(new StorageEvent('storage', { key: 'tt_token', newValue: 'other' }));
    await settle();

    expect(client.GET).toHaveBeenCalledWith('/user');
    expect(useAuthStore.getState().user).toEqual(bob);
    expect(resetData).toHaveBeenCalled();
    expect(resetUi).toHaveBeenCalled();
    expect(useSchedulePollStore.getState()).toMatchObject({ accountId: null, actions: [] });
  });

  it('keeps this tab untouched when the same account was merely renewed', async () => {
    vi.mocked(client.GET).mockResolvedValue({ data: alice } as never);
    const resetData = vi.spyOn(useCalendarStore.getState(), 'resetSessionData');

    window.dispatchEvent(new StorageEvent('storage', { key: 'tt_token', newValue: 'renewed' }));
    await settle();

    expect(useAuthStore.getState().user).toEqual(alice);
    expect(resetData).not.toHaveBeenCalled();
  });

  it('ignores unrelated storage keys', async () => {
    window.dispatchEvent(new StorageEvent('storage', { key: 'tt_theme', newValue: 'dark' }));
    await settle();
    expect(client.GET).not.toHaveBeenCalled();
  });
});
