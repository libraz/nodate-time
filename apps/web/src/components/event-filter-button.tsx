import { FunnelIcon, useFilterPanelHost } from '@/components/event-filter-panel';
import { useT } from '@/i18n';
import { effectiveConditions } from '@/lib/event-filter';
import { useCalendarStore } from '@/stores/calendar-store';

/**
 * The header's way into the member filter. While a filter is on it carries the
 * accent and the number of conditions, so a thinner grid has a visible cause.
 */
export function EventFilterButton({ variant }: { variant: 'desktop' | 'mobile' }) {
  const t = useT();
  const count = useCalendarStore((s) => effectiveConditions(s.eventFilter).length);
  const { anchorRef, open, toggle, panel } = useFilterPanelHost(
    variant === 'desktop' ? 'header-desktop' : 'header-mobile',
  );
  const size = variant === 'desktop' ? 'h-11 w-11 rounded-xl' : 'h-8 w-8';

  return (
    <>
      <button
        ref={anchorRef}
        type="button"
        onClick={toggle}
        aria-label={
          count > 0 ? `${t('filter.button')} (${t('filter.badge', { count })})` : t('filter.button')
        }
        aria-haspopup="dialog"
        aria-expanded={open}
        data-active={count > 0}
        className={`icon-btn relative ${size} ${
          count > 0
            ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)]'
            : 'text-[var(--color-text-primary)]'
        }`}
      >
        <FunnelIcon size={variant === 'desktop' ? 19 : 18} />
        {count > 0 && (
          <span
            aria-hidden
            className="pop-in absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-[var(--color-accent)] px-1 text-micro font-bold tabular-nums text-[var(--color-text-on-accent)] ring-2 ring-[var(--color-surface)]"
          >
            {count}
          </span>
        )}
      </button>
      {panel}
    </>
  );
}
