import { create } from 'zustand';
import { getT } from '@/i18n';
import { ApiError, api, clearToken, hasToken, setToken } from '@/lib/api';
import { resizeImageForAvatar } from '@/lib/image-resize';
import { useCalendarStore } from '@/stores/calendar-store';

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
    clearToken();
    set({ user: null, isAuthenticated: false, error: null });
    // Clear all local data
    for (const key of Object.keys(localStorage)) {
      if (key.startsWith('tt_')) {
        localStorage.removeItem(key);
      }
    }
    // Reset calendar store in-memory state
    useCalendarStore.setState({
      calendars: [],
      events: [],
      memos: [],
      activeCalendarIds: [],
    });
  },

  fetchMe: async () => {
    if (!hasToken() || fetchMeInFlight) return;
    fetchMeInFlight = true;
    try {
      const user = await api.get<User>('/user');
      set({ user, isAuthenticated: true, isInitializing: false });
    } catch {
      clearToken();
      set({ user: null, isAuthenticated: false, isInitializing: false });
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
    const presign = await api.post<{ avatarId: string; uploadUrl: string }>(
      '/user/avatar/presign',
      { contentType: resized.contentType, byteSize: resized.bytes.byteLength },
    );
    const putRes = await fetch(presign.uploadUrl, {
      method: 'PUT',
      headers: { 'Content-Type': resized.contentType },
      body: resized.bytes,
    });
    if (!putRes.ok) throw new ApiError(putRes.status, 'avatar upload failed');
    const user = await api.put<User>('/user/avatar', { avatarId: presign.avatarId });
    set({ user });
  },

  removeAvatar: async () => {
    const user = await api.delete<User>('/user/avatar');
    set({ user });
  },

  clearError: () => set({ error: null }),
}));
