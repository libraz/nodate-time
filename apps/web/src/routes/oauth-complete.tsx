import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';
import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { setToken } from '@/lib/api';
import { APP_NAME } from '@/lib/app-name';
import { safeRedirect } from '@/lib/safe-redirect';
import { useAuthStore } from '@/stores/auth-store';

export interface OAuthSearch {
  redirect?: string | undefined;
}

export const Route = createFileRoute('/oauth-complete')({
  validateSearch: (search: Record<string, unknown>): OAuthSearch => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
  }),
  component: OAuthCompletePage,
});

function readTokensFromHash(): { token: string; refreshToken: string } | null {
  if (typeof window === 'undefined') return null;
  const hash = window.location.hash.replace(/^#/, '');
  if (!hash) return null;
  const params = new URLSearchParams(hash);
  const token = params.get('token');
  if (!token) return null;
  return { token, refreshToken: params.get('refresh') ?? '' };
}

function OAuthCompletePage() {
  const t = useT();
  const navigate = useNavigate();
  const { redirect } = Route.useSearch();
  const fetchMe = useAuthStore((s) => s.fetchMe);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const tokens = readTokensFromHash();
      if (!tokens) {
        navigate({ to: '/login' });
        return;
      }
      // Strip the tokens from the URL hash so they do not stay in history.
      window.history.replaceState(null, '', window.location.pathname + window.location.search);
      setToken(tokens.token, tokens.refreshToken);
      // fetchMe reports a failed profile load through the store rather than by
      // throwing, so the outcome is read from there.
      await fetchMe();
      if (cancelled) return;
      if (!useAuthStore.getState().user) {
        // Surface the failure on the login screen instead of leaving a blank shell.
        useAuthStore.getState().logout();
        navigate({ to: '/login', search: { error: 'oauth_failed' } });
        return;
      }
      navigate({ to: safeRedirect(redirect) });
    })();
    return () => {
      cancelled = true;
    };
  }, [redirect, navigate, fetchMe]);

  return (
    <AuthShell title={APP_NAME} subtitle={t('auth.signingIn')}>
      <div className="flex flex-col items-center gap-4 py-6">
        <div
          aria-hidden
          className="h-10 w-10 animate-spin rounded-full border-2 border-[var(--color-border)] border-t-[var(--color-accent)]"
        />
      </div>
    </AuthShell>
  );
}
