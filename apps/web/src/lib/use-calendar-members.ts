import { useCallback, useEffect } from 'react';
import { useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { assignableRoles, canManage, canOwn, type Role, roleOnCalendar } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import type { Member } from '@/types/calendar';

export interface CalendarMembers {
  members: Member[];
  /** The signed-in user's own role here, as the server reported it with the calendar. */
  myRole: Role | undefined;
  /** Whether the caller may change membership at all. */
  canManageMembers: boolean;
  amOwner: boolean;
  /** The roles the caller may assign, which is not every role there is. */
  roleOptions: Role[];
  ownerCount: number;
  changeRole: (member: Member, role: string) => Promise<void>;
  /**
   * Removes a member, or gives up the caller's own membership when the member
   * is the caller. Returns true when the caller left, which is the case where
   * the calendar itself is gone and a screen holding a selection needs another.
   */
  removeMember: (member: Member) => Promise<boolean>;
}

/**
 * A calendar's member list and the operations on it.
 *
 * The rules a screen has to get right — a calendar keeps its last owner, an
 * owner's membership is the owner's own business, leaving is not the same
 * operation as being removed — live here rather than in each screen that shows
 * members, so there is one place they can be read and one place they can be
 * corrected.
 */
export function useCalendarMembers(calendarId: string): CalendarMembers {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const leaveCalendar = useCalendarStore((s) => s.leaveCalendar);
  const me = useAuthStore((s) => s.user);

  useEffect(() => {
    if (calendarId) fetchMembers(calendarId);
  }, [calendarId, fetchMembers]);

  const members = (calendarId && membersMap[calendarId]) || [];
  // Read from the calendar, not from recognising an account in the member
  // list: the list carries an address only on the rows the caller may see one
  // on, and it arrives later than the first render either way.
  const myRole = roleOnCalendar(calendars, calendarId);
  const ownerCount = members.filter((m) => m.role === 'owner').length;

  const changeRole = useCallback(
    async (member: Member, role: string) => {
      // A calendar must keep one owner, or nobody is left who can administer
      // it. The server enforces this too; refusing here saves a round trip and
      // says why in words the reader has a chance with.
      if (member.role === 'owner' && role !== 'owner' && ownerCount <= 1) {
        toast.error(t('members.lastOwner'));
        return;
      }
      try {
        await api.put(`/calendars/${calendarId}/members/${member.id}/role`, { role });
        await fetchMembers(calendarId);
        toast.success(t('panel.updated'));
      } catch (e) {
        toast.error(errorMessage(e));
      }
    },
    [calendarId, fetchMembers, ownerCount, t],
  );

  const removeMember = useCallback(
    async (member: Member) => {
      const leaving = member.id === me?.id;
      if (!confirm(leaving ? t('members.leaveConfirm') : t('members.removeConfirm'))) return false;
      try {
        if (leaving) {
          // Leaving takes the calendar with it. Refetching its members instead
          // asks a calendar the caller has just left, and the 403 that comes
          // back reads as a failure to leave.
          await leaveCalendar(calendarId, member.id);
          toast.success(t('members.leftCalendar'));
          return true;
        }
        await api.delete(`/calendars/${calendarId}/members/${member.id}`);
        await fetchMembers(calendarId);
        toast.success(t('panel.updated'));
      } catch (e) {
        toast.error(errorMessage(e));
      }
      return false;
    },
    [calendarId, fetchMembers, leaveCalendar, me?.id, t],
  );

  return {
    members,
    myRole,
    canManageMembers: canManage(myRole),
    amOwner: canOwn(myRole),
    roleOptions: assignableRoles(myRole),
    ownerCount,
    changeRole,
    removeMember,
  };
}
