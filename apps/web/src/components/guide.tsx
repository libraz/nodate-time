import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useT } from '@/i18n';
import type { HelpTopic } from '@/lib/help-topics';
import { type TipId, useGuideStore, useTipVisible } from '@/stores/guide-store';

/** The accent tint the tip card and help badge share. */
const TINT_STYLE = {
  background: 'color-mix(in srgb, var(--color-accent) 8%, var(--color-surface))',
  borderColor: 'color-mix(in srgb, var(--color-accent) 28%, transparent)',
} as const;

function BulbIcon() {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M9 18h6M10 21h4" />
      <path d="M12 3a6 6 0 00-3.6 10.8c.6.5 1 1.2 1.1 2l.1.7h4.8l.1-.7c.1-.8.5-1.5 1.1-2A6 6 0 0012 3z" />
    </svg>
  );
}

interface TipCardProps {
  id: TipId;
  children: ReactNode;
  /** Help section the "learn more" link opens. */
  topic?: HelpTopic;
  /** Replaces the "learn more" link with an action of the caller's. */
  action?: { label: string; onClick: () => void };
  className?: string;
}

/** A one-time hint, gone for good once dismissed on this device. */
export function TipCard({ id, children, topic, action, className = '' }: TipCardProps) {
  const t = useT();
  const visible = useTipVisible(id);
  const dismissTip = useGuideStore((s) => s.dismissTip);
  if (!visible) return null;

  return (
    <aside
      aria-label={t('tip.label')}
      className={`flex gap-3 rounded-xl border p-3 ${className}`}
      style={TINT_STYLE}
    >
      <span className="mt-0.5 shrink-0 text-[var(--color-accent)]">
        <BulbIcon />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-body leading-relaxed text-[var(--color-text-primary)]">{children}</p>
        <div className="mt-2 flex items-center justify-end gap-1">
          {action ? (
            <button
              type="button"
              onClick={action.onClick}
              className="rounded-lg px-2.5 py-1 text-footnote font-semibold text-[var(--color-accent)] hover:bg-[var(--color-hover)]"
            >
              {action.label}
            </button>
          ) : (
            topic && (
              <Link
                to="/help"
                hash={topic}
                className="rounded-lg px-2.5 py-1 text-footnote font-medium text-[var(--color-accent)] hover:bg-[var(--color-hover)]"
              >
                {t('tip.learnMore')}
              </Link>
            )
          )}
          <button
            type="button"
            onClick={() => dismissTip(id)}
            className="rounded-lg px-2.5 py-1 text-footnote font-medium text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
          >
            {t('tip.dismiss')}
          </button>
        </div>
      </div>
    </aside>
  );
}

interface EmptyStateProps {
  icon: ReactNode;
  title: string;
  description: string;
  action?: { label: string; onClick: () => void } | undefined;
  /** Tighter spacing for narrow places such as the sidebar. */
  compact?: boolean;
}

/** What an empty list is for and the one thing to do next. */
export function EmptyState({ icon, title, description, action, compact }: EmptyStateProps) {
  return (
    <div
      className={`flex flex-col items-center text-center ${compact ? 'px-3 py-5' : 'px-6 py-10'}`}
    >
      <span
        aria-hidden="true"
        className={`mb-3 flex items-center justify-center rounded-full bg-[var(--color-surface-inset)] text-[var(--color-text-secondary)] ${
          compact ? 'h-10 w-10' : 'h-12 w-12'
        }`}
      >
        <span className="h-[22px] w-[22px]">{icon}</span>
      </span>
      <p className="text-default font-semibold text-[var(--color-text-primary)]">{title}</p>
      <p className="mt-1 max-w-[280px] text-body leading-relaxed text-[var(--color-text-secondary)]">
        {description}
      </p>
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          className="btn-primary mt-4 h-9 px-4 text-body font-medium"
        >
          {action.label}
        </button>
      )}
    </div>
  );
}

/** A "?" that opens the help page at one section. */
export function HelpLink({ topic, label }: { topic: HelpTopic; label: string }) {
  const t = useT();
  return (
    <Link
      to="/help"
      hash={topic}
      aria-label={t('help.openTopic', { topic: label })}
      title={t('help.open')}
      className="icon-btn h-8 w-8 text-[var(--color-text-secondary)]"
    >
      <svg
        width="17"
        height="17"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="9" />
        <path d="M9.5 9.5a2.5 2.5 0 014.6 1.3c0 1.7-2.1 2.2-2.1 3.7" />
        <circle cx="12" cy="17.2" r="0.6" fill="currentColor" />
      </svg>
    </Link>
  );
}

/** Line icons for the empty states, drawn on the 24-unit grid the app uses. */
export const GUIDE_ICONS = {
  calendar: (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
    >
      <rect x="3" y="5" width="18" height="16" rx="2" />
      <path d="M3 10h18M8 3v4M16 3v4" />
    </svg>
  ),
  memo: (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
    >
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
      <path d="M14 2v6h6M16 13H8M16 17H8" />
    </svg>
  ),
  photo: (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
    >
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <circle cx="8.5" cy="8.5" r="1.5" />
      <path d="M21 15l-5-5L5 21" />
    </svg>
  ),
  poll: (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
    >
      <rect x="3" y="5" width="18" height="16" rx="2" />
      <path d="M3 10h18M8 3v4M16 3v4M9 15l2 2 4-4" />
    </svg>
  ),
  link: (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
    >
      <path d="M10 13a5 5 0 007.5.5l3-3a5 5 0 00-7-7l-1.7 1.7" />
      <path d="M14 11a5 5 0 00-7.5-.5l-3 3a5 5 0 007 7l1.7-1.7" />
    </svg>
  ),
  activity: (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
    >
      <path d="M12 8v4l3 3" />
      <circle cx="12" cy="12" r="9" />
    </svg>
  ),
} as const;
