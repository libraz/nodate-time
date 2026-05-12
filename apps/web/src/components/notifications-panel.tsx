import { useT } from '@/i18n';
import { useUiStore } from '@/stores/ui-store';

export function NotificationsPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);

  if (rightPanel !== 'notifications') return null;

  return (
    <>
      <div
        className="fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={() => toggleRightPanel('notifications')}
        onKeyDown={undefined}
      />
      <div className="glass-surface-heavy fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="text-[16px] font-semibold">{t('panel.notifications')}</h2>
          <button
            type="button"
            onClick={() => toggleRightPanel('notifications')}
            className="flex h-8 w-8 items-center justify-center text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
            style={{ borderRadius: 'var(--radius-sm)' }}
            aria-label={t('common.close')}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-[var(--color-text-tertiary)]">
          <svg
            width="40"
            height="40"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            opacity="0.5"
          >
            <path d="M18 8A6 6 0 106 8c0 7-3 9-3 9h18s-3-2-3-9z" />
            <path d="M13.73 21a2 2 0 01-3.46 0" />
          </svg>
          <p className="text-[14px]">{t('panel.noNotifications')}</p>
        </div>
      </div>
    </>
  );
}
