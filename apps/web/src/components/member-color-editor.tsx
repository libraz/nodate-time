import { ColorEditor } from '@/components/color-editor';
import { useT } from '@/i18n';
import type { Member } from '@/types/calendar';

/**
 * A member's layer colour. Colours other members already carry are marked but
 * stay choosable, since a repeat is sometimes what people want.
 */
export function MemberColorEditor({
  member,
  others,
  onCommit,
}: {
  member: Member;
  /** The rest of the calendar, to mark the colours already in use. */
  others: Member[];
  onCommit: (color: string) => void;
}) {
  const t = useT();
  return (
    <div className="member-color-editor mt-3 sm:pl-12">
      <ColorEditor
        value={member.color}
        onChange={onCommit}
        inUse={others.map((o) => ({ color: o.color, name: o.name }))}
        legend={t('members.colorPalette')}
        inUseHint={t('members.colorInUseHint')}
      />
    </div>
  );
}
