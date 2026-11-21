import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: unknown) => ({ options, useSearch: () => ({ token: 'tok' }) }),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ children }: { children?: unknown }) => children,
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Navigate: ({ to }: { to: string }) => <div data-testid="navigate">{to}</div>,
  useNavigate: () => vi.fn(),
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

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: () => 'error',
  // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
  client: { GET: vi.fn(), POST: vi.fn() },
}));

import { resetAuthConfig } from '@/lib/auth-config';
import { client } from '@/lib/client';
import { Route as ForgotRoute } from './forgot-password';
import { Route as ResetRoute } from './reset-password';

const apiGet = vi.mocked(client.GET);

function pageOf(route: unknown): () => React.ReactNode {
  return (route as { options: { component: () => React.ReactNode } }).options.component;
}

function config(over: Record<string, unknown>) {
  return { providers: [], passwordEnabled: true, mailEnabled: true, ...over };
}

beforeEach(() => {
  apiGet.mockReset();
  resetAuthConfig();
});
afterEach(cleanup);

describe.each([
  ['forgot-password', ForgotRoute],
  ['reset-password', ResetRoute],
])('%s page', (_name, route) => {
  const Page = pageOf(route);

  it('renders nothing while the config is loading', () => {
    apiGet.mockReturnValue(new Promise(() => {}));
    const { container } = render(<Page />);
    expect(container.innerHTML).toBe('');
  });

  it('is shown once mail and password sign-in are known to be on', async () => {
    apiGet.mockResolvedValue({ data: config({}) } as never);
    render(<Page />);
    await waitFor(() => expect(screen.getByTestId('shell')).toBeTruthy());
    expect(screen.queryByTestId('navigate')).toBeNull();
  });

  it('redirects to the login screen once mail is known to be off', async () => {
    apiGet.mockResolvedValue({ data: config({ mailEnabled: false }) } as never);
    render(<Page />);
    await waitFor(() => expect(screen.getByTestId('navigate').textContent).toBe('/login'));
  });

  it('redirects to the login screen once password sign-in is known to be off', async () => {
    apiGet.mockResolvedValue({ data: config({ passwordEnabled: false }) } as never);
    render(<Page />);
    await waitFor(() => expect(screen.getByTestId('navigate').textContent).toBe('/login'));
  });

  it('shows the failure with a retry when the config cannot be read', async () => {
    apiGet.mockRejectedValueOnce(new Error('network'));
    render(<Page />);
    await waitFor(() => expect(screen.getByText('error.authConfigUnavailable')).toBeTruthy());
    expect(screen.queryByTestId('navigate')).toBeNull();

    apiGet.mockResolvedValue({ data: config({}) } as never);
    fireEvent.click(screen.getByText('error.retry'));
    await waitFor(() => expect(screen.queryByText('error.authConfigUnavailable')).toBeNull());
    expect(screen.getByTestId('shell')).toBeTruthy();
  });
});
