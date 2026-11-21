import { useId, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '@/i18n';
import { useModalA11y } from '@/lib/use-modal-a11y';

interface ConfirmSheetProps {
  open: boolean;
  title: string;
  hint?: string;
  confirmLabel: string;
  /** Defaults to `common.cancel`. */
  cancelLabel?: string;
  busy?: boolean;
  /** `dark` over the photo viewer, `theme` everywhere else. */
  tone?: 'dark' | 'theme';
  /** Colours the confirm action as dangerous. Defaults to true. */
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

const SHEET_TONES = {
  dark: {
    card: 'bg-neutral-800/95 text-white',
    hint: 'text-white/60',
    rule: 'border-white/10',
    hover: 'hover:bg-white/5',
    danger: 'text-[var(--color-danger)]',
    accent: 'text-white',
  },
  theme: {
    card: 'glass-surface-heavy ring-1 ring-[var(--color-border)] text-[var(--color-text-primary)]',
    hint: 'text-[var(--color-text-tertiary)]',
    rule: 'border-[var(--color-border)]',
    hover: 'hover:bg-[var(--color-hover)]',
    danger: 'text-[var(--color-danger)]',
    accent: 'text-[var(--color-accent)]',
  },
} as const;

/**
 * A confirmation as a bottom action sheet, portalled above modals and the
 * photo viewer. Focus starts on Cancel and Escape cancels.
 */
export function ConfirmSheet({
  open,
  title,
  hint,
  confirmLabel,
  cancelLabel,
  busy = false,
  tone = 'theme',
  destructive = true,
  onConfirm,
  onCancel,
}: ConfirmSheetProps) {
  const t = useT();
  const titleId = useId();
  const hintId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const panelRef = useModalA11y<HTMLDivElement>(open, onCancel, cancelRef);
  if (!open) return null;
  const c = SHEET_TONES[tone];
  return createPortal(
    <div className="fixed inset-0 z-[65] flex items-end justify-center bg-black/40 p-3 pb-[max(env(safe-area-inset-bottom),0.75rem)]">
      <button
        type="button"
        tabIndex={-1}
        aria-hidden="true"
        className="absolute inset-0 cursor-default"
        onClick={onCancel}
      />
      <div
        ref={panelRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={hint ? hintId : undefined}
        className="confirm-sheet relative flex w-full max-w-[420px] flex-col gap-2"
      >
        <div
          className={`overflow-hidden rounded-[var(--radius-md)] text-center backdrop-blur-xl ${c.card}`}
        >
          <p id={titleId} className="px-4 pt-3.5 pb-1 text-callout font-semibold">
            {title}
          </p>
          {hint && (
            <p id={hintId} className={`px-4 pb-3 text-caption ${c.hint}`}>
              {hint}
            </p>
          )}
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className={`w-full border-t py-3.5 text-callout font-medium transition-colors disabled:opacity-50 ${c.rule} ${c.hover} ${destructive ? c.danger : c.accent}`}
          >
            {busy ? t('common.processing') : confirmLabel}
          </button>
        </div>
        <button
          ref={cancelRef}
          type="button"
          onClick={onCancel}
          disabled={busy}
          className={`w-full rounded-[var(--radius-md)] py-3.5 text-callout font-semibold backdrop-blur-xl transition-colors disabled:opacity-50 ${c.card} ${c.hover}`}
        >
          {cancelLabel ?? t('common.cancel')}
        </button>
      </div>
    </div>,
    document.body,
  );
}
