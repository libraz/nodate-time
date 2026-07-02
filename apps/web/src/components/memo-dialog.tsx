import { useCallback, useEffect, useRef, useState } from 'react';
import { HistoryTimeline } from '@/components/history-timeline';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/api';
import { toast } from '@/lib/toast';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useCalendarStore } from '@/stores/calendar-store';

interface MemoDialogProps {
  calendarId: string;
  memoId?: string | undefined;
  onClose: () => void;
}

/** Create/edit dialog for a calendar memo (bottom sheet on mobile, modal on desktop). */
export function MemoDialog({ calendarId, memoId, onClose }: MemoDialogProps) {
  const t = useT();
  const memos = useCalendarStore((s) => s.memos);
  const addMemo = useCalendarStore((s) => s.addMemo);
  const updateMemo = useCalendarStore((s) => s.updateMemo);
  const deleteMemo = useCalendarStore((s) => s.deleteMemo);

  const editing = memoId ? (memos.find((m) => m.id === memoId) ?? null) : null;
  const titleRef = useRef<HTMLInputElement>(null);
  const dialogRef = useModalA11y<HTMLDivElement>(true, onClose);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState({ title: '', body: '', done: false });

  useEffect(() => {
    if (editing) {
      setForm({ title: editing.title, body: editing.body ?? '', done: editing.done });
    } else {
      setForm({ title: '', body: '', done: false });
    }
  }, [editing]);

  useEffect(() => {
    setTimeout(() => titleRef.current?.focus(), 100);
  }, []);

  const handleSave = useCallback(async () => {
    const title = form.title.trim();
    if (!title || saving) return;
    setSaving(true);
    try {
      if (editing) {
        await updateMemo(calendarId, editing.id, {
          title,
          body: form.body,
          done: form.done,
        });
      } else {
        await addMemo(calendarId, { title, body: form.body });
      }
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, t('error.saveFailed')));
    } finally {
      setSaving(false);
    }
  }, [form, saving, editing, calendarId, addMemo, updateMemo, onClose, t]);

  const handleDelete = useCallback(async () => {
    if (!editing || saving) return;
    setSaving(true);
    try {
      await deleteMemo(calendarId, editing.id);
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, t('error.deleteFailed')));
    } finally {
      setSaving(false);
    }
  }, [editing, saving, calendarId, deleteMemo, onClose, t]);

  const formContent = (
    <>
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-3">
        <button
          type="button"
          onClick={onClose}
          className="flex h-10 w-10 items-center justify-center bg-[var(--color-surface-secondary)] hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
          style={{ borderRadius: 'var(--radius-sm)' }}
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-text-secondary)"
            strokeWidth="2.5"
          >
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </button>
        <span className="text-callout font-medium text-[var(--color-text-secondary)]">
          {editing ? t('memo.edit') : t('memo.add')}
        </span>
      </div>

      {/* Title — with an inline done checkbox in edit mode (a natural completion gesture) */}
      <div className="flex items-start gap-3 px-6 pt-1 pb-3">
        {editing && (
          <button
            type="button"
            onClick={() => setForm((f) => ({ ...f, done: !f.done }))}
            aria-pressed={form.done}
            aria-label={t('memo.done')}
            className="mt-1 shrink-0"
          >
            {form.done ? (
              <svg width="26" height="26" viewBox="0 0 24 24" fill="none">
                <circle cx="12" cy="12" r="9" fill="var(--color-accent)" />
                <path
                  d="M8 12l3 3 5-5"
                  stroke="var(--color-text-on-accent)"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            ) : (
              <svg width="26" height="26" viewBox="0 0 24 24" fill="none">
                <circle cx="12" cy="12" r="9" stroke="var(--color-text-tertiary)" strokeWidth="2" />
              </svg>
            )}
          </button>
        )}
        <input
          ref={titleRef}
          type="text"
          value={form.title}
          onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
          placeholder={t('memo.title')}
          className="flex-1 bg-transparent text-heading font-semibold text-[var(--color-text-primary)] outline-none placeholder:font-normal placeholder:text-[var(--color-text-tertiary)]"
          style={{
            textDecoration: form.done ? 'line-through' : 'none',
            color: form.done ? 'var(--color-text-tertiary)' : undefined,
          }}
        />
      </div>

      <div className="mx-6 border-t border-[var(--color-border)]" />

      {/* Body — a clean note area, not a boxed card */}
      <div className="px-6 py-3">
        <textarea
          value={form.body}
          onChange={(e) => setForm((f) => ({ ...f, body: e.target.value }))}
          placeholder={t('memo.bodyPlaceholder')}
          rows={4}
          maxLength={16000}
          className="w-full resize-none bg-transparent text-callout leading-relaxed text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
        />
      </div>

      {/* History (edit mode only) */}
      {editing && (
        <div className="border-t border-[var(--color-border)] px-6 pt-4 pb-2">
          <div className="mb-3 flex items-center gap-2 text-[var(--color-text-tertiary)]">
            <svg
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M12 8v4l3 3" />
              <circle cx="12" cy="12" r="9" />
            </svg>
            <span className="text-footnote font-semibold uppercase tracking-wide">
              {t('history.title')}
            </span>
          </div>
          <HistoryTimeline kind="memo" calendarId={calendarId} entityId={editing.id} />
        </div>
      )}

      {/* Delete (edit mode only) — a quiet text affordance, not a heavy block */}
      {editing && (
        <div className="px-6 pt-3 pb-1">
          <button
            type="button"
            onClick={handleDelete}
            disabled={saving}
            className="inline-flex items-center gap-1.5 text-footnote font-medium text-[var(--color-danger)] transition-opacity hover:opacity-70 disabled:opacity-50"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6" />
            </svg>
            {t('common.delete')}
          </button>
        </div>
      )}

      {/* Spacer so content doesn't hide behind sticky action bar on mobile */}
      <div className="h-20 sm:h-0" />
    </>
  );

  const actionBar = (
    <div className="flex gap-3 border-t border-[var(--color-border)] px-6 py-4">
      <button
        type="button"
        onClick={onClose}
        className="btn-secondary flex-1 py-3 text-default font-medium"
      >
        {t('common.cancel')}
      </button>
      <button
        type="button"
        onClick={handleSave}
        disabled={saving || !form.title.trim()}
        className="btn-primary flex-1 py-3 text-default font-medium disabled:opacity-50"
      >
        {saving ? t('common.saving') : t('common.save')}
      </button>
    </div>
  );

  return (
    <div ref={dialogRef}>
      {/* Mobile: bottom sheet */}
      <div className="sm:hidden">
        <button
          type="button"
          aria-label={t('common.close')}
          className="modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)]"
          onClick={onClose}
        />
        <div className="glass-surface-heavy bottom-sheet fixed inset-x-0 bottom-0 z-50 flex max-h-[92vh] flex-col overflow-hidden">
          <div className="drag-handle mx-auto mt-2 mb-1 h-1 w-10 rounded-full bg-[var(--color-text-tertiary)] opacity-30" />
          <div className="flex-1 overflow-y-auto">{formContent}</div>
          {actionBar}
        </div>
      </div>

      {/* Desktop: centered modal */}
      <div className="hidden sm:contents">
        <button
          type="button"
          aria-label={t('common.close')}
          className="modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)]"
          onClick={onClose}
        />
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 pointer-events-none">
          <div className="glass-surface-heavy modal-panel pointer-events-auto flex w-full max-w-[480px] max-h-[90vh] flex-col overflow-hidden ring-1 ring-[var(--color-border)]">
            <div className="flex-1 overflow-y-auto">{formContent}</div>
            {actionBar}
          </div>
        </div>
      </div>
    </div>
  );
}
