import { useT } from '@/i18n';
import { useAuthStore } from '@/stores/auth-store';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';

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
      if (redirect?.startsWith('/')) {
        navigate({ to: redirect });
      } else {
        navigate({ to: '/' });
      }
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
      // navigate is handled by useEffect watching isAuthenticated
    } catch {
      // error is set in store
    }
  };

  const switchMode = () => {
    clearError();
    setPassword('');
    setMode(mode === 'login' ? 'register' : 'login');
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden">
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            'linear-gradient(135deg, var(--color-surface) 0%, var(--color-surface-secondary) 50%, var(--color-surface) 100%)',
        }}
      />
      <div
        className="pointer-events-none absolute -top-40 -right-40 h-80 w-80 rounded-full opacity-20"
        style={{
          background: 'radial-gradient(circle, var(--color-accent), transparent 70%)',
        }}
      />
      <div
        className="pointer-events-none absolute -bottom-40 -left-40 h-80 w-80 rounded-full opacity-15"
        style={{
          background: 'radial-gradient(circle, #a855f7, transparent 70%)',
        }}
      />

      <div className="relative z-10 w-full max-w-[420px] px-6">
        {/* Logo */}
        <div className="mb-8 text-center">
          <div
            className="inline-flex h-18 w-18 items-center justify-center rounded-2xl"
            style={{
              background: 'linear-gradient(135deg, var(--color-accent), #a855f7)',
            }}
          >
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none">
              <rect x="3" y="4" width="18" height="16" rx="2" stroke="white" strokeWidth="2" />
              <line x1="3" y1="9" x2="21" y2="9" stroke="white" strokeWidth="2" />
              <line
                x1="8"
                y1="2"
                x2="8"
                y2="6"
                stroke="white"
                strokeWidth="2"
                strokeLinecap="round"
              />
              <line
                x1="16"
                y1="2"
                x2="16"
                y2="6"
                stroke="white"
                strokeWidth="2"
                strokeLinecap="round"
              />
            </svg>
          </div>
          <h1 className="mt-4 text-[26px] font-bold tracking-tight text-[var(--color-text-primary)]">
            Nodate Time
          </h1>
          <p className="mt-1 text-[15px] text-[var(--color-text-secondary)]">
            {mode === 'login' ? t('auth.loginToAccount') : t('auth.createAccount')}
          </p>
        </div>

        {/* Form */}
        <form
          onSubmit={handleSubmit}
          className="glass-surface-heavy rounded-3xl p-8 ring-1 ring-[var(--color-border)]"
        >
          {error && (
            <div className="mb-4 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-[14px] text-[var(--color-danger)]">
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
              className="input-modern w-full"
              placeholder="email@example.com"
            />
          </div>

          <div className="mb-6">
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
              className="input-modern w-full"
              placeholder={t('auth.passwordMinLength')}
            />
          </div>

          <button
            type="submit"
            disabled={isLoading}
            className="btn-primary w-full rounded-xl text-[16px]"
          >
            {isLoading ? '...' : mode === 'login' ? t('auth.login') : t('auth.register')}
          </button>
        </form>

        {/* Quick demo login (dev only) */}
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

        {/* Switch mode */}
        <p className="mt-6 text-center text-[14px] text-[var(--color-text-secondary)]">
          {mode === 'login' ? t('auth.noAccount') : t('auth.hasAccount')}
          <button
            type="button"
            onClick={switchMode}
            className="ml-1 font-semibold text-[var(--color-accent)] hover:underline"
          >
            {mode === 'login' ? t('auth.newRegistration') : t('auth.login')}
          </button>
        </p>
      </div>
    </div>
  );
}
