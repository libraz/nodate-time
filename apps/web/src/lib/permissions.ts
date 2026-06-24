import type { TranslationKey } from '@/i18n';
import type { Member } from '@/types/calendar';

/** Canonical calendar member roles, mirroring the server-side role model. */
export type Role = 'admin' | 'member' | 'viewer';

/** The single source of truth for the role option set shown in pickers. */
export const ROLE_OPTIONS: Role[] = ['admin', 'member', 'viewer'];

/** Default role for newly created invites / share links. */
export const DEFAULT_INVITE_ROLE: Role = 'member';

/** i18n key for each role's display label. */
export function roleLabelKey(role: string): TranslationKey {
  switch (role) {
    case 'admin':
      return 'members.roleAdmin';
    case 'viewer':
      return 'members.roleViewer';
    default:
      return 'members.roleMember';
  }
}

/** Returns true when the role may create, edit, or delete events (admin or member). */
export function canEdit(role: string | undefined | null): boolean {
  return role === 'admin' || role === 'member';
}

/** Returns true when the role may manage calendar settings and members. */
export function isAdmin(role: string | undefined | null): boolean {
  return role === 'admin';
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
