import { CalendarList } from '@/components/calendar-list';
import type { TranslationKey } from '@/i18n';
import { useT } from '@/i18n';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { type RightPanelId, useUiStore } from '@/stores/ui-store';

interface MenuAction {
  key: string;
  labelKey: TranslationKey;
  icon: React.ReactNode;
  onSelect: () => void;
}

/**
 * Mobile-only slide-in drawer that surfaces controls living in the desktop
 * sidebars: the calendar list (visibility + create) plus the shared-calendar
 * panels (album, members, share, notifications) and the activity feed.
 */
export function MobileMenu() {
  const t = useT();
  const show = useUiStore((s) => s.showMobileMenu);
  const setShow = useUiStore((s) => s.setShowMobileMenu);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const setShowActivity = useUiStore((s) => s.setShowActivity);
  const panelRef = useModalA11y<HTMLDivElement>(show, () => setShow(false));

  if (!show) return null;

  const openPanel = (id: RightPanelId) => {
    setShow(false);
    toggleRightPanel(id);
  };

  const actions: MenuAction[] = [
    {
      key: 'album',
      labelKey: 'panel.album',
      icon: (
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          width="20"
          height="20"
        >
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <circle cx="8.5" cy="8.5" r="1.5" />
          <path d="M21 15l-5-5L5 21" />
        </svg>
      ),
      onSelect: () => openPanel('album'),
    },
    {
      key: 'members',
      labelKey: 'panel.members',
      icon: (
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          width="20"
          height="20"
        >
          <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2" />
          <circle cx="9" cy="7" r="4" />
          <path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75" />
        </svg>
      ),
      onSelect: () => openPanel('members'),
    },
    {
      key: 'share',
      labelKey: 'panel.share',
      icon: (
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          width="20"
          height="20"
        >
          <circle cx="18" cy="5" r="3" />
          <circle cx="6" cy="12" r="3" />
          <circle cx="18" cy="19" r="3" />
          <line x1="8.59" y1="13.51" x2="15.42" y2="17.49" />
          <line x1="15.41" y1="6.51" x2="8.59" y2="10.49" />
        </svg>
      ),
      onSelect: () => openPanel('share'),
    },
    {
      key: 'notifications',
      labelKey: 'panel.notifications',
      icon: (
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          width="20"
          height="20"
        >
          <path d="M18 8A6 6 0 106 8c0 7-3 9-3 9h18s-3-2-3-9z" />
          <path d="M13.73 21a2 2 0 01-3.46 0" />
        </svg>
      ),
      onSelect: () => openPanel('notifications'),
    },
    {
      key: 'activity',
      labelKey: 'activity.title',
      icon: (
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          width="20"
          height="20"
        >
          <path d="M12 8v4l3 3" />
          <circle cx="12" cy="12" r="9" />
        </svg>
      ),
      onSelect: () => {
        setShow(false);
        setShowActivity(true);
      },
    },
  ];

  return (
    <div className="sm:hidden">
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)]"
        onClick={() => setShow(false)}
      />
      <div
        ref={panelRef}
        className="glass-surface-heavy drawer-panel fixed left-0 top-0 z-50 flex h-full w-[280px] max-w-[82vw] flex-col border-r border-[var(--color-border)]"
      >
        <div className="flex items-center justify-between px-4 py-3">
          <span className="text-title font-semibold text-[var(--color-text-primary)]">
            {t('tabs.calendar')}
          </span>
          <button
            type="button"
            onClick={() => setShow(false)}
            aria-label={t('common.close')}
            className="flex h-9 w-9 items-center justify-center rounded-lg text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
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

        <div className="min-h-0 flex-1 overflow-y-auto">
          <CalendarList />

          <div className="mx-4 my-2 border-t border-[var(--color-border)]" />

          <div className="px-2 pb-3">
            {actions.map((a) => (
              <button
                key={a.key}
                type="button"
                onClick={a.onSelect}
                className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-hover)]"
              >
                <span className="shrink-0">{a.icon}</span>
                <span className="text-body">{t(a.labelKey)}</span>
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
