import {
  CloseIcon,
  FIELD_LABEL,
  FunnelIcon,
  useFilterPanelHost,
} from '@/components/event-filter-panel';
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

const DOTS_SHOWN = 4;

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
  const live = (p: FilterPerson) => resolvePerson(p, candidates);
  const nameOf = (p: FilterPerson) => (p.id === meId ? t('filter.me') : live(p).name);
  const names = first.people.map(nameOf).join(t('filter.nameJoin'));
  const field = t(FIELD_LABEL[first.field]);
  const summary = t(first.exclude ? 'filter.summaryExclude' : 'filter.summaryInclude', {
    field,
    names,
  });
  const dots = rows
    .filter((r) => !r.exclude)
    .flatMap((r) => r.people)
    .filter((p, i, all) => all.findIndex((x) => x.id === p.id) === i)
    .slice(0, DOTS_SHOWN)
    .map(live);

  return (
    <div className="shrink-0 px-3 pt-2 pb-1">
      <div
        className="reveal-in flex h-9 items-center gap-1 rounded-full pr-1 pl-1"
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
          className="flex h-7 min-w-0 flex-1 items-center gap-2 rounded-full pr-2 pl-2 text-left text-footnote hover:bg-[var(--color-hover)]"
        >
          <span className="shrink-0 text-[var(--color-accent-ink)]">
            <FunnelIcon size={14} />
          </span>
          <span className="shrink-0 font-semibold text-[var(--color-accent-ink)]">
            {t('filter.active')}
          </span>
          {dots.length > 0 && (
            <span aria-hidden className="flex shrink-0 -space-x-1">
              {dots.map((p) => (
                <span
                  key={p.id}
                  className="h-2.5 w-2.5 rounded-full ring-2 ring-[var(--color-surface)]"
                  style={{ background: p.color ?? 'var(--color-border-strong)' }}
                />
              ))}
            </span>
          )}
          <span className="min-w-0 truncate text-[var(--color-text-primary)]">
            {summary}
            {rows.length > 1 && (
              <span className="text-[var(--color-text-secondary)]">
                {' '}
                {t('filter.more', { count: rows.length - 1 })}
              </span>
            )}
          </span>
        </button>
        <button
          type="button"
          onClick={() => setEventFilter(EMPTY_FILTER)}
          className="flex h-7 shrink-0 items-center gap-1 rounded-full px-2.5 text-footnote font-medium text-[var(--color-accent-ink)] hover:bg-[var(--color-hover)]"
        >
          <CloseIcon size={10} />
          {t('filter.clear')}
        </button>
      </div>
      {panel}
    </div>
  );
}
