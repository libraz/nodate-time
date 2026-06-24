import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api, clearToken, hasToken, setToken } from './api';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
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
    vi.stubGlobal('location', { href: '' } as Location);
  });

  it('clears the token and redirects to /login by default', async () => {
    setToken('expired');
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'nope' }, 401));

    await expect(api.get('/calendars')).rejects.toMatchObject({ status: 401 });
    expect(hasToken()).toBe(false);
    expect(window.location.href).toBe('/login');
  });

  it('does not redirect when skipAuthRedirect is set', async () => {
    setToken('expired');
    fetchMock.mockResolvedValue(jsonResponse({ detail: 'nope' }, 401));

    await expect(api.get('/calendars', true)).rejects.toMatchObject({ status: 401 });
    expect(window.location.href).toBe('');
  });
});
