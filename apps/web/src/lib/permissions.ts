import type { TranslationKey } from '@/i18n';
import type { Member } from '@/types/calendar';

/**
 * Calendar member roles, mirroring the shared schema's ordering:
 * owner > manager > editor > viewer.
 */
export type Role = 'owner' | 'manager' | 'editor' | 'viewer';

/** The single source of truth for the role option set shown in pickers. */
export const ROLE_OPTIONS: Role[] = ['owner', 'manager', 'editor', 'viewer'];

/**
 * Roles an invite link may grant. Owner and manager are deliberately absent:
 * a link that handed out the power to hand out access would let whoever
 * holds it widen its own reach.
 */
export const INVITE_ROLE_OPTIONS: Role[] = ['editor', 'viewer'];

/** Default role for newly created invites / share links. */
export const DEFAULT_INVITE_ROLE: Role = 'editor';

/** i18n key for each role's display label. */
export function roleLabelKey(role: string): TranslationKey {
  switch (role) {
    case 'owner':
      return 'members.roleOwner';
    case 'manager':
      return 'members.roleManager';
    case 'viewer':
      return 'members.roleViewer';
    default:
      return 'members.roleEditor';
  }
}

/** Returns true when the role may create, edit, or delete calendar contents. */
export function canEdit(role: string | undefined | null): boolean {
  return role === 'owner' || role === 'manager' || role === 'editor';
}

/** Returns true when the role may change calendar settings and membership. */
export function canManage(role: string | undefined | null): boolean {
  return role === 'owner' || role === 'manager';
}

/**
 * Resolves the current user's role for a calendar from its members list.
 * Returns undefined when membership is unknown (e.g. members not yet loaded).
 */
export function roleForCalendar(
  members: Member[] | undefined,
  userEmail: string | undefined,
): Role | undefined {
  if (!members || !userEmail) return undefined;
  const me = members.find((m) => m.email === userEmail);
  return (me?.role as Role | undefined) ?? undefined;
}
