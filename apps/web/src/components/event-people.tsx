import { Avatar } from '@/components/avatar';
import { LabelledButton } from '@/components/labelled-button';
import { CustomSelect } from '@/components/pickers';
import { type TranslationKey, useT } from '@/i18n';
import { showAsLabelKey } from '@/lib/permissions';
import { type Member, SHOW_AS_OPTIONS, type ShowAs } from '@/types/calendar';

/** What each availability means where it matters: when a time is being found. */
const SHOW_AS_HINT: Record<ShowAs, TranslationKey> = {
  busy: 'event.showAsBusyHint',
  tentative: 'event.showAsTentativeHint',
  free: 'event.showAsFreeHint',
  oof: 'event.showAsOofHint',
};

/** A participant as a pill: picture or initial, name, and a way to take them off. */
function ParticipantChip({
  member,
  name,
  disabled,
  onRemove,
}: {
  member: Member;
  name: string;
  disabled: boolean;
  onRemove: () => void;
}) {
  const t = useT();
  return (
    <span className="flex h-8 max-w-full min-w-0 items-center gap-1.5 rounded-full border border-[var(--color-border)] bg-[var(--color-surface)] pr-1 pl-1 text-footnote text-[var(--color-text-primary)]">
      <Avatar
        src={member.avatar}
        name={member.name}
        color={member.color}
        round
        size="h-6 w-6"
        className="text-caption"
      />
      <span className={`truncate ${disabled ? 'pr-2' : ''}`}>{name}</span>
      {!disabled && (
        <LabelledButton
          label={t('event.removeParticipant', { name: member.name })}
          onClick={onRemove}
          className="icon-btn h-6 w-6 shrink-0 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
        >
          <svg
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </LabelledButton>
      )}
    </span>
  );
}

interface EventPeopleProps {
  members: Member[];
  myId: string | undefined;
  /** The member whose layer the event sits on; null on a new event means the author. */
  ownerId: string | null;
  participants: string[];
  disabled: boolean;
  onOwnerChange: (id: string) => void;
  onParticipantsChange: (ids: string[]) => void;
}

/** Lists past this length get a filter box. */
const SEARCHABLE_FROM = 8;

/**
 * Who an event is about: whose plan it is, and who else takes part. On a
 * shared calendar this is the first thing a plan says, so it sits with the
 * title rather than behind the optional fields. Both stay one line tall
 * however many members the calendar has: the owner is one choice, and only
 * the people already taking part are drawn.
 */
export function EventPeople({
  members,
  myId,
  ownerId,
  participants,
  disabled,
  onOwnerChange,
  onParticipantsChange,
}: EventPeopleProps) {
  const t = useT();
  // The author reads themselves first: most plans are one's own.
  const ordered = [...members].sort((a, b) => Number(b.id === myId) - Number(a.id === myId));
  const nameOf = (m: Member) => (m.id === myId ? t('event.memberMe', { name: m.name }) : m.name);
  const option = (m: Member) => ({
    value: m.id,
    label: nameOf(m),
    color: m.color,
    person: { name: m.name, avatarUrl: m.avatar },
  });
  const owner = ordered.find((m) => m.id === (ownerId ?? myId));
  const taking = participants
    .map((id) => ordered.find((m) => m.id === id))
    .filter((m): m is Member => !!m);
  const addable = ordered.filter((m) => !participants.includes(m.id));
  const searchable = members.length >= SEARCHABLE_FROM;

  return (
    <div className="card-section bg-[var(--color-surface-secondary)] p-4">
      <div className="flex items-center justify-between gap-3">
        <span className="shrink-0 text-default text-[var(--color-text-primary)]">
          {t('event.whose')}
        </span>
        {disabled ? (
          owner && (
            <span className="flex min-w-0 items-center gap-2 text-default text-[var(--color-text-primary)]">
              <Avatar
                src={owner.avatar}
                name={owner.name}
                color={owner.color}
                round
                size="h-5 w-5"
                className="text-micro"
              />
              <span className="truncate">{nameOf(owner)}</span>
            </span>
          )
        ) : (
          <CustomSelect
            value={owner?.id ?? ''}
            options={ordered.map(option)}
            onChange={onOwnerChange}
            ariaLabel={t('event.whose')}
            searchable={searchable}
            className="min-w-0"
          />
        )}
      </div>

      <div className="my-3 border-t border-[var(--color-border)] opacity-50" />

      <fieldset className="m-0 min-w-0 border-0 p-0">
        <legend className="mb-2 p-0 text-default text-[var(--color-text-primary)]">
          {t('event.participants')}
        </legend>
        <div className="flex flex-wrap items-center gap-1.5">
          {taking.map((m) => (
            <ParticipantChip
              key={m.id}
              member={m}
              name={nameOf(m)}
              disabled={disabled}
              onRemove={() => onParticipantsChange(participants.filter((id) => id !== m.id))}
            />
          ))}
          {!disabled && addable.length > 0 && (
            <CustomSelect
              value=""
              placeholder={t('event.addParticipant')}
              options={addable.map(option)}
              onChange={(id) => onParticipantsChange([...participants, id])}
              ariaLabel={t('event.addParticipant')}
              searchable={searchable}
              triggerClassName="h-8 rounded-full border border-dashed border-[var(--color-border-strong)] px-3 text-footnote font-medium text-[var(--color-accent-ink)] hover:bg-[var(--color-hover)]"
            />
          )}
        </div>
        <p className="mt-2 text-caption text-[var(--color-text-tertiary)]">
          {t('event.participantsHint')}
        </p>
      </fieldset>
    </div>
  );
}

/**
 * Whether the time reads as taken. Four equal segments rather than a menu:
 * on a shared calendar "tentative" and "free" are everyday answers, and the
 * line under them says what each does when a time is being found.
 */
export function ShowAsPicker({
  value,
  disabled,
  onChange,
}: {
  value: ShowAs;
  disabled: boolean;
  onChange: (value: ShowAs) => void;
}) {
  const t = useT();
  return (
    <div className="card-section bg-[var(--color-surface-secondary)] p-4">
      <fieldset className="m-0 min-w-0 border-0 p-0">
        <legend className="mb-2 p-0 text-default text-[var(--color-text-primary)]">
          {t('event.showAs')}
        </legend>
        <div className="segmented-control w-full">
          {SHOW_AS_OPTIONS.map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={value === v}
              data-active={value === v}
              disabled={disabled}
              onClick={() => onChange(v)}
            >
              {t(showAsLabelKey(v))}
            </button>
          ))}
        </div>
        <p className="mt-2 text-caption text-[var(--color-text-tertiary)]">
          {t(SHOW_AS_HINT[value])}
        </p>
      </fieldset>
    </div>
  );
}
