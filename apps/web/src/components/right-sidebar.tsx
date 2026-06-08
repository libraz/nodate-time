import type { TranslationKey } from '@/i18n';
import { useT } from '@/i18n';
import { type RightPanelId, useUiStore } from '@/stores/ui-store';

type SidebarItemId = Exclude<RightPanelId, null> | 'settings';

interface SidebarItemDef {
  id: SidebarItemId;
  labelKey: TranslationKey;
  icon: React.ReactNode;
}

const ITEM_DEFS: SidebarItemDef[] = [
  {
    id: 'memo',
    labelKey: 'panel.memo',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
        <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" />
      </svg>
    ),
  },
  {
    id: 'album',
    labelKey: 'panel.album',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <rect x="3" y="3" width="18" height="18" rx="2" />
        <circle cx="8.5" cy="8.5" r="1.5" />
        <path d="M21 15l-5-5L5 21" />
      </svg>
    ),
  },
  {
    id: 'members',
    labelKey: 'panel.members',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2" />
        <circle cx="9" cy="7" r="4" />
        <path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75" />
      </svg>
    ),
  },
  {
    id: 'share',
    labelKey: 'panel.share',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <circle cx="18" cy="5" r="3" />
        <circle cx="6" cy="12" r="3" />
        <circle cx="18" cy="19" r="3" />
        <line x1="8.59" y1="13.51" x2="15.42" y2="17.49" />
        <line x1="15.41" y1="6.51" x2="8.59" y2="10.49" />
      </svg>
    ),
  },
  {
    id: 'notifications',
    labelKey: 'panel.notifications',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <path d="M18 8A6 6 0 106 8c0 7-3 9-3 9h18s-3-2-3-9z" />
        <path d="M13.73 21a2 2 0 01-3.46 0" />
      </svg>
    ),
  },
  {
    id: 'settings',
    labelKey: 'panel.settings',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <line x1="4" y1="6" x2="20" y2="6" />
        <line x1="4" y1="12" x2="20" y2="12" />
        <line x1="4" y1="18" x2="20" y2="18" />
        <circle cx="8" cy="6" r="2" fill="var(--color-surface)" />
        <circle cx="16" cy="12" r="2" fill="var(--color-surface)" />
        <circle cx="10" cy="18" r="2" fill="var(--color-surface)" />
      </svg>
    ),
  },
];

export function RightSidebar() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const showSettings = useUiStore((s) => s.showSettings);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const toggleSettings = useUiStore((s) => s.toggleSettings);

  return (
    <div className="flex w-10 shrink-0 flex-col items-center gap-1.5 border-l border-[var(--color-border)] pt-3 sm:w-14 sm:gap-2 sm:pt-4">
      {ITEM_DEFS.map((item) => {
        const active = item.id === 'settings' ? showSettings : rightPanel === item.id;
        return (
          <button
            key={item.id}
            type="button"
            onClick={() => {
              if (item.id === 'settings') {
                toggleSettings();
                return;
              }
              toggleRightPanel(item.id satisfies RightPanelId);
            }}
            className={[
              'relative flex h-9 w-9 items-center justify-center rounded-xl transition-colors sm:h-11 sm:w-11',
              active
                ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent)]'
                : 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-hover)] hover:text-[var(--color-text-secondary)]',
            ].join(' ')}
            aria-label={t(item.labelKey)}
          >
            {active && (
              <span className="absolute top-1/2 left-0 h-5 w-[3px] -translate-y-1/2 rounded-r-full bg-[var(--color-accent)]" />
            )}
            <span className="h-5 w-5 sm:h-[22px] sm:w-[22px]">{item.icon}</span>
          </button>
        );
      })}
    </div>
  );
}
