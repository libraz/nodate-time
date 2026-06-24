import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { AuthShell } from '@/components/auth-shell';
import { useT } from '@/i18n';
import { api } from '@/lib/api';
import { useAuthStore } from '@/stores/auth-store';

type OAuthProvider = 'google' | 'line';

const API_BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';

const ICON_PROPS = {
  width: 18,
  height: 18,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
};

function MailIcon() {
  return (
    <svg {...ICON_PROPS} aria-hidden>
      <rect x="3" y="5" width="18" height="14" rx="2.5" />
      <path d="m3.5 7 7.3 5.1a2 2 0 0 0 2.4 0L20.5 7" />
    </svg>
  );
}

function UserIcon() {
  return (
    <svg {...ICON_PROPS} aria-hidden>
      <circle cx="12" cy="8" r="3.5" />
      <path d="M5 20c0-3.6 3.1-6 7-6s7 2.4 7 6" />
    </svg>
  );
}

function LockIcon() {
  return (
    <svg {...ICON_PROPS} aria-hidden>
      <rect x="4.5" y="10.5" width="15" height="10" rx="2.5" />
      <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5" />
    </svg>
  );
}

function EyeIcon() {
  return (
    <svg {...ICON_PROPS} aria-hidden>
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

function EyeOffIcon() {
  return (
    <svg {...ICON_PROPS} aria-hidden>
      <path d="M3 3l18 18" />
      <path d="M10.6 6.1A8.6 8.6 0 0 1 12 6c6 0 9.5 6 9.5 6a16 16 0 0 1-3 3.6M6.4 8.4A16 16 0 0 0 2.5 12S6 18 12 18a8.3 8.3 0 0 0 3.4-.7" />
      <path d="M9.9 10a3 3 0 0 0 4.2 4.2" />
    </svg>
  );
}

export interface LoginSearch {
  redirect?: string | undefined;
  error?: string | undefined;
}

export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
    error: typeof search.error === 'string' ? search.error : undefined,
  }),
  component: LoginPage,
});

function LoginPage() {
  const t = useT();
  const { redirect, error: oauthError } = Route.useSearch();
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const login = useAuthStore((s) => s.login);
  const devLogin = useAuthStore((s) => s.devLogin);
  const register = useAuthStore((s) => s.register);
  const isLoading = useAuthStore((s) => s.isLoading);
  const error = useAuthStore((s) => s.error);
  const clearError = useAuthStore((s) => s.clearError);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const navigate = useNavigate();

  // Only render social buttons for providers that are actually configured.
  const [providers, setProviders] = useState<OAuthProvider[] | null>(null);
  const [oauthShown, setOauthShown] = useState(false);
  // Whether email+password sign-in is offered; when false the screen is
  // OAuth/OIDC-only. Defaults to true so the form shows while loading.
  const [passwordEnabled, setPasswordEnabled] = useState(true);

  useEffect(() => {
    if (isAuthenticated) {
      const dest = redirect?.startsWith('/') && !redirect.startsWith('//') ? redirect : '/';
      navigate({ to: dest });
    }
  }, [isAuthenticated, navigate, redirect]);

  useEffect(() => {
    let active = true;
    api
      .get<{ providers: OAuthProvider[]; passwordEnabled?: boolean }>('/auth/oauth/providers', true)
      .then((r) => {
        if (!active) return;
        setProviders(r.providers);
        // Only an explicit `false` disables password login; a missing field
        // (older API) keeps the form visible so sign-in never silently breaks.
        setPasswordEnabled(r.passwordEnabled !== false);
      })
      .catch(() => active && setProviders([]));
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (providers && providers.length > 0) {
      const id = requestAnimationFrame(() => setOauthShown(true));
      return () => cancelAnimationFrame(id);
    }
  }, [providers]);

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
      subtitle={
        !passwordEnabled
          ? t('auth.oauthOnlySubtitle')
          : mode === 'login'
            ? t('auth.loginToAccount')
            : t('auth.createAccount')
      }
      footer={
        passwordEnabled ? (
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
        ) : undefined
      }
    >
      <form onSubmit={handleSubmit} noValidate>
        {oauthError === 'oauth_not_allowed' && (
          <div
            role="alert"
            className="mb-4 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-default text-[var(--color-danger)]"
          >
            {t('auth.oauthNotAllowed')}
          </div>
        )}
        {error && (
          <div
            role="alert"
            className="mb-4 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-default text-[var(--color-danger)]"
          >
            {error}
          </div>
        )}

        {passwordEnabled && (
          <>
            {mode === 'register' && (
              <div className="auth-field mb-4" style={{ animationDelay: '0.13s' }}>
                <label
                  htmlFor="name"
                  className="mb-1.5 block text-footnote font-semibold text-[var(--color-text-secondary)]"
                >
                  {t('auth.name')}
                </label>
                <div className="group relative">
                  <span className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)] transition-colors group-focus-within:text-[var(--color-accent)]">
                    <UserIcon />
                  </span>
                  <input
                    id="name"
                    type="text"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    required
                    autoComplete="name"
                    className="input-modern input-leading-icon w-full"
                    placeholder={t('auth.namePlaceholder')}
                  />
                </div>
              </div>
            )}

            <div className="auth-field mb-4" style={{ animationDelay: '0.18s' }}>
              <label
                htmlFor="email"
                className="mb-1.5 block text-footnote font-semibold text-[var(--color-text-secondary)]"
              >
                {t('auth.email')}
              </label>
              <div className="group relative">
                <span className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)] transition-colors group-focus-within:text-[var(--color-accent)]">
                  <MailIcon />
                </span>
                <input
                  id="email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                  autoComplete={mode === 'login' ? 'email' : 'username'}
                  className="input-modern input-leading-icon w-full"
                  placeholder={t('auth.emailPlaceholder')}
                />
              </div>
            </div>

            <div className="auth-field mb-2" style={{ animationDelay: '0.23s' }}>
              <label
                htmlFor="password"
                className="mb-1.5 block text-footnote font-semibold text-[var(--color-text-secondary)]"
              >
                {t('auth.password')}
              </label>
              <div className="group relative">
                <span className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)] transition-colors group-focus-within:text-[var(--color-accent)]">
                  <LockIcon />
                </span>
                <input
                  id="password"
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  minLength={8}
                  autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                  className="input-modern input-leading-icon input-trailing-action w-full"
                  placeholder={t('auth.passwordMinLength')}
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((v) => !v)}
                  aria-label={showPassword ? t('auth.hidePassword') : t('auth.showPassword')}
                  className="absolute right-2 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-lg text-[var(--color-text-tertiary)] transition-colors hover:text-[var(--color-text-secondary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]/40"
                >
                  {showPassword ? <EyeOffIcon /> : <EyeIcon />}
                </button>
              </div>
            </div>

            {mode === 'login' && (
              <div className="mb-4 text-right">
                <Link
                  to="/forgot-password"
                  className="text-body text-[var(--color-accent)] hover:underline"
                >
                  {t('auth.forgotPassword')}
                </Link>
              </div>
            )}

            <button
              type="submit"
              disabled={isLoading}
              style={{ animationDelay: '0.28s' }}
              className="auth-field btn-primary mt-2 w-full rounded-xl text-subhead"
            >
              {isLoading ? (
                <span className="auth-spinner" aria-hidden />
              ) : mode === 'login' ? (
                t('auth.login')
              ) : (
                t('auth.register')
              )}
            </button>
          </>
        )}

        {providers && providers.length > 0 && (
          <div
            className={`transition-all duration-500 ease-out motion-reduce:transition-none motion-reduce:translate-y-0 motion-reduce:opacity-100 ${
              oauthShown ? 'translate-y-0 opacity-100' : 'translate-y-2 opacity-0'
            }`}
          >
            {passwordEnabled && (
              <div className="my-5 flex items-center gap-3 text-footnote text-[var(--color-text-tertiary)]">
                <span aria-hidden className="h-px flex-1 bg-[var(--color-separator)]" />
                <span>{t('auth.oauthOr')}</span>
                <span aria-hidden className="h-px flex-1 bg-[var(--color-separator)]" />
              </div>
            )}

            <div className="space-y-2.5">
              {providers.includes('google') && (
                <a
                  href={`${API_BASE}/auth/oauth/google/start${oauthSuffix}`}
                  className="flex h-[46px] w-full items-center justify-center gap-2.5 rounded-xl border border-[var(--color-border-strong)] bg-white text-default font-semibold text-[#1f1f1f] shadow-sm transition-all duration-200 hover:-translate-y-px hover:shadow-md active:translate-y-0 active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#4285F4]/45"
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
              )}
              {providers.includes('line') && (
                <a
                  href={`${API_BASE}/auth/oauth/line/start${oauthSuffix}`}
                  className="flex h-[46px] w-full items-center justify-center gap-2.5 rounded-xl bg-[#06C755] text-default font-semibold text-white shadow-sm transition-all duration-200 hover:-translate-y-px hover:shadow-md hover:brightness-105 active:translate-y-0 active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#06C755]/45"
                >
                  <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden fill="currentColor">
                    <path d="M19.365 9.89c.40 0 .73.33.73.74 0 .4-.33.73-.73.73h-2.03v1.3h2.03c.4 0 .73.33.73.74 0 .4-.33.73-.73.73h-2.76a.73.73 0 0 1-.73-.73V8.85c0-.4.33-.73.73-.73h2.76c.4 0 .73.33.73.74 0 .4-.33.73-.73.73h-2.03v1.3h2.03zm-3.85 3.51a.73.73 0 0 1-.59.72.74.74 0 0 1-.79-.27l-2.83-3.84v3.39a.73.73 0 1 1-1.46 0V8.85a.73.73 0 0 1 .59-.72.74.74 0 0 1 .8.28l2.81 3.84V8.85a.73.73 0 1 1 1.46 0v4.55zm-6.85 0a.73.73 0 0 1-.73.73.73.73 0 0 1-.73-.73V8.85a.73.73 0 0 1 .73-.73.73.73 0 0 1 .73.73v4.55zm-2.5.73H3.4a.73.73 0 0 1-.73-.73V8.85a.73.73 0 1 1 1.46 0v3.82h2.03a.73.73 0 1 1 0 1.46zM12 0C5.37 0 0 4.39 0 9.81c0 4.86 4.27 8.93 10.04 9.7.39.08.92.26 1.06.59.12.3.08.78.04 1.09l-.17 1.03c-.05.3-.24 1.19 1.03.65 1.27-.54 6.83-4.02 9.31-6.89 1.72-1.88 2.55-3.79 2.55-6.17C23.86 4.39 18.49 0 12 0z" />
                  </svg>
                  {t('auth.oauthLine')}
                </a>
              )}
            </div>
          </div>
        )}
      </form>

      {import.meta.env.DEV && (
        <div className="auth-footer mt-5 rounded-2xl border border-dashed border-[var(--color-border-strong)] bg-[var(--color-surface-inset)] p-3.5">
          <div className="mb-2.5 flex items-center justify-center gap-2 text-footnote font-medium text-[var(--color-text-tertiary)]">
            <span className="rounded-md border border-[var(--color-border-strong)] px-1.5 py-px text-micro font-bold uppercase tracking-wider text-[var(--color-text-secondary)]">
              Dev
            </span>
            {t('auth.quickLogin')}
          </div>
          <div className="grid grid-cols-2 gap-2">
            <button
              type="button"
              onClick={() => devLogin('demo@example.com').catch(() => {})}
              disabled={isLoading}
              className="flex h-[42px] items-center justify-center rounded-xl border border-transparent bg-[var(--color-accent-bg)] text-default font-semibold text-[var(--color-accent)] transition-all hover:bg-[var(--color-accent-subtle)] active:scale-[0.98] disabled:opacity-50"
            >
              {t('auth.demoLogin')}
            </button>
            <button
              type="button"
              onClick={() => devLogin('admin@example.com').catch(() => {})}
              disabled={isLoading}
              className="flex h-[42px] items-center justify-center rounded-xl border border-transparent bg-[var(--color-danger-bg)] text-default font-semibold text-[var(--color-danger)] transition-all hover:brightness-105 active:scale-[0.98] disabled:opacity-50"
            >
              {t('auth.adminLogin')}
            </button>
          </div>
        </div>
      )}
    </AuthShell>
  );
}
