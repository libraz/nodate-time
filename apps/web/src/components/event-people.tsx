import { Avatar } from '@/components/avatar';
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

/** A member as a pill: their picture or initial in their colour, then their name. */
function MemberChip({
  member,
  selected,
  isMe,
  disabled,
  onClick,
}: {
  member: Member;
  selected: boolean;
  isMe: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  const t = useT();
  const name = isMe ? t('event.memberMe', { name: member.name }) : member.name;
  return (
    <button
      type="button"
      aria-pressed={selected}
      disabled={disabled}
      onClick={onClick}
      className="flex h-8 max-w-full min-w-0 items-center gap-1.5 rounded-full border py-0 pr-3 pl-1 text-footnote transition-colors disabled:cursor-default"
      style={
        selected
          ? {
              background: `color-mix(in srgb, ${member.color} 12%, var(--color-surface))`,
              borderColor: `color-mix(in srgb, ${member.color} 50%, transparent)`,
              color: 'var(--color-text-primary)',
              fontWeight: 600,
            }
          : {
              background: 'var(--color-surface)',
              borderColor: 'var(--color-border)',
              color: 'var(--color-text-secondary)',
            }
      }
    >
      <Avatar
        src={member.avatar}
        name={member.name}
        color={member.color}
        round
        size="h-6 w-6"
        className="text-caption"
      />
      <span className="truncate">{name}</span>
    </button>
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

/**
 * Who an event is about: whose plan it is, and who else takes part. On a
 * shared calendar this is the first thing a plan says, so it sits with the
 * title rather than behind the optional fields.
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
  const owner = ownerId ?? myId;
  const ownerMember = members.find((m) => m.id === owner);

  return (
    <div className="card-section bg-[var(--color-surface-secondary)] p-4">
      <fieldset className="m-0 min-w-0 border-0 p-0">
        <legend className="mb-2 p-0 text-footnote font-medium text-[var(--color-text-secondary)]">
          {t('event.whose')}
        </legend>
        <div className="flex flex-wrap gap-1.5">
          {ordered.map((m) => (
            <MemberChip
              key={m.id}
              member={m}
              isMe={m.id === myId}
              selected={m.id === owner}
              disabled={disabled}
              onClick={() => onOwnerChange(m.id)}
            />
          ))}
        </div>
        {ownerMember && (
          <p className="mt-2 text-caption text-[var(--color-text-tertiary)]">
            {t('event.whoseHint', { name: ownerMember.name })}
          </p>
        )}
      </fieldset>

      <div className="my-3 border-t border-[var(--color-border)] opacity-50" />

      <fieldset className="m-0 min-w-0 border-0 p-0">
        <legend className="mb-2 p-0 text-footnote font-medium text-[var(--color-text-secondary)]">
          {t('event.participants')}
        </legend>
        <div className="flex flex-wrap gap-1.5">
          {ordered.map((m) => {
            const selected = participants.includes(m.id);
            return (
              <MemberChip
                key={m.id}
                member={m}
                isMe={m.id === myId}
                selected={selected}
                disabled={disabled}
                onClick={() =>
                  onParticipantsChange(
                    selected ? participants.filter((id) => id !== m.id) : [...participants, m.id],
                  )
                }
              />
            );
          })}
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
        <legend className="mb-2 p-0 text-footnote font-medium text-[var(--color-text-secondary)]">
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
