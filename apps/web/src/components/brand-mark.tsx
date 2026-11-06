import { APP_LOGO_URL, APP_NAME } from '@/lib/brand';

interface BrandMarkProps {
  size: 'lg' | 'sm';
}

/** The calendar tile shown when a deployment has no logo of its own. */
export function BrandMark({ size }: BrandMarkProps) {
  const large = size === 'lg';
  return (
    <div
      aria-hidden
      data-brand-mark={size}
      className={
        large
          ? 'auth-brand-mark relative inline-flex h-16 w-16 items-center justify-center rounded-2xl shadow-lg'
          : 'relative inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg shadow-sm'
      }
      style={{
        background: 'linear-gradient(135deg, var(--color-accent), var(--color-accent-hover))',
      }}
    >
      {large && <span aria-hidden className="auth-halo" />}
      <svg width={large ? 30 : 16} height={large ? 30 : 16} viewBox="0 0 24 24" fill="none">
        <rect x="3" y="4" width="18" height="16" rx="2" stroke="white" strokeWidth="2" />
        <line x1="3" y1="9" x2="21" y2="9" stroke="white" strokeWidth="2" />
        <line x1="8" y1="2" x2="8" y2="6" stroke="white" strokeWidth="2" strokeLinecap="round" />
        <line x1="16" y1="2" x2="16" y2="6" stroke="white" strokeWidth="2" strokeLinecap="round" />
      </svg>
    </div>
  );
}

/** Logo or tile followed by the deployment name, for sidebar and drawer headers. */
export function BrandRow() {
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      {APP_LOGO_URL ? (
        <img
          src={APP_LOGO_URL}
          alt=""
          className="h-7 w-auto max-w-[8rem] shrink-0 object-contain"
        />
      ) : (
        <BrandMark size="sm" />
      )}
      <span
        className="truncate text-callout font-semibold text-[var(--color-text-primary)]"
        title={APP_NAME}
      >
        {APP_NAME}
      </span>
    </div>
  );
}
