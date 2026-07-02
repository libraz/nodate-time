import { create } from 'zustand';
import { getT } from '@/i18n';
import { ApiError, api, clearToken, hasToken, SESSION_EXPIRED_EVENT, setToken } from '@/lib/api';
import { resizeImageForAvatar } from '@/lib/image-resize';
import { uploadViaPresign } from '@/lib/upload';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

interface User {
  id: string;
  name: string;
  email: string;
  icon: string;
  color: string;
  avatarUrl?: string;
  isAdmin?: boolean;
}

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isInitializing: boolean;
  isLoading: boolean;
  error: string | null;

  login: (email: string, password: string) => Promise<void>;
  devLogin: (email: string) => Promise<void>;
  register: (name: string, email: string, password: string) => Promise<void>;
  logout: () => void;
  fetchMe: () => Promise<void>;
  updateProfile: (data: { name: string; icon: string; color: string }) => Promise<void>;
  uploadAvatar: (file: File) => Promise<void>;
  removeAvatar: () => Promise<void>;
  clearError: () => void;
}

interface AuthResponse {
  token: string;
  user: User;
}

function extractErrorMessage(e: unknown, fallback: string): string {
  if (e instanceof ApiError) return e.detail || fallback;
  return fallback;
}

let fetchMeInFlight = false;

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isAuthenticated: hasToken(),
  isInitializing: hasToken(),
  isLoading: false,
  error: null,

  login: async (email, password) => {
    set({ isLoading: true, error: null });
    try {
      const data = await api.post<AuthResponse>('/auth/login', { email, password });
      setToken(data.token);
      set({ user: data.user, isAuthenticated: true, isInitializing: false, isLoading: false });
    } catch (e) {
      set({ isLoading: false, error: extractErrorMessage(e, getT()('auth.loginFailed')) });
      throw e;
    }
  },

  devLogin: async (email) => {
    set({ isLoading: true, error: null });
    try {
      const data = await api.post<AuthResponse>('/auth/dev-login', { email });
      setToken(data.token);
      set({ user: data.user, isAuthenticated: true, isInitializing: false, isLoading: false });
    } catch (e) {
      set({ isLoading: false, error: extractErrorMessage(e, getT()('auth.loginFailed')) });
      throw e;
    }
  },

  register: async (name, email, password) => {
    set({ isLoading: true, error: null });
    try {
      const data = await api.post<AuthResponse>('/auth/register', { name, email, password });
      setToken(data.token);
      set({ user: data.user, isAuthenticated: true, isInitializing: false, isLoading: false });
    } catch (e) {
      set({ isLoading: false, error: extractErrorMessage(e, getT()('auth.registerFailed')) });
      throw e;
    }
  },

  logout: () => {
    // Drop the auth token and per-session calendar state, but keep user
    // preferences (theme, locale, timezone) that are not tied to the account.
    clearToken();
    localStorage.removeItem('tt_activeCalendarIds');
    set({ user: null, isAuthenticated: false, error: null });
    // Reset calendar store in-memory state, including per-calendar data.
    useCalendarStore.setState({
      calendars: [],
      events: [],
      memos: [],
      membersMap: {},
      labels: [],
      activeCalendarIds: [],
    });
    useUiStore.getState().resetSessionUi();
  },

  fetchMe: async () => {
    if (!hasToken() || fetchMeInFlight) return;
    fetchMeInFlight = true;
    try {
      const user = await api.get<User>('/user');
      set({ user, isAuthenticated: true, isInitializing: false });
    } catch (e) {
      if (e instanceof ApiError && (e.status === 401 || e.code === 'AUTH.TOKEN_INVALID')) {
        clearToken();
        set({ user: null, isAuthenticated: false, isInitializing: false });
        return;
      }
      set({ isInitializing: false });
    } finally {
      fetchMeInFlight = false;
    }
  },

  updateProfile: async (data) => {
    const user = await api.put<User>('/user', data);
    set({ user });
  },

  uploadAvatar: async (file: File) => {
    const resized = await resizeImageForAvatar(file);
    const presign = await uploadViaPresign<{ avatarId: string; uploadUrl: string }>({
      kind: 'avatar',
      presignPath: '/user/avatar/presign',
      presignBody: {
        contentType: resized.contentType,
        byteSize: resized.bytes.byteLength,
      },
      contentType: resized.contentType,
      body: resized.bytes,
      byteSize: resized.bytes.byteLength,
    });
    const user = await api.put<User>('/user/avatar', { avatarId: presign.avatarId });
    set({ user });
  },

  removeAvatar: async () => {
    const user = await api.delete<User>('/user/avatar');
    set({ user });
  },

  clearError: () => set({ error: null }),
}));

if (typeof window !== 'undefined') {
  window.addEventListener(SESSION_EXPIRED_EVENT, () => {
    useAuthStore.setState({
      user: null,
      isAuthenticated: false,
      isInitializing: false,
      isLoading: false,
      error: null,
    });
  });
}
