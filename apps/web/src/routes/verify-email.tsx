import { createFileRoute, Link } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { api } from '@/lib/api';

export interface VerifyEmailSearch {
  token?: string | undefined;
}

export const Route = createFileRoute('/verify-email')({
  validateSearch: (search: Record<string, unknown>): VerifyEmailSearch => ({
    token: typeof search.token === 'string' ? search.token : undefined,
  }),
  component: VerifyEmailPage,
});

type Status = 'pending' | 'done' | 'invalid';

function VerifyEmailPage() {
  const t = useT();
  const { token } = Route.useSearch();
  const [status, setStatus] = useState<Status>(token ? 'pending' : 'invalid');
  // The link is followed from a mail client, and React runs effects twice in
  // development. Redeeming is single-use, so a second call would report the
  // first success as a failure.
  const redeemed = useRef(false);

  useEffect(() => {
    if (!token || redeemed.current) return;
    redeemed.current = true;
    let active = true;
    api
      .post('/auth/verify-email/confirm', { token })
      .then(() => {
        if (active) setStatus('done');
      })
      .catch(() => {
        if (active) setStatus('invalid');
      });
    return () => {
      active = false;
    };
  }, [token]);

  return (
    <AuthShell title={t('auth.verifyEmailTitle')}>
      {status === 'pending' && (
        <output aria-live="polite" className="block py-6 text-center text-callout">
          {t('auth.verifyEmailPending')}
        </output>
      )}

      {status === 'done' && (
        <output aria-live="polite" className="flex flex-col items-center gap-4 py-6 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-[var(--color-accent-bg)] text-[var(--color-accent)]">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden>
              <path
                d="M5 13l4 4L19 7"
                stroke="currentColor"
                strokeWidth="2.4"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </div>
          <p className="text-callout text-[var(--color-text-primary)]">
            {t('auth.verifyEmailSuccess')}
          </p>
        </output>
      )}

      {status === 'invalid' && (
        <div
          role="alert"
          className="rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-default text-[var(--color-danger)]"
        >
          {t('auth.verifyEmailInvalid')}
        </div>
      )}

      <p className="mt-6 text-center text-default">
        <Link to="/login" className="text-[var(--color-accent)] hover:underline">
          {t('auth.backToLogin')}
        </Link>
      </p>
    </AuthShell>
  );
}
