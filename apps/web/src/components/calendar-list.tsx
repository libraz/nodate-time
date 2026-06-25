import { useEffect, useRef, useState } from 'react';
import { useT } from '@/i18n';
import { useCalendarStore } from '@/stores/calendar-store';
import { MEMBER_COLORS } from '@/types/calendar';

/**
 * Calendar list with per-calendar visibility toggles, a select-all control, and
 * an inline create form. Shared by the desktop left sidebar and the mobile menu.
 */
export function CalendarList() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const toggleCalendarFilter = useCalendarStore((s) => s.toggleCalendarFilter);
  const setActiveCalendarIds = useCalendarStore((s) => s.setActiveCalendarIds);
  const addCalendar = useCalendarStore((s) => s.addCalendar);
  const [showNewForm, setShowNewForm] = useState(false);
  const [newName, setNewName] = useState('');
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
    <div className="flex min-h-0 flex-col">
      <div className="flex items-center justify-between px-4 pt-4 pb-1">
        <span className="text-caption font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">
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
                  className="checkbox-box"
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
                  stroke="var(--color-text-on-accent)"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            ) : (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                <rect
                  className="checkbox-box"
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
                className="flex-1 truncate text-default"
                style={{
                  color: isActive ? 'var(--color-text-primary)' : 'var(--color-text-tertiary)',
                }}
              >
                {cal.name}
              </span>
              {cal.publicShared && (
                <span
                  role="img"
                  title={t('calendar.publicShared')}
                  aria-label={t('calendar.publicShared')}
                  className="shrink-0 text-[var(--color-danger)]"
                >
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <circle cx="12" cy="12" r="10" />
                    <path d="M2 12h20" />
                    <path d="M12 2a15.3 15.3 0 014 10 15.3 15.3 0 01-4 10 15.3 15.3 0 01-4-10 15.3 15.3 0 014-10z" />
                  </svg>
                </span>
              )}
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
                className="btn-secondary h-8 flex-1 text-footnote"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                onClick={handleCreateCalendar}
                className="btn-primary h-8 flex-1 text-footnote"
              >
                {t('common.create')}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
