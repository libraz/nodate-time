import { describe, expect, it } from 'vitest';
import { inviteTokenToJoin } from './invite-join';

describe('inviteTokenToJoin', () => {
  it('reads the token from a share path that asks to join', () => {
    expect(inviteTokenToJoin('/share/abc123?join=1')).toBe('abc123');
    expect(inviteTokenToJoin('/share/a%2Fb?join=1')).toBe('a/b');
  });

  it('is null without the join flag, for other paths, and for unsafe values', () => {
    expect(inviteTokenToJoin('/share/abc123')).toBeNull();
    expect(inviteTokenToJoin('/share/abc123?join=0')).toBeNull();
    expect(inviteTokenToJoin('/embed/abc123?join=1')).toBeNull();
    expect(inviteTokenToJoin('/share/abc/extra?join=1')).toBeNull();
    expect(inviteTokenToJoin('//evil.test/share/x?join=1')).toBeNull();
    expect(inviteTokenToJoin(undefined)).toBeNull();
  });
});
