import type { Member } from '@/types/calendar';

/** What the member list may offer for one row. */
export interface MemberRowRules {
  isMe: boolean;
  /** Whether the viewer may act on this member at all. */
  mayTouch: boolean;
  canChangeRole: boolean;
  canRemove: boolean;
}

interface MemberRowContext {
  meId: string | undefined;
  canManageMembers: boolean;
  amOwner: boolean;
  ownerCount: number;
}

/**
 * Resolves which controls a member row shows. An owner's role and membership
 * are the owner's own business, so a manager sees that row without controls;
 * nobody changes their own role (the server refuses it); removing yourself is
 * leaving, which any member may do except the last owner.
 */
export function memberRowRules(
  member: Pick<Member, 'id' | 'role'>,
  { meId, canManageMembers, amOwner, ownerCount }: MemberRowContext,
): MemberRowRules {
  const isMe = member.id === meId;
  const lastOwner = member.role === 'owner' && ownerCount <= 1;
  const mayTouch = canManageMembers && (member.role !== 'owner' || amOwner);
  return {
    isMe,
    mayTouch,
    canChangeRole: mayTouch && !isMe && !lastOwner,
    canRemove: isMe ? !lastOwner : mayTouch && !lastOwner,
  };
}
