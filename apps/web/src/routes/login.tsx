import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { useAuthStore } from '@/stores/auth-store';

const API_BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';

export interface LoginSearch {
  redirect?: string | undefined;
}

export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
  }),
  component: LoginPage,
});

function LoginPage() {
  const t = useT();
  const { redirect } = Route.useSearch();
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const login = useAuthStore((s) => s.login);
  const register = useAuthStore((s) => s.register);
  const isLoading = useAuthStore((s) => s.isLoading);
  const error = useAuthStore((s) => s.error);
  const clearError = useAuthStore((s) => s.clearError);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const navigate = useNavigate();

  useEffect(() => {
    if (isAuthenticated) {
      const dest = redirect?.startsWith('/') && !redirect.startsWith('//') ? redirect : '/';
      navigate({ to: dest });
    }
  }, [isAuthenticated, navigate, redirect]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      if (mode === 'login') {
        await login(email, password);
      } else {
        await register(name, email, password);
      }
      // navigation handled by useEffect on isAuthenticated
    } catch {
      // error is set in store
    }
  };

  const switchMode = () => {
    clearError();
    setPassword('');
    setMode(mode === 'login' ? 'register' : 'login');
  };

  const oauthSuffix = redirect ? `?redirect=${encodeURIComponent(redirect)}` : '';

  return (
    <AuthShell
      title="Nodate Time"
      subtitle={mode === 'login' ? t('auth.loginToAccount') : t('auth.createAccount')}
      footer={
        <span className="text-[var(--color-text-secondary)]">
          {mode === 'login' ? t('auth.noAccount') : t('auth.hasAccount')}
          <button
            type="button"
            onClick={switchMode}
            className="ml-1 font-semibold text-[var(--color-accent)] hover:underline"
          >
            {mode === 'login' ? t('auth.newRegistration') : t('auth.login')}
          </button>
        </span>
      }
    >
      <form onSubmit={handleSubmit} noValidate>
        {error && (
          <div
            role="alert"
            className="mb-4 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-[14px] text-[var(--color-danger)]"
          >
            {error}
          </div>
        )}

        {mode === 'register' && (
          <div className="mb-4">
            <label
              htmlFor="name"
              className="mb-1 block text-[14px] font-medium text-[var(--color-text-primary)]"
            >
              {t('auth.name')}
            </label>
            <input
              id="name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              autoComplete="name"
              className="input-modern w-full"
              placeholder={t('auth.namePlaceholder')}
            />
          </div>
        )}

        <div className="mb-4">
          <label
            htmlFor="email"
            className="mb-1 block text-[14px] font-medium text-[var(--color-text-primary)]"
          >
            {t('auth.email')}
          </label>
          <input
            id="email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoComplete={mode === 'login' ? 'email' : 'username'}
            className="input-modern w-full"
            placeholder={t('auth.emailPlaceholder')}
          />
        </div>

        <div className="mb-2">
          <label
            htmlFor="password"
            className="mb-1 block text-[14px] font-medium text-[var(--color-text-primary)]"
          >
            {t('auth.password')}
          </label>
          <input
            id="password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            minLength={8}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            className="input-modern w-full"
            placeholder={t('auth.passwordMinLength')}
          />
        </div>

        {mode === 'login' && (
          <div className="mb-4 text-right">
            <Link
              to="/forgot-password"
              className="text-[13px] text-[var(--color-accent)] hover:underline"
            >
              {t('auth.forgotPassword')}
            </Link>
          </div>
        )}

        <button
          type="submit"
          disabled={isLoading}
          className="btn-primary mt-2 w-full rounded-xl text-[16px]"
        >
          {isLoading ? '...' : mode === 'login' ? t('auth.login') : t('auth.register')}
        </button>

        <div className="my-5 flex items-center gap-3 text-[12px] text-[var(--color-text-tertiary)]">
          <span aria-hidden className="h-px flex-1 bg-[var(--color-separator)]" />
          <span>{t('auth.oauthOr')}</span>
          <span aria-hidden className="h-px flex-1 bg-[var(--color-separator)]" />
        </div>

        <div className="space-y-2">
          <a
            href={`${API_BASE}/auth/oauth/google/start${oauthSuffix}`}
            className="flex h-[44px] w-full items-center justify-center gap-2 rounded-xl border border-[var(--color-border)] bg-white text-[14px] font-semibold text-[#1f1f1f] transition hover:bg-[#f5f5f5]"
          >
            <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden>
              <path
                fill="#4285F4"
                d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.49h4.84a4.14 4.14 0 0 1-1.8 2.71v2.26h2.92c1.71-1.58 2.7-3.9 2.7-6.62z"
              />
              <path
                fill="#34A853"
                d="M9 18c2.43 0 4.47-.81 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.03-3.7H.96v2.34A9 9 0 0 0 9 18z"
              />
              <path
                fill="#FBBC05"
                d="M3.97 10.71a5.41 5.41 0 0 1 0-3.42V4.96H.96a9 9 0 0 0 0 8.08l3.01-2.33z"
              />
              <path
                fill="#EA4335"
                d="M9 3.58c1.32 0 2.5.45 3.44 1.34l2.58-2.58A8.97 8.97 0 0 0 9 0 9 9 0 0 0 .96 4.96l3.01 2.33C4.68 5.16 6.66 3.58 9 3.58z"
              />
            </svg>
            {t('auth.oauthGoogle')}
          </a>
          <a
            href={`${API_BASE}/auth/oauth/line/start${oauthSuffix}`}
            className="flex h-[44px] w-full items-center justify-center gap-2 rounded-xl bg-[#06C755] text-[14px] font-semibold text-white transition hover:brightness-110"
          >
            <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden fill="currentColor">
              <path d="M19.365 9.89c.40 0 .73.33.73.74 0 .4-.33.73-.73.73h-2.03v1.3h2.03c.4 0 .73.33.73.74 0 .4-.33.73-.73.73h-2.76a.73.73 0 0 1-.73-.73V8.85c0-.4.33-.73.73-.73h2.76c.4 0 .73.33.73.74 0 .4-.33.73-.73.73h-2.03v1.3h2.03zm-3.85 3.51a.73.73 0 0 1-.59.72.74.74 0 0 1-.79-.27l-2.83-3.84v3.39a.73.73 0 1 1-1.46 0V8.85a.73.73 0 0 1 .59-.72.74.74 0 0 1 .8.28l2.81 3.84V8.85a.73.73 0 1 1 1.46 0v4.55zm-6.85 0a.73.73 0 0 1-.73.73.73.73 0 0 1-.73-.73V8.85a.73.73 0 0 1 .73-.73.73.73 0 0 1 .73.73v4.55zm-2.5.73H3.4a.73.73 0 0 1-.73-.73V8.85a.73.73 0 1 1 1.46 0v3.82h2.03a.73.73 0 1 1 0 1.46zM12 0C5.37 0 0 4.39 0 9.81c0 4.86 4.27 8.93 10.04 9.7.39.08.92.26 1.06.59.12.3.08.78.04 1.09l-.17 1.03c-.05.3-.24 1.19 1.03.65 1.27-.54 6.83-4.02 9.31-6.89 1.72-1.88 2.55-3.79 2.55-6.17C23.86 4.39 18.49 0 12 0z" />
            </svg>
            {t('auth.oauthLine')}
          </a>
        </div>
      </form>

      {import.meta.env.DEV && (
        <button
          type="button"
          onClick={() => login('demo@example.com', 'password123').catch(() => {})}
          disabled={isLoading}
          className="mt-4 h-[44px] w-full rounded-xl border-2 border-dashed border-[var(--color-accent)] text-[14px] font-semibold text-[var(--color-accent)] transition-colors hover:bg-[var(--color-accent-bg)] disabled:opacity-50"
        >
          {t('auth.demoLogin')} (demo@example.com)
        </button>
      )}
    </AuthShell>
  );
}
