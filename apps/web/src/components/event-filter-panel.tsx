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

const FIELD_LABEL: Record<FilterField, TranslationKey> = {
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

/** One condition in words, e.g. "誰の予定: A・B を除く". */
export function conditionSummary(
  row: FilterCondition,
  nameOf: (p: FilterPerson) => string,
  t: ReturnType<typeof useT>,
): string {
  const names = row.people.map(nameOf).join(t('filter.nameJoin'));
  return t(row.exclude ? 'filter.summaryExclude' : 'filter.summaryInclude', {
    field: t(FIELD_LABEL[row.field]),
    names: names || t('filter.nobody'),
  });
}

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
  // One condition is edited at a time; the rest read as a line each.
  const [openId, setOpenId] = useState<string | undefined>(() => shown.conditions[0]?.id);
  const openRow = shown.conditions.find((c) => c.id === openId) ?? shown.conditions.at(-1);
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
    const row = newRow();
    setEventFilter(EMPTY_FILTER);
    setDraft({ match: 'all', conditions: [row] });
    setOpenId(row.id);
  };
  const addRow = () => {
    const row = newRow();
    commit({ ...shown, conditions: [...shown.conditions, row] });
    setOpenId(row.id);
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
            open={row.id === openRow?.id}
            onOpen={() => setOpenId(row.id)}
            candidates={candidates}
            removable={shown.conditions.length > 1 || row.people.length > 0}
            onChange={(patch) => updateRow(row.id, patch)}
            onRemove={() => removeRow(row.id)}
          />
        ))}
      </div>

      <div className="shrink-0 px-0.5 pt-1.5 sm:px-3 sm:pb-2.5">
        <button
          type="button"
          onClick={addRow}
          className="inline-flex h-9 items-center gap-1.5 rounded-full px-2.5 text-footnote font-medium text-[var(--color-accent-ink)] transition-colors hover:bg-[var(--color-hover)]"
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
  open,
  onOpen,
  candidates,
  removable,
  onChange,
  onRemove,
}: {
  row: FilterCondition;
  index: number;
  open: boolean;
  onOpen: () => void;
  candidates: ReturnType<typeof useCandidates>;
  removable: boolean;
  onChange: (patch: Partial<FilterCondition>) => void;
  onRemove: () => void;
}) {
  const t = useT();
  const [query, setQuery] = useState('');
  const chosen = new Set(row.people.map((p) => p.id));
  const nameOf = (p: FilterPerson) =>
    p.id === candidates.meId ? t('filter.me') : resolvePerson(p, candidates.people).name;
  const q = query.trim().toLowerCase();
  const listed = q
    ? candidates.people.filter((p) => p.name.toLowerCase().includes(q))
    : candidates.people;

  const toggle = (p: FilterPerson) =>
    onChange({
      people: chosen.has(p.id) ? row.people.filter((x) => x.id !== p.id) : [...row.people, p],
    });

  const removeButton = removable && (
    <button
      type="button"
      onClick={onRemove}
      aria-label={t('filter.removeCondition')}
      className="icon-btn h-8 w-8 shrink-0 rounded-full text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
    >
      <CloseIcon size={13} />
    </button>
  );

  if (!open) {
    return (
      <section
        aria-label={t('filter.conditionN', { n: index + 1 })}
        className="flex items-center gap-1 rounded-[var(--radius-md)] bg-[var(--color-surface)] pr-1.5 ring-1 ring-[var(--color-border)]"
      >
        <button
          type="button"
          onClick={onOpen}
          aria-expanded={false}
          className="min-w-0 flex-1 truncate rounded-[var(--radius-md)] px-3.5 py-3 text-left text-default text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
        >
          {conditionSummary(row, nameOf, t)}
        </button>
        {removeButton}
      </section>
    );
  }

  return (
    <section
      aria-label={t('filter.conditionN', { n: index + 1 })}
      className="reveal-in rounded-[var(--radius-md)] bg-[var(--color-surface)] p-2.5 ring-1 ring-[var(--color-border)]"
    >
      <div className="flex items-center gap-1.5">
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
              title={t(FIELD_HINT[f])}
              onClick={() => onChange({ field: f })}
              className="!px-1 !text-footnote"
            >
              {t(FIELD_LABEL[f])}
            </button>
          ))}
        </div>
        {removeButton}
      </div>

      <div className="mt-2">
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
          muted={row.exclude}
          nameOf={nameOf}
          onToggle={toggle}
        />
      </div>

      {row.people.length > 0 && (
        <div className="mt-2 flex items-center justify-between gap-2 border-t border-[var(--color-border)] pt-2.5 pl-1">
          <span className="text-footnote tabular-nums text-[var(--color-text-secondary)]">
            {t('filter.chosenCount', { count: row.people.length })}
          </span>
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
    </section>
  );
}

function CandidateList({
  candidates,
  listed,
  chosen,
  muted,
  nameOf,
  onToggle,
}: {
  candidates: ReturnType<typeof useCandidates>;
  listed: FilterPerson[];
  chosen: Set<string>;
  muted: boolean;
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
              <PersonAvatar person={p} size="h-8 w-8" muted={on && muted} />
              <span
                className={`min-w-0 flex-1 truncate text-default text-[var(--color-text-primary)] ${
                  on && muted ? 'line-through decoration-1 opacity-70' : ''
                }`}
              >
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
 * A person in their layer colour. A photo would hide that colour, so a photo
 * sits inside a ring of it.
 */
function PersonAvatar({
  person,
  size,
  muted = false,
}: {
  person: FilterPerson;
  size: string;
  muted?: boolean;
}) {
  const ringed = Boolean(person.avatarUrl);
  return (
    <span
      className={`${size} relative flex shrink-0 items-center justify-center rounded-full transition-opacity ${
        ringed ? 'p-[2px]' : ''
      } ${muted ? 'opacity-50' : ''}`}
      style={ringed ? { background: person.color ?? 'var(--color-border-strong)' } : undefined}
    >
      <Avatar
        src={person.avatarUrl}
        name={person.name}
        color={person.avatarUrl ? undefined : person.color}
        round
        size="h-full w-full"
        className={`text-micro ${ringed ? 'ring-2 ring-[var(--color-surface)]' : ''}`}
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

function CloseIcon({ size }: { size: number }) {
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
