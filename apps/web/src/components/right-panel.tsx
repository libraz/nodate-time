import { useNavigate } from '@tanstack/react-router';
import { useMemo, useState } from 'react';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { EmptyState, GUIDE_ICONS } from '@/components/guide';
import { LabelledButton } from '@/components/labelled-button';
import { MemoCalendarTag, MemoDialog } from '@/components/memo-dialog';
import { CustomSelect } from '@/components/pickers';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/api';
import { canEdit } from '@/lib/permissions';
import { THEME_OPTIONS } from '@/lib/theme';
import { toast } from '@/lib/toast';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { Calendar, Memo } from '@/types/calendar';

export function SettingsModal() {
  const t = useT();
  const showSettings = useUiStore((s) => s.showSettings);
  const toggleSettings = useUiStore((s) => s.toggleSettings);
  const panelRef = useModalA11y<HTMLDivElement>(showSettings, toggleSettings);
  const theme = useUiStore((s) => s.theme);
  const colorMode = useUiStore((s) => s.colorMode);
  const locale = useUiStore((s) => s.locale);
  const setTheme = useUiStore((s) => s.setTheme);
  const setColorMode = useUiStore((s) => s.setColorMode);
  const saveAccountPreference = useAuthStore((s) => s.saveAccountPreference);
  const navigate = useNavigate();

  if (!showSettings) return null;

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)]"
        onClick={toggleSettings}
      />

      <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div
          ref={panelRef}
          role="dialog"
          aria-modal="true"
          aria-label={t('settings.appearance')}
          className="glass-surface-heavy modal-panel w-full max-w-[400px] ring-1 ring-[var(--color-border)]"
        >
          <div className="flex items-center justify-between px-6 py-4">
            <h2 className="text-title font-semibold text-[var(--color-text-primary)]">
              {t('settings.appearance')}
            </h2>
            <LabelledButton
              label={t('common.close')}
              onClick={toggleSettings}
              className="icon-btn h-9 w-9"
            >
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </LabelledButton>
          </div>

          <div className="px-6 pb-6 space-y-5">
            {/* Theme */}
            <div>
              <span className="mb-2 block text-body text-[var(--color-text-primary)]">
                {t('settings.theme')}
              </span>
              <div className="segmented-control w-full">
                {THEME_OPTIONS.map((o) => (
                  <button
                    key={o.value}
                    type="button"
                    data-active={theme === o.value}
                    onClick={() => setTheme(o.value)}
                    className="flex-1"
                  >
                    {t(o.labelKey)}
                  </button>
                ))}
              </div>
            </div>

            {/* Color mode */}
            <div>
              <span className="mb-2 block text-body text-[var(--color-text-primary)]">
                {t('settings.colorMode')}
              </span>
              <div className="segmented-control w-full">
                {(['light', 'dark', 'system'] as const).map((v) => (
                  <button
                    key={v}
                    type="button"
                    data-active={colorMode === v}
                    onClick={() => setColorMode(v)}
                    className="flex-1"
                  >
                    {v === 'light'
                      ? t('settings.modeLight')
                      : v === 'dark'
                        ? t('settings.modeDark')
                        : t('settings.modeSystem')}
                  </button>
                ))}
              </div>
            </div>

            {/* Language */}
            <div>
              <span className="mb-2 block text-body text-[var(--color-text-primary)]">
                {t('settings.language')}
              </span>
              <div className="segmented-control w-full">
                {(['ja', 'en'] as const).map((v) => (
                  <button
                    key={v}
                    type="button"
                    data-active={locale === v}
                    onClick={() => {
                      saveAccountPreference({ locale: v }).catch((e) =>
                        toast.error(errorMessage(e)),
                      );
                    }}
                    className="flex-1"
                  >
                    {v === 'ja' ? '\u65E5\u672C\u8A9E' : 'English'}
                  </button>
                ))}
              </div>
            </div>
          </div>

          <div className="border-t border-[var(--color-separator)] px-6 py-3">
            <button
              type="button"
              onClick={() => {
                toggleSettings();
                navigate({ to: '/settings' });
              }}
              className="flex w-full items-center justify-between rounded-lg py-1.5 text-body text-[var(--color-accent-ink)] hover:opacity-80"
            >
              {t('settings.openAll')}
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                aria-hidden="true"
              >
                <path d="M9 6l6 6-6 6" />
              </svg>
            </button>
          </div>
        </div>
      </div>
    </>
  );
}

type MemoDialogState = { calendarId: string; memoId?: string } | null;

const postable = (calendar: Calendar) => canEdit(calendar.role);

export function MemoSection() {
  const t = useT();
  const memos = useCalendarStore((s) => s.memos);
  const toggleMemo = useCalendarStore((s) => s.toggleMemo);
  const deleteMemo = useCalendarStore((s) => s.deleteMemo);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const [dialog, setDialog] = useState<MemoDialogState>(null);
  const [pendingDelete, setPendingDelete] = useState<Memo | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Calendars the user can post a memo to: active in the sidebar and editable.
  const { options: postableCalendars, target, setTargetId } = usePanelCalendar(postable);
  const editable = target !== null;

  // Memos belong to a calendar, not to the reader: group the visible ones under
  // the calendar they are shared in, in sidebar order.
  const memoGroups = useMemo(
    () =>
      calendars
        .filter((c) => activeCalendarIds.includes(c.id))
        .map((calendar) => ({
          calendar,
          memos: memos.filter((m) => m.calendarId === calendar.id),
        }))
        .filter((g) => g.memos.length > 0),
    [calendars, memos, activeCalendarIds],
  );

  const handleToggle = async (memo: Memo) => {
    if (!editable) return;
    try {
      await toggleMemo(memo.calendarId, memo.id, !memo.done);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const handleDelete = async (memo: Memo) => {
    if (!editable) return;
    setDeleting(true);
    try {
      await deleteMemo(memo.calendarId, memo.id);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setDeleting(false);
      setPendingDelete(null);
    }
  };

  return (
    <div className="flex h-full flex-col">
      {editable && target && (
        <div className="space-y-2 px-5 py-4">
          <div className="flex items-center gap-2 text-caption text-[var(--color-text-secondary)]">
            <span className="shrink-0">{t('panel.postTo')}</span>
            {postableCalendars.length > 1 ? (
              <CustomSelect
                value={target.id}
                onChange={setTargetId}
                ariaLabel={t('panel.postTo')}
                className="min-w-0 flex-1"
                triggerClassName="input-modern input-compact"
                options={postableCalendars.map((c) => ({
                  value: c.id,
                  label: c.name,
                  color: c.color,
                }))}
              />
            ) : (
              <MemoCalendarTag calendar={target} />
            )}
          </div>
          <button
            type="button"
            onClick={() => setDialog({ calendarId: target.id })}
            className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] px-2.5 py-1.5 text-caption font-medium text-[var(--color-accent-ink)] transition-colors hover:bg-[var(--color-accent-bg)]"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
            >
              <path d="M12 5v14M5 12h14" />
            </svg>
            {t('memo.add')}
          </button>
        </div>
      )}
      <div className="flex-1 overflow-y-auto">
        {memoGroups.length === 0 ? (
          <EmptyState
            compact
            icon={GUIDE_ICONS.memo}
            title={t('empty.memosTitle')}
            description={t('empty.memosDesc')}
          />
        ) : (
          memoGroups.map(({ calendar: memoCal, memos: groupMemos }) => (
            <section key={memoCal.id} aria-label={memoCal.name}>
              <div className="px-5 pt-3 pb-1.5">
                <MemoCalendarTag calendar={memoCal} />
              </div>
              {groupMemos.map((memo) => {
                const memoEditable = canEdit(memoCal.role);
                const rowContent = (
                  <>
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleToggle(memo);
                      }}
                      disabled={!memoEditable}
                      aria-pressed={memo.done}
                      aria-label={memo.done ? t('memo.markUndone') : t('memo.markDone')}
                      title={memo.done ? t('memo.markUndone') : t('memo.markDone')}
                      className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full disabled:opacity-60"
                    >
                      {memo.done ? (
                        <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
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
                        <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
                          <circle
                            cx="12"
                            cy="12"
                            r="9"
                            stroke="var(--color-text-tertiary)"
                            strokeWidth="2"
                          />
                        </svg>
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => setDialog({ calendarId: memo.calendarId, memoId: memo.id })}
                      className="flex min-w-0 flex-1 flex-col gap-0.5 text-left transition-colors"
                    >
                      <span
                        className="truncate text-default"
                        style={{
                          textDecoration: memo.done ? 'line-through' : 'none',
                          color: memo.done
                            ? 'var(--color-text-tertiary)'
                            : 'var(--color-text-primary)',
                        }}
                      >
                        {memo.title}
                      </span>
                      {memo.body?.trim() && (
                        <span className="truncate text-caption text-[var(--color-text-tertiary)]">
                          {memo.body}
                        </span>
                      )}
                    </button>
                    {memoEditable && (
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          setPendingDelete(memo);
                        }}
                        aria-label={t('common.delete')}
                        title={t('common.delete')}
                        className="flex h-[26px] w-[26px] items-center justify-center text-[var(--color-text-tertiary)] hover:bg-[var(--color-danger-bg)] hover:text-[var(--color-danger)]"
                        style={{ borderRadius: 'var(--radius-sm)' }}
                      >
                        <svg
                          width="15"
                          height="15"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                        >
                          <path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6" />
                        </svg>
                      </button>
                    )}
                  </>
                );
                return (
                  <div
                    key={memo.id}
                    className="flex w-full items-center gap-3 border-b border-[var(--color-border)]/50 px-5 py-3.5 transition-colors hover:bg-[var(--color-hover)]"
                  >
                    {rowContent}
                  </div>
                );
              })}
            </section>
          ))
        )}
      </div>
      {dialog && (
        <MemoDialog
          calendarId={dialog.calendarId}
          memoId={dialog.memoId}
          onClose={() => setDialog(null)}
        />
      )}
      <ConfirmSheet
        open={pendingDelete !== null}
        title={t('confirm.deleteMemo')}
        confirmLabel={t('common.delete')}
        busy={deleting}
        onConfirm={() => pendingDelete && handleDelete(pendingDelete)}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
