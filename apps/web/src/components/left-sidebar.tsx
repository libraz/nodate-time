import { useNavigate } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { MemoSection } from '@/components/right-panel';
import { useT } from '@/i18n';
import { useCalendarStore } from '@/stores/calendar-store';
import { MEMBER_COLORS } from '@/types/calendar';

export function LeftSidebar() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const toggleCalendarFilter = useCalendarStore((s) => s.toggleCalendarFilter);
  const setActiveCalendarIds = useCalendarStore((s) => s.setActiveCalendarIds);
  const addCalendar = useCalendarStore((s) => s.addCalendar);
  const navigate = useNavigate();
  const [showNewForm, setShowNewForm] = useState(false);
  const [newName, setNewName] = useState('');
  const [memoExpanded, setMemoExpanded] = useState(true);
  const inputRef = useRef<HTMLInputElement>(null);

  const allActive = calendars.every((c) => activeCalendarIds.includes(c.id));

  const handleToggleAll = () => {
    if (allActive) {
      setActiveCalendarIds([]);
    } else {
      setActiveCalendarIds(calendars.map((c) => c.id));
    }
  };

  const handleCreateCalendar = () => {
    if (!newName.trim()) return;
    addCalendar({
      name: newName.trim(),
      color: MEMBER_COLORS[calendars.length % MEMBER_COLORS.length] ?? '#47B2F7',
    });
    setNewName('');
    setShowNewForm(false);
  };

  useEffect(() => {
    if (showNewForm) inputRef.current?.focus();
  }, [showNewForm]);

  return (
    <div className="glass-surface hidden w-[260px] shrink-0 flex-col border-r border-[var(--color-border)] sm:flex">
      {/* Calendars section */}
      <div className="flex items-center justify-between px-4 pt-4 pb-1">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">
          {t('calendar.calendarList')}
        </span>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={handleToggleAll}
            className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--color-text-tertiary)] hover:bg-[var(--color-hover)]"
            aria-label={t('calendar.selectAll')}
          >
            {allActive ? (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
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
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
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
          <button
            type="button"
            onClick={() => setShowNewForm(true)}
            className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--color-text-tertiary)] hover:bg-[var(--color-hover)]"
            aria-label={t('calendar.newCalendar')}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <line x1="12" y1="5" x2="12" y2="19" />
              <line x1="5" y1="12" x2="19" y2="12" />
            </svg>
          </button>
        </div>
      </div>

      {/* Calendar list */}
      <div className="overflow-y-auto px-2 py-1">
        {calendars.map((cal) => {
          const isActive = activeCalendarIds.includes(cal.id);
          return (
            <button
              key={cal.id}
              type="button"
              onClick={() => toggleCalendarFilter(cal.id)}
              className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-[var(--color-hover)]"
            >
              <span
                className="h-3 w-3 shrink-0 rounded-full"
                style={{
                  backgroundColor: isActive ? cal.color : 'var(--color-text-tertiary)',
                  opacity: isActive ? 1 : 0.3,
                }}
              />
              <span
                className="flex-1 truncate text-[14px]"
                style={{
                  color: isActive ? 'var(--color-text-primary)' : 'var(--color-text-tertiary)',
                }}
              >
                {cal.name}
              </span>
              {isActive && (
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                  <path
                    d="M5 12l5 5L19 7"
                    stroke="var(--color-accent)"
                    strokeWidth="2.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              )}
            </button>
          );
        })}

        {/* Inline create form */}
        {showNewForm && (
          <div className="px-2 py-2">
            <input
              ref={inputRef}
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleCreateCalendar();
                if (e.key === 'Escape') setShowNewForm(false);
              }}
              placeholder={t('calendar.calendarName')}
              className="input-modern h-9 w-full text-sm"
            />
            <div className="mt-2 flex gap-2">
              <button
                type="button"
                onClick={() => setShowNewForm(false)}
                className="btn-secondary h-8 flex-1 text-[12px]"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                onClick={handleCreateCalendar}
                className="btn-primary h-8 flex-1 text-[12px]"
              >
                {t('common.create')}
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Divider */}
      <div className="mx-4 border-t border-[var(--color-border)]" />

      {/* Memo section */}
      <div className="flex items-center justify-between px-4 pt-3 pb-1">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">
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

      {/* Settings button at bottom */}
      <div className="border-t border-[var(--color-border)] px-3 py-2">
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
          <span className="text-[13px]">{t('tabs.settings')}</span>
        </button>
      </div>
    </div>
  );
}
