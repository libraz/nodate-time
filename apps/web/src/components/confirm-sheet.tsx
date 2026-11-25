import { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '@/i18n';
import { useIsDesktop } from '@/lib/use-is-desktop';
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
    panel: 'bg-neutral-800/95 text-white ring-1 ring-white/10',
    secondary: 'rounded-[var(--radius-control)] bg-white/10 text-white hover:bg-white/15',
  },
  theme: {
    card: 'glass-surface-heavy ring-1 ring-[var(--color-border)] text-[var(--color-text-primary)]',
    hint: 'text-[var(--color-text-secondary)]',
    rule: 'border-[var(--color-border)]',
    hover: 'hover:bg-[var(--color-hover)]',
    danger: 'text-[var(--color-danger)]',
    accent: 'text-[var(--color-accent)]',
    panel: 'glass-surface-heavy ring-1 ring-[var(--color-border)] text-[var(--color-text-primary)]',
    secondary: 'btn-secondary',
  },
} as const;

/**
 * A confirmation portalled above modals and the photo viewer: a bottom action
 * sheet on a phone, a centred dialog with a right-aligned button row from the
 * `sm` breakpoint up. Focus starts on Cancel and Escape cancels.
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
  const isDesktop = useIsDesktop();
  // Every opening starts from an empty field.
  useEffect(() => {
    if (open) setTyped('');
  }, [open]);
  if (!open) return null;
  const locked = confirmText !== undefined && typed.trim() !== confirmText.trim();
  const c = SHEET_TONES[tone];
  const confirmField = confirmText !== undefined && (
    <div className={isDesktop ? 'mt-4' : 'px-4 pb-3.5 text-left'}>
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
  );
  const confirmCaption = busy ? t('common.processing') : confirmLabel;
  const cancelCaption = cancelLabel ?? t('common.cancel');
  const dialogProps = {
    ref: panelRef,
    role: 'alertdialog',
    'aria-modal': true,
    'aria-labelledby': titleId,
    'aria-describedby': hint ? hintId : undefined,
  } as const;
  return createPortal(
    <div
      className={`fixed inset-0 z-[65] flex justify-center bg-black/40 ${
        isDesktop
          ? 'items-center p-6'
          : 'items-end p-3 pb-[max(env(safe-area-inset-bottom),0.75rem)]'
      }`}
    >
      <button
        type="button"
        tabIndex={-1}
        aria-hidden="true"
        className="absolute inset-0 cursor-default"
        onClick={onCancel}
      />
      {isDesktop ? (
        <div
          {...dialogProps}
          className={`confirm-dialog modal-panel relative w-full max-w-[400px] p-5 backdrop-blur-xl ${c.panel}`}
        >
          <p id={titleId} className="text-subhead font-semibold">
            {title}
          </p>
          {hint && (
            <p id={hintId} className={`mt-1.5 text-default leading-relaxed ${c.hint}`}>
              {hint}
            </p>
          )}
          {confirmField}
          <div className="mt-5 flex justify-end gap-2">
            <button
              ref={cancelRef}
              type="button"
              onClick={onCancel}
              disabled={busy}
              className={`btn-sm font-semibold transition-colors disabled:opacity-50 ${c.secondary}`}
            >
              {cancelCaption}
            </button>
            <button
              type="button"
              onClick={onConfirm}
              disabled={busy || locked}
              className={`btn-sm disabled:opacity-50 ${destructive ? 'btn-secondary btn-danger' : 'btn-primary'}`}
            >
              {confirmCaption}
            </button>
          </div>
        </div>
      ) : (
        <div
          {...dialogProps}
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
            {confirmField}
            <button
              type="button"
              onClick={onConfirm}
              disabled={busy || locked}
              className={`w-full border-t py-3.5 text-callout font-medium transition-colors disabled:opacity-50 ${c.rule} ${c.hover} ${destructive ? c.danger : c.accent}`}
            >
              {confirmCaption}
            </button>
          </div>
          <button
            ref={cancelRef}
            type="button"
            onClick={onCancel}
            disabled={busy}
            className={`w-full rounded-[var(--radius-md)] py-3.5 text-callout font-semibold backdrop-blur-xl transition-colors disabled:opacity-50 ${c.card} ${c.hover}`}
          >
            {cancelCaption}
          </button>
        </div>
      )}
    </div>,
    document.body,
  );
}
