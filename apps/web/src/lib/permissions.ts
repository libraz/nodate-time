import type { TranslationKey } from '@/i18n';
import type { Attendee, Calendar, CalendarEvent, Member } from '@/types/calendar';

/**
 * Calendar member roles, mirroring the shared schema's ordering:
 * owner > manager > editor > viewer.
 */
export type Role = 'owner' | 'manager' | 'editor' | 'viewer';

/** The single source of truth for the role option set shown in pickers. */
export const ROLE_OPTIONS: Role[] = ['owner', 'manager', 'editor', 'viewer'];

/**
 * The roles the given actor may assign. Ownership only moves by the owner's
 * hand, so a manager is offered everything below it -- the server refuses the
 * promotion either way, and an option that always fails reads as a bug.
 */
export function assignableRoles(actorRole: string | undefined | null): Role[] {
  return canOwn(actorRole) ? ROLE_OPTIONS : ROLE_OPTIONS.filter((r) => r !== 'owner');
}

/**
 * Roles an invite link may grant. Owner and manager are deliberately absent:
 * a link that handed out the power to hand out access would let whoever
 * holds it widen its own reach.
 */
export const INVITE_ROLE_OPTIONS: Role[] = ['editor', 'viewer'];

/** Default role for newly created invites / share links. */
export const DEFAULT_INVITE_ROLE: Role = 'editor';

/** i18n key for each visibility's display label. */
export function visibilityLabelKey(visibility: string): TranslationKey {
  switch (visibility) {
    case 'public':
      return 'event.visibilityPublic';
    case 'private':
      return 'event.visibilityPrivate';
    case 'confidential':
      return 'event.visibilityConfidential';
    default:
      return 'event.visibilityDefault';
  }
}

/** i18n key for each availability value's display label. */
export function showAsLabelKey(showAs: string): TranslationKey {
  switch (showAs) {
    case 'free':
      return 'event.showAsFree';
    case 'tentative':
      return 'event.showAsTentative';
    case 'oof':
      return 'event.showAsOof';
    default:
      return 'event.showAsBusy';
  }
}

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

/**
 * Returns true when the caller may change this particular event.
 *
 * Writing to a calendar and rewriting a given event are different grants: a
 * shared calendar carries one layer per person, and an editor who may add to
 * their own has no business rewriting somebody else's appointment. This
 * mirrors the server's rule so the UI does not offer a drag that comes back
 * 403.
 */
export function canEditEvent(
  event: Pick<CalendarEvent, 'ownerId'> & { attendees?: Attendee[] },
  role: string | undefined | null,
  myUserId: string | undefined,
): boolean {
  if (!canEdit(role)) return false;
  if (canManage(role)) return true;
  if (!myUserId) return false;
  if (event.ownerId === myUserId) return true;
  return (event.attendees ?? []).some((a) => a.userId === myUserId && a.canEdit);
}

/** Returns true when the role may change calendar settings and membership. */
export function canManage(role: string | undefined | null): boolean {
  return role === 'owner' || role === 'manager';
}

/**
 * Returns true when the caller may take this comment off the thread.
 *
 * Comments are open to every member of a shared calendar, so removal has to
 * reach further than authorship: otherwise the only remedy for something
 * posted on a family calendar is deleting the event it hangs off. Rewriting
 * stays with the author -- see {@link canEditComment}. This mirrors the
 * server's rule.
 */
export function canDeleteComment(
  comment: { userPublicId: string },
  role: string | undefined | null,
  myUserId: string | undefined,
): boolean {
  return canEditComment(comment, myUserId) || canManage(role);
}

/**
 * Returns true when the caller may rewrite this comment. Only its author may:
 * taking words off the wall and putting different words in someone's mouth
 * are not the same power.
 */
export function canEditComment(
  comment: { userPublicId: string },
  myUserId: string | undefined,
): boolean {
  return !!myUserId && comment.userPublicId === myUserId;
}

/**
 * Returns true when the role may grant or revoke ownership, remove an owner,
 * and delete the calendar. Managing membership does not carry these.
 */
export function canOwn(role: string | undefined | null): boolean {
  return role === 'owner';
}

/**
 * What is known about a user's standing in a calendar.
 *
 * `unknown` and `none` deny exactly the same actions, which is why they used
 * to be one value. They are not the same situation: one is an answer, the
 * other is the absence of one. A member list that failed to arrive left the
 * calendar looking read-only for the rest of the session, indistinguishable
 * from a role that really is read-only -- and so nothing offered to ask
 * again, because nothing knew there was anything to ask.
 */
export type Membership =
  | { status: 'unknown' }
  | { status: 'none' }
  | { status: 'member'; role: Role };

/**
 * Resolves what is known about the current user's standing in a calendar from
 * its members list. An absent list is unknown, not a denial.
 */
export function membershipFor(
  members: Member[] | undefined,
  userEmail: string | undefined,
): Membership {
  if (!members || !userEmail) return { status: 'unknown' };
  const me = members.find((m) => m.email === userEmail);
  if (!me?.role) return { status: 'none' };
  return { status: 'member', role: me.role as Role };
}

/**
 * Resolves the current user's role for a calendar from its members list.
 * Returns undefined when there is no role to report -- either because the
 * membership is unknown or because there is none. Callers that need to tell
 * those apart want {@link membershipFor}.
 *
 * Callers holding the calendar list want {@link roleOnCalendar} instead: the
 * server states the role there, so there is no account to recognise.
 */
export function roleForCalendar(
  members: Member[] | undefined,
  userEmail: string | undefined,
): Role | undefined {
  const membership = membershipFor(members, userEmail);
  return membership.status === 'member' ? membership.role : undefined;
}

/**
 * The signed-in user's own role on a calendar, as the server reported it with
 * the calendar itself.
 *
 * The server resolves the caller's membership to answer the request at all,
 * so the role travels with the calendar and there is no account to recognise
 * here. That matters: the member list carries an address only on the rows the
 * caller is allowed to see one on, and an address is not an identity in any
 * case.
 *
 * Returns undefined when the calendar is not in the list: one the caller is
 * not a member of is not there to have a role on.
 */
export function roleOnCalendar(
  calendars: readonly Pick<Calendar, 'id' | 'role'>[] | undefined,
  calendarId: string | undefined,
): Role | undefined {
  if (!calendars || !calendarId) return undefined;
  const calendar = calendars.find((c) => c.id === calendarId);
  return calendar?.role ? (calendar.role as Role) : undefined;
}
