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

  // Show nothing while verifying token on page reload
  if (isInitializing && !isPublic) {
    return null;
  }

  if (!isAuthenticated && !isPublic) {
    return <Navigate to="/login" />;
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
