import { useEffect, useMemo, useState } from 'react';
import { create } from 'zustand';
import { Avatar } from '@/components/avatar';
import {
  handlePopupKeys,
  PickerSurface,
  useFloating,
  useOutsideClose,
  useSheetPicker,
} from '@/components/picker-surface';
import { type TranslationKey, useT } from '@/i18n';
import {
  EMPTY_FILTER,
  type EventFilter,
  effectiveConditions,
  FILTER_FIELDS,
  type FilterCondition,
  type FilterField,
  type FilterPerson,
  filterCandidates,
  isFilterActive,
  resolvePerson,
  SEARCHABLE_FROM,
} from '@/lib/event-filter';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';

const PANEL_WIDTH = 400;

export const FIELD_LABEL: Record<FilterField, TranslationKey> = {
  involved: 'filter.field.involved',
  owner: 'filter.field.owner',
  creator: 'filter.field.creator',
  participant: 'filter.field.participant',
};

const FIELD_HINT: Record<FilterField, TranslationKey> = {
  involved: 'filter.hint.involved',
  owner: 'filter.hint.owner',
  creator: 'filter.hint.creator',
  participant: 'filter.hint.participant',
};

/** Which control the panel is open from; each control anchors its own popover. */
type PanelHost = 'header-desktop' | 'header-mobile' | 'bar';

export const useFilterPanel = create<{
  openFrom: PanelHost | null;
  toggle: (host: PanelHost) => void;
  close: () => void;
}>((set) => ({
  openFrom: null,
  toggle: (host) => set((s) => ({ openFrom: s.openFrom === host ? null : host })),
  close: () => set({ openFrom: null }),
}));

let nextRowId = 1;
function newRow(): FilterCondition {
  return { id: `row-${Date.now()}-${nextRowId++}`, field: 'involved', exclude: false, people: [] };
}

/**
 * The open/close wiring one control needs: its anchor, whether the panel is
 * open from it, and the panel itself when it is.
 */
export function useFilterPanelHost(host: PanelHost) {
  const open = useFilterPanel((s) => s.openFrom === host);
  const toggle = useFilterPanel((s) => s.toggle);
  const { anchorRef, floatingRef, pos } = useFloating(open, PANEL_WIDTH);
  const sheet = useSheetPicker();
  const panel = open ? (
    <EventFilterPanel
      sheet={sheet}
      anchorRef={anchorRef}
      floatingRef={floatingRef}
      style={{ top: pos.top, left: pos.left, width: PANEL_WIDTH }}
    />
  ) : null;
  return { anchorRef, open, toggle: () => toggle(host), panel };
}

/** The members a condition can name, fetching any shown calendar not yet asked. */
function useCandidates() {
  const membersMap = useCalendarStore((s) => s.membersMap);
  const memberErrors = useCalendarStore((s) => s.memberErrors);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const meId = useAuthStore((s) => s.user?.id);

  const missing = activeCalendarIds.filter((id) => !membersMap[id] && !memberErrors[id]);
  const failed = activeCalendarIds.filter((id) => !membersMap[id] && memberErrors[id]);
  const missingKey = missing.join(',');

  useEffect(() => {
    if (!missingKey) return;
    for (const id of missingKey.split(',')) void fetchMembers(id);
  }, [missingKey, fetchMembers]);

  const people = useMemo(
    () => filterCandidates(membersMap, activeCalendarIds, meId),
    [membersMap, activeCalendarIds, meId],
  );
  return {
    people,
    meId,
    noCalendars: activeCalendarIds.length === 0,
    loading: missing.length > 0,
    failed: failed.length > 0 && people.length === 0,
    retry: () => {
      for (const id of failed) void fetchMembers(id);
    },
  };
}

function EventFilterPanel({
  sheet,
  anchorRef,
  floatingRef,
  style,
}: {
  sheet: boolean;
  anchorRef: React.RefObject<HTMLButtonElement | null>;
  floatingRef: React.RefObject<HTMLDivElement | null>;
  style: React.CSSProperties;
}) {
  const t = useT();
  const close = useFilterPanel((s) => s.close);
  const filter = useCalendarStore((s) => s.eventFilter);
  const setEventFilter = useCalendarStore((s) => s.setEventFilter);
  const candidates = useCandidates();
  // Rows nobody has chosen anyone for yet stay here: only a filter that does
  // something is saved, so a half-built one never hides events behind a reload.
  const [draft, setDraft] = useState<EventFilter>(() => ({ match: 'all', conditions: [newRow()] }));
  const active = isFilterActive(filter);
  const shown = active ? filter : draft;
  const effectiveCount = effectiveConditions(shown).length;

  useOutsideClose([floatingRef, anchorRef], close, true);

  const commit = (next: EventFilter) => {
    if (isFilterActive(next)) {
      setEventFilter(next);
    } else {
      if (active) setEventFilter(EMPTY_FILTER);
      setDraft(next.conditions.length > 0 ? next : { ...next, conditions: [newRow()] });
    }
  };
  const updateRow = (id: string, patch: Partial<FilterCondition>) =>
    commit({
      ...shown,
      conditions: shown.conditions.map((c) => (c.id === id ? { ...c, ...patch } : c)),
    });
  const removeRow = (id: string) =>
    commit({ ...shown, conditions: shown.conditions.filter((c) => c.id !== id) });
  const clearAll = () => {
    setEventFilter(EMPTY_FILTER);
    setDraft({ match: 'all', conditions: [newRow()] });
  };

  return (
    <PickerSurface
      sheet={sheet}
      dialogLabel={t('filter.title')}
      floatingRef={floatingRef}
      style={style}
      popoverClassName="flex max-h-[min(640px,calc(100dvh-96px))] flex-col rounded-[var(--radius-lg)]"
      onKeyDown={(e) => handlePopupKeys(e, close)}
    >
      <div className="flex shrink-0 items-center justify-between gap-3 px-1 pb-2 sm:px-4 sm:pt-3.5">
        <h2 className="text-subhead font-semibold text-[var(--color-text-primary)]">
          {t('filter.title')}
        </h2>
        {active && (
          <button
            type="button"
            onClick={clearAll}
            className="rounded-full px-2.5 py-1 text-footnote font-medium text-[var(--color-accent-ink)] hover:bg-[var(--color-hover)]"
          >
            {t('filter.clearAll')}
          </button>
        )}
      </div>

      <div className="min-h-0 flex-1 space-y-2.5 overflow-y-auto overscroll-contain px-0.5 pb-1 sm:px-3">
        {effectiveCount >= 2 && (
          <div className="flex items-center justify-between gap-3 px-1 pb-0.5">
            <span className="text-footnote text-[var(--color-text-secondary)]">
              {t('filter.matchLead')}
            </span>
            <div className="segmented-control" role="radiogroup" aria-label={t('filter.matchLead')}>
              {(['all', 'any'] as const).map((m) => (
                // biome-ignore lint/a11y/useSemanticElements: the segmented control styles buttons, not radio inputs
                <button
                  key={m}
                  type="button"
                  role="radio"
                  aria-checked={shown.match === m}
                  data-active={shown.match === m}
                  onClick={() => commit({ ...shown, match: m })}
                  className="!px-3 !text-footnote"
                >
                  {t(m === 'all' ? 'filter.matchAll' : 'filter.matchAny')}
                </button>
              ))}
            </div>
          </div>
        )}

        {shown.conditions.map((row, i) => (
          <ConditionCard
            key={row.id}
            row={row}
            index={i}
            candidates={candidates}
            removable={shown.conditions.length > 1 || row.people.length > 0}
            onChange={(patch) => updateRow(row.id, patch)}
            onRemove={() => removeRow(row.id)}
          />
        ))}
      </div>

      <div className="shrink-0 px-0.5 pt-2 sm:px-3 sm:pb-3">
        <button
          type="button"
          onClick={() => commit({ ...shown, conditions: [...shown.conditions, newRow()] })}
          className="flex h-10 w-full items-center justify-center gap-1.5 rounded-[var(--radius-control)] border border-dashed border-[var(--color-border-strong)] text-default font-medium text-[var(--color-accent-ink)] transition-colors hover:bg-[var(--color-hover)]"
        >
          <PlusIcon />
          {t('filter.addCondition')}
        </button>
      </div>
    </PickerSurface>
  );
}

function ConditionCard({
  row,
  index,
  candidates,
  removable,
  onChange,
  onRemove,
}: {
  row: FilterCondition;
  index: number;
  candidates: ReturnType<typeof useCandidates>;
  removable: boolean;
  onChange: (patch: Partial<FilterCondition>) => void;
  onRemove: () => void;
}) {
  const t = useT();
  const [picking, setPicking] = useState(false);
  const [query, setQuery] = useState('');
  const chosen = new Set(row.people.map((p) => p.id));
  // Nobody chosen yet: the list is the next step, so it is already open.
  const listOpen = picking || row.people.length === 0;
  const nameOf = (p: FilterPerson) => (p.id === candidates.meId ? t('filter.me') : p.name);
  const q = query.trim().toLowerCase();
  const listed = q
    ? candidates.people.filter((p) => p.name.toLowerCase().includes(q))
    : candidates.people;

  const toggle = (p: FilterPerson) =>
    onChange({
      people: chosen.has(p.id) ? row.people.filter((x) => x.id !== p.id) : [...row.people, p],
    });

  return (
    <section
      aria-label={t('filter.conditionN', { n: index + 1 })}
      className="reveal-in rounded-[var(--radius-md)] bg-[var(--color-surface)] p-3 ring-1 ring-[var(--color-border)]"
    >
      <div className="flex items-center gap-2">
        <div
          className="segmented-control min-w-0 flex-1"
          role="radiogroup"
          aria-label={t('filter.fieldLabel')}
        >
          {FILTER_FIELDS.map((f) => (
            // biome-ignore lint/a11y/useSemanticElements: the segmented control styles buttons, not radio inputs
            <button
              key={f}
              type="button"
              role="radio"
              aria-checked={row.field === f}
              data-active={row.field === f}
              onClick={() => onChange({ field: f })}
              className="!px-1 !text-footnote"
            >
              {t(FIELD_LABEL[f])}
            </button>
          ))}
        </div>
        {removable && (
          <button
            type="button"
            onClick={onRemove}
            aria-label={t('filter.removeCondition')}
            className="icon-btn h-8 w-8 shrink-0 rounded-full text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
          >
            <CloseIcon size={14} />
          </button>
        )}
      </div>
      <p className="mt-1.5 px-0.5 text-caption text-[var(--color-text-tertiary)]">
        {t(FIELD_HINT[row.field])}
      </p>

      {row.people.length > 0 && (
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
          {row.people.map((p) => {
            const live = resolvePerson(p, candidates.people);
            return (
              <span
                key={p.id}
                className={`pop-in inline-flex h-7 max-w-[11rem] items-center gap-1.5 rounded-full pr-1 pl-0.5 text-footnote font-medium ${
                  row.exclude
                    ? 'bg-[var(--color-surface-inset)] text-[var(--color-text-secondary)]'
                    : 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)]'
                }`}
              >
                <PersonAvatar person={live} size="h-6 w-6" muted={row.exclude} />
                <span className={`truncate ${row.exclude ? 'line-through decoration-1' : ''}`}>
                  {nameOf(live)}
                </span>
                <button
                  type="button"
                  onClick={() => toggle(p)}
                  aria-label={t('filter.removePerson', { name: nameOf(live) })}
                  className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full opacity-70 hover:bg-[var(--color-hover)] hover:opacity-100"
                >
                  <CloseIcon size={10} />
                </button>
              </span>
            );
          })}
        </div>
      )}

      {row.people.length > 0 && (
        <div className="mt-2.5 flex items-center justify-between gap-2">
          <button
            type="button"
            onClick={() => setPicking((v) => !v)}
            aria-expanded={listOpen}
            className="inline-flex h-8 items-center gap-1 rounded-full px-2 text-footnote font-medium text-[var(--color-accent-ink)] hover:bg-[var(--color-hover)]"
          >
            <PlusIcon size={13} />
            {t('filter.choosePeople')}
          </button>
          <div className="segmented-control" role="radiogroup" aria-label={t('filter.opLabel')}>
            {([false, true] as const).map((ex) => (
              // biome-ignore lint/a11y/useSemanticElements: the segmented control styles buttons, not radio inputs
              <button
                key={String(ex)}
                type="button"
                role="radio"
                aria-checked={row.exclude === ex}
                data-active={row.exclude === ex}
                onClick={() => onChange({ exclude: ex })}
                className="!px-3 !text-footnote"
              >
                {t(ex ? 'filter.op.exclude' : 'filter.op.include')}
              </button>
            ))}
          </div>
        </div>
      )}

      {listOpen && (
        <div className="mt-2.5 border-t border-[var(--color-border)] pt-2">
          {candidates.people.length >= SEARCHABLE_FROM && (
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('filter.searchPeople')}
              aria-label={t('filter.searchPeople')}
              className="input-modern mb-1.5 h-9 w-full px-3 text-default"
            />
          )}
          <CandidateList
            candidates={candidates}
            listed={listed}
            chosen={chosen}
            nameOf={nameOf}
            onToggle={toggle}
          />
        </div>
      )}
    </section>
  );
}

function CandidateList({
  candidates,
  listed,
  chosen,
  nameOf,
  onToggle,
}: {
  candidates: ReturnType<typeof useCandidates>;
  listed: FilterPerson[];
  chosen: Set<string>;
  nameOf: (p: FilterPerson) => string;
  onToggle: (p: FilterPerson) => void;
}) {
  const t = useT();
  const note = 'px-2 py-3 text-center text-footnote text-[var(--color-text-tertiary)]';
  if (candidates.noCalendars) return <p className={note}>{t('filter.noCalendars')}</p>;
  if (candidates.failed) {
    return (
      <div className={`${note} flex items-center justify-center gap-2`}>
        <span>{t('filter.membersFailed')}</span>
        <button
          type="button"
          onClick={candidates.retry}
          className="font-medium text-[var(--color-accent-ink)] underline"
        >
          {t('filter.retry')}
        </button>
      </div>
    );
  }
  if (candidates.people.length === 0 && candidates.loading) {
    return <p className={note}>{t('common.loading')}</p>;
  }
  return (
    <ul className="max-h-60 space-y-0.5 overflow-y-auto overscroll-contain">
      {listed.map((p) => {
        const on = chosen.has(p.id);
        return (
          <li key={p.id}>
            <button
              type="button"
              aria-pressed={on}
              onClick={() => onToggle(p)}
              className={`flex w-full items-center gap-2.5 rounded-[var(--radius-control)] px-2 py-1.5 text-left transition-colors ${
                on ? 'bg-[var(--color-accent-bg)]' : 'hover:bg-[var(--color-hover)]'
              }`}
            >
              <PersonAvatar person={p} size="h-8 w-8" />
              <span className="min-w-0 flex-1 truncate text-default text-[var(--color-text-primary)]">
                {nameOf(p)}
              </span>
              <span
                aria-hidden
                className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-[1.5px] transition-colors ${
                  on
                    ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-accent)]'
                    : 'border-[var(--color-border-strong)]'
                }`}
              >
                {on && <CheckIcon />}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * A person as the calendar draws them: their picture inside a ring of their
 * layer colour, so a chosen name reads as the colour it brings to the grid.
 */
export function PersonAvatar({
  person,
  size,
  muted = false,
}: {
  person: FilterPerson;
  size: string;
  muted?: boolean;
}) {
  return (
    <span
      className={`${size} relative flex shrink-0 items-center justify-center rounded-full p-[2px] transition-opacity ${muted ? 'opacity-60' : ''}`}
      style={{ background: person.color ?? 'var(--color-border-strong)' }}
    >
      <Avatar
        src={person.avatarUrl}
        name={person.name}
        color={person.avatarUrl ? undefined : person.color}
        round
        size="h-full w-full"
        className="text-micro ring-2 ring-[var(--color-surface)]"
      />
    </span>
  );
}

export function FunnelIcon({ size = 18 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M4 5h16l-6 7.5V19l-4-2v-4.5L4 5z" />
    </svg>
  );
}

function PlusIcon({ size = 15 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      aria-hidden="true"
    >
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

export function CloseIcon({ size }: { size: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.6"
      strokeLinecap="round"
      aria-hidden="true"
    >
      <path d="M18 6L6 18M6 6l12 12" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg
      className="pop-in"
      width="11"
      height="11"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M5 12.5l4.5 4.5L19 7.5" />
    </svg>
  );
}
