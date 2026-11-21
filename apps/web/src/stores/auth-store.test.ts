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
    setToken: vi.fn(),
    endSession: vi.fn(),
    hasToken: vi.fn(() => false),
    isSessionStorageEvent: (e: StorageEvent) =>
      e.key === null || e.key === 'tt_token' || e.key === 'tt_refresh',
    errorMessage: (e: unknown, fallback?: string) =>
      e instanceof ApiError ? e.detail : (fallback ?? 'error.generic'),
    // biome-ignore lint/style/useNamingConvention: must mirror the real module export
    SESSION_EXPIRED_EVENT: 'nodate:session-expired',
  };
});

vi.mock('@/lib/client', () => ({
  // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
  client: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
  createApiClient: vi.fn(),
}));

vi.mock('@/i18n', () => ({
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/image-resize', () => ({
  resizeImageForAvatar: vi.fn(),
}));

import { ApiError, endSession, hasToken, setToken } from '@/lib/api';
import { client } from '@/lib/client';
import { resizeImageForAvatar } from '@/lib/image-resize';
import { useCalendarStore } from '@/stores/calendar-store';
import { useSchedulePollStore } from '@/stores/schedule-poll-store';
import { useUiStore } from '@/stores/ui-store';
import { useAuthStore } from './auth-store';

const mockClient = vi.mocked(client);
const mockSetToken = vi.mocked(setToken);
const mockEndSession = vi.mocked(endSession);
const mockHasToken = vi.mocked(hasToken);

const sampleUser = {
  id: 'u1',
  name: 'Alice',
  email: 'alice@example.com',
  locale: 'ja',
  timezone: 'Asia/Tokyo',
  hasPassword: true,
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
  useSchedulePollStore.getState().reset();
  useUiStore.setState({
    showEventModal: false,
    editingEventId: null,
    eventDraftStart: null,
    eventDraftEnd: null,
    showDayDetail: false,
    rightPanel: null,
    showSearch: false,
    searchQuery: '',
    mobileTab: 'calendar',
    showMobileMenu: false,
    showActivity: false,
  });
});

describe('login', () => {
  it('stores the token and marks the user authenticated', async () => {
    mockClient.POST.mockResolvedValue({
      data: {
        token: 'tok',
        refreshToken: 'refresh-tok',
        user: sampleUser,
      },
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
    mockClient.POST.mockRejectedValue(new ApiError(401, 'Invalid credentials'));

    await expect(useAuthStore.getState().login('a@b.c', 'bad')).rejects.toBeInstanceOf(ApiError);

    const s = useAuthStore.getState();
    expect(s.isAuthenticated).toBe(false);
    expect(s.isLoading).toBe(false);
    expect(s.error).toBe('Invalid credentials');
  });

  it('falls back to a generic message for non-ApiError failures', async () => {
    mockClient.POST.mockRejectedValue(new Error('network'));

    await expect(useAuthStore.getState().login('a@b.c', 'pw')).rejects.toThrow();

    expect(useAuthStore.getState().error).toBe('auth.loginFailed');
  });
});

describe('logout', () => {
  // Forgetting the token locally is not signing out. Anyone who copied the
  // storage of a shared machine keeps working access until it expires.
  it('ends the session on the server too', () => {
    useAuthStore.getState().logout();

    expect(mockEndSession).toHaveBeenCalledTimes(1);
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
          createdAt: '',
          publicShared: false,
          memberCount: 1,
          role: 'owner',
          memberColor: '#000',
          coverUploadAllowed: false,
          etag: '',
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
      eventDraftEnd: DateTime.fromISO('2026-04-20T11:30:00+09:00'),
      showDayDetail: true,
      rightPanel: 'memo',
      showSearch: true,
      searchQuery: 'draft',
      mobileTab: 'memo',
      showMobileMenu: true,
      showActivity: true,
    });
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });
    useSchedulePollStore.setState({
      accountId: sampleUser.id,
      actions: [
        {
          calendarId: 'cal-1',
          calendarName: 'A',
          pollId: 'poll-1',
          title: 'Meeting',
          kind: 'answer',
        },
      ],
    });

    useAuthStore.getState().logout();

    expect(mockEndSession).toHaveBeenCalled();
    expect(localStorage.getItem('tt_activeCalendarIds')).toBeNull();
    expect(localStorage.getItem('unrelated')).toBe('keep');
    const auth = useAuthStore.getState();
    expect(auth.user).toBeNull();
    expect(auth.isAuthenticated).toBe(false);
    expect(useCalendarStore.getState().calendars).toEqual([]);
    expect(useSchedulePollStore.getState()).toMatchObject({ accountId: null, actions: [] });
    expect(useUiStore.getState()).toMatchObject({
      showEventModal: false,
      editingEventId: null,
      eventDraftStart: null,
      eventDraftEnd: null,
      showDayDetail: false,
      rightPanel: null,
      showSearch: false,
      searchQuery: '',
      mobileTab: 'calendar',
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
          createdAt: '',
          publicShared: false,
          memberCount: 1,
          role: 'owner',
          memberColor: '#000',
          coverUploadAllowed: false,
          etag: '',
        },
      ],
      events: [],
      memos: [],
      membersMap: { 'cal-1': [] },
      labels: [{ id: 'label-1', nameKey: 'work', color: '#000' }],
      activeCalendarIds: ['cal-1'],
    });

    window.dispatchEvent(new Event('nodate:session-expired'));

    expect(mockEndSession).toHaveBeenCalled();
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
    mockClient.PUT.mockResolvedValue({
      data: {
        token: 'rotated-token',
        refreshToken: 'rotated-refresh',
      },
    } as never);

    await useAuthStore.getState().changePassword('old-pw', 'new-pw-1234');

    expect(mockClient.PUT).toHaveBeenCalledWith('/user/password', {
      body: { currentPassword: 'old-pw', newPassword: 'new-pw-1234' },
    });
    expect(mockSetToken).toHaveBeenCalledWith('rotated-token', 'rotated-refresh');
    // The current session is preserved, not cleared.
    expect(mockEndSession).not.toHaveBeenCalled();
  });

  it('does not persist a token and rethrows when the change fails', async () => {
    mockClient.PUT.mockRejectedValue(new ApiError(400, 'wrong password'));

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

    expect(mockEndSession).toHaveBeenCalled();
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

    expect(mockEndSession).not.toHaveBeenCalled();
    expect(useAuthStore.getState().isAuthenticated).toBe(true);
  });
});

describe('fetchMe', () => {
  it('does nothing when there is no token', async () => {
    mockHasToken.mockReturnValue(false);

    await useAuthStore.getState().fetchMe();

    expect(mockClient.GET).not.toHaveBeenCalled();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });

  it('hydrates the user when the token is valid', async () => {
    mockHasToken.mockReturnValue(true);
    mockClient.GET.mockResolvedValue({ data: sampleUser } as never);

    await useAuthStore.getState().fetchMe();

    const s = useAuthStore.getState();
    expect(s.user).toEqual(sampleUser);
    expect(s.isAuthenticated).toBe(true);
    expect(s.isInitializing).toBe(false);
  });

  it('clears the session when the token is rejected', async () => {
    mockHasToken.mockReturnValue(true);
    mockClient.GET.mockRejectedValue(new ApiError(401, 'expired'));

    await useAuthStore.getState().fetchMe();

    expect(mockEndSession).toHaveBeenCalled();
    const s = useAuthStore.getState();
    expect(s.user).toBeNull();
    expect(s.isAuthenticated).toBe(false);
    expect(s.isInitializing).toBe(false);
  });

  it('does not restore isAuthenticated when logout lands during an in-flight request', async () => {
    mockHasToken.mockReturnValue(true);
    let resolveUser: ((response: { data: typeof sampleUser }) => void) | undefined;
    mockClient.GET.mockReturnValue(
      new Promise<{ data: typeof sampleUser }>((resolve) => {
        resolveUser = resolve;
      }) as never,
    );
    useAuthStore.setState({ isAuthenticated: true, isInitializing: true });

    const request = useAuthStore.getState().fetchMe();
    // Logout clears the token and auth flag mid-flight.
    mockHasToken.mockReturnValue(false);
    useAuthStore.setState({ user: null, isAuthenticated: false });
    resolveUser?.({ data: sampleUser });
    await request;

    const s = useAuthStore.getState();
    expect(s.user).toBeNull();
    expect(s.isAuthenticated).toBe(false);
  });

  it('keeps the current session when user hydration fails with a server error', async () => {
    mockHasToken.mockReturnValue(true);
    mockClient.GET.mockRejectedValue(new ApiError(500, 'temporary outage'));
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true, isInitializing: true });

    await useAuthStore.getState().fetchMe();

    expect(mockEndSession).not.toHaveBeenCalled();
    const s = useAuthStore.getState();
    expect(s.user).toEqual(sampleUser);
    expect(s.isAuthenticated).toBe(true);
    expect(s.isInitializing).toBe(false);
  });

  it('says why the profile is missing, and recovers on a second attempt', async () => {
    mockHasToken.mockReturnValue(true);
    mockClient.GET.mockRejectedValueOnce(new ApiError(500, 'temporary outage'));
    useAuthStore.setState({ user: null, isAuthenticated: true, isInitializing: true });

    await useAuthStore.getState().fetchMe();

    // A failed hydration used to leave a signed-in shell with no user behind
    // it, which is indistinguishable from an account with no permissions.
    expect(useAuthStore.getState().user).toBeNull();
    expect(useAuthStore.getState().initError).toBe('temporary outage');

    mockClient.GET.mockResolvedValueOnce({ data: sampleUser } as never);
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
    mockClient.POST.mockResolvedValue({
      data: {
        token: 't',
        refreshToken: 'r',
        user: { ...sampleUser, locale: 'ja', timezone: 'Europe/Paris' },
      },
    } as never);

    await useAuthStore.getState().login('alice@example.com', 'pw');

    expect(useUiStore.getState().timezone).toBe('Europe/Paris');
    expect(useUiStore.getState().locale).toBe('ja');
  });

  it('takes it on when the session is restored, not only when signing in', async () => {
    useUiStore.setState({ timezone: 'America/New_York' });
    mockHasToken.mockReturnValue(true);
    mockClient.GET.mockResolvedValue({
      data: { ...sampleUser, timezone: 'Europe/Paris' },
    } as never);

    await useAuthStore.getState().fetchMe();

    expect(useUiStore.getState().timezone).toBe('Europe/Paris');
  });

  it('ignores a preference this build cannot honour', async () => {
    useUiStore.setState({ timezone: 'Asia/Tokyo', locale: 'ja' });
    mockHasToken.mockReturnValue(true);
    mockClient.GET.mockResolvedValue({
      data: {
        ...sampleUser,
        locale: 'fr',
        timezone: 'Mars/Olympus_Mons',
      },
    } as never);

    await useAuthStore.getState().fetchMe();

    // Adopting either would render every label as its own key, or make every
    // date format throw. Keeping what works is the lesser wrong.
    expect(useUiStore.getState().locale).toBe('ja');
    expect(useUiStore.getState().timezone).toBe('Asia/Tokyo');
  });

  it('stores a changed preference against the account as a partial update', async () => {
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });
    mockClient.PATCH.mockResolvedValue({
      data: { ...sampleUser, timezone: 'Europe/Paris' },
    } as never);

    await useAuthStore.getState().saveAccountPreference({ timezone: 'Europe/Paris' });

    // A merge patch: the name is left out, so it stays as it is.
    expect(mockClient.PATCH).toHaveBeenCalledWith('/user', { body: { timezone: 'Europe/Paris' } });
    expect(useUiStore.getState().timezone).toBe('Europe/Paris');
  });
});

describe('profile', () => {
  it('updates the profile with a partial PATCH', async () => {
    useAuthStore.setState({ user: sampleUser, isAuthenticated: true });
    mockClient.PATCH.mockResolvedValue({ data: { ...sampleUser, name: 'Alicia' } } as never);

    await useAuthStore.getState().updateProfile({ name: 'Alicia' });

    expect(mockClient.PATCH).toHaveBeenCalledWith('/user', { body: { name: 'Alicia' } });
    expect(useAuthStore.getState().user?.name).toBe('Alicia');
  });

  it('sends the device locale and zone after registering, without the name', async () => {
    mockClient.POST.mockResolvedValue({
      data: { token: 't', refreshToken: 'r', user: sampleUser },
    } as never);
    mockClient.PATCH.mockResolvedValue({ data: sampleUser } as never);

    await useAuthStore.getState().register('Alice', 'alice@example.com', 'pw-12345678');

    expect(mockClient.POST).toHaveBeenCalledWith('/auth/register', {
      body: { name: 'Alice', email: 'alice@example.com', password: 'pw-12345678' },
    });
    const [path, init] = mockClient.PATCH.mock.calls[0] as unknown as [
      string,
      { body: Record<string, unknown> },
    ];
    expect(path).toBe('/user');
    expect(Object.keys(init.body).sort()).toEqual(['locale', 'timezone']);
  });
});

describe('avatar upload', () => {
  const bytes = new TextEncoder().encode('abc').buffer as ArrayBuffer;
  // sha256('abc')
  const digest = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad';

  it('presigns with the lowercase hex digest, uploads, then confirms', async () => {
    vi.mocked(resizeImageForAvatar).mockResolvedValue({
      bytes,
      contentType: 'image/png',
      width: 1,
      height: 1,
    });
    const put = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
    vi.stubGlobal('fetch', put);
    mockClient.POST.mockResolvedValue({
      data: { avatarId: 'av-1', uploadUrl: 'https://blob.test/put' },
    } as never);
    mockClient.PUT.mockResolvedValue({
      data: { ...sampleUser, avatarUrl: 'https://a/1' },
    } as never);

    await useAuthStore.getState().uploadAvatar(new File([bytes], 'a.png', { type: 'image/png' }));
    vi.unstubAllGlobals();

    expect(mockClient.POST).toHaveBeenCalledWith('/user/avatar/presign', {
      body: { contentType: 'image/png', byteSize: 3, sha256: digest },
    });
    expect(put).toHaveBeenCalledWith(
      'https://blob.test/put',
      expect.objectContaining({ method: 'PUT' }),
    );
    expect(mockClient.PUT).toHaveBeenCalledWith('/user/avatar', { body: { avatarId: 'av-1' } });
    expect(useAuthStore.getState().user?.avatarUrl).toBe('https://a/1');
  });
});
