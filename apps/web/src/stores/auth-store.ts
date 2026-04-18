import { getT } from '@/i18n';
import { ApiError, api, clearToken, hasToken, setToken } from '@/lib/api';
import { useCalendarStore } from '@/stores/calendar-store';
import { create } from 'zustand';

interface User {
  id: string;
  name: string;
  email: string;
  icon: string;
  color: string;
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

  clearError: () => set({ error: null }),
}));
