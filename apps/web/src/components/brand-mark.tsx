import { APP_ICON, APP_LOGO, APP_NAME } from '@/lib/brand';
import type { BrandAsset } from '@/lib/brand-rules';

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
          ? 'auth-brand-mark relative inline-flex h-16 w-16 items-center justify-center rounded-[var(--radius-lg)] shadow-lg'
          : 'relative inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-control)] shadow-sm'
      }
      style={{
        background: 'linear-gradient(135deg, var(--color-accent), var(--color-accent-hover))',
      }}
    >
      {large && <span aria-hidden className="auth-halo" />}
      <svg width={large ? 30 : 16} height={large ? 30 : 16} viewBox="0 0 24 24" fill="none">
        <rect
          x="3"
          y="4"
          width="18"
          height="16"
          rx="2"
          stroke="var(--color-text-on-accent)"
          strokeWidth="2"
        />
        <line x1="3" y1="9" x2="21" y2="9" stroke="var(--color-text-on-accent)" strokeWidth="2" />
        <line
          x1="8"
          y1="2"
          x2="8"
          y2="6"
          stroke="var(--color-text-on-accent)"
          strokeWidth="2"
          strokeLinecap="round"
        />
        <line
          x1="16"
          y1="2"
          x2="16"
          y2="6"
          stroke="var(--color-text-on-accent)"
          strokeWidth="2"
          strokeLinecap="round"
        />
      </svg>
    </div>
  );
}

interface BrandImageProps {
  asset: BrandAsset;
  alt: string;
  /** Size classes only; the component owns its own display. */
  className?: string;
}

/** A brand image that swaps to its dark variant under data-mode="dark". */
export function BrandImage({ asset, alt, className = '' }: BrandImageProps) {
  const img = 'h-full w-auto max-w-full object-contain';
  return (
    <span className={`inline-flex ${className}`}>
      <img
        src={asset.src}
        alt={alt}
        className={asset.darkSrc ? `${img} block [[data-mode=dark]_&]:hidden` : img}
      />
      {asset.darkSrc && (
        <img src={asset.darkSrc} alt={alt} className={`${img} hidden [[data-mode=dark]_&]:block`} />
      )}
    </span>
  );
}

/** The logo alone, or icon or tile followed by the deployment name, for sidebar and drawer headers. */
export function BrandRow() {
  if (APP_LOGO) {
    return (
      <div className="flex min-w-0 items-center">
        <BrandImage asset={APP_LOGO} alt={APP_NAME} className="h-7 max-w-full" />
      </div>
    );
  }
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      {APP_ICON ? (
        <BrandImage asset={APP_ICON} alt="" className="h-7 w-7 shrink-0" />
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

/** The large brand figure above a login card; `alt` is empty when the name is rendered beside it. */
export function BrandHero({ alt }: { alt: string }) {
  if (APP_LOGO) {
    return <BrandImage asset={APP_LOGO} alt={alt} className="mx-auto h-16 max-w-[15rem]" />;
  }
  if (APP_ICON) {
    return <BrandImage asset={APP_ICON} alt="" className="mx-auto h-16 w-16" />;
  }
  return <BrandMark size="lg" />;
}
