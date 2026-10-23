import { cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const navigate = vi.hoisted(() => vi.fn());
const search = vi.hoisted(() => ({ redirect: undefined as string | undefined }));

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: object) => ({ ...options, useSearch: () => search }),
  useNavigate: () => navigate,
}));

vi.mock('@/components/auth-shell', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  AuthShell: ({ children }: { children?: unknown }) => children,
}));

import { useAuthStore } from '@/stores/auth-store';
import { Route } from './oauth-complete';

const fetchMock = vi.fn();
const user = { id: 'u1', name: 'Alice', email: 'a@x.test', locale: 'ja', timezone: 'Asia/Tokyo' };

function renderPage() {
  const Page = (Route as unknown as { component: () => React.ReactNode }).component;
  render(<Page />);
}

beforeEach(() => {
  localStorage.clear();
  navigate.mockReset();
  fetchMock.mockReset();
  search.redirect = undefined;
  vi.stubGlobal('fetch', fetchMock);
  window.history.replaceState(null, '', '/oauth-complete#token=access-1&refresh=refresh-1');
  useAuthStore.setState({ user: null, isAuthenticated: false, isInitializing: false });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('OAuth completion', () => {
  it('ends on the login screen with no stored token when the profile cannot be loaded', async () => {
    fetchMock.mockResolvedValue(new Response('{}', { status: 503 }));

    renderPage();

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: '/login', search: { error: 'oauth_failed' } }),
    );
    expect(localStorage.getItem('tt_token')).toBeNull();
    expect(localStorage.getItem('tt_refresh')).toBeNull();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });

  it('continues to the requested page once the profile is loaded', async () => {
    search.redirect = '/settings?tab=export';
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify(user), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    renderPage();

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/settings?tab=export' }));
    expect(localStorage.getItem('tt_token')).toBe('access-1');
  });

  it('falls back to the home page for a return path that could leave the origin', async () => {
    search.redirect = '/\\evil.example';
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify(user), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    renderPage();

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/' }));
  });
});
