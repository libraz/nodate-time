import { describe, expect, it } from 'vitest';
import {
  assignableRoles,
  canDeleteComment,
  canEdit,
  canEditComment,
  canEditEvent,
  canManage,
  canOwn,
  DEFAULT_INVITE_ROLE,
  INVITE_ROLE_OPTIONS,
  membershipFor,
  ROLE_OPTIONS,
  roleForCalendar,
  roleLabelKey,
  roleOnCalendar,
} from './permissions';

describe('invite roles', () => {
  // The API enumerates exactly these two for an invite body. An option the
  // server does not accept fails every create with 422, and because
  // roleLabelKey falls back to the editor label, the broken option is the one
  // that reads as "Editor".
  it('offers only the roles the API accepts', () => {
    expect(INVITE_ROLE_OPTIONS).toEqual(['editor', 'viewer']);
  });

  it('defaults to a role that is actually on offer', () => {
    expect(INVITE_ROLE_OPTIONS).toContain(DEFAULT_INVITE_ROLE);
  });

  it('gives every offered role its own label rather than the fallback', () => {
    const keys = INVITE_ROLE_OPTIONS.map(roleLabelKey);
    expect(new Set(keys).size).toBe(INVITE_ROLE_OPTIONS.length);
  });

  it('never lets an invite hand out administration', () => {
    for (const role of INVITE_ROLE_OPTIONS) {
      expect(canManage(role)).toBe(false);
    }
  });
});

describe('calendar roles', () => {
  it('labels every role distinctly', () => {
    const keys = ROLE_OPTIONS.map(roleLabelKey);
    expect(new Set(keys).size).toBe(ROLE_OPTIONS.length);
  });

  it('treats owner and manager as administrators', () => {
    expect(canManage('owner')).toBe(true);
    expect(canManage('manager')).toBe(true);
    expect(canManage('editor')).toBe(false);
    expect(canManage('viewer')).toBe(false);
  });

  it('treats everything above viewer as writable', () => {
    expect(canEdit('editor')).toBe(true);
    expect(canEdit('viewer')).toBe(false);
    expect(canEdit(undefined)).toBe(false);
  });

  it('separates owning from managing', () => {
    expect(canOwn('owner')).toBe(true);
    expect(canOwn('manager')).toBe(false);
    expect(canOwn(undefined)).toBe(false);
  });

  // The server refuses a manager's attempt to hand out ownership, so offering
  // the option would produce a picker whose top entry always fails.
  it('offers ownership only to whoever already has it', () => {
    expect(assignableRoles('owner')).toEqual(ROLE_OPTIONS);
    expect(assignableRoles('manager')).not.toContain('owner');
    expect(assignableRoles('manager')).toEqual(['manager', 'editor', 'viewer']);
  });
});

describe('canEditEvent', () => {
  const someoneElses = { ownerId: 'u-other', attendees: [] };

  it('lets a member change what sits on their own layer', () => {
    expect(canEditEvent({ ownerId: 'u-me' }, 'editor', 'u-me')).toBe(true);
  });

  it('does not let an editor rewrite what sits on another layer', () => {
    expect(canEditEvent(someoneElses, 'editor', 'u-me')).toBe(false);
  });

  it('admits the people who run the calendar', () => {
    expect(canEditEvent(someoneElses, 'manager', 'u-me')).toBe(true);
    expect(canEditEvent(someoneElses, 'owner', 'u-me')).toBe(true);
  });

  it('honours a delegation the owner granted', () => {
    const delegated = {
      ownerId: 'u-other',
      attendees: [
        { userId: 'u-me', rsvp: 'accepted' as const, canEdit: true },
        { userId: 'u-third', rsvp: 'pending' as const, canEdit: false },
      ],
    };
    expect(canEditEvent(delegated, 'editor', 'u-me')).toBe(true);
    expect(canEditEvent(delegated, 'editor', 'u-third')).toBe(false);
  });

  // Attending is not permission; only an explicit grant is.
  it('does not treat participation as permission', () => {
    const attending = {
      ownerId: 'u-other',
      attendees: [{ userId: 'u-me', rsvp: 'accepted' as const, canEdit: false }],
    };
    expect(canEditEvent(attending, 'editor', 'u-me')).toBe(false);
  });

  it('never admits a read-only member', () => {
    expect(canEditEvent({ ownerId: 'u-me' }, 'viewer', 'u-me')).toBe(false);
  });
});

describe('roleForCalendar', () => {
  const members = [
    { id: 'm1', name: 'A', email: 'a@example.com', role: 'owner' },
    { id: 'm2', name: 'B', email: 'b@example.com', role: 'viewer' },
  ] as Parameters<typeof roleForCalendar>[0];

  it('finds the caller among the members', () => {
    expect(roleForCalendar(members, 'b@example.com')).toBe('viewer');
  });

  it('reports unknown rather than guessing when members are not loaded', () => {
    expect(roleForCalendar(undefined, 'a@example.com')).toBeUndefined();
    expect(roleForCalendar(members, undefined)).toBeUndefined();
  });
});

describe('roleOnCalendar', () => {
  const calendars = [
    { id: 'cal-1', role: 'owner' },
    { id: 'cal-2', role: 'viewer' },
  ];

  it('reports the role the server stated for the calendar', () => {
    expect(roleOnCalendar(calendars, 'cal-1')).toBe('owner');
    expect(roleOnCalendar(calendars, 'cal-2')).toBe('viewer');
  });

  it('does not fall back to the role of another calendar', () => {
    // Each membership stands alone: administering one calendar says nothing
    // about the next one, and a lookup that missed used to leave the caller
    // holding whatever the member list happened to answer.
    expect(canManage(roleOnCalendar(calendars, 'cal-2'))).toBe(false);
  });

  it('has nothing to report for a calendar the caller is not on', () => {
    expect(roleOnCalendar(calendars, 'cal-3')).toBeUndefined();
    expect(roleOnCalendar(undefined, 'cal-1')).toBeUndefined();
    expect(roleOnCalendar(calendars, undefined)).toBeUndefined();
  });
});

describe('membershipFor', () => {
  const members = [
    { id: 'm1', name: 'A', email: 'a@example.com', role: 'owner' },
    { id: 'm2', name: 'B', email: 'b@example.com', role: 'viewer' },
  ] as Parameters<typeof membershipFor>[0];

  it('reports the role of a member', () => {
    expect(membershipFor(members, 'b@example.com')).toEqual({ status: 'member', role: 'viewer' });
  });

  it('separates a list that never arrived from one that answered no', () => {
    // Both deny every action. Only the first is worth asking about again, and
    // collapsing them is what made one failed fetch look like a lost role.
    expect(membershipFor(undefined, 'a@example.com')).toEqual({ status: 'unknown' });
    expect(membershipFor(members, 'nobody@example.com')).toEqual({ status: 'none' });
  });
});

describe('comment moderation', () => {
  const theirs = { userPublicId: 'u-them' };
  const mine = { userPublicId: 'u-me' };

  it('lets the author edit and remove their own comment', () => {
    expect(canEditComment(mine, 'u-me')).toBe(true);
    expect(canDeleteComment(mine, 'editor', 'u-me')).toBe(true);
  });

  it('does not let one editor touch another member’s comment', () => {
    expect(canEditComment(theirs, 'u-me')).toBe(false);
    expect(canDeleteComment(theirs, 'editor', 'u-me')).toBe(false);
  });

  it('lets whoever runs the calendar take a comment down', () => {
    // The only remedy for something posted on a shared calendar, short of
    // deleting the event it hangs off.
    expect(canDeleteComment(theirs, 'manager', 'u-me')).toBe(true);
    expect(canDeleteComment(theirs, 'owner', 'u-me')).toBe(true);
  });

  it('still refuses to let them rewrite it', () => {
    // Removing words and substituting different ones are not the same power.
    expect(canEditComment(theirs, 'u-me')).toBe(false);
  });
});
