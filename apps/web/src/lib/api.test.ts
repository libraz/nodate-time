import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ja } from '@/i18n/ja';
import {
  ApiError,
  api,
  clearToken,
  decodeJwtExp,
  errorMessage,
  filenameFromDisposition,
  hasToken,
  isAbortError,
  isTokenExpired,
  setToken,
} from './api';

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

describe('session renewal', () => {
  const live = () => makeJwt({ exp: Math.floor(Date.now() / 1000) + 3600 });
  const expired = () => makeJwt({ exp: Math.floor(Date.now() / 1000) - 3600 });

  it('keeps the sign-in alive past the access token', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    const renewed = live();
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ token: renewed, refreshToken: 'refresh-2' }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }));

    await expect(api.get('/calendars')).resolves.toEqual({ ok: true });

    expect(fetchMock.mock.calls[0]?.[0]).toContain('/auth/refresh');
    expect(localStorage.getItem('tt_token')).toBe(renewed);
    // The refresh token rotates; keeping the old one would present a retired
    // value on the next renewal.
    expect(localStorage.getItem('tt_refresh')).toBe('refresh-2');
  });

  it('retries the request once after a 401 rather than signing the user out', async () => {
    localStorage.setItem('tt_token', live());
    localStorage.setItem('tt_refresh', 'refresh-1');
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ message: 'nope' }, 401))
      .mockResolvedValueOnce(jsonResponse({ token: live(), refreshToken: 'refresh-2' }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }));

    await expect(api.get('/calendars')).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  // Renewal rotates the refresh token, so two concurrent renewals would race
  // and the loser would present one that had just been retired.
  it('renews once for concurrent requests', async () => {
    localStorage.setItem('tt_token', expired());
    localStorage.setItem('tt_refresh', 'refresh-1');
    let refreshCalls = 0;
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/auth/refresh')) {
        refreshCalls += 1;
        return Promise.resolve(jsonResponse({ token: live(), refreshToken: 'refresh-2' }));
      }
      return Promise.resolve(jsonResponse({ ok: true }));
    });

    await Promise.all([api.get('/calendars'), api.get('/user'), api.get('/user/sessions')]);

    expect(refreshCalls).toBe(1);
  });

  it('gives up when there is no refresh token to renew with', async () => {
    localStorage.setItem('tt_token', live());
    fetchMock.mockResolvedValueOnce(jsonResponse({ message: 'nope' }, 401));

    await expect(api.get('/calendars', true)).rejects.toBeInstanceOf(ApiError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
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

    await api.post('/calendars/1/albums/2/confirm');

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

describe('request cancellation', () => {
  it('hands the caller its signal on every verb', async () => {
    // A fresh response per call: a body can only be read once.
    fetchMock.mockImplementation(() => Promise.resolve(jsonResponse({ ok: true })));
    const controller = new AbortController();

    await api.get('/calendars', false, controller.signal);
    await api.getWithRevision('/calendars/1', controller.signal);
    await api.post('/calendars', { name: 'Team' }, controller.signal);
    await api.put('/calendars/1', { name: 'Team' }, undefined, controller.signal);
    await api.delete('/calendars/1', controller.signal);
    await api.getBlob('/calendars/1/albums/2', controller.signal);

    expect(fetchMock).toHaveBeenCalledTimes(6);
    for (const [, init] of fetchMock.mock.calls as [string, RequestInit][]) {
      expect(init.signal).toBe(controller.signal);
    }
  });

  it('leaves the signal unset for callers that pass none', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));

    await api.get('/calendars');

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.signal).toBeUndefined();
  });

  it('reports an aborted request as a cancellation, not a failure', async () => {
    // A superseded request rejects like any other. A caller that cannot tell
    // the two apart shows an error for something the user did not do.
    fetchMock.mockImplementation(
      (_url: string, init: RequestInit) =>
        new Promise((_resolve, reject) => {
          init.signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          );
        }),
    );
    const controller = new AbortController();
    const pending = api.get('/calendars', false, controller.signal);
    controller.abort();

    const err = await pending.catch((e: unknown) => e);
    expect(isAbortError(err)).toBe(true);
    expect(err).not.toBeInstanceOf(ApiError);
  });

  it('does not mistake a server failure for a cancellation', () => {
    expect(isAbortError(new ApiError(500, 'boom'))).toBe(false);
    expect(isAbortError(new Error('network down'))).toBe(false);
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

describe('error envelopes', () => {
  it('reads the application envelope', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        { status: 403, code: 'CALENDAR.ROLE_REQUIRED', message: 'Insufficient role' },
        403,
      ),
    );

    const err = await api.get('/calendars/x/events').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).code).toBe('CALENDAR.ROLE_REQUIRED');
    expect(errorMessage(err)).toBe(ja['apiError.CALENDAR.ROLE_REQUIRED']);
  });

  it('reads the validator envelope, which carries no code of its own', async () => {
    // A schema rejection used to arrive with nothing to branch on and its
    // field complaints unread, so the interface could say only that something
    // was wrong.
    fetchMock.mockResolvedValue(
      jsonResponse(
        {
          title: 'Unprocessable Entity',
          status: 422,
          detail: 'validation failed',
          errors: [{ message: 'expected required property', location: 'body.title' }],
        },
        422,
      ),
    );

    const err = (await api.post('/calendars/x/events', {}).catch((e: unknown) => e)) as ApiError;
    expect(err.code).toBe('REQUEST.INVALID');
    expect(err.issues).toHaveLength(1);
    expect(err.issues[0]?.location).toBe('body.title');
    expect(errorMessage(err)).toBe(ja['apiError.REQUEST.INVALID']);
  });

  it('says something when the response carries no reason phrase and no body', async () => {
    // Over HTTP/2 there is no reason phrase at all. Starting from it left a
    // gateway error rendering as a toast with nothing written in it.
    fetchMock.mockResolvedValue(new Response('<html>bad gateway</html>', { status: 502 }));

    const err = (await api.get('/calendars').catch((e: unknown) => e)) as ApiError;
    expect(err.detail).not.toBe('');
    expect(errorMessage(err)).toBe(ja['error.serverUnavailable']);
  });

  it('falls back to the server sentence for a code it has no message for', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        { status: 418, code: 'SOMETHING.NEW', message: 'A newer server said this' },
        418,
      ),
    );

    const err = (await api.get('/calendars').catch((e: unknown) => e)) as ApiError;
    expect(errorMessage(err)).toBe('A newer server said this');
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

  it('carries the name off the response, alongside the body', async () => {
    fetchMock.mockResolvedValue(
      new Response('BEGIN:VCALENDAR', {
        status: 200,
        headers: { 'Content-Disposition': 'attachment; filename="Family.ics"' },
      }),
    );

    const { blob, filename } = await api.getBlob('/calendars/cal-1/export?format=ics');

    expect(filename).toBe('Family.ics');
    expect(await blob.text()).toBe('BEGIN:VCALENDAR');
  });
});
