import type { TranslationKey } from '@/i18n';
import { useT } from '@/i18n';
import { type RightPanelId, useUiStore } from '@/stores/ui-store';

type SidebarItemId = Exclude<RightPanelId, null>;

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
    id: 'labels',
    labelKey: 'panel.labels',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <path d="M20.6 13.4l-7.2 7.2a2 2 0 0 1-2.8 0L3 13V3h10l7.6 7.6a2 2 0 0 1 0 2.8z" />
        <circle cx="7.5" cy="7.5" r="1.2" />
      </svg>
    ),
  },
  {
    id: 'public',
    labelKey: 'panel.public',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3a14 14 0 010 18M12 3a14 14 0 000 18" />
      </svg>
    ),
  },
];

export function RightSidebar() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);

  return (
    <div className="flex w-16 shrink-0 flex-col items-center gap-1 border-l border-[var(--color-border)] pt-3">
      {ITEM_DEFS.map((item) => {
        const active = rightPanel === item.id;
        return (
          <button
            key={item.id}
            type="button"
            data-tour={item.id}
            onClick={() => {
              toggleRightPanel(item.id satisfies RightPanelId);
            }}
            className={[
              'relative flex w-14 flex-col items-center gap-0.5 rounded-[var(--radius-control)] py-1.5 transition-colors',
              active
                ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)] shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--color-accent)_28%,transparent)]'
                : 'text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)] hover:text-[var(--color-text-primary)]',
            ].join(' ')}
            aria-pressed={active}
          >
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
