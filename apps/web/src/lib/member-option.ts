import type { TranslationKey } from '@/i18n';
import type { Member } from '@/types/calendar';

type Translate = (key: TranslationKey, params?: Record<string, string | number>) => string;

/** A member as a picker option: colour, avatar, and "(you)" on the viewer's own row. */
export function memberOption(member: Member, myId: string | undefined, t: Translate) {
  return {
    value: member.id,
    label: member.id === myId ? t('event.memberMe', { name: member.name }) : member.name,
    color: member.color,
    person: { name: member.name, avatarUrl: member.avatarUrl },
  };
}
