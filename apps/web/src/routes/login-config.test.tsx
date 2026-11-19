import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const navigate = vi.hoisted(() => vi.fn());
const search = vi.hoisted(() => ({ redirect: undefined as string | undefined }));

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: unknown) => ({
    options,
    useSearch: () => search,
  }),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ children }: { children?: unknown }) => children,
  useNavigate: () => navigate,
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/components/auth-shell', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  AuthShell: ({ children }: { children?: unknown }) => (
    <div data-testid="shell">{children as never}</div>
  ),
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
  errorMessage: () => 'error',
}));

const authState = {
  isAuthenticated: false,
  isLoading: false,
  error: null,
  login: vi.fn(),
  register: vi.fn(),
  devLogin: vi.fn(),
  clearError: vi.fn(),
};
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { api } from '@/lib/api';
import { resetAuthConfig } from '@/lib/auth-config';
import { Route } from './login';

const apiGet = vi.mocked(api.get);
const Page = (Route as unknown as { options: { component: () => React.ReactNode } }).options
  .component;

function config(over: Record<string, unknown>) {
  return { providers: ['google'], passwordEnabled: true, mailEnabled: true, ...over };
}

beforeEach(() => {
  navigate.mockReset();
  search.redirect = undefined;
  authState.isAuthenticated = false;
  apiGet.mockReset();
  resetAuthConfig();
});
afterEach(cleanup);

describe('login page', () => {
  it('passes an authenticated return path to the router with its query intact', async () => {
    apiGet.mockResolvedValue(config({ providers: [], mailEnabled: false }));
    const redirect = '/schedule-polls/calendar/poll?view=respond';
    search.redirect = redirect;
    authState.isAuthenticated = true;

    render(<Page />);

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ href: redirect, replace: true }));
  });

  it('renders no form, link or provider button while the config is loading', () => {
    apiGet.mockReturnValue(new Promise(() => {}));
    render(<Page />);
    expect(screen.queryByText('auth.forgotPassword')).toBeNull();
    expect(screen.queryByText('auth.oauthGoogle')).toBeNull();
    expect(screen.queryByLabelText('auth.email')).toBeNull();
  });

  it('shows the form, the forgot-password link and the providers when all are on', async () => {
    apiGet.mockResolvedValue(config({}));
    render(<Page />);
    await waitFor(() => expect(screen.getByText('auth.forgotPassword')).toBeTruthy());
    expect(screen.getByLabelText('auth.email')).toBeTruthy();
    expect(screen.getByText('auth.oauthGoogle')).toBeTruthy();
  });

  it('hides the forgot-password link when mail is off', async () => {
    apiGet.mockResolvedValue(config({ mailEnabled: false }));
    render(<Page />);
    await waitFor(() => expect(screen.getByLabelText('auth.email')).toBeTruthy());
    expect(screen.queryByText('auth.forgotPassword')).toBeNull();
  });

  it('hides the password form when password sign-in is off', async () => {
    apiGet.mockResolvedValue(config({ passwordEnabled: false }));
    render(<Page />);
    await waitFor(() => expect(screen.getByText('auth.oauthGoogle')).toBeTruthy());
    expect(screen.queryByLabelText('auth.email')).toBeNull();
    expect(screen.queryByText('auth.forgotPassword')).toBeNull();
  });

  it('shows the failure with a retry when the config cannot be read', async () => {
    apiGet.mockRejectedValueOnce(new Error('network'));
    render(<Page />);
    await waitFor(() => expect(screen.getByText('error.authConfigUnavailable')).toBeTruthy());
    expect(screen.queryByLabelText('auth.email')).toBeNull();

    apiGet.mockResolvedValue(config({}));
    fireEvent.click(screen.getByText('error.retry'));
    await waitFor(() => expect(screen.getByLabelText('auth.email')).toBeTruthy());
  });
});
