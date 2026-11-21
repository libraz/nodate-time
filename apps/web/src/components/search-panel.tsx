import { OwnerMark } from '@/components/owner-mark';
import { Highlight, handleResultListKeys } from '@/components/search-results';
import { useT } from '@/i18n';
import { isGroupEvent } from '@/lib/event-group';
import { flexibilityLabelKey } from '@/lib/event-style';
import { showAsLabelKey } from '@/lib/permissions';
import { searchResultDay, searchResultLabel, useEventSearch } from '@/lib/search';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

export function SearchPanel() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const showSearch = useUiStore((s) => s.showSearch);
  const searchQuery = useUiStore((s) => s.searchQuery);
  const setSearchQuery = useUiStore((s) => s.setSearchQuery);
  const toggleSearch = useUiStore((s) => s.toggleSearch);
  const setCurrentMonth = useUiStore((s) => s.setCurrentMonth);
  const setSelectedDate = useUiStore((s) => s.setSelectedDate);
  const openEventModal = useUiStore((s) => s.openEventModal);
  // Both layouts are in the document at once, so the trap is anchored above
  // them: it walks whichever one CSS has painted, and the search box of that
  // layout is the first focusable inside it.
  const panelRef = useModalA11y<HTMLDivElement>(showSearch, toggleSearch);

  const search = useEventSearch(showSearch ? searchQuery : '');
  const results = search.status === 'done' ? search.items : [];

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
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="7" />
            <line x1="16.65" y1="16.65" x2="21" y2="21" />
          </svg>
          <span className="text-callout">{t('search.searchEvents')}</span>
        </div>
      ) : search.status === 'loading' ? (
        <div
          role="status"
          className="flex items-center justify-center py-16 text-callout text-[var(--color-text-tertiary)]"
        >
          {t('common.loading')}
        </div>
      ) : search.status === 'error' ? (
        <div
          role="alert"
          className="reveal-in px-8 py-16 text-center text-callout text-balance text-[var(--color-text-tertiary)]"
        >
          {search.message}
        </div>
      ) : results.length === 0 ? (
        <div className="reveal-in flex flex-col items-center justify-center gap-2 px-8 py-16 text-center text-[var(--color-text-tertiary)]">
          <svg
            width="24"
            height="24"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            opacity="0.5"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="7" />
            <line x1="16.65" y1="16.65" x2="21" y2="21" />
            <line x1="8.5" y1="11" x2="13.5" y2="11" />
          </svg>
          <span className="text-callout text-balance">{t('search.noResults')}</span>
        </div>
      ) : (
        <div className="py-1">
          {results.map((evt) => {
            const dateLabel = searchResultLabel(evt, timezone, locale);
            const flexKey = flexibilityLabelKey(evt.flexibility, { short: true });
            const state = [
              evt.showAs !== 'busy' ? t(showAsLabelKey(evt.showAs)) : '',
              flexKey ? t(flexKey) : '',
            ]
              .filter(Boolean)
              .join(' · ');

            return (
              <button
                key={evt.id}
                type="button"
                data-search-result
                onClick={() => handleSelect(evt)}
                className="search-result mx-2 flex w-[calc(100%-16px)] items-start gap-3 px-5 py-3.5 text-left hover:bg-[var(--color-hover)] focus-visible:bg-[var(--color-hover)]"
                style={{ borderRadius: 'var(--radius-sm)' }}
              >
                <OwnerMark
                  name={evt.ownerName}
                  avatarUrl={evt.ownerAvatarUrl}
                  color={evt.color}
                  size={22}
                  group={isGroupEvent(evt)}
                  className="mt-0.5"
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
                  {state && (
                    <p className="truncate text-body text-[var(--color-text-tertiary)]">{state}</p>
                  )}
                </div>
              </button>
            );
          })}
          {search.status === 'done' && search.hasMore && (
            <p className="px-7 py-3 text-body text-[var(--color-text-tertiary)]">
              {t('search.hasMore')}
            </p>
          )}
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
          data-1p-ignore
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={t('search.placeholder')}
          className="flex-1 bg-transparent text-callout outline-none placeholder:text-[var(--color-text-tertiary)]"
        />
        <button
          type="button"
          onClick={toggleSearch}
          className="icon-btn h-7 w-7"
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
