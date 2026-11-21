import type { ReactNode } from 'react';

interface CalloutProps {
  /** `warning` for a consequence the reader must know before acting. Defaults to `info`. */
  tone?: 'info' | 'warning';
  children: ReactNode;
  className?: string;
}

/** A boxed note placed ahead of the content or action it qualifies. */
export function Callout({ tone = 'info', children, className = '' }: CalloutProps) {
  return (
    <div
      className={`callout ${tone === 'warning' ? 'callout-warning' : ''} text-footnote text-[var(--color-text-primary)] ${className}`}
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        className="callout-icon"
        aria-hidden="true"
      >
        {tone === 'warning' ? (
          <>
            <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </>
        ) : (
          <>
            <circle cx="12" cy="12" r="9" />
            <path d="M12 11v5M12 8h.01" />
          </>
        )}
      </svg>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
