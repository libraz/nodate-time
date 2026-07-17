import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';
import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { setToken } from '@/lib/api';
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

function readTokenFromHash(): string | null {
  if (typeof window === 'undefined') return null;
  const hash = window.location.hash.replace(/^#/, '');
  if (!hash) return null;
  const params = new URLSearchParams(hash);
  return params.get('token');
}

function OAuthCompletePage() {
  const t = useT();
  const navigate = useNavigate();
  const { redirect } = Route.useSearch();
  const fetchMe = useAuthStore((s) => s.fetchMe);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const token = readTokenFromHash();
      if (!token) {
        navigate({ to: '/login' });
        return;
      }
      // Strip token from URL hash so it does not stay in browser history.
      window.history.replaceState(null, '', window.location.pathname + window.location.search);
      setToken(token);
      try {
        await fetchMe();
      } catch {
        // User resolution failed after a successful redirect; surface the error
        // on the login screen instead of leaving the user on a blank shell.
        if (!cancelled) navigate({ to: '/login', search: { error: 'oauth_failed' } });
        return;
      }
      if (cancelled) return;
      const dest = redirect?.startsWith('/') && !redirect.startsWith('//') ? redirect : '/';
      navigate({ to: dest });
    })();
    return () => {
      cancelled = true;
    };
  }, [redirect, navigate, fetchMe]);

  return (
    <AuthShell title="Nodate Time" subtitle={t('auth.signingIn')}>
      <div className="flex flex-col items-center gap-4 py-6">
        <div
          aria-hidden
          className="h-10 w-10 animate-spin rounded-full border-2 border-[var(--color-border)] border-t-[var(--color-accent)]"
        />
      </div>
    </AuthShell>
  );
}
