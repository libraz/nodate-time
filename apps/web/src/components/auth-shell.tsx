import type { ReactNode } from 'react';

interface AuthShellProps {
  title: string;
  subtitle?: string;
  children: ReactNode;
  footer?: ReactNode;
}

/**
 * Shared visual chrome for the auth pages (login, forgot password, reset
 * password, oauth complete). Provides the gradient background, brand mark,
 * and the glassy card frame so each page only renders its form contents.
 */
export function AuthShell({ title, subtitle, children, footer }: AuthShellProps) {
  return (
    <div className="app-bg relative flex min-h-screen items-center justify-center overflow-hidden">
      {/* Atmospheric depth — drifting accent orbs + film grain (glass themes only) */}
      <div aria-hidden className="auth-atmosphere">
        <span className="auth-orb auth-orb-1" />
        <span className="auth-orb auth-orb-2" />
        <span className="auth-orb auth-orb-3" />
        <span className="auth-grain" />
      </div>

      <div className="relative z-10 w-full max-w-[420px] px-6">
        <div className="auth-brand mb-8 text-center">
          <div
            aria-hidden
            className="auth-brand-mark relative inline-flex h-16 w-16 items-center justify-center rounded-2xl shadow-lg"
            style={{
              background: 'linear-gradient(135deg, var(--color-accent), var(--color-accent-hover))',
            }}
          >
            <span aria-hidden className="auth-halo" />
            <svg width="30" height="30" viewBox="0 0 24 24" fill="none">
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
          <h1 className="auth-title mt-4 text-display font-bold tracking-tight text-[var(--color-text-primary)]">
            {title}
          </h1>
          {subtitle && (
            <p className="mt-1.5 text-callout text-[var(--color-text-secondary)]">{subtitle}</p>
          )}
        </div>

        <div className="glass-surface-heavy auth-card rounded-3xl p-7 ring-1 ring-[var(--color-border)] sm:p-8">
          {children}
        </div>

        {footer && <div className="auth-footer mt-6 text-center text-default">{footer}</div>}
      </div>
    </div>
  );
}
