import type { ReactNode } from 'react';
import { BrandHero } from '@/components/brand-mark';
import { APP_LOGO, APP_NAME } from '@/lib/brand';

interface AuthShellProps {
  /** Page heading; omitted, the heading is the deployment name. */
  title?: string;
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
    // Scrolls itself: the body never does, and a short screen or an open
    // keyboard leaves the form taller than the viewport.
    <div className="app-bg relative h-full overflow-y-auto">
      {/* Atmospheric depth — drifting accent orbs + film grain (glass themes only) */}
      <div aria-hidden className="auth-atmosphere">
        <span className="auth-orb auth-orb-1" />
        <span className="auth-orb auth-orb-2" />
        <span className="auth-orb auth-orb-3" />
        <span className="auth-grain" />
      </div>

      <div className="relative z-10 flex min-h-full items-center justify-center py-8">
        <div className="w-full max-w-[420px] px-6">
          <div className="auth-brand mb-8 text-center">
            <BrandHero alt={title === undefined ? '' : APP_NAME} />
            <h1
              className={`auth-title mt-4 text-display font-bold tracking-tight text-[var(--color-text-primary)]${
                title === undefined && APP_LOGO ? ' sr-only' : ''
              }`}
            >
              {title ?? APP_NAME}
            </h1>
            {subtitle && (
              <p className="mt-1.5 text-callout text-[var(--color-text-secondary)]">{subtitle}</p>
            )}
          </div>

          <div className="glass-surface-heavy auth-card rounded-[var(--radius-xl)] p-7 ring-1 ring-[var(--color-border)] sm:p-8">
            {children}
          </div>

          {footer && <div className="auth-footer mt-6 text-center text-default">{footer}</div>}
        </div>
      </div>
    </div>
  );
}
