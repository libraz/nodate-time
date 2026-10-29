import { useMemo } from 'react';
import { Highlight, handleResultListKeys } from '@/components/search-results';
import { useT } from '@/i18n';
import {
  filterEventsForSearch,
  searchedMonths,
  searchResultDay,
  searchResultLabel,
} from '@/lib/search';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

export function SearchPanel() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const calendarView = useUiStore((s) => s.calendarView);
  const currentMonth = useUiStore((s) => s.currentMonth);
  const showSearch = useUiStore((s) => s.showSearch);
  const searchQuery = useUiStore((s) => s.searchQuery);
  const setSearchQuery = useUiStore((s) => s.setSearchQuery);
  const toggleSearch = useUiStore((s) => s.toggleSearch);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const openEventModal = useUiStore((s) => s.openEventModal);
  const events = useCalendarStore((s) => s.events);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  // Both layouts are in the document at once, so the trap is anchored above
  // them: it walks whichever one CSS has painted, and the search box of that
  // layout is the first focusable inside it.
  const panelRef = useModalA11y<HTMLDivElement>(showSearch, toggleSearch);

  const filtered = useMemo(
    () => filterEventsForSearch(events, activeCalendarIds, searchQuery),
    [searchQuery, events, activeCalendarIds],
  );

  const handleSelect = (evt: CalendarEvent) => {
    const day = searchResultDay(evt, timezone);
    setCurrentMonth(day.startOf('month'));
    setSelectedDate(day);
    toggleSearch();
    openEventModal(evt.id);
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
          {t('search.noResults', searchedMonths(calendarView, currentMonth, locale))}
        </div>
      ) : (
        <div className="py-1">
          {filtered.map((evt) => {
            const dateLabel = searchResultLabel(evt, timezone, locale);

            return (
              <button
                key={evt.id}
                type="button"
                data-search-result
                onClick={() => handleSelect(evt)}
                className="search-result mx-2 flex w-[calc(100%-16px)] items-start gap-3 px-5 py-3.5 text-left hover:bg-[var(--color-hover)] focus-visible:bg-[var(--color-hover)]"
                style={{ borderRadius: 'var(--radius-sm)' }}
              >
                <span
                  className="color-dot mt-1.5 h-2.5 w-2.5 shrink-0"
                  style={{ backgroundColor: evt.color }}
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-callout font-medium text-[var(--color-text-primary)]">
                    <Highlight text={evt.title} query={searchQuery} />
                  </p>
                  <p className="text-body tabular-nums text-[var(--color-text-secondary)]">
                    {dateLabel}
                  </p>
                  {evt.location && (
                    <p className="truncate text-body text-[var(--color-text-tertiary)]">
                      <Highlight text={evt.location} query={searchQuery} />
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
      {/* PC: backdrop, outside the trap so it is not a stop on the way round */}
      <button
        type="button"
        aria-label={t('common.close')}
        className="fixed inset-0 z-40 hidden sm:block"
        onClick={toggleSearch}
      />

      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={t('search.searchEvents')}
        onKeyDown={handleResultListKeys}
      >
        {/* SP: fullscreen overlay */}
        <div
          data-search-layout
          className="glass-surface-heavy fixed inset-0 z-50 flex flex-col sm:hidden"
        >
          {searchInput}
          {resultsList}
        </div>

        {/* PC: dropdown panel */}
        <div
          data-search-layout
          className="glass-surface-heavy modal-panel absolute right-16 top-[60px] z-50 hidden max-h-[520px] w-[420px] flex-col overflow-hidden ring-1 ring-[var(--color-border)] sm:flex"
        >
          {searchInput}
          {resultsList}
        </div>
      </div>
    </>
  );
}
