import { DateTime } from 'luxon';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => {
  class ApiError extends Error {
    constructor(
      public status: number,
      public detail: string,
      public code = '',
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
    errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error.generic'),
    // biome-ignore lint/style/useNamingConvention: must mirror the real module export
    SESSION_EXPIRED_EVENT: 'nodate:session-expired',
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
import { useUiStore } from '@/stores/ui-store';
import { useAuthStore } from './auth-store';

const mockApi = vi.mocked(api);
const mockSetToken = vi.mocked(setToken);
const mockClearToken = vi.mocked(clearToken);
const mockHasToken = vi.mocked(hasToken);

const sampleUser = {
  id: 'u1',
  name: 'Alice',
  email: 'alice@example.com',
  locale: 'ja',
  timezone: 'Asia/Tokyo',
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
  useUiStore.setState({
    showEventModal: false,
    editingEventId: null,
    eventDraftStart: null,
    showDayDetail: false,
    rightPanel: null,
    showSearch: false,
    searchQuery: '',
    mobileTab: 'calendar',
    showSettings: false,
    showMobileMenu: false,
    showActivity: false,
  });
});

describe('login', () => {
  it('stores the token and marks the user authenticated', async () => {
    mockApi.post.mockResolvedValue({
      token: 'tok',
      refreshToken: 'refresh-tok',
      user: sampleUser,
    } as never);

    await useAuthStore.getState().login('alice@example.com', 'pw');

    // The refresh token is stored alongside: without it the sign-in ends when
    // the access token does, whatever the server was willing to allow.
    expect(mockSetToken).toHaveBeenCalledWith('tok', 'refresh-tok');
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
  // Forgetting the token locally is not signing out. Anyone who copied the
  // storage of a shared machine keeps working access until it expires, which
  // is the whole day the user believed they had just ended.
  it('ends the session on the server too', () => {
    mockHasToken.mockReturnValue(true);
    mockApi.post.mockResolvedValue(undefined as never);

    useAuthStore.getState().logout();

    expect(mockApi.post).toHaveBeenCalledWith('/auth/logout');
  });

  it('does not call the server when there was nothing to end', () => {
    mockHasToken.mockReturnValue(false);

    useAuthStore.getState().logout();

    expect(mockApi.post).not.toHaveBeenCalled();
  });

  it('clears the token, tt_ keys, and resets the calendar store', () => {
    localStorage.setItem('tt_token', 'tok');
    localStorage.setItem('tt_activeCalendarIds', '["cal-1"]');
    localStorage.setItem('unrelated', 'keep');
    useCalendarStore.setState({
      calendars: [
        {
          id: 'cal-1',
          name: 'A',
          color: '#000',
          coverUrl: '',
          createdAt: '',
          publicShared: false,
          role: 'owner',
          memberColor: '#000',
        },
      ],
      events: [],
      memos: [],
      activeCalendarIds: ['cal-1'],
    });
    useUiStore.setState({
      showEventModal: true,
      editingEventId: 'evt-1',
      eventDraftStart: DateTime.fromISO('2026-04-20T10:00:00+09:00'),
      showDayDetail: true,
      rightPanel: 'memo',
      showSearch: true,
      searchQuery: 'draft',
      mobileTab: 'memo',
      showSettings: true,
      showMobileMenu: true,
      showActivity: true,
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
    expect(useUiStore.getState()).toMatchObject({
      showEventModal: false,
      editingEventId: null,
      eventDraftStart: null,
      showDayDetail: false,
      rightPanel: null,
      showSearch: false,
      searchQuery: '',
      mobileTab: 'calendar',
      showSettings: false,
      showMobileMenu: false,
      showActivity: false,
    });
  });

  it('clears account-owned state when the API expires the session', () => {
    localStorage.setItem('tt_token', 'tok');
    localStorage.setItem('tt_activeCalendarIds', '["cal-1"]');
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });
    useCalendarStore.setState({
      calendars: [
        {
          id: 'cal-1',
          name: 'A',
          color: '#000',
          coverUrl: '',
          createdAt: '',
          publicShared: false,
          role: 'owner',
          memberColor: '#000',
        },
      ],
      events: [],
      memos: [],
      membersMap: { 'cal-1': [] },
      labels: [{ id: 'label-1', nameKey: 'work', color: '#000' }],
      activeCalendarIds: ['cal-1'],
    });

    window.dispatchEvent(new Event('nodate:session-expired'));

    expect(mockClearToken).toHaveBeenCalled();
    expect(localStorage.getItem('tt_activeCalendarIds')).toBeNull();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
    expect(useCalendarStore.getState()).toMatchObject({
      calendars: [],
      events: [],
      memos: [],
      membersMap: {},
      labels: [],
      activeCalendarIds: [],
    });
  });
});

describe('changePassword', () => {
  it('persists the rotated token so the device stays signed in', async () => {
    mockApi.put.mockResolvedValue({
      token: 'rotated-token',
      refreshToken: 'rotated-refresh',
    } as never);

    await useAuthStore.getState().changePassword('old-pw', 'new-pw-1234');

    expect(mockApi.put).toHaveBeenCalledWith('/user/password', {
      currentPassword: 'old-pw',
      newPassword: 'new-pw-1234',
    });
    expect(mockSetToken).toHaveBeenCalledWith('rotated-token', 'rotated-refresh');
    // The current session is preserved, not cleared.
    expect(mockClearToken).not.toHaveBeenCalled();
  });

  it('does not persist a token and rethrows when the change fails', async () => {
    mockApi.put.mockRejectedValue(new ApiError(400, 'wrong password'));

    await expect(
      useAuthStore.getState().changePassword('bad', 'new-pw-1234'),
    ).rejects.toBeInstanceOf(ApiError);

    expect(mockSetToken).not.toHaveBeenCalled();
  });
});

describe('visibilitychange', () => {
  it('logs out when the tab refocuses with an expired token', () => {
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      configurable: true,
    });
    mockHasToken.mockReturnValue(false);
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });

    document.dispatchEvent(new Event('visibilitychange'));

    expect(mockClearToken).toHaveBeenCalled();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });

  it('leaves a valid session untouched on refocus', () => {
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      configurable: true,
    });
    mockHasToken.mockReturnValue(true);
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });

    document.dispatchEvent(new Event('visibilitychange'));

    expect(mockClearToken).not.toHaveBeenCalled();
    expect(useAuthStore.getState().isAuthenticated).toBe(true);
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

  it('does not restore isAuthenticated when logout lands during an in-flight request', async () => {
    mockHasToken.mockReturnValue(true);
    let resolveUser: ((user: typeof sampleUser) => void) | undefined;
    mockApi.get.mockReturnValue(
      new Promise<typeof sampleUser>((resolve) => {
        resolveUser = resolve;
      }) as never,
    );
    useAuthStore.setState({ isAuthenticated: true, isInitializing: true });

    const request = useAuthStore.getState().fetchMe();
    // Logout clears the token and auth flag mid-flight.
    mockHasToken.mockReturnValue(false);
    useAuthStore.setState({ user: null, isAuthenticated: false });
    resolveUser?.(sampleUser);
    await request;

    const s = useAuthStore.getState();
    expect(s.user).toBeNull();
    expect(s.isAuthenticated).toBe(false);
  });

  it('keeps the current session when user hydration fails with a server error', async () => {
    mockHasToken.mockReturnValue(true);
    mockApi.get.mockRejectedValue(new ApiError(500, 'temporary outage'));
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true, isInitializing: true });

    await useAuthStore.getState().fetchMe();

    expect(mockClearToken).not.toHaveBeenCalled();
    const s = useAuthStore.getState();
    expect(s.user).toEqual(sampleUser);
    expect(s.isAuthenticated).toBe(true);
    expect(s.isInitializing).toBe(false);
  });

  it('says why the profile is missing, and recovers on a second attempt', async () => {
    mockHasToken.mockReturnValue(true);
    mockApi.get.mockRejectedValueOnce(new ApiError(500, 'temporary outage'));
    useAuthStore.setState({ user: null, isAuthenticated: true, isInitializing: true });

    await useAuthStore.getState().fetchMe();

    // A failed hydration used to leave a signed-in shell with no user behind
    // it, which is indistinguishable from an account with no permissions.
    expect(useAuthStore.getState().user).toBeNull();
    expect(useAuthStore.getState().initError).toBe('temporary outage');

    mockApi.get.mockResolvedValueOnce(sampleUser as never);
    await useAuthStore.getState().fetchMe();

    const s = useAuthStore.getState();
    expect(s.user).toEqual(sampleUser);
    expect(s.initError).toBeNull();
  });
});

/**
 * Language and timezone belong to the account. A timezone that lives only in
 * one browser makes the same calendar say different times on a phone and a
 * laptop, and neither of them is wrong from where it is standing.
 */
describe('account preferences', () => {
  it('takes on the account timezone on sign-in rather than the device one', async () => {
    useUiStore.setState({ timezone: 'America/New_York', locale: 'en' });
    mockApi.post.mockResolvedValue({
      token: 't',
      refreshToken: 'r',
      user: { ...sampleUser, locale: 'ja', timezone: 'Europe/Paris' },
    } as never);

    await useAuthStore.getState().login('alice@example.com', 'pw');

    expect(useUiStore.getState().timezone).toBe('Europe/Paris');
    expect(useUiStore.getState().locale).toBe('ja');
  });

  it('takes it on when the session is restored, not only when signing in', async () => {
    useUiStore.setState({ timezone: 'America/New_York' });
    mockHasToken.mockReturnValue(true);
    mockApi.get.mockResolvedValue({ ...sampleUser, timezone: 'Europe/Paris' } as never);

    await useAuthStore.getState().fetchMe();

    expect(useUiStore.getState().timezone).toBe('Europe/Paris');
  });

  it('ignores a preference this build cannot honour', async () => {
    useUiStore.setState({ timezone: 'Asia/Tokyo', locale: 'ja' });
    mockHasToken.mockReturnValue(true);
    mockApi.get.mockResolvedValue({
      ...sampleUser,
      locale: 'fr',
      timezone: 'Mars/Olympus_Mons',
    } as never);

    await useAuthStore.getState().fetchMe();

    // Adopting either would render every label as its own key, or make every
    // date format throw. Keeping what works is the lesser wrong.
    expect(useUiStore.getState().locale).toBe('ja');
    expect(useUiStore.getState().timezone).toBe('Asia/Tokyo');
  });

  it('stores a changed preference against the account, keeping the name', async () => {
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });
    mockApi.put.mockResolvedValue({ ...sampleUser, timezone: 'Europe/Paris' } as never);

    await useAuthStore.getState().saveAccountPreference({ timezone: 'Europe/Paris' });

    // The endpoint requires a name; sending the preference alone is a 422.
    expect(mockApi.put).toHaveBeenCalledWith('/user', {
      name: 'Alice',
      timezone: 'Europe/Paris',
    });
    expect(useUiStore.getState().timezone).toBe('Europe/Paris');
  });
});
