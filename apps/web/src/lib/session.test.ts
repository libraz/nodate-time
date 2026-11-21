import { beforeEach, describe, expect, it } from 'vitest';
import {
  clearToken,
  decodeJwtExp,
  hasToken,
  isSessionStorageEvent,
  isTokenExpired,
  setToken,
} from './session';

/** Builds an unsigned JWT (`header.payload.signature`) carrying the given claims. */
function makeJwt(claims: Record<string, unknown>): string {
  const encode = (obj: unknown) =>
    btoa(JSON.stringify(obj)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return `${encode({ alg: 'none', typ: 'JWT' })}.${encode(claims)}.sig`;
}

beforeEach(() => {
  localStorage.clear();
});

describe('token helpers', () => {
  it('stores, reports, and clears the token', () => {
    expect(hasToken()).toBe(false);
    setToken('abc');
    expect(hasToken()).toBe(true);
    expect(localStorage.getItem('tt_token')).toBe('abc');
    clearToken();
    expect(hasToken()).toBe(false);
  });

  it('counts a refresh token beside an expired access token as a way in', () => {
    setToken(makeJwt({ exp: Math.floor(Date.now() / 1000) - 60 }), 'refresh-1');
    expect(hasToken()).toBe(true);
  });

  it('recognizes a change to the stored session, or to the whole storage', () => {
    expect(isSessionStorageEvent(new StorageEvent('storage', { key: 'tt_token' }))).toBe(true);
    expect(isSessionStorageEvent(new StorageEvent('storage', { key: 'tt_refresh' }))).toBe(true);
    expect(isSessionStorageEvent(new StorageEvent('storage', { key: null }))).toBe(true);
    expect(isSessionStorageEvent(new StorageEvent('storage', { key: 'locale' }))).toBe(false);
  });
});

describe('JWT expiry helpers', () => {
  it('decodes the exp claim from a well-formed token', () => {
    expect(decodeJwtExp(makeJwt({ exp: 1700000000 }))).toBe(1700000000);
  });

  it('returns null for a token without an exp claim', () => {
    expect(decodeJwtExp(makeJwt({ sub: 'user' }))).toBeNull();
  });

  it('returns null for a malformed token', () => {
    expect(decodeJwtExp('not-a-jwt')).toBeNull();
    expect(decodeJwtExp('')).toBeNull();
    expect(decodeJwtExp('a.b.c')).toBeNull();
  });

  it('reports an expired token as expired', () => {
    const past = Math.floor(Date.now() / 1000) - 60;
    expect(isTokenExpired(makeJwt({ exp: past }))).toBe(true);
  });

  it('reports a still-valid token as not expired', () => {
    const future = Math.floor(Date.now() / 1000) + 3600;
    expect(isTokenExpired(makeJwt({ exp: future }))).toBe(false);
  });

  it('treats a token with no decodable exp as not expired', () => {
    expect(isTokenExpired(makeJwt({ sub: 'user' }))).toBe(false);
    expect(isTokenExpired('garbage')).toBe(false);
  });
});

describe('getToken expiry handling', () => {
  it('clears an expired stored token and reports no token', () => {
    const past = Math.floor(Date.now() / 1000) - 60;
    setToken(makeJwt({ exp: past }));

    expect(hasToken()).toBe(false);
    expect(localStorage.getItem('tt_token')).toBeNull();
  });

  it('keeps a still-valid stored token', () => {
    const future = Math.floor(Date.now() / 1000) + 3600;
    const token = makeJwt({ exp: future });
    setToken(token);

    expect(hasToken()).toBe(true);
    expect(localStorage.getItem('tt_token')).toBe(token);
  });
});
