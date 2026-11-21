import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const navigate = vi.hoisted(() => vi.fn());
const search = vi.hoisted(
  () => ({}) as { redirect?: string | undefined; mode?: string | undefined },
);

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: unknown) => ({ options, useSearch: () => search }),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ children }: { children?: unknown }) => children,
  useNavigate: () => navigate,
}));

vi.mock('@/i18n', () => {
  const t = (key: string, params?: Record<string, string>) =>
    params ? `${key}:${Object.values(params).join(',')}` : key;
  return { useT: () => t, getT: () => t };
});

vi.mock('@/components/auth-shell', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  AuthShell: ({ children }: { children?: unknown }) => <div>{children as never}</div>,
}));

vi.mock('@/lib/toast', () => ({ toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
// biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
vi.mock('@/lib/client', () => ({ client: { GET: vi.fn(), POST: vi.fn() } }));

import { ApiError } from '@/lib/api';
import { resetAuthConfig } from '@/lib/auth-config';
import { client } from '@/lib/client';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { Route as LoginRoute } from './login';
import { Route as OAuthRoute } from './oauth-complete';

const pageOf = (route: unknown) =>
  (route as { options: { component: () => React.ReactNode } }).options.component;
const LoginPage = pageOf(LoginRoute);
const OAuthPage = pageOf(OAuthRoute);

const user = {
  id: 'u1',
  name: 'Alice',
  email: 'alice@example.com',
  locale: 'ja',
  timezone: 'Asia/Tokyo',
  hasPassword: true,
};
const JOIN_LINK = '/share/tok-1?join=1';

interface Routes {
  accept?: () => unknown;
}

/** Answers the endpoints a sign-in followed by an invite join touches. */
function serve({ accept }: Routes = {}) {
  vi.mocked(client.GET).mockImplementation((async (path: string) => {
    if (path === '/auth/oauth/providers') {
      return { data: { providers: [], passwordEnabled: true, mailEnabled: false } };
    }
    if (path === '/user') return { data: user };
    if (path === '/calendars/{calendarId}') return { data: { id: 'cal-9', name: 'Family' } };
    throw new Error(`unrouted GET ${path}`);
  }) as never);
  vi.mocked(client.POST).mockImplementation((async (path: string) => {
    if (path === '/auth/login') return { data: { token: 't', refreshToken: 'r', user } };
    if (path === '/invites/{token}/accept') {
      return accept ? accept() : { data: { calendarId: 'cal-9', role: 'editor' } };
    }
    throw new Error(`unrouted POST ${path}`);
  }) as never);
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  search.redirect = undefined;
  search.mode = undefined;
  window.location.hash = '';
  resetAuthConfig();
  useAuthStore.setState({ user: null, isAuthenticated: false, isInitializing: false });
});
afterEach(cleanup);

const accepted = () =>
  (vi.mocked(client.POST).mock.calls as unknown as [string, unknown][]).filter(
    ([path]) => path === '/invites/{token}/accept',
  );

describe('joining an invite after password sign-in', () => {
  async function signIn() {
    render(<LoginPage />);
    fireEvent.change(await screen.findByLabelText('auth.email'), {
      target: { value: 'alice@example.com' },
    });
    fireEvent.change(screen.getByLabelText('auth.password'), { target: { value: 'pw-12345678' } });
    fireEvent.click(screen.getByRole('button', { name: 'auth.login' }));
  }

  it('accepts the invite once, reports the calendar joined and opens the app', async () => {
    serve();
    search.redirect = JOIN_LINK;

    await signIn();

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ href: '/', replace: true }));
    expect(accepted()).toHaveLength(1);
    expect(accepted()[0]?.[1]).toEqual({ params: { path: { token: 'tok-1' } } });
    expect(toast.success).toHaveBeenCalledWith('invite.joined:Family');
    expect(toast.error).not.toHaveBeenCalled();
  });

  it('reports a refused join and stays on the share page', async () => {
    serve({
      accept: () => {
        throw new ApiError(410, 'gone', 'INVITE.EXPIRED');
      },
    });
    search.redirect = JOIN_LINK;

    await signIn();

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ href: '/share/tok-1', replace: true }),
    );
    expect(toast.error).toHaveBeenCalledWith('invite.joinFailed');
    expect(toast.success).not.toHaveBeenCalled();
  });

  it('does not join when the return path carries no join intent', async () => {
    serve();
    search.redirect = '/share/tok-1';

    await signIn();

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ href: '/share/tok-1', replace: true }),
    );
    expect(accepted()).toHaveLength(0);
  });

  it('opens on the registration form when sent from an invite', async () => {
    serve();
    search.mode = 'register';

    render(<LoginPage />);

    expect(await screen.findByLabelText('auth.name')).toBeTruthy();
  });
});

describe('joining an invite after OAuth sign-in', () => {
  function returnFromProvider() {
    window.location.hash = '#token=oauth-tok&refresh=oauth-refresh';
    render(<OAuthPage />);
  }

  it('accepts the invite the return path carries and opens the app', async () => {
    serve();
    search.redirect = JOIN_LINK;

    returnFromProvider();

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ href: '/', replace: true }));
    expect(localStorage.getItem('tt_token')).toBe('oauth-tok');
    expect(accepted()).toHaveLength(1);
    expect(accepted()[0]?.[1]).toEqual({ params: { path: { token: 'tok-1' } } });
    expect(toast.success).toHaveBeenCalledWith('invite.joined:Family');
  });

  it('reports a refused join and stays on the share page', async () => {
    serve({
      accept: () => {
        throw new ApiError(409, 'full', 'INVITE.LIMIT_REACHED');
      },
    });
    search.redirect = JOIN_LINK;

    returnFromProvider();

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ href: '/share/tok-1', replace: true }),
    );
    expect(toast.error).toHaveBeenCalledWith('invite.joinFailed');
  });
});
