import { describe, expect, it } from 'vitest';
import {
  INVITE_EXPIRY_HOURS,
  INVITE_MAX_USES,
  type InviteData,
  inviteCreateBody,
  inviteExpiryLabelKey,
  inviteUsesLabelKey,
  isPublicLink,
  mergeInviteTokens,
} from './invite';

function makeInvite(overrides: Partial<InviteData> = {}): InviteData {
  return {
    id: 'a1b2c3d4-0000-0000-0000-000000000000',
    role: 'editor',
    maxUses: 1,
    useCount: 0,
    isPublic: false,
    expiresAt: null,
    createdAt: '2026-08-05T00:00:00Z',
    ...overrides,
  };
}

describe('mergeInviteTokens', () => {
  it('keeps a token the session already holds when the listing omits it', () => {
    const created = makeInvite({ token: 'secret-token' });
    const listed = makeInvite();
    expect(mergeInviteTokens([listed], [created])[0]?.token).toBe('secret-token');
  });

  it('leaves an invite the session never created without a token', () => {
    const listed = makeInvite({ id: 'unknown-id' });
    expect(mergeInviteTokens([listed], [])[0]?.token).toBeUndefined();
  });

  it('drops revoked invites along with their tokens', () => {
    const created = makeInvite({ token: 'secret-token' });
    expect(mergeInviteTokens([], [created])).toEqual([]);
  });

  it('does not overwrite a token the listing did carry', () => {
    const created = makeInvite({ token: 'stale' });
    const listed = makeInvite({ token: 'fresh' });
    expect(mergeInviteTokens([listed], [created])[0]?.token).toBe('fresh');
  });
});

describe('isPublicLink', () => {
  it('separates embed links from join links', () => {
    expect(isPublicLink(makeInvite({ isPublic: true }))).toBe(true);
    expect(isPublicLink(makeInvite())).toBe(false);
  });
});

describe('inviteCreateBody', () => {
  it('sends the bounds the user chose', () => {
    expect(inviteCreateBody('editor', 168, 5)).toEqual({
      role: 'editor',
      expiresInHours: 168,
      maxUses: 5,
    });
  });

  // The API reads an absent field as "no limit". Sending zero would ask for a
  // link that expires immediately, or one nobody may use.
  it('omits a bound rather than sending zero for it', () => {
    expect(inviteCreateBody('viewer', 0, 0)).toEqual({ role: 'viewer' });
    expect(inviteCreateBody('viewer', 24, 0)).toEqual({ role: 'viewer', expiresInHours: 24 });
    expect(inviteCreateBody('viewer', 0, 1)).toEqual({ role: 'viewer', maxUses: 1 });
  });
});

describe('invite bound options', () => {
  it('labels every choice distinctly', () => {
    const expiryKeys = INVITE_EXPIRY_HOURS.map(inviteExpiryLabelKey);
    expect(new Set(expiryKeys).size).toBe(INVITE_EXPIRY_HOURS.length);
    const usesKeys = INVITE_MAX_USES.map(inviteUsesLabelKey);
    expect(new Set(usesKeys).size).toBe(INVITE_MAX_USES.length);
  });

  // An unbounded link is the one that cannot be taken back once forwarded, so
  // it is available but not what the picker opens on.
  it('offers an unbounded choice without leading with it', () => {
    expect(INVITE_EXPIRY_HOURS).toContain(0);
    expect(INVITE_EXPIRY_HOURS[0]).not.toBe(0);
    expect(INVITE_MAX_USES).toContain(0);
    expect(INVITE_MAX_USES[0]).not.toBe(0);
  });
});
