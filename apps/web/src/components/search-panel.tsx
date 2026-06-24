import { DateTime } from 'luxon';
import { useEffect, useMemo, useRef } from 'react';
import { useT } from '@/i18n';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

export function SearchPanel() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const showSearch = useUiStore((s) => s.showSearch);
  const searchQuery = useUiStore((s) => s.searchQuery);
  const setSearchQuery = useUiStore((s) => s.setSearchQuery);
  const toggleSearch = useUiStore((s) => s.toggleSearch);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const events = useCalendarStore((s) => s.events);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (showSearch) {
      // Small delay to let the panel render before focusing
      const timer = setTimeout(() => inputRef.current?.focus(), 50);
      return () => clearTimeout(timer);
    }
  }, [showSearch]);

  // Close on Escape
  useEffect(() => {
    if (!showSearch) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') toggleSearch();
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [showSearch, toggleSearch]);

  const filtered = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    if (!q) return [];
    return events.filter((evt) => {
      return (
        evt.title.toLowerCase().includes(q) ||
        evt.location.toLowerCase().includes(q) ||
        evt.memo.toLowerCase().includes(q)
      );
    });
  }, [searchQuery, events]);

  const handleSelect = (eventId: string, startAt: string) => {
    const dt = DateTime.fromISO(startAt);
    setCurrentMonth(dt.startOf('month'));
    setSelectedDate(dt);
    toggleSearch();
    openEventModal(eventId);
  };

  if (!showSearch) return null;

  const resultsList = (
    <div className="flex-1 overflow-y-auto">
      {searchQuery.trim() === '' ? (
        <div className="flex flex-col items-center justify-center gap-2 py-16 text-[var(--color-text-tertiary)]">
          <svg
            width="24"
            height="24"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            opacity="0.5"
          >
            <circle cx="11" cy="11" r="7" />
            <line x1="16.65" y1="16.65" x2="21" y2="21" />
          </svg>
          <span className="text-callout">{t('search.searchEvents')}</span>
        </div>
      ) : filtered.length === 0 ? (
        <div className="flex items-center justify-center py-16 text-callout text-[var(--color-text-tertiary)]">
          {t('search.noResults')}
        </div>
      ) : (
        <div className="py-1">
          {filtered.map((evt) => {
            const dt = DateTime.fromISO(evt.startAt);
            const endDt = DateTime.fromISO(evt.endAt);
            const dateLabel = evt.allDay
              ? dt.toFormat('yyyy/MM/dd (EEE)', { locale })
              : `${dt.toFormat('yyyy/MM/dd (EEE) HH:mm', { locale })} - ${endDt.toFormat('HH:mm')}`;

            return (
              <button
                key={evt.id}
                type="button"
                onClick={() => handleSelect(evt.id, evt.startAt)}
                className="mx-2 flex w-[calc(100%-16px)] items-start gap-3 px-5 py-3.5 text-left hover:bg-[var(--color-hover)]"
                style={{ borderRadius: 'var(--radius-sm)' }}
              >
                <span
                  className="color-dot mt-1.5 h-2.5 w-2.5 shrink-0"
                  style={{ backgroundColor: evt.color }}
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-callout font-medium text-[var(--color-text-primary)]">
                    {evt.title}
                  </p>
                  <p className="text-body tabular-nums text-[var(--color-text-secondary)]">
                    {dateLabel}
                  </p>
                  {evt.location && (
                    <p className="truncate text-body text-[var(--color-text-tertiary)]">
                      {evt.location}
                    </p>
                  )}
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );

  const searchInput = (
    <div className="px-5 py-4">
      <div
        className="flex items-center gap-3 bg-[var(--color-surface-inset)] px-4 py-2.5"
        style={{ borderRadius: 'var(--radius-sm)' }}
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--color-text-secondary, #8F8F8F)"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="11" cy="11" r="7" />
          <line x1="16.65" y1="16.65" x2="21" y2="21" />
        </svg>
        <input
          ref={inputRef}
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={t('search.placeholder')}
          className="flex-1 bg-transparent text-callout outline-none placeholder:text-[var(--color-text-tertiary)]"
        />
        <button
          type="button"
          onClick={toggleSearch}
          className="flex h-7 w-7 shrink-0 items-center justify-center text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
          style={{ borderRadius: 'var(--radius-sm)' }}
          aria-label={t('common.close')}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>
    </div>
  );

  return (
    <>
      {/* SP: fullscreen overlay */}
      <div className="glass-surface-heavy fixed inset-0 z-50 flex flex-col sm:hidden">
        {searchInput}
        {resultsList}
      </div>

      {/* PC: dropdown panel */}
      <div className="hidden sm:block">
        {/* backdrop */}
        <button
          type="button"
          aria-label={t('common.close')}
          className="fixed inset-0 z-40"
          onClick={toggleSearch}
        />
        <div className="glass-surface-heavy modal-panel absolute right-16 top-[60px] z-50 flex max-h-[520px] w-[420px] flex-col overflow-hidden ring-1 ring-[var(--color-border)]">
          {searchInput}
          {resultsList}
        </div>
      </div>
    </>
  );
}
