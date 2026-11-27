import { Avatar } from '@/components/avatar';
import { Callout } from '@/components/callout';
import { CustomSelect } from '@/components/custom-select';
import { LabelledButton } from '@/components/labelled-button';
import { Switch } from '@/components/switch';
import { type TranslationKey, useT } from '@/i18n';
import { SEARCHABLE_FROM } from '@/lib/event-filter';
import { memberOption } from '@/lib/member-option';
import { showAsLabelKey } from '@/lib/permissions';
import {
  type Attendee,
  type Member,
  type Rsvp,
  SHOW_AS_OPTIONS,
  type ShowAs,
} from '@/types/calendar';

/** What each availability means where it matters: when a time is being found. */
const SHOW_AS_HINT: Record<ShowAs, TranslationKey> = {
  busy: 'event.showAsBusyHint',
  tentative: 'event.showAsTentativeHint',
  free: 'event.showAsFreeHint',
  oof: 'event.showAsOofHint',
};

/** The i18n key naming an RSVP answer; pending carries no mark. */
const RSVP_LABEL: Record<Exclude<Rsvp, 'pending'>, TranslationKey> = {
  accepted: 'event.rsvpAccepted',
  tentative: 'event.rsvpTentative',
  declined: 'event.rsvpDeclined',
};

/** A small badge on a participant's avatar giving their answer. */
function RsvpMark({ rsvp }: { rsvp: Rsvp | undefined }) {
  const t = useT();
  if (!rsvp || rsvp === 'pending') return null;
  const label = t(RSVP_LABEL[rsvp]);
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      data-rsvp={rsvp}
      className="absolute -right-1 -bottom-1 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-[var(--color-surface)] text-micro leading-none font-bold text-[var(--color-text-secondary)] ring-1 ring-[var(--color-border)]"
    >
      {rsvp === 'tentative' ? (
        '?'
      ) : (
        <svg
          width="8"
          height="8"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="4"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <path d={rsvp === 'accepted' ? 'M20 6L9 17l-5-5' : 'M6 6l12 12M18 6L6 18'} />
        </svg>
      )}
    </span>
  );
}

/** A participant as a pill: picture or initial, their answer, name, and a way to take them off. */
function ParticipantChip({
  member,
  name,
  rsvp,
  removable,
  onRemove,
  canEdit,
  onCanEditChange,
}: {
  member: Member;
  name: string;
  rsvp: Rsvp | undefined;
  removable: boolean;
  onRemove: () => void;
  /** The participant's grant; the toggle shows only with a handler. */
  canEdit: boolean;
  onCanEditChange: ((next: boolean) => void) | undefined;
}) {
  const t = useT();
  const declined = rsvp === 'declined';
  return (
    <span className="flex h-8 max-w-full min-w-0 items-center gap-1.5 rounded-full border border-[var(--color-border)] bg-[var(--color-surface)] pr-1 pl-1 text-footnote text-[var(--color-text-primary)]">
      <span className="relative shrink-0">
        <Avatar
          src={member.avatarUrl}
          name={member.name}
          color={member.color}
          round
          size="h-6 w-6"
          className="text-caption"
        />
        <RsvpMark rsvp={rsvp} />
      </span>
      <span
        className={`truncate ${removable ? '' : 'pr-2'} ${
          declined ? 'text-[var(--color-text-secondary)] line-through' : ''
        }`}
      >
        {name}
      </span>
      {onCanEditChange && (
        <span className="flex shrink-0 items-center gap-1 text-caption text-[var(--color-text-secondary)]">
          {t('attendee.canEdit')}
          <Switch
            size="sm"
            checked={canEdit}
            label={t('attendee.canEdit')}
            onChange={onCanEditChange}
          />
        </span>
      )}
      {removable && (
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
  /** Shows who is involved without any control to change it. */
  readOnly: boolean;
  /** Offers the owner as a choice; otherwise the owner is shown as a plain row. */
  canChangeOwner: boolean;
  /**
   * Whether a participant trusted to edit the event may be taken off. Doing so
   * changes who may edit it, which only a manager or the event's owner may do.
   */
  canRemoveEditors: boolean;
  /** Shown under the owner picker. */
  ownerHint?: string | undefined;
  /** The saved answers and grants, absent on a new event. */
  attendees?: Attendee[] | undefined;
  /** Grants or withdraws a participant's right to edit; absent for anyone who may not. */
  onCanEditChange?: ((userId: string, canEdit: boolean) => void) | undefined;
  onOwnerChange: (id: string) => void;
  onParticipantsChange: (ids: string[]) => void;
}

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
  readOnly,
  canChangeOwner,
  canRemoveEditors,
  ownerHint,
  attendees,
  onCanEditChange,
  onOwnerChange,
  onParticipantsChange,
}: EventPeopleProps) {
  const t = useT();
  // The author reads themselves first: most plans are one's own.
  const ordered = [...members].sort((a, b) => Number(b.id === myId) - Number(a.id === myId));
  const nameOf = (m: Member) => (m.id === myId ? t('event.memberMe', { name: m.name }) : m.name);
  const owner = ordered.find((m) => m.id === (ownerId ?? myId));
  const taking = participants
    .map((id) => ordered.find((m) => m.id === id))
    .filter((m): m is Member => !!m);
  const addable = ordered.filter((m) => !participants.includes(m.id));
  const searchable = members.length >= SEARCHABLE_FROM;
  const attendeeOf = (id: string) => attendees?.find((a) => a.userId === id);

  return (
    <div className="card-section bg-[var(--color-surface-secondary)] p-4">
      <div className="flex items-center justify-between gap-3">
        <span className="shrink-0 text-default text-[var(--color-text-primary)]">
          {t('event.whose')}
        </span>
        {readOnly || !canChangeOwner ? (
          owner && (
            <span className="flex min-w-0 items-center gap-2 text-default text-[var(--color-text-primary)]">
              <Avatar
                src={owner.avatarUrl}
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
            options={ordered.map((m) => memberOption(m, myId, t))}
            onChange={onOwnerChange}
            ariaLabel={t('event.whose')}
            searchable={searchable}
            className="min-w-0"
          />
        )}
      </div>
      {ownerHint && !readOnly && canChangeOwner && (
        <Callout tone="warning" className="mt-2">
          {ownerHint}
        </Callout>
      )}

      <div className="my-3 border-t border-[var(--color-border)] opacity-50" />

      <fieldset className="m-0 min-w-0 border-0 p-0">
        <legend className="mb-2 p-0 text-default text-[var(--color-text-primary)]">
          {t('event.participants')}
        </legend>
        <div className="flex flex-wrap items-center gap-1.5">
          {taking.map((m) => {
            const attendee = attendeeOf(m.id);
            return (
              <ParticipantChip
                key={m.id}
                member={m}
                name={nameOf(m)}
                rsvp={attendee?.rsvp}
                removable={!readOnly && (canRemoveEditors || !attendee?.canEdit)}
                onRemove={() => onParticipantsChange(participants.filter((id) => id !== m.id))}
                canEdit={attendee?.canEdit ?? false}
                onCanEditChange={
                  !readOnly && onCanEditChange && attendee && m.id !== ownerId
                    ? (next) => onCanEditChange(m.id, next)
                    : undefined
                }
              />
            );
          })}
          {!readOnly && addable.length > 0 && (
            <CustomSelect
              value=""
              placeholder={t('event.addParticipant')}
              options={addable.map((m) => memberOption(m, myId, t))}
              onChange={(id) => onParticipantsChange([...participants, id])}
              ariaLabel={t('event.addParticipant')}
              searchable={searchable}
              triggerClassName="h-8 rounded-full border border-dashed border-[var(--color-border-strong)] px-3 text-footnote font-medium text-[var(--color-accent-ink)] hover:bg-[var(--color-hover)]"
            />
          )}
        </div>
        <p className="mt-2 text-footnote text-[var(--color-text-secondary)]">
          {t('event.participantsHint')}
        </p>
      </fieldset>
    </div>
  );
}

/**
 * Whether the time reads as taken. Four equal segments rather than a menu:
 * on a shared calendar "tentative" and "free" are everyday answers. A line
 * under them says what a non-default answer does when a time is being found.
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
    <fieldset className="m-0 min-w-0 border-0 p-0">
      <legend className="mb-2 p-0 text-body text-[var(--color-text-secondary)]">
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
      {/* "Busy" is what everyone assumes; only a departure from it needs saying. */}
      {value !== 'busy' && (
        <p className="mt-1.5 text-footnote text-[var(--color-text-tertiary)]">
          {t(SHOW_AS_HINT[value])}
        </p>
      )}
    </fieldset>
  );
}
