import { useT } from '@/i18n';
import { api } from '@/lib/api';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import { useCallback, useState } from 'react';

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
      <div
        className="fixed inset-0 z-50 bg-[var(--color-overlay)]"
        onClick={toggleSettings}
        onKeyDown={undefined}
      />

      <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div className="glass-surface-heavy modal-panel w-full max-w-[400px] ring-1 ring-[var(--color-border)]">
          <div className="flex items-center justify-between px-6 py-4">
            <h2 className="text-[18px] font-semibold text-[var(--color-text-primary)]">
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
              <span className="mb-2 block text-[13px] text-[var(--color-text-primary)]">
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
              <span className="mb-2 block text-[13px] text-[var(--color-text-primary)]">
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
              <span className="mb-2 block text-[13px] text-[var(--color-text-primary)]">
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
  const [newTitle, setNewTitle] = useState('');

  const handleAdd = () => {
    if (!newTitle.trim()) return;
    const calendarId = calendars[0]?.id ?? '';
    addMemo(calendarId, { title: newTitle.trim() });
    setNewTitle('');
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 px-5 py-4">
        <input
          type="text"
          value={newTitle}
          onChange={(e) => setNewTitle(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') handleAdd();
          }}
          placeholder={t('panel.enterToPost')}
          className="input-modern h-10 flex-1 text-sm"
        />
      </div>
      <div className="flex-1 overflow-y-auto">
        {memos.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-[var(--color-text-tertiary)]">
            <p className="text-[14px]">{t('panel.noMemos')}</p>
          </div>
        ) : (
          memos.map((memo) => (
            <div
              key={memo.id}
              className="flex items-center gap-3 border-b border-[var(--color-border)]/50 px-5 py-3.5"
            >
              <button
                type="button"
                onClick={() => toggleMemo(memo.calendarId, memo.id, !memo.done, memo.title)}
                className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded"
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
              <span
                className="flex-1 text-[14px]"
                style={{
                  textDecoration: memo.done ? 'line-through' : 'none',
                  color: memo.done ? 'var(--color-text-tertiary)' : 'var(--color-text-primary)',
                }}
              >
                {memo.title}
              </span>
              <button
                type="button"
                onClick={() => deleteMemo(memo.calendarId, memo.id)}
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
            </div>
          ))
        )}
      </div>
    </div>
  );
}

interface InviteData {
  id: number;
  token: string;
  role: string;
  maxUses: number | null;
  useCount: number;
  expiresAt: string | null;
  createdAt: string;
}

export function ShareModal({ calendarId, onClose }: { calendarId: string; onClose: () => void }) {
  const t = useT();
  const [invite, setInvite] = useState<InviteData | null>(null);
  const [copied, setCopied] = useState(false);
  const [loading, setLoading] = useState(false);

  const linkUrl = invite ? `${window.location.origin}/share/${invite.token}` : '';

  const createLink = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.post<InviteData>(`/calendars/${calendarId}/invites`, {
        role: 'viewer',
      });
      setInvite(data);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [calendarId]);

  const revokeLink = useCallback(async () => {
    if (!invite) return;
    try {
      await api.delete(`/calendars/${calendarId}/invites/${invite.id}`);
      setInvite(null);
    } catch {
      // ignore
    }
  }, [calendarId, invite]);

  const copyLink = useCallback(() => {
    navigator.clipboard.writeText(linkUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [linkUrl]);

  return (
    <>
      <div
        className="fixed inset-0 z-50 bg-[var(--color-overlay)]"
        onClick={onClose}
        onKeyDown={undefined}
      />

      <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div className="glass-surface-heavy modal-panel w-full max-w-[400px] ring-1 ring-[var(--color-border)]">
          <div className="flex items-center justify-between px-6 py-4">
            <h2 className="text-[18px] font-semibold text-[var(--color-text-primary)]">
              {t('panel.share')}
            </h2>
            <button
              type="button"
              onClick={onClose}
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

          <div className="px-6 pb-6">
            {!invite ? (
              <button
                type="button"
                onClick={createLink}
                disabled={loading}
                className="btn-primary flex w-full items-center justify-center gap-2 text-[13px] disabled:opacity-50"
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71" />
                  <path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71" />
                </svg>
                {loading ? t('share.creating') : t('share.createLink')}
              </button>
            ) : (
              <div className="space-y-2">
                <div
                  className="flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface-secondary)] px-3 py-2"
                  style={{ borderRadius: 'var(--radius-sm)' }}
                >
                  <input
                    type="text"
                    readOnly
                    value={linkUrl}
                    className="flex-1 truncate bg-transparent text-[12px] text-[var(--color-text-secondary)] outline-none"
                  />
                  <button
                    type="button"
                    onClick={copyLink}
                    className="btn-primary shrink-0 px-3 py-1 text-[12px]"
                  >
                    {copied ? t('common.copied') : t('common.copy')}
                  </button>
                </div>
                <p className="text-[11px] text-[var(--color-text-secondary)]">
                  {t('share.linkDescription')}
                </p>
                <button
                  type="button"
                  onClick={revokeLink}
                  className="text-[12px] text-[var(--color-danger)] hover:underline"
                >
                  {t('share.revokeLink')}
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
