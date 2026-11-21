import { createFileRoute, Link, Navigate } from '@tanstack/react-router';
import { useState } from 'react';
import { AuthShell } from '@/components/auth-shell';
import { LoadFailure } from '@/components/load-failure';
import { useT } from '@/i18n';
import { useAuthConfig } from '@/lib/auth-config';
import { client, errorMessage } from '@/lib/client';

export const Route = createFileRoute('/forgot-password')({
  component: ForgotPasswordPage,
});

function ForgotPasswordPage() {
  const t = useT();
  const authConfig = useAuthConfig();
  const [email, setEmail] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await client.POST('/auth/password-reset/request', { body: { email } });
      setDone(true);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSubmitting(false);
    }
  };

  // Without mail or password sign-in this flow does not exist; nothing renders
  // until the deployment's config says which.
  if (authConfig.status === 'loading') return null;
  if (authConfig.status === 'error') {
    return (
      <AuthShell title={t('auth.forgotPasswordTitle')}>
        <LoadFailure
          title={t('error.authConfigUnavailable')}
          detail={authConfig.detail}
          onRetry={authConfig.retry}
        />
      </AuthShell>
    );
  }
  if (!authConfig.mailEnabled || !authConfig.passwordEnabled) {
    return <Navigate to="/login" replace />;
  }

  return (
    <AuthShell
      title={t('auth.forgotPasswordTitle')}
      subtitle={t('auth.forgotPasswordDescription')}
      footer={
        <Link to="/login" className="text-[var(--color-accent-ink)] hover:underline">
          {t('auth.backToLogin')}
        </Link>
      }
    >
      {done ? (
        <output aria-live="polite" className="flex flex-col items-center gap-3 py-4 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)]">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden>
              <path
                d="M3 8l9 6 9-6M5 5h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2z"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </div>
          <p className="text-callout text-[var(--color-text-primary)]">
            {t('auth.resetEmailSent')}
          </p>
        </output>
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
            htmlFor="email"
            className="mb-1 block text-default font-medium text-[var(--color-text-primary)]"
          >
            {t('auth.email')}
          </label>
          <input
            id="email"
            type="email"
            required
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="input-modern w-full"
            placeholder={t('auth.emailPlaceholder')}
          />
          <button
            type="submit"
            disabled={submitting}
            className="btn-primary mt-6 w-full text-subhead"
          >
            {submitting ? '...' : t('auth.sendResetEmail')}
          </button>
        </form>
      )}
    </AuthShell>
  );
}
