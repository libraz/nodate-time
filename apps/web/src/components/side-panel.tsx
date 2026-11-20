import type { ReactNode } from 'react';
import { HelpLink } from '@/components/guide';
import { useT } from '@/i18n';
import type { HelpTopic } from '@/lib/help-topics';
import { useModalA11y } from '@/lib/use-modal-a11y';
import type { Presence } from '@/lib/use-presence';

interface SidePanelProps {
  presence: Presence;
  /** Called by the backdrop, the close button and Escape. */
  onClose: () => void;
  title: string;
  /** Controls placed before the close button in the header. */
  actions?: ReactNode;
  /** Help section a "?" in the header opens. */
  helpTopic?: HelpTopic;
  children: ReactNode;
}

/**
 * The right-hand sheet the memo, album, members, share and activity panels
 * share: a backdrop, a header with the close control, and an entrance and exit
 * that play the same way for every one of them.
 */
export function SidePanel({
  presence,
  onClose,
  title,
  actions,
  helpTopic,
  children,
}: SidePanelProps) {
  const t = useT();
  const { mounted, closing } = presence;
  const panelRef = useModalA11y<HTMLDivElement>(mounted && !closing, onClose);

  if (!mounted) return null;

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className={`modal-backdrop fixed inset-0 z-40 bg-[var(--color-overlay)] ${
          closing ? 'backdrop-out pointer-events-none' : ''
        }`}
        onClick={onClose}
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={`glass-surface-heavy side-panel fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)] ${
          closing ? 'side-panel-exit pointer-events-none' : ''
        }`}
      >
        <div className="flex items-center justify-between gap-3 border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="min-w-0 truncate text-subhead font-semibold">{title}</h2>
          <div className="flex shrink-0 items-center gap-1.5">
            {actions}
            {helpTopic && <HelpLink topic={helpTopic} label={title} />}
            <button
              type="button"
              onClick={onClose}
              className="icon-btn h-8 w-8"
              aria-label={t('common.close')}
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                aria-hidden="true"
              >
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>
        {children}
      </div>
    </>
  );
}
