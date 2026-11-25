import { useEffect, useId, useRef, useState } from 'react';
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
  /**
   * Text the reader must type before the confirm action unlocks, for an
   * action nobody can undo. Surrounding whitespace is ignored.
   */
  confirmText?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

const SHEET_TONES = {
  dark: {
    card: 'bg-neutral-800/95 text-white',
    hint: 'text-white/80',
    rule: 'border-white/10',
    hover: 'hover:bg-white/5',
    danger: 'text-[var(--color-danger)]',
    accent: 'text-white',
  },
  theme: {
    card: 'glass-surface-heavy ring-1 ring-[var(--color-border)] text-[var(--color-text-primary)]',
    hint: 'text-[var(--color-text-secondary)]',
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
  confirmText,
  onConfirm,
  onCancel,
}: ConfirmSheetProps) {
  const t = useT();
  const titleId = useId();
  const hintId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const typedId = useId();
  const [typed, setTyped] = useState('');
  const panelRef = useModalA11y<HTMLDivElement>(open, onCancel, cancelRef);
  // Every opening starts from an empty field.
  useEffect(() => {
    if (open) setTyped('');
  }, [open]);
  if (!open) return null;
  const locked = confirmText !== undefined && typed.trim() !== confirmText.trim();
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
            <p id={hintId} className={`px-4 pb-3 text-footnote ${c.hint}`}>
              {hint}
            </p>
          )}
          {confirmText !== undefined && (
            <div className="px-4 pb-3.5 text-left">
              <label htmlFor={typedId} className={`mb-1.5 block text-footnote ${c.hint}`}>
                {t('confirm.typeToConfirm', { text: confirmText })}
              </label>
              <input
                id={typedId}
                data-1p-ignore
                type="text"
                autoComplete="off"
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                placeholder={confirmText}
                className="input-modern h-10 w-full text-default"
              />
            </div>
          )}
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy || locked}
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
