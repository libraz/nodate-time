import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const routerState = vi.hoisted(() => ({ pathname: '/' }));

vi.mock('@tanstack/react-router', () => ({
  createRootRoute: (options: unknown) => ({ options }),
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Navigate: ({ to }: { to: string }) => <div data-testid="navigate">{to}</div>,
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Outlet: () => <div data-testid="outlet" />,
  useLocation: () => ({ pathname: routerState.pathname }),
}));

vi.mock('@/i18n', () => ({ useT: () => (key: string) => key }));
vi.mock('@/components/theme-initializer', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  ThemeInitializer: () => null,
}));
vi.mock('@/components/toaster', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Toaster: () => null,
}));
vi.mock('@/components/load-failure', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  LoadFailure: () => null,
}));

vi.mock('@/stores/auth-store', () => {
  const state = {
    isAuthenticated: false,
    isInitializing: false,
    initError: null,
    user: null,
    fetchMe: vi.fn(),
  };
  return { useAuthStore: (select: (s: typeof state) => unknown) => select(state) };
});

import { Route } from './__root';

function renderAt(pathname: string) {
  routerState.pathname = pathname;
  const RootLayout = (Route as unknown as { options: { component: () => React.ReactNode } }).options
    .component;
  render(<RootLayout />);
}

describe('root layout for a signed-out visitor', () => {
  afterEach(cleanup);

  it.each([
    '/verify-email',
    '/reset-password',
    '/forgot-password',
    '/login',
    '/oauth-complete',
    '/share/abc',
  ])('renders %s instead of redirecting to the login screen', (pathname) => {
    renderAt(pathname);
    expect(screen.getByTestId('outlet')).toBeTruthy();
    expect(screen.queryByTestId('navigate')).toBeNull();
  });

  it('redirects a protected path to the login screen', () => {
    renderAt('/settings');
    expect(screen.getByTestId('navigate').textContent).toBe('/login');
  });
});
