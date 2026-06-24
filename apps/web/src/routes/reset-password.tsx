import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { ApiError, api } from '@/lib/api';

export interface ResetSearch {
  token?: string | undefined;
}

export const Route = createFileRoute('/reset-password')({
  validateSearch: (search: Record<string, unknown>): ResetSearch => ({
    token: typeof search.token === 'string' ? search.token : undefined,
  }),
  component: ResetPasswordPage,
});

function ResetPasswordPage() {
  const t = useT();
  const { token } = Route.useSearch();
  const navigate = useNavigate();
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const redirectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (redirectTimer.current) clearTimeout(redirectTimer.current);
    };
  }, []);

  const tokenInvalid = !token;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) {
      setError(t('auth.resetInvalid'));
      return;
    }
    if (password !== confirm) {
      setError(t('profile.passwordMismatch'));
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await api.post('/auth/password-reset/confirm', { token, newPassword: password });
      setDone(true);
      redirectTimer.current = setTimeout(() => {
        navigate({ to: '/login' });
      }, 1800);
    } catch (e) {
      setError(e instanceof ApiError ? e.detail : 'Error');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AuthShell title={t('auth.resetTitle')}>
      {done ? (
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
          <p className="text-callout text-[var(--color-text-primary)]">{t('auth.resetSuccess')}</p>
          <Link to="/login" className="text-default text-[var(--color-accent)] hover:underline">
            {t('auth.backToLogin')}
          </Link>
        </output>
      ) : tokenInvalid ? (
        <div
          role="alert"
          className="rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-default text-[var(--color-danger)]"
        >
          {t('auth.resetInvalid')}
        </div>
      ) : (
        <form onSubmit={handleSubmit} noValidate>
          {error && (
            <div
              role="alert"
              className="mb-4 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-default text-[var(--color-danger)]"
            >
              {error}
            </div>
          )}
          <label
            htmlFor="password"
            className="mb-1 block text-default font-medium text-[var(--color-text-primary)]"
          >
            {t('profile.newPassword')}
          </label>
          <input
            id="password"
            type="password"
            required
            minLength={8}
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="input-modern mb-4 w-full"
            placeholder={t('auth.passwordMinLength')}
          />
          <label
            htmlFor="confirm"
            className="mb-1 block text-default font-medium text-[var(--color-text-primary)]"
          >
            {t('profile.confirmPassword')}
          </label>
          <input
            id="confirm"
            type="password"
            required
            minLength={8}
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            className="input-modern w-full"
          />
          <button
            type="submit"
            disabled={submitting}
            className="btn-primary mt-6 w-full rounded-xl text-subhead"
          >
            {submitting ? '...' : t('auth.resetSubmit')}
          </button>
        </form>
      )}

      {!done && (
        <p className="mt-6 text-center text-default">
          <Link to="/login" className="text-[var(--color-accent)] hover:underline">
            {t('auth.backToLogin')}
          </Link>
        </p>
      )}
    </AuthShell>
  );
}
