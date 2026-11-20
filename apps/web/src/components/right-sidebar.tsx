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
    id: 'settings',
    labelKey: 'settings.appearance',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <path d="M12 3a9 9 0 000 18c1.1 0 1.8-.9 1.8-1.8 0-.5-.2-.9-.5-1.2-.3-.3-.5-.7-.5-1.2 0-1 .8-1.8 1.8-1.8H17a4 4 0 004-4c0-4.4-4-8-9-8z" />
        <circle cx="7.5" cy="11.5" r="1.2" fill="currentColor" />
        <circle cx="10.5" cy="7.5" r="1.2" fill="currentColor" />
        <circle cx="15.5" cy="8.5" r="1.2" fill="currentColor" />
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
    <div className="flex w-16 shrink-0 flex-col items-center gap-1 border-l border-[var(--color-border)] pt-3">
      {ITEM_DEFS.map((item) => {
        const active = item.id === 'settings' ? showSettings : rightPanel === item.id;
        return (
          <button
            key={item.id}
            type="button"
            data-tour={item.id}
            onClick={() => {
              if (item.id === 'settings') {
                toggleSettings();
                return;
              }
              toggleRightPanel(item.id satisfies RightPanelId);
            }}
            className={[
              'relative flex w-14 flex-col items-center gap-0.5 rounded-xl py-1.5 transition-colors',
              active
                ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent)]'
                : 'text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)] hover:text-[var(--color-text-primary)]',
            ].join(' ')}
            aria-pressed={active}
          >
            {active && (
              <span className="absolute top-1/2 left-0 h-5 w-[3px] -translate-y-1/2 rounded-r-full bg-[var(--color-accent)]" />
            )}
            <span className="h-[22px] w-[22px]">{item.icon}</span>
            <span className="w-full truncate text-center text-micro leading-tight">
              {t(item.labelKey)}
            </span>
          </button>
        );
      })}
    </div>
  );
}
