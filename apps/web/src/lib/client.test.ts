import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, SESSION_EXPIRED_EVENT } from '@/lib/api';
import { createApiClient } from './client';

interface TestPaths {
  '/events': {
    get: { responses: { 200: { content: { 'application/json': { ok: boolean } } } } };
    post: {
      requestBody: { content: { 'application/json': { title: string } } };
      responses: { 200: { content: { 'application/json': { ok: boolean } } } };
    };
  };
  '/auth/login': {
    post: { responses: { 200: { content: { 'application/json': { ok: boolean } } } } };
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeJwt(claims: Record<string, unknown>): string {
  const encode = (obj: unknown) =>
    btoa(JSON.stringify(obj)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return `${encode({ alg: 'none', typ: 'JWT' })}.${encode(claims)}.sig`;
}

const live = () => makeJwt({ exp: Math.floor(Date.now() / 1000) + 3600 });
const expired = () => makeJwt({ exp: Math.floor(Date.now() / 1000) - 3600 });

const fetchMock = vi.fn();
const api = () => createApiClient<TestPaths>('http://api.test');

function callAt(i: number): Request {
  return fetchMock.mock.calls[i]?.[0] as Request;
}

beforeEach(() => {
  localStorage.clear();
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.pushState(null, '', '/');
});

describe('session handling', () => {
  it('sends the stored access token as a bearer', async () => {
    const token = live();
    localStorage.setItem('tt_token', token);
    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true }));
    const { data } = await api().GET('/events');
    expect(data).toEqual({ ok: true });
    expect(callAt(0).headers.get('Authorization')).toBe(`Bearer ${token}`);
  });

  it('renews before sending when only an expired access token remains', async () => {
    const renewed = live();
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ token: renewed, refreshToken: 'refresh-2' }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }));
    await api().GET('/events');
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/auth/refresh');
    expect(callAt(1).headers.get('Authorization')).toBe(`Bearer ${renewed}`);
    expect(localStorage.getItem('tt_refresh')).toBe('refresh-2');
  });

  it('renews after a 401 and retries the request once with its body intact', async () => {
    const stale = live();
    const renewed = `${live()}x`;
    localStorage.setItem('tt_token', stale);
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ message: 'nope' }, 401))
      .mockResolvedValueOnce(jsonResponse({ token: renewed, refreshToken: 'refresh-2' }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }));
    const { data } = await api().POST('/events', { body: { title: 'a' } });
    expect(data).toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(String(fetchMock.mock.calls[1]?.[0])).toContain('/auth/refresh');
    const retry = callAt(2);
    expect(retry.headers.get('Authorization')).toBe(`Bearer ${renewed}`);
    expect(await retry.json()).toEqual({ title: 'a' });
  });

  it('shares one renewal between concurrent 401s', async () => {
    localStorage.setItem('tt_token', live());
    localStorage.setItem('tt_refresh', 'refresh-1');
    const renewed = `${live()}y`;
    fetchMock.mockImplementation(async (input: Request | string) => {
      const url = typeof input === 'string' ? input : input.url;
      if (url.includes('/auth/refresh')) {
        return jsonResponse({ token: renewed, refreshToken: 'refresh-2' });
      }
      const auth = typeof input === 'string' ? null : input.headers.get('Authorization');
      return auth === `Bearer ${renewed}` ? jsonResponse({ ok: true }) : jsonResponse({}, 401);
    });
    const c = api();
    await Promise.all([c.GET('/events'), c.GET('/events')]);
    const refreshes = fetchMock.mock.calls.filter(([r]) =>
      String(typeof r === 'string' ? r : r.url).includes('/auth/refresh'),
    );
    expect(refreshes).toHaveLength(1);
  });

  it('ends the session and sends the user to sign-in when renewal is refused', async () => {
    localStorage.setItem('tt_token', live());
    localStorage.setItem('tt_refresh', 'refresh-1');
    window.history.pushState(null, '', '/calendar/week');
    const expiredEvent = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, expiredEvent);
    fetchMock
      .mockResolvedValueOnce(jsonResponse({}, 401))
      .mockResolvedValueOnce(jsonResponse({}, 401));
    await expect(api().GET('/events')).rejects.toMatchObject({
      status: 401,
      code: 'AUTH.TOKEN_INVALID',
    });
    window.removeEventListener(SESSION_EXPIRED_EVENT, expiredEvent);
    expect(expiredEvent).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem('tt_token')).toBeNull();
    expect(localStorage.getItem('tt_refresh')).toBeNull();
    expect(window.location.pathname).toBe('/login');
    expect(window.location.search).toBe('?redirect=%2Fcalendar%2Fweek');
  });

  it('does not renew or redirect for the auth endpoints', async () => {
    localStorage.setItem('tt_token', live());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ status: 401, code: 'AUTH.INVALID_CREDENTIALS', message: 'bad' }, 401),
    );
    const err = await api()
      .POST('/auth/login')
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).code).toBe('AUTH.INVALID_CREDENTIALS');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem('tt_token')).not.toBeNull();
  });
});

describe('error shaping', () => {
  it('reads the application envelope', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ status: 403, code: 'CALENDAR.ROLE_REQUIRED', message: 'no' }, 403),
    );
    await expect(api().GET('/events')).rejects.toMatchObject({
      status: 403,
      code: 'CALENDAR.ROLE_REQUIRED',
      detail: 'no',
    });
  });

  it('reads the validator envelope and falls back to the status line for non-JSON bodies', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ detail: 'bad', errors: [{ message: 'm', location: 'body.title' }] }, 422),
    );
    await expect(api().GET('/events')).rejects.toMatchObject({
      status: 422,
      code: 'REQUEST.INVALID',
      issues: [{ message: 'm', location: 'body.title' }],
    });
    fetchMock.mockResolvedValueOnce(new Response('<html>', { status: 502 }));
    await expect(api().GET('/events')).rejects.toMatchObject({ status: 502, detail: 'HTTP 502' });
  });
});
