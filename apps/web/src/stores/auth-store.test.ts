import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => {
  class ApiError extends Error {
    constructor(
      public status: number,
      public detail: string,
    ) {
      super(detail);
    }
  }
  return {
    // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported class name
    ApiError,
    api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
    setToken: vi.fn(),
    clearToken: vi.fn(),
    hasToken: vi.fn(() => false),
  };
});

vi.mock('@/i18n', () => ({
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/image-resize', () => ({
  resizeImageForAvatar: vi.fn(),
}));

import { ApiError, api, clearToken, hasToken, setToken } from '@/lib/api';
import { useCalendarStore } from '@/stores/calendar-store';
import { useAuthStore } from './auth-store';

const mockApi = vi.mocked(api);
const mockSetToken = vi.mocked(setToken);
const mockClearToken = vi.mocked(clearToken);
const mockHasToken = vi.mocked(hasToken);

const sampleUser = {
  id: 'u1',
  name: 'Alice',
  email: 'alice@example.com',
  icon: 'A',
  color: '#000',
};

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockHasToken.mockReturnValue(false);
  useAuthStore.setState({
    user: null,
    isAuthenticated: false,
    isInitializing: false,
    isLoading: false,
    error: null,
  });
});

describe('login', () => {
  it('stores the token and marks the user authenticated', async () => {
    mockApi.post.mockResolvedValue({ token: 'tok', user: sampleUser } as never);

    await useAuthStore.getState().login('alice@example.com', 'pw');

    expect(mockSetToken).toHaveBeenCalledWith('tok');
    const s = useAuthStore.getState();
    expect(s.isAuthenticated).toBe(true);
    expect(s.user).toEqual(sampleUser);
    expect(s.isLoading).toBe(false);
  });

  it('records the error detail and rethrows on failure', async () => {
    mockApi.post.mockRejectedValue(new ApiError(401, 'Invalid credentials'));

    await expect(useAuthStore.getState().login('a@b.c', 'bad')).rejects.toBeInstanceOf(ApiError);

    const s = useAuthStore.getState();
    expect(s.isAuthenticated).toBe(false);
    expect(s.isLoading).toBe(false);
    expect(s.error).toBe('Invalid credentials');
  });

  it('falls back to a generic message for non-ApiError failures', async () => {
    mockApi.post.mockRejectedValue(new Error('network'));

    await expect(useAuthStore.getState().login('a@b.c', 'pw')).rejects.toThrow();

    expect(useAuthStore.getState().error).toBe('auth.loginFailed');
  });
});

describe('logout', () => {
  it('clears the token, tt_ keys, and resets the calendar store', () => {
    localStorage.setItem('tt_token', 'tok');
    localStorage.setItem('tt_activeCalendarIds', '["cal-1"]');
    localStorage.setItem('unrelated', 'keep');
    useCalendarStore.setState({
      calendars: [
        { id: 'cal-1', name: 'A', color: '#000', coverUrl: '', createdAt: '', publicShared: false },
      ],
      events: [],
      memos: [],
      activeCalendarIds: ['cal-1'],
    });
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });

    useAuthStore.getState().logout();

    expect(mockClearToken).toHaveBeenCalled();
    expect(localStorage.getItem('tt_activeCalendarIds')).toBeNull();
    expect(localStorage.getItem('unrelated')).toBe('keep');
    const auth = useAuthStore.getState();
    expect(auth.user).toBeNull();
    expect(auth.isAuthenticated).toBe(false);
    expect(useCalendarStore.getState().calendars).toEqual([]);
  });
});

describe('fetchMe', () => {
  it('does nothing when there is no token', async () => {
    mockHasToken.mockReturnValue(false);

    await useAuthStore.getState().fetchMe();

    expect(mockApi.get).not.toHaveBeenCalled();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });

  it('hydrates the user when the token is valid', async () => {
    mockHasToken.mockReturnValue(true);
    mockApi.get.mockResolvedValue(sampleUser as never);

    await useAuthStore.getState().fetchMe();

    const s = useAuthStore.getState();
    expect(s.user).toEqual(sampleUser);
    expect(s.isAuthenticated).toBe(true);
    expect(s.isInitializing).toBe(false);
  });

  it('clears the session when the token is rejected', async () => {
    mockHasToken.mockReturnValue(true);
    mockApi.get.mockRejectedValue(new ApiError(401, 'expired'));

    await useAuthStore.getState().fetchMe();

    expect(mockClearToken).toHaveBeenCalled();
    const s = useAuthStore.getState();
    expect(s.user).toBeNull();
    expect(s.isAuthenticated).toBe(false);
    expect(s.isInitializing).toBe(false);
  });
});
