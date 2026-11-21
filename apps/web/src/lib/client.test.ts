import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ja } from '@/i18n/ja';
import {
  ApiError,
  createApiClient,
  endSession,
  errorMessage,
  filenameFromDisposition,
  isAbortError,
  isStaleError,
  present,
  withSignal,
} from '@/lib/client';
import { hasToken, SESSION_EXPIRED_EVENT, setToken } from '@/lib/session';

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
  '/auth/verify-email/confirm': {
    post: { responses: { 204: { content: never } } };
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

/** The URL of the i-th fetch, whether it was issued as a string or a Request. */
function urlAt(i: number): string {
  const first = fetchMock.mock.calls[i]?.[0] as Request | string;
  return typeof first === 'string' ? first : first.url;
}

const authorization = (call: unknown[]) =>
  ((call[1] as RequestInit).headers as Record<string, string>).Authorization;

describe('401 handling', () => {
  beforeEach(() => {
    window.history.replaceState(null, '', '/calendar?view=month#draft');
  });

  it('clears the token and navigates to /login with the current path as redirect', async () => {
    setToken('expired');
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'nope' }, 401));

    await expect(api().GET('/events')).rejects.toMatchObject({ status: 401 });
    expect(hasToken()).toBe(false);
    expect(window.location.pathname).toBe('/login');
    expect(window.location.search).toBe(
      `?redirect=${encodeURIComponent('/calendar?view=month#draft')}`,
    );
  });

  it('keeps the return destination if an in-flight protected request expires on the login page', async () => {
    const redirect = `?redirect=${encodeURIComponent('/schedule-polls/calendar/poll?view=respond')}`;
    window.history.replaceState(null, '', `/login${redirect}`);
    setToken('expired');
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'nope' }, 401));

    await expect(api().GET('/events')).rejects.toMatchObject({ status: 401 });
    expect(window.location.pathname).toBe('/login');
    expect(window.location.search).toBe(redirect);
  });
});

describe('email confirmation', () => {
  // A confirmation without the account's own session is read as a takeover
  // and costs the account its password; an owner whose access token merely
  // expired has to renew first, not send it bare.
  it('renews an expired session first and confirms with the new token', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    const renewed = live();
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ token: renewed, refreshToken: 'refresh-2' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));

    await api().POST('/auth/verify-email/confirm');

    expect(urlAt(0)).toContain('/auth/refresh');
    expect(urlAt(1)).toContain('/auth/verify-email/confirm');
    expect(callAt(1).headers.get('Authorization')).toBe(`Bearer ${renewed}`);
  });

  it('confirms with no token when signed out', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));

    await api().POST('/auth/verify-email/confirm');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(callAt(0).headers.get('Authorization')).toBeNull();
  });

  it('leaves sign-in requests alone', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true }));

    await api().POST('/auth/login');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(urlAt(0)).toContain('/auth/login');
  });
});

describe('renewal failures', () => {
  let expiredEvents = 0;
  const onExpired = () => {
    expiredEvents += 1;
  };

  beforeEach(() => {
    expiredEvents = 0;
    window.history.replaceState(null, '', '/calendar');
    window.addEventListener(SESSION_EXPIRED_EVENT, onExpired);
  });

  afterEach(() => {
    window.removeEventListener(SESSION_EXPIRED_EVENT, onExpired);
  });

  it('keeps the session when the renewal request cannot reach the server', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock.mockRejectedValueOnce(new TypeError('network down'));

    await expect(api().GET('/events')).rejects.toBeInstanceOf(TypeError);

    expect(localStorage.getItem('tt_refresh')).toBe('refresh-1');
    expect(expiredEvents).toBe(0);
    expect(window.location.pathname).toBe('/calendar');
  });

  it('keeps the session when the renewal answers 503', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock.mockResolvedValueOnce(jsonResponse({ message: 'down' }, 503));

    const err = await api()
      .GET('/events')
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(503);
    expect(localStorage.getItem('tt_refresh')).toBe('refresh-1');
    expect(expiredEvents).toBe(0);
  });

  it('keeps the session when a renewal after a 401 hits a 5xx', async () => {
    localStorage.setItem('tt_token', live());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ message: 'nope' }, 401))
      .mockResolvedValueOnce(jsonResponse({ message: 'down' }, 502));

    await expect(api().GET('/events')).rejects.toMatchObject({ status: 502 });

    expect(localStorage.getItem('tt_refresh')).toBe('refresh-1');
    expect(expiredEvents).toBe(0);
  });

  it('keeps the session when the renewal body is malformed', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock.mockResolvedValueOnce(new Response('not json', { status: 200 }));

    await expect(api().GET('/events')).rejects.toBeInstanceOf(ApiError);

    expect(localStorage.getItem('tt_refresh')).toBe('refresh-1');
    expect(expiredEvents).toBe(0);
  });

  it('still signs out when the server refuses the refresh token', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock.mockResolvedValue(jsonResponse({ message: 'nope' }, 401));

    await expect(api().GET('/events')).rejects.toMatchObject({ status: 401 });

    expect(hasToken()).toBe(false);
    expect(expiredEvents).toBe(1);
    expect(window.location.pathname).toBe('/login');
  });

  // Two tabs wake with the same retired pair; the second one's refresh is
  // refused as a replay of the rotation the first already completed.
  it('retries with the pair another tab stored while a duplicate refresh was refused', async () => {
    localStorage.setItem('tt_token', live());
    localStorage.setItem('tt_refresh', 'refresh-1');
    const other = makeJwt({ exp: Math.floor(Date.now() / 1000) + 7200 });
    fetchMock.mockImplementation((input: Request | string) => {
      if ((typeof input === 'string' ? input : input.url).includes('/auth/refresh')) {
        localStorage.setItem('tt_token', other);
        localStorage.setItem('tt_refresh', 'refresh-2');
        return Promise.resolve(jsonResponse({ message: 'replayed' }, 401));
      }
      const auth = typeof input === 'string' ? null : input.headers.get('Authorization');
      return Promise.resolve(
        auth === `Bearer ${other}` ? jsonResponse({ ok: true }) : jsonResponse({}, 401),
      );
    });

    const { data } = await api().GET('/events');

    expect(data).toEqual({ ok: true });
    expect(localStorage.getItem('tt_token')).toBe(other);
    expect(localStorage.getItem('tt_refresh')).toBe('refresh-2');
    expect(expiredEvents).toBe(0);
  });

  it('does not clear a pair stored after the failing request started', async () => {
    localStorage.setItem('tt_token', 'old-access');
    const stored = live();
    fetchMock
      .mockImplementationOnce(() => {
        localStorage.setItem('tt_token', stored);
        localStorage.setItem('tt_refresh', 'refresh-2');
        return Promise.resolve(jsonResponse({}, 401));
      })
      .mockResolvedValueOnce(jsonResponse({ ok: true }));

    const { data } = await api().GET('/events');

    expect(data).toEqual({ ok: true });
    // The newer pair was used as is: no renewal spent its refresh token.
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(localStorage.getItem('tt_token')).toBe(stored);
    expect(localStorage.getItem('tt_refresh')).toBe('refresh-2');
    expect(expiredEvents).toBe(0);
  });
});

describe('endSession', () => {
  it('revokes the server session with the access token it holds', async () => {
    const access = live();
    localStorage.setItem('tt_token', access);
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));

    endSession();

    expect(hasToken()).toBe(false);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(urlAt(0)).toContain('/auth/logout');
    expect(authorization(fetchMock.mock.calls[0] as unknown[])).toBe(`Bearer ${access}`);
  });

  // An expired access token cannot authenticate the logout; sending it bare
  // left the session, and the refresh token a copied storage still holds,
  // alive for the rest of its thirty days.
  it('trades the refresh token for one it can sign out with when the access token expired', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    const renewed = live();
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ token: renewed, refreshToken: 'refresh-2' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));

    endSession();

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(urlAt(0)).toContain('/auth/refresh');
    expect(urlAt(1)).toContain('/auth/logout');
    expect(authorization(fetchMock.mock.calls[1] as unknown[])).toBe(`Bearer ${renewed}`);
    // The pair exists only to sign out with; keeping it would sign back in.
    expect(localStorage.getItem('tt_token')).toBeNull();
    expect(localStorage.getItem('tt_refresh')).toBeNull();
  });

  it('signs out with the pair a renewal already in flight brings back', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    const renewed = live();
    let answerRefresh: (r: Response) => void = () => {};
    fetchMock.mockImplementation((input: Request | string) => {
      if ((typeof input === 'string' ? input : input.url).includes('/auth/refresh')) {
        return new Promise<Response>((resolve) => {
          answerRefresh = resolve;
        });
      }
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    const request = api()
      .GET('/events')
      .catch(() => {});
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    endSession();
    answerRefresh(jsonResponse({ token: renewed, refreshToken: 'refresh-2' }));
    await request;

    await vi.waitFor(() =>
      expect(fetchMock.mock.calls.some((_, i) => urlAt(i).includes('/auth/logout'))).toBe(true),
    );
    const logoutAt = fetchMock.mock.calls.findIndex((_, i) => urlAt(i).includes('/auth/logout'));
    expect(authorization(fetchMock.mock.calls[logoutAt] as unknown[])).toBe(`Bearer ${renewed}`);
    // The spent refresh token is not presented a second time.
    expect(fetchMock.mock.calls.filter((_, i) => urlAt(i).includes('/auth/refresh'))).toHaveLength(
      1,
    );
    expect(hasToken()).toBe(false);
  });

  it('does not call the server when there is nothing to end', async () => {
    endSession();
    await Promise.resolve();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('error messages', () => {
  const failure = async (body: unknown, status: number) => {
    fetchMock.mockResolvedValue(jsonResponse(body, status));
    return (await api()
      .GET('/events')
      .catch((e: unknown) => e)) as ApiError;
  };

  it('localizes by the error code', async () => {
    const err = await failure(
      { code: 'CALENDAR.ROLE_REQUIRED', message: 'Insufficient role' },
      403,
    );
    expect(errorMessage(err)).toBe(ja['apiError.CALENDAR.ROLE_REQUIRED']);
  });

  it('localizes a schema rejection as an invalid request', async () => {
    const err = await failure({ detail: 'validation failed', errors: [{ message: 'm' }] }, 422);
    expect(errorMessage(err)).toBe(ja['apiError.REQUEST.INVALID']);
  });

  it('falls back to the status for a gateway page with no code', async () => {
    fetchMock.mockResolvedValue(new Response('<html>bad gateway</html>', { status: 502 }));
    const err = (await api()
      .GET('/events')
      .catch((e: unknown) => e)) as ApiError;
    expect(err.detail).not.toBe('');
    expect(errorMessage(err)).toBe(ja['error.serverUnavailable']);
  });

  it('falls back to the server sentence for a code it has no message for', async () => {
    const err = await failure({ code: 'SOMETHING.NEW', message: 'A newer server said this' }, 418);
    expect(errorMessage(err)).toBe('A newer server said this');
  });

  it('uses the fallback for anything that is not an ApiError', () => {
    expect(errorMessage(new Error('x'), 'custom')).toBe('custom');
    expect(errorMessage('x')).toBe(ja['error.generic']);
  });
});

describe('small helpers', () => {
  it('tells a cancellation from a failure', () => {
    expect(isAbortError(new DOMException('aborted', 'AbortError'))).toBe(true);
    expect(isAbortError(new ApiError(500, 'boom'))).toBe(false);
    expect(isAbortError(new Error('network down'))).toBe(false);
  });

  it('recognizes only a 412 as a stale write', () => {
    expect(isStaleError(new ApiError(412, 'stale'))).toBe(true);
    expect(isStaleError(new ApiError(409, 'conflict'))).toBe(false);
    expect(isStaleError(new Error('stale'))).toBe(false);
  });

  it('rejects an empty body and passes a present one through', () => {
    expect(present({ a: 1 })).toEqual({ a: 1 });
    expect(() => present(undefined)).toThrow(ApiError);
  });

  it('leaves the signal out when there is none', () => {
    const signal = new AbortController().signal;
    expect(withSignal(signal)).toEqual({ signal });
    expect(withSignal()).toEqual({});
  });
});

/**
 * A download is named by the server: an export is named after the calendar it
 * came from, and the name is the only thing telling two of them apart in a
 * downloads folder.
 */
describe('download filenames', () => {
  it('reads the name the export handler quotes', () => {
    expect(filenameFromDisposition('attachment; filename="Family.ics"')).toBe('Family.ics');
  });

  it('prefers the encoded name, which is the one that survives a name in Japanese', () => {
    // What the storage client sends: both spellings, the ASCII one lossy.
    const header = `attachment; filename="_____.ics"; filename*=UTF-8''%E5%AE%B6%E6%97%8F.ics`;
    expect(filenameFromDisposition(header)).toBe('家族.ics');
  });

  it('keeps the name and drops any path in front of it', () => {
    expect(filenameFromDisposition('attachment; filename="../../etc/passwd"')).toBe('passwd');
  });

  it('reads an unquoted name', () => {
    expect(filenameFromDisposition('attachment; filename=Work.csv')).toBe('Work.csv');
  });

  it('has nothing to say about a header that names no file', () => {
    expect(filenameFromDisposition(null)).toBe('');
    expect(filenameFromDisposition('attachment')).toBe('');
  });

  it('falls back to the quoted name when the encoding is malformed', () => {
    const header = `attachment; filename="Family.ics"; filename*=UTF-8''%E5%AE`;
    expect(filenameFromDisposition(header)).toBe('Family.ics');
  });
});
