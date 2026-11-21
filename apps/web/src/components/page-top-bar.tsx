import { Link } from '@tanstack/react-router';
import { BrandRow } from '@/components/brand-mark';

/** The row above a full-page screen: the way back on the left, the brand on the right. */
export function PageTopBar({ to, label }: { to: '/' | '/schedule-polls'; label: string }) {
  return (
    <div className="mb-5 flex items-center justify-between gap-4">
      <Link
        to={to}
        className="-ml-2 inline-flex h-9 items-center gap-1 rounded-[var(--radius-control)] pr-3 pl-1.5 text-body font-medium text-[var(--color-accent-ink)] transition-colors hover:bg-[var(--color-hover)]"
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M15 18l-6-6 6-6" />
        </svg>
        {label}
      </Link>
      <BrandRow />
    </div>
  );
}
