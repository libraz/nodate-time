import { conditionSummary, FunnelIcon, useFilterPanelHost } from '@/components/event-filter-panel';
import { useT } from '@/i18n';
import {
  EMPTY_FILTER,
  effectiveConditions,
  type FilterPerson,
  filterCandidates,
  resolvePerson,
} from '@/lib/event-filter';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';

/**
 * Says the views are filtered, and by what, above the calendar. Without it a
 * filter set last week reads as events that went missing. Tapping the summary
 * opens the panel; "clear" drops the filter in one step.
 */
export function EventFilterBar() {
  const t = useT();
  const filter = useCalendarStore((s) => s.eventFilter);
  const setEventFilter = useCalendarStore((s) => s.setEventFilter);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const meId = useAuthStore((s) => s.user?.id);
  const { anchorRef, open, toggle, panel } = useFilterPanelHost('bar');

  const rows = effectiveConditions(filter);
  const first = rows[0];
  // The panel outlives the bar: emptying the last row from it must not shut it.
  if (!first) return panel;

  const candidates = filterCandidates(membersMap, activeCalendarIds, meId);
  const nameOf = (p: FilterPerson) =>
    p.id === meId ? t('filter.me') : resolvePerson(p, candidates).name;

  return (
    <div className="shrink-0 px-3 pt-2 pb-1">
      <div
        className="reveal-in flex h-9 items-center gap-1 rounded-full px-1"
        style={{
          background: 'color-mix(in srgb, var(--color-accent) 9%, var(--color-surface))',
          boxShadow: 'inset 0 0 0 1px color-mix(in srgb, var(--color-accent) 28%, transparent)',
        }}
      >
        <button
          ref={anchorRef}
          type="button"
          onClick={toggle}
          aria-haspopup="dialog"
          aria-expanded={open}
          className="flex h-7 min-w-0 flex-1 items-center gap-2 rounded-full px-2 text-left text-footnote hover:bg-[var(--color-hover)]"
        >
          <span className="shrink-0 text-[var(--color-accent-ink)]">
            <FunnelIcon size={14} />
          </span>
          <span className="min-w-0 truncate font-medium text-[var(--color-text-primary)]">
            {conditionSummary(first, nameOf, t)}
          </span>
          {rows.length > 1 && (
            <span className="shrink-0 whitespace-nowrap tabular-nums text-[var(--color-text-secondary)]">
              {t('filter.more', { count: rows.length - 1 })}
            </span>
          )}
        </button>
        <button
          type="button"
          onClick={() => setEventFilter(EMPTY_FILTER)}
          className="flex h-7 shrink-0 items-center rounded-full px-3 text-footnote font-medium text-[var(--color-accent-ink)] hover:bg-[var(--color-hover)]"
        >
          {t('filter.clear')}
        </button>
      </div>
      {panel}
    </div>
  );
}
