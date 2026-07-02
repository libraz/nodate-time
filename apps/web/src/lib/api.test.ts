import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api, clearToken, decodeJwtExp, hasToken, isTokenExpired, setToken } from './api';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** Builds an unsigned JWT (`header.payload.signature`) carrying the given claims. */
function makeJwt(claims: Record<string, unknown>): string {
  const encode = (obj: unknown) =>
    btoa(JSON.stringify(obj)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return `${encode({ alg: 'none', typ: 'JWT' })}.${encode(claims)}.sig`;
}

const fetchMock = vi.fn();

beforeEach(() => {
  localStorage.clear();
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
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

describe('request', () => {
  it('attaches a Bearer header when a token is present', async () => {
    setToken('secret');
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));

    await api.get('/calendars');

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer secret');
  });

  it('omits the Authorization header when there is no token', async () => {
    fetchMock.mockResolvedValue(jsonResponse([]));

    await api.get('/calendars');

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
  });

  it('serializes a JSON body for POST and sets the method', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ id: '1' }));

    await api.post('/calendars', { name: 'Team' });

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ name: 'Team' }));
  });

  it('omits the body when none is provided', async () => {
    fetchMock.mockResolvedValue(jsonResponse({}));

    await api.post('/calendars/1/leave');

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.body).toBeUndefined();
  });

  it('returns undefined for a 204 response', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));

    const result = await api.delete('/calendars/1');

    expect(result).toBeUndefined();
  });

  it('throws an ApiError carrying the parsed detail message', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'Calendar not found' }, 404));

    await expect(api.get('/calendars/missing')).rejects.toMatchObject({
      status: 404,
      detail: 'Calendar not found',
    });
  });

  it('falls back to statusText when the error body is not JSON', async () => {
    fetchMock.mockResolvedValue(new Response('boom', { status: 500, statusText: 'Server Error' }));

    await expect(api.get('/calendars')).rejects.toBeInstanceOf(ApiError);
  });
});

describe('401 handling', () => {
  beforeEach(() => {
    window.history.replaceState(null, '', '/calendar?view=month#draft');
  });

  it('clears the token and navigates to /login with the current path as redirect by default', async () => {
    setToken('expired');
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'nope' }, 401));

    await expect(api.get('/calendars')).rejects.toMatchObject({ status: 401 });
    expect(hasToken()).toBe(false);
    expect(window.location.pathname).toBe('/login');
    expect(window.location.search).toBe(
      `?redirect=${encodeURIComponent('/calendar?view=month#draft')}`,
    );
  });

  it('does not redirect when skipAuthRedirect is set', async () => {
    setToken('expired');
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'nope' }, 401));

    await expect(api.get('/calendars', true)).rejects.toMatchObject({ status: 401 });
    expect(window.location.pathname).toBe('/calendar');
  });

  it('does not redirect or clear the token for /auth/* failures', async () => {
    setToken('still-current');
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'Invalid credentials' }, 401));

    await expect(
      api.post('/auth/login', { email: 'a@b.c', password: 'bad' }),
    ).rejects.toMatchObject({ status: 401, detail: 'Invalid credentials' });

    expect(hasToken()).toBe(true);
    expect(window.location.pathname).toBe('/calendar');
  });
});
