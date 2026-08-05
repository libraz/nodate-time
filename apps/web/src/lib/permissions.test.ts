import { describe, expect, it } from 'vitest';
import {
  canEdit,
  canManage,
  DEFAULT_INVITE_ROLE,
  INVITE_ROLE_OPTIONS,
  ROLE_OPTIONS,
  roleForCalendar,
  roleLabelKey,
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
