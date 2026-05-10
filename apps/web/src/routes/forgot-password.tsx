import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { ApiError, api } from '@/lib/api';
import { Link, createFileRoute } from '@tanstack/react-router';
import { useState } from 'react';

export const Route = createFileRoute('/forgot-password')({
  component: ForgotPasswordPage,
});

function ForgotPasswordPage() {
  const t = useT();
  const [email, setEmail] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await api.post('/auth/password-reset/request', { email });
      setDone(true);
    } catch (e) {
      setError(e instanceof ApiError ? e.detail : 'Error');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AuthShell
      title={t('auth.forgotPasswordTitle')}
      subtitle={t('auth.forgotPasswordDescription')}
      footer={
        <Link to="/login" className="text-[var(--color-accent)] hover:underline">
          {t('auth.backToLogin')}
        </Link>
      }
    >
      {done ? (
        <output aria-live="polite" className="flex flex-col items-center gap-3 py-4 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-[var(--color-accent-bg)] text-[var(--color-accent)]">
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
          <p className="text-[15px] text-[var(--color-text-primary)]">{t('auth.resetEmailSent')}</p>
        </output>
      ) : (
        <form onSubmit={handleSubmit} noValidate>
          {error && (
            <div
              role="alert"
              className="mb-4 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-[14px] text-[var(--color-danger)]"
            >
              {error}
            </div>
          )}
          <label
            htmlFor="email"
            className="mb-1 block text-[14px] font-medium text-[var(--color-text-primary)]"
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
            className="btn-primary mt-6 w-full rounded-xl text-[16px]"
          >
            {submitting ? '...' : t('auth.sendResetEmail')}
          </button>
        </form>
      )}
    </AuthShell>
  );
}
