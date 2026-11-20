import { createRootRoute, Navigate, Outlet, useLocation } from '@tanstack/react-router';
import { useEffect } from 'react';
import { LoadFailure } from '@/components/load-failure';
import { ThemeInitializer } from '@/components/theme-initializer';
import { Toaster } from '@/components/toaster';
import { useT } from '@/i18n';
import { useAuthStore } from '@/stores/auth-store';

export const Route = createRootRoute({
  component: RootLayout,
});

const PUBLIC_PATHS = [
  '/login',
  '/share/',
  '/embed/',
  '/forgot-password',
  '/reset-password',
  '/verify-email',
  '/oauth-complete',
];

function RootLayout() {
  const t = useT();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const isInitializing = useAuthStore((s) => s.isInitializing);
  const initError = useAuthStore((s) => s.initError);
  const fetchMe = useAuthStore((s) => s.fetchMe);
  const user = useAuthStore((s) => s.user);
  const location = useLocation();

  const isPublic = PUBLIC_PATHS.some((p) => location.pathname.startsWith(p));

  useEffect(() => {
    if (isAuthenticated && !user) {
      fetchMe();
    }
  }, [isAuthenticated, user, fetchMe]);

  // A bare splash while the token is verified on page reload
  if (isInitializing && !isPublic) {
    return (
      <>
        <ThemeInitializer />
        <output className="app-bg flex min-h-screen items-center justify-center">
          <span
            className="h-8 w-8 animate-spin rounded-full border-2 border-[var(--color-border)] border-t-[var(--color-accent)]"
            aria-hidden="true"
          />
          <span className="sr-only">{t('common.loading')}</span>
        </output>
      </>
    );
  }

  if (!isAuthenticated && !isPublic) {
    const hash = location.hash ? `#${location.hash.replace(/^#/, '')}` : '';
    const redirect = `${location.pathname}${location.searchStr ?? ''}${hash}`;
    return <Navigate to="/login" search={{ redirect }} />;
  }

  // The session is intact -- the profile behind it is not. Rendering the
  // signed-in shell without it would compare every permission against an
  // address that never loaded, so the whole account would look read-only.
  if (isAuthenticated && !user && initError && !isPublic) {
    return (
      <>
        <ThemeInitializer />
        <div className="app-bg flex min-h-screen items-center justify-center">
          <LoadFailure title={t('error.profileUnavailable')} detail={initError} onRetry={fetchMe} />
        </div>
        <Toaster />
      </>
    );
  }

  return (
    <>
      <ThemeInitializer />
      <Outlet />
      <Toaster />
    </>
  );
}
