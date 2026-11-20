import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { BrandRow } from '@/components/brand-mark';
import { CalendarList } from '@/components/calendar-list';
import { GuideCard } from '@/components/guide';
import { LabelledButton } from '@/components/labelled-button';
import { MemoSection } from '@/components/right-panel';
import { useT } from '@/i18n';
import { useUiStore } from '@/stores/ui-store';

export function LeftSidebar() {
  const t = useT();
  const navigate = useNavigate();
  const setShowActivity = useUiStore((s) => s.setShowActivity);
  const [memoExpanded, setMemoExpanded] = useState(true);

  return (
    <div className="glass-surface app-sidebar hidden w-[260px] shrink-0 flex-col border-r border-[var(--color-border)] sm:flex">
      <div className="px-4 pt-4 pb-2">
        <BrandRow />
      </div>

      {/* Calendars section */}
      <CalendarList />

      {/* Divider */}
      <div className="mx-4 border-t border-[var(--color-border)]" />

      {/* Memo section */}
      <div className="flex items-center justify-between px-4 pt-3 pb-1">
        <span className="text-caption font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">
          {t('tabs.memo')}
        </span>
        <LabelledButton
          label={t('tabs.memo')}
          expanded={memoExpanded}
          onClick={() => setMemoExpanded((v) => !v)}
          className="icon-btn h-7 w-7 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
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
              transition: 'transform 0.28s var(--ease-press)',
            }}
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
        </LabelledButton>
      </div>

      {memoExpanded && (
        <div className="reveal-in flex-1 overflow-hidden">
          <MemoSection />
        </div>
      )}

      <GuideCard className="mx-3 mt-2 mb-3" />

      {/* Activity, help and settings at the bottom */}
      <div className="border-t border-[var(--color-border)] px-3 py-2">
        <button
          type="button"
          onClick={() => setShowActivity(true)}
          className="flex w-full items-center gap-3 rounded-[var(--radius-control)] px-3 py-2 text-left text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-hover)]"
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
          onClick={() => navigate({ to: '/help' })}
          data-tour="help"
          className="flex w-full items-center gap-3 rounded-[var(--radius-control)] px-3 py-2 text-left text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-hover)]"
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
            aria-hidden="true"
          >
            <circle cx="12" cy="12" r="9" />
            <path d="M9.5 9.5a2.5 2.5 0 014.6 1.3c0 1.7-2.1 2.2-2.1 3.7" />
            <circle cx="12" cy="17.2" r="0.6" fill="currentColor" />
          </svg>
          <span className="text-body">{t('help.title')}</span>
        </button>
        <button
          type="button"
          onClick={() => navigate({ to: '/settings' })}
          className="flex w-full items-center gap-3 rounded-[var(--radius-control)] px-3 py-2 text-left text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-hover)]"
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
