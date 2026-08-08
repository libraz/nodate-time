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

  /**
   * The expiry is always stated. A request that omits it is not asking for an
   * unlimited link: the API answers an absent expiry with a default of its own
   * -- seven days for a join link -- so leaving the field out would quietly
   * replace whatever the person chose.
   */
  it('states the expiry rather than leaving the API to supply one', () => {
    expect(inviteCreateBody('viewer', 24, 0)).toEqual({ role: 'viewer', expiresInHours: 24 });
    expect(inviteCreateBody('viewer', 720, 5)).toEqual({
      role: 'viewer',
      expiresInHours: 720,
      maxUses: 5,
    });
  });

  // An absent use limit does still mean unlimited, and a zero would ask for a
  // link nobody may use.
  it('omits a zero use limit rather than sending it', () => {
    expect(inviteCreateBody('viewer', 24, 0)).not.toHaveProperty('maxUses');
  });
});

describe('invite bound options', () => {
  it('labels every choice distinctly', () => {
    const expiryKeys = INVITE_EXPIRY_HOURS.map(inviteExpiryLabelKey);
    expect(new Set(expiryKeys).size).toBe(INVITE_EXPIRY_HOURS.length);
    const usesKeys = INVITE_MAX_USES.map(inviteUsesLabelKey);
    expect(new Set(usesKeys).size).toBe(INVITE_MAX_USES.length);
  });

  /**
   * A join link cannot be asked to live forever. The API takes an expiry of
   * between one hour and a year, and reads its absence as its own seven-day
   * default, so there is no value the client can send that means "never" --
   * and an option saying otherwise produces a link that dies in a week under
   * a label promising it never would.
   */
  it('offers no expiry the API cannot honour', () => {
    expect(INVITE_EXPIRY_HOURS).not.toContain(0);
    expect(INVITE_EXPIRY_HOURS.every((hours) => hours >= 1 && hours <= 8760)).toBe(true);
  });

  // Uses are a different bound: an absent one really is unlimited, so the
  // choice is offered -- just not led with.
  it('offers an unbounded use count without leading with it', () => {
    expect(INVITE_MAX_USES).toContain(0);
    expect(INVITE_MAX_USES[0]).not.toBe(0);
  });
});
