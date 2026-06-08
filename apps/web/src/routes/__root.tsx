import { createRootRoute, Navigate, Outlet, useLocation } from '@tanstack/react-router';
import { useEffect } from 'react';
import { ThemeInitializer } from '@/components/theme-initializer';
import { Toaster } from '@/components/toaster';
import { useAuthStore } from '@/stores/auth-store';

export const Route = createRootRoute({
  component: RootLayout,
});

const PUBLIC_PATHS = [
  '/login',
  '/share/',
  '/forgot-password',
  '/reset-password',
  '/oauth-complete',
];

function RootLayout() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const isInitializing = useAuthStore((s) => s.isInitializing);
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

  return (
    <>
      <ThemeInitializer />
      <Outlet />
      <Toaster />
    </>
  );
}
