import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { setToken } from '@/lib/api';
import { useAuthStore } from '@/stores/auth-store';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';

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
  const [error, setError] = useState<string | null>(null);

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
        if (!cancelled) setError(t('auth.oauthFailed'));
        return;
      }
      if (cancelled) return;
      const dest = redirect?.startsWith('/') && !redirect.startsWith('//') ? redirect : '/';
      navigate({ to: dest });
    })();
    return () => {
      cancelled = true;
    };
  }, [redirect, navigate, fetchMe, t]);

  return (
    <AuthShell title="Nodate Time" subtitle={error ?? t('auth.signingIn')}>
      <div className="flex flex-col items-center gap-4 py-6">
        {error ? (
          <>
            <div
              aria-hidden
              className="flex h-12 w-12 items-center justify-center rounded-full bg-[var(--color-danger-bg)] text-[var(--color-danger)]"
            >
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
                <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2" />
                <path
                  d="M12 8v4M12 16h.01"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                />
              </svg>
            </div>
            <button
              type="button"
              onClick={() => navigate({ to: '/login' })}
              className="btn-primary rounded-xl px-5"
            >
              {t('auth.backToLogin')}
            </button>
          </>
        ) : (
          <div
            aria-hidden
            className="h-10 w-10 animate-spin rounded-full border-2 border-[var(--color-border)] border-t-[var(--color-accent)]"
          />
        )}
      </div>
    </AuthShell>
  );
}
