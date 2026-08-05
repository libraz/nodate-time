import { describe, expect, it } from 'vitest';
import { type InviteData, isPublicLink, mergeInviteTokens } from './invite';

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
