import { useMemo, useState } from 'react';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/api';
import { canEdit, roleForCalendar } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { Memo } from '@/types/calendar';

export function SettingsModal() {
  const t = useT();
  const showSettings = useUiStore((s) => s.showSettings);
  const toggleSettings = useUiStore((s) => s.toggleSettings);
  const theme = useUiStore((s) => s.theme);
  const colorMode = useUiStore((s) => s.colorMode);
  const locale = useUiStore((s) => s.locale);
  const setTheme = useUiStore((s) => s.setTheme);
  const setColorMode = useUiStore((s) => s.setColorMode);
  const setLocale = useUiStore((s) => s.setLocale);

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
        <div className="glass-surface-heavy modal-panel w-full max-w-[400px] ring-1 ring-[var(--color-border)]">
          <div className="flex items-center justify-between px-6 py-4">
            <h2 className="text-title font-semibold text-[var(--color-text-primary)]">
              {t('tabs.settings')}
            </h2>
            <button
              type="button"
              onClick={toggleSettings}
              className="flex h-9 w-9 items-center justify-center text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
              style={{ borderRadius: 'var(--radius-sm)' }}
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
            </button>
          </div>

          <div className="px-6 pb-6 space-y-5">
            {/* Theme */}
            <div>
              <span className="mb-2 block text-body text-[var(--color-text-primary)]">
                {t('settings.theme')}
              </span>
              <div className="segmented-control w-full">
                {(['glass', 'classic', 'nothing'] as const).map((v) => (
                  <button
                    key={v}
                    type="button"
                    data-active={theme === v}
                    onClick={() => setTheme(v)}
                    className="flex-1"
                  >
                    {v === 'glass'
                      ? t('settings.themeGlass')
                      : v === 'classic'
                        ? t('settings.themeClassic')
                        : t('settings.themeNothing')}
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
                    onClick={() => setLocale(v)}
                    className="flex-1"
                  >
                    {v === 'ja' ? '\u65E5\u672C\u8A9E' : 'English'}
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}

export function MemoSection() {
  const t = useT();
  const memos = useCalendarStore((s) => s.memos);
  const addMemo = useCalendarStore((s) => s.addMemo);
  const toggleMemo = useCalendarStore((s) => s.toggleMemo);
  const deleteMemo = useCalendarStore((s) => s.deleteMemo);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const me = useAuthStore((s) => s.user);
  const [newTitle, setNewTitle] = useState('');
  const [adding, setAdding] = useState(false);
  const [targetId, setTargetId] = useState('');

  // Calendars the user can post a memo to: active in the sidebar and editable.
  const postableCalendars = useMemo(
    () =>
      calendars.filter(
        (c) =>
          activeCalendarIds.includes(c.id) && canEdit(roleForCalendar(membersMap[c.id], me?.email)),
      ),
    [calendars, activeCalendarIds, membersMap, me?.email],
  );

  // Fall back to the first postable calendar when the chosen target is gone.
  const target = postableCalendars.find((c) => c.id === targetId) ?? postableCalendars[0] ?? null;
  const editable = target !== null;

  // Show only memos from the calendars currently active in the sidebar.
  const visibleMemos = useMemo(
    () => memos.filter((m) => activeCalendarIds.includes(m.calendarId)),
    [memos, activeCalendarIds],
  );
  const calendarById = useMemo(() => new Map(calendars.map((c) => [c.id, c])), [calendars]);
  const showCalendarTag = activeCalendarIds.length > 1;

  const handleAdd = async () => {
    const title = newTitle.trim();
    if (!title || adding || !target) return;
    setAdding(true);
    try {
      await addMemo(target.id, { title });
      setNewTitle('');
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setAdding(false);
    }
  };

  const handleToggle = async (memo: Memo) => {
    if (!editable) return;
    try {
      await toggleMemo(memo.calendarId, memo.id, !memo.done, memo.title);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const handleDelete = async (memo: Memo) => {
    if (!editable) return;
    try {
      await deleteMemo(memo.calendarId, memo.id);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <div className="flex h-full flex-col">
      {editable && target && (
        <div className="space-y-2 px-5 py-4">
          <div className="space-y-1 text-caption text-[var(--color-text-tertiary)]">
            <span className="inline-flex w-fit items-center gap-1.5 whitespace-nowrap rounded-full bg-[var(--color-surface-inset)] px-2 py-0.5 font-medium text-[var(--color-text-secondary)]">
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                className="shrink-0"
              >
                <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
                <circle cx="9" cy="7" r="4" />
                <path d="M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
              </svg>
              {t('panel.memoShared')}
            </span>
            <p className="truncate">{t('panel.memoSharedHint', { name: target.name })}</p>
          </div>
          {postableCalendars.length > 1 && (
            <label className="flex items-center gap-2 text-caption text-[var(--color-text-secondary)]">
              <span className="shrink-0">{t('panel.postTo')}</span>
              <select
                value={target.id}
                onChange={(e) => setTargetId(e.target.value)}
                disabled={adding}
                className="input-modern h-8 flex-1 text-sm disabled:opacity-50"
              >
                {postableCalendars.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <input
            type="text"
            value={newTitle}
            onChange={(e) => setNewTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleAdd();
            }}
            disabled={adding}
            placeholder={t('panel.enterToPost')}
            className="input-modern h-10 w-full text-sm disabled:opacity-50"
          />
        </div>
      )}
      <div className="flex-1 overflow-y-auto">
        {visibleMemos.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-[var(--color-text-tertiary)]">
            <p className="text-default">{t('panel.noMemos')}</p>
          </div>
        ) : (
          visibleMemos.map((memo) => {
            const memoCal = calendarById.get(memo.calendarId);
            const memoEditable = canEdit(roleForCalendar(membersMap[memo.calendarId], me?.email));
            return (
              <div
                key={memo.id}
                className="flex items-center gap-3 border-b border-[var(--color-border)]/50 px-5 py-3.5"
              >
                <button
                  type="button"
                  onClick={() => handleToggle(memo)}
                  disabled={!memoEditable}
                  className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded disabled:opacity-60"
                >
                  {memo.done ? (
                    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
                      <rect
                        x="3"
                        y="3"
                        width="18"
                        height="18"
                        rx="3"
                        fill="var(--color-accent)"
                        stroke="var(--color-accent)"
                        strokeWidth="2"
                      />
                      <path
                        d="M8 12l3 3 5-5"
                        stroke="white"
                        strokeWidth="2.5"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      />
                    </svg>
                  ) : (
                    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
                      <rect
                        x="3"
                        y="3"
                        width="18"
                        height="18"
                        rx="3"
                        stroke="var(--color-text-tertiary)"
                        strokeWidth="2"
                      />
                    </svg>
                  )}
                </button>
                <div className="flex flex-1 flex-col gap-0.5">
                  <span
                    className="text-default"
                    style={{
                      textDecoration: memo.done ? 'line-through' : 'none',
                      color: memo.done ? 'var(--color-text-tertiary)' : 'var(--color-text-primary)',
                    }}
                  >
                    {memo.title}
                  </span>
                  {showCalendarTag && memoCal && (
                    <span className="flex items-center gap-1.5 text-caption text-[var(--color-text-tertiary)]">
                      <span
                        className="inline-block h-2 w-2 shrink-0 rounded-full"
                        style={{ backgroundColor: memoCal.color }}
                      />
                      <span className="truncate">{memoCal.name}</span>
                    </span>
                  )}
                </div>
                {memoEditable && (
                  <button
                    type="button"
                    onClick={() => handleDelete(memo)}
                    className="flex h-[22px] w-[22px] items-center justify-center text-[var(--color-text-tertiary)] hover:bg-[var(--color-danger-bg)] hover:text-[var(--color-danger)]"
                    style={{ borderRadius: 'var(--radius-sm)' }}
                  >
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <path d="M18 6L6 18M6 6l12 12" />
                    </svg>
                  </button>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}
