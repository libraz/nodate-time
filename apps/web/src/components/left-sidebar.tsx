import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { CalendarList } from '@/components/calendar-list';
import { MemoSection } from '@/components/right-panel';
import { useT } from '@/i18n';
import { useUiStore } from '@/stores/ui-store';

export function LeftSidebar() {
  const t = useT();
  const navigate = useNavigate();
  const setShowActivity = useUiStore((s) => s.setShowActivity);
  const [memoExpanded, setMemoExpanded] = useState(true);

  return (
    <div className="glass-surface hidden w-[260px] shrink-0 flex-col border-r border-[var(--color-border)] sm:flex">
      {/* Calendars section */}
      <CalendarList />

      {/* Divider */}
      <div className="mx-4 border-t border-[var(--color-border)]" />

      {/* Memo section */}
      <div className="flex items-center justify-between px-4 pt-3 pb-1">
        <span className="text-caption font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">
          {t('tabs.memo')}
        </span>
        <button
          type="button"
          onClick={() => setMemoExpanded((v) => !v)}
          className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--color-text-tertiary)] hover:bg-[var(--color-hover)]"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            style={{
              transform: memoExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
              transition: 'transform 0.15s ease',
            }}
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
      </div>

      {memoExpanded && (
        <div className="flex-1 overflow-hidden">
          <MemoSection />
        </div>
      )}

      {/* Activity + Settings buttons at bottom */}
      <div className="border-t border-[var(--color-border)] px-3 py-2">
        <button
          type="button"
          onClick={() => setShowActivity(true)}
          className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-hover)]"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M12 8v4l3 3" />
            <circle cx="12" cy="12" r="9" />
          </svg>
          <span className="text-body">{t('activity.title')}</span>
        </button>
        <button
          type="button"
          onClick={() => navigate({ to: '/settings' })}
          className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-hover)]"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z" />
          </svg>
          <span className="text-body">{t('tabs.settings')}</span>
        </button>
      </div>
    </div>
  );
}
