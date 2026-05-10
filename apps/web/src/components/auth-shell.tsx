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
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            'linear-gradient(135deg, var(--color-surface) 0%, var(--color-surface-secondary) 50%, var(--color-surface) 100%)',
        }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 -right-40 h-80 w-80 rounded-full opacity-20"
        style={{ background: 'radial-gradient(circle, var(--color-accent), transparent 70%)' }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -bottom-40 -left-40 h-80 w-80 rounded-full opacity-15"
        style={{ background: 'radial-gradient(circle, #a855f7, transparent 70%)' }}
      />

      <div className="relative z-10 w-full max-w-[420px] px-6">
        <div className="mb-8 text-center">
          <div
            aria-hidden
            className="inline-flex h-16 w-16 items-center justify-center rounded-2xl shadow-lg"
            style={{ background: 'linear-gradient(135deg, var(--color-accent), #a855f7)' }}
          >
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
          <h1 className="mt-4 text-[24px] font-bold tracking-tight text-[var(--color-text-primary)]">
            {title}
          </h1>
          {subtitle && (
            <p className="mt-1 text-[15px] text-[var(--color-text-secondary)]">{subtitle}</p>
          )}
        </div>

        <div className="glass-surface-heavy rounded-3xl p-7 ring-1 ring-[var(--color-border)] sm:p-8">
          {children}
        </div>

        {footer && <div className="mt-6 text-center text-[14px]">{footer}</div>}
      </div>
    </div>
  );
}
