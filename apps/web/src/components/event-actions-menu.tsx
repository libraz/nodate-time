import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { handlePopupKeys, useOutsideClose, useSheetPicker } from '@/components/picker-surface';
import { useT } from '@/i18n';

/**
 * The "···" button in the event panel's header and the menu it opens. It holds
 * the destructive action, so delete sits away from Save at the foot of the panel.
 * Below `sm` the menu opens as a bottom sheet, like the pickers do.
 */
export function EventActionsMenu({
  deleteLabel,
  disabled,
  onDelete,
}: {
  deleteLabel: string;
  disabled: boolean;
  onDelete: () => void;
}) {
  const t = useT();
  const sheet = useSheetPicker();
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState({ top: 0, right: 0 });
  const anchorRef = useRef<HTMLButtonElement>(null);
  const floatingRef = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useOutsideClose([floatingRef, anchorRef], close, open);

  // Right-aligned under the button, since it sits at the panel's right edge.
  useLayoutEffect(() => {
    if (!open || !anchorRef.current) return;
    const r = anchorRef.current.getBoundingClientRect();
    setPos({ top: r.bottom + 4, right: window.innerWidth - r.right });
  }, [open]);

  useEffect(() => {
    if (open) floatingRef.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus();
  }, [open]);

  const dismiss = () => {
    setOpen(false);
    anchorRef.current?.focus();
  };

  const item = (
    <button
      type="button"
      role="menuitem"
      onClick={() => {
        setOpen(false);
        onDelete();
      }}
      className={`flex w-full items-center gap-3 text-left text-default text-[var(--color-danger)] hover:bg-[var(--color-hover)] ${
        sheet ? 'rounded-[var(--radius-md)] px-3 py-3.5' : 'px-4 py-2.5'
      }`}
    >
      <svg
        aria-hidden
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        className="shrink-0"
      >
        <path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6" />
      </svg>
      {deleteLabel}
    </button>
  );

  return (
    <>
      <button
        ref={anchorRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        disabled={disabled}
        aria-label={t('event.moreActions')}
        aria-haspopup="menu"
        aria-expanded={open}
        className="icon-btn h-10 w-10 text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)] disabled:opacity-50"
      >
        <svg aria-hidden width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
          <circle cx="5" cy="12" r="1.8" />
          <circle cx="12" cy="12" r="1.8" />
          <circle cx="19" cy="12" r="1.8" />
        </svg>
      </button>
      {open &&
        createPortal(
          sheet ? (
            <>
              <div
                aria-hidden
                className="modal-backdrop fixed inset-0 z-[9998] bg-[var(--color-overlay)]"
              />
              <div
                ref={floatingRef}
                role="menu"
                aria-label={t('event.moreActions')}
                onKeyDown={(e) => handlePopupKeys(e, dismiss)}
                className="glass-surface-heavy bottom-sheet fixed inset-x-0 bottom-0 z-[9999] rounded-t-[var(--radius-xl)] px-4 pt-2 pb-[max(env(safe-area-inset-bottom),16px)] ring-1 ring-[var(--color-border)]"
                style={{ boxShadow: 'var(--shadow-elevated)' }}
              >
                <div
                  aria-hidden
                  className="mx-auto mb-2 h-1 w-9 rounded-full bg-[var(--color-border-strong)]"
                />
                {item}
              </div>
            </>
          ) : (
            <div
              ref={floatingRef}
              role="menu"
              aria-label={t('event.moreActions')}
              onKeyDown={(e) => handlePopupKeys(e, dismiss)}
              className="dropdown-panel fixed z-[9999] min-w-48 bg-[var(--color-surface-elevated)] py-1 ring-1 ring-[var(--color-border)]"
              style={{
                top: pos.top,
                right: pos.right,
                boxShadow: 'var(--shadow-elevated)',
                backdropFilter: 'blur(20px)',
              }}
            >
              {item}
            </div>
          ),
          document.body,
        )}
    </>
  );
}
