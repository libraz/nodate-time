import { describe, expect, it } from 'vitest';
import { memberRowRules } from './member-row';

const ctx = { meId: 'me', canManageMembers: true, amOwner: false, ownerCount: 2 };

describe('memberRowRules', () => {
  it('lets a manager change and remove another member', () => {
    expect(memberRowRules({ id: 'x', role: 'editor' }, ctx)).toEqual({
      isMe: false,
      mayTouch: true,
      canChangeRole: true,
      canRemove: true,
    });
  });

  it('hides the controls of an owner from a manager who is not one', () => {
    const r = memberRowRules({ id: 'o', role: 'owner' }, ctx);
    expect(r.mayTouch).toBe(false);
    expect(r.canChangeRole).toBe(false);
    expect(r.canRemove).toBe(false);
  });

  it('never offers a role change on yourself, but lets any member leave', () => {
    const r = memberRowRules({ id: 'me', role: 'viewer' }, { ...ctx, canManageMembers: false });
    expect(r.isMe).toBe(true);
    expect(r.canChangeRole).toBe(false);
    expect(r.canRemove).toBe(true);
  });

  it('keeps the last owner in place', () => {
    const r = memberRowRules({ id: 'me', role: 'owner' }, { ...ctx, amOwner: true, ownerCount: 1 });
    expect(r.canRemove).toBe(false);
    expect(r.canChangeRole).toBe(false);
  });
});
