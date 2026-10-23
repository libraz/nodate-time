import { getT } from '@/i18n';
import { errorMessageKey } from '@/lib/error-codes';
import { isSafeRedirect } from '@/lib/safe-redirect';
import { toast } from '@/lib/toast';

const API_BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';
export const SESSION_EXPIRED_EVENT = 'nodate:session-expired';

/**
 * Decodes the `exp` claim (seconds since epoch) from a JWT without verifying
 * its signature. Returns null when the token is malformed or has no `exp`.
 */
export function decodeJwtExp(token: string): number | null {
  try {
    const payload = token.split('.')[1];
    if (!payload) return null;
    let base64 = payload.replace(/-/g, '+').replace(/_/g, '/');
    base64 += '='.repeat((4 - (base64.length % 4)) % 4);
    const claims = JSON.parse(atob(base64));
    return typeof claims.exp === 'number' ? claims.exp : null;
  } catch {
    return null;
  }
}

/**
 * Reports whether a JWT is already expired. A token with no decodable `exp` is
 * treated as not expired so the server, not the client, makes the final call.
 */
export function isTokenExpired(token: string): boolean {
  const exp = decodeJwtExp(token);
  if (exp === null) return false;
  return exp * 1000 <= Date.now();
}

function getToken(): string | null {
  const token = localStorage.getItem('tt_token');
  if (token !== null && isTokenExpired(token)) {
    localStorage.removeItem('tt_token');
    return null;
  }
  return token;
}

/**
 * The refresh token is what makes a sign-in outlast its access token. Keeping
 * only the access token means every session ends when that token does, however
 * long the server was willing to keep it alive.
 */
export function getRefreshToken(): string | null {
  return localStorage.getItem('tt_refresh');
}

export function setToken(token: string, refreshToken?: string): void {
  localStorage.setItem('tt_token', token);
  if (refreshToken) localStorage.setItem('tt_refresh', refreshToken);
}

export function clearToken(): void {
  localStorage.removeItem('tt_token');
  localStorage.removeItem('tt_refresh');
}

/**
 * Reports whether a storage event is a change to the stored session, made by
 * another tab (the event never fires in the tab that wrote). A null key is
 * the whole storage being cleared.
 */
export function isSessionStorageEvent(e: StorageEvent): boolean {
  return e.key === null || e.key === 'tt_token' || e.key === 'tt_refresh';
}

/**
 * Reports whether this browser still has a way in. An expired access token
 * with a refresh token beside it counts: the session is alive, the request
 * just has to renew first.
 */
export function hasToken(): boolean {
  return getToken() !== null || getRefreshToken() !== null;
}

/** An access token and the refresh token issued beside it. */
interface TokenPair {
  token: string;
  refreshToken?: string;
}

/**
 * The renewal currently in flight, if any.
 *
 * One at a time, deliberately. Renewal rotates the refresh token -- the old
 * one stops working the moment the new one is issued -- so two requests that
 * both noticed a 401 and both tried would race, and the loser would present a
 * token that had just been retired and be signed out for it.
 */
let refreshInFlight: Promise<TokenPair | null> | null = null;

/**
 * Trades a refresh token for a new pair without storing it.
 *
 * Resolves null only when the server definitively refuses the token (401/403).
 * A network error, a 5xx or a malformed body throws instead: the session may
 * be perfectly alive, and treating the failure as a refusal would sign the
 * user out of it.
 */
async function exchangeRefreshToken(refreshToken: string): Promise<TokenPair | null> {
  const res = await fetch(`${API_BASE}/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refreshToken }),
  });
  if (res.status === 401 || res.status === 403) return null;
  if (!res.ok) throw await buildError(res);
  const body = (await res.json().catch(() => null)) as {
    token?: string;
    refreshToken?: string;
  } | null;
  if (!body?.token) throw new ApiError(502, 'Malformed refresh response');
  return body.refreshToken
    ? { token: body.token, refreshToken: body.refreshToken }
    : { token: body.token };
}

/** Runs a renewal exclusively across the tabs of this browser where locks exist. */
function withRenewalLock<T>(fn: () => Promise<T>): Promise<T> {
  const locks = typeof navigator === 'undefined' ? undefined : navigator.locks;
  return locks ? locks.request('nodate:session-renewal', fn) : fn();
}

async function refreshSession(): Promise<TokenPair | null> {
  const started = getRefreshToken();
  if (!started) return null;
  return withRenewalLock(async () => {
    const current = getRefreshToken();
    if (!current) return null;
    // Another tab rotated the pair while this one waited its turn; spending the
    // retired token again would be refused as a replay.
    if (current !== started) {
      const token = getToken();
      if (token) return { token, refreshToken: current };
    }
    const pair = await exchangeRefreshToken(current);
    // A sign-out that landed meanwhile has already forgotten this session;
    // storing the new pair would sign the browser back in.
    if (pair && getRefreshToken() === current) setToken(pair.token, pair.refreshToken);
    return pair;
  });
}

function renewSession(): Promise<TokenPair | null> {
  if (!refreshInFlight) {
    refreshInFlight = refreshSession().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

/**
 * The token to retry a request with after the server refused `failed`, or null
 * when the session cannot be renewed. A pair another tab stored in the meantime
 * is taken as is; a refusal of the refresh token only counts when no such pair
 * appeared while it was in flight.
 */
async function renewedToken(failed: string | null): Promise<string | null> {
  const stored = getToken();
  if (stored && stored !== failed) return stored;
  const pair = await renewSession();
  if (pair) return pair.token;
  const rotated = getToken();
  return rotated && rotated !== failed ? rotated : null;
}

/**
 * Signs this browser out: forgets the tokens at once and revokes the session
 * on the server in the background.
 *
 * Forgetting is not enough on its own -- anyone who copied the storage of a
 * shared machine would keep a refresh token good for the rest of the session.
 * An access token that has already expired cannot authenticate the logout,
 * so the refresh token is traded for one first, and the pair it yields is
 * used for nothing else.
 */
export function endSession(): void {
  const stored = localStorage.getItem('tt_token');
  const access = stored !== null && !isTokenExpired(stored) ? stored : null;
  const refresh = getRefreshToken();
  const pending = refreshInFlight;
  clearToken();
  if (access || refresh || pending) void revokeSession(access, refresh, pending);
}

async function revokeSession(
  access: string | null,
  refresh: string | null,
  pending: Promise<TokenPair | null> | null,
): Promise<void> {
  const logout = (token: string) => send('/auth/logout', { method: 'POST' }, token);
  try {
    // A renewal in flight has already spent the refresh token; the pair it
    // brings back is the one the session now answers to.
    const renewed = pending ? await pending : null;
    const token = renewed?.token ?? access;
    if (token && (await logout(token)).status !== 401) return;
    if (!refresh || pending) return;
    const pair = await exchangeRefreshToken(refresh);
    if (pair) await logout(pair.token);
  } catch {
    // Signing out locally does not wait on the network.
  }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public detail: string,
    /** Machine-readable error code from the API envelope (e.g. `CALENDAR.ROLE_REQUIRED`). */
    public code = '',
    /** Field-level complaints, when the failure was a schema rejection. */
    public issues: { message?: string; location?: string }[] = [],
  ) {
    super(detail);
  }
}

/**
 * The message to show for an API failure, in the reader's language.
 *
 * Driven by the error code, which every failure the application raises
 * carries. What is left over -- a proxy's own error page, a gateway timeout --
 * has no code, and falls back to the status rather than to a sentence written
 * in a language the reader may not have.
 */
function localizeError(err: ApiError): string {
  const t = getT();
  const key = errorMessageKey(err.code);
  if (key) return t(key);
  if (err.status === 403) return t('error.noPermission');
  if (err.status >= 500) return t('error.serverUnavailable');
  return err.detail;
}

/** Returns a localized, user-facing message for any thrown error. */
export function errorMessage(e: unknown, fallback?: string): string {
  if (e instanceof ApiError) return localizeError(e);
  return fallback ?? getT()('error.generic');
}

/** A field-level complaint from the schema validator. */
interface FieldIssue {
  message?: string;
  location?: string;
}

/**
 * Turns a failed response into one error shape.
 *
 * The API answers in two envelopes: the application's own `{status, code,
 * message}`, and the schema validator's `{title, status, detail, errors[]}`.
 * Reading only the first leaves a rejected request with no code to branch on
 * and nothing to say about which field was wrong.
 *
 * The status text is a last resort rather than the starting point: over HTTP/2
 * there is no reason phrase at all, so a gateway error with a non-JSON body
 * used to produce a toast with nothing written in it.
 */
async function buildError(res: Response): Promise<ApiError> {
  let detail = '';
  let code = '';
  let issues: FieldIssue[] = [];
  try {
    const body = await res.json();
    detail = body.message ?? body.detail ?? '';
    code = typeof body.code === 'string' ? body.code : '';
    if (Array.isArray(body.errors)) issues = body.errors as FieldIssue[];
  } catch {
    // A body that is not JSON says nothing beyond the status line.
  }
  if (!code && issues.length > 0) {
    // The validator has no code of its own, but the failure is always the
    // same kind: the request did not match the schema.
    code = 'REQUEST.INVALID';
  }

  if (!detail) detail = res.statusText || `HTTP ${res.status}`;
  return new ApiError(res.status, detail, code, issues);
}

function redirectTarget(): string | null {
  const { pathname, search, hash } = window.location;
  if (pathname === '/login') return null;
  const target = `${pathname}${search}${hash}`;
  return isSafeRedirect(target) ? target : null;
}

function navigateToLogin(): void {
  const redirect = redirectTarget();
  const next = redirect ? `/login?redirect=${encodeURIComponent(redirect)}` : '/login';
  if (`${window.location.pathname}${window.location.search}` === next) return;
  window.history.pushState(null, '', next);
  window.dispatchEvent(
    typeof PopStateEvent === 'function' ? new PopStateEvent('popstate') : new Event('popstate'),
  );
}

/**
 * Ends the session after the server refused `failed`, unless another tab has
 * stored a newer pair since: that one is alive and must not be cleared.
 */
function expireSession(failed: string | null): never {
  if (getToken() === failed) {
    clearToken();
    window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
    toast.error(getT()('error.sessionExpired'));
    navigateToLogin();
  }
  throw new ApiError(401, 'Unauthorized', 'AUTH.TOKEN_INVALID');
}

async function send(
  path: string,
  options: RequestInit,
  token: string | null = getToken(),
): Promise<Response> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((options.headers as Record<string, string>) ?? {}),
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return fetch(`${API_BASE}${path}`, { ...options, headers });
}

/**
 * Whether a request speaks for the signed-in session, and so renews it first.
 *
 * The rest of /auth/* is how a session is obtained and has none to renew.
 * Confirming an email is the exception: the server treats a confirmation
 * that arrives without the account's own session as someone else's, and
 * revokes the account's password and sessions -- so an owner whose access
 * token merely expired must not send it bare.
 */
function carriesSession(path: string): boolean {
  return !path.startsWith('/auth/') || path === '/auth/verify-email/confirm';
}

/**
 * Runs a request through the session handling (pre-emptive renewal, one retry
 * after a 401, the shared error envelope) and hands back the raw response, for
 * the callers that need something out of its headers.
 */
async function requestRaw(
  path: string,
  options: RequestInit,
  skipAuthRedirect = false,
): Promise<Response> {
  // An expired access token is not an ended session while a refresh token
  // remains. Renewing before the request saves a guaranteed 401.
  if (!getToken() && getRefreshToken() && carriesSession(path)) {
    await renewSession();
  }

  let sent = getToken();
  let res = await send(path, options, sent);

  if (res.status === 401 && carriesSession(path)) {
    const next = await renewedToken(sent);
    if (next) {
      sent = next;
      res = await send(path, options, sent);
    }
  }

  if (!res.ok) {
    const shouldRedirectOnAuth = !skipAuthRedirect && !path.startsWith('/auth/');
    if (res.status === 401 && shouldRedirectOnAuth) {
      expireSession(sent);
    }
    throw await buildError(res);
  }
  return res;
}

async function request<T>(
  path: string,
  options: RequestInit = {},
  skipAuthRedirect = false,
): Promise<T> {
  const res = await requestRaw(path, options, skipAuthRedirect);
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

/**
 * Reports whether a rejection is a caller's own cancellation rather than a
 * failure. A superseded request rejects like any other, and a caller that
 * cannot tell the two apart shows the user an error for something the user
 * did not do.
 */
export function isAbortError(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'AbortError';
}

/**
 * The filename a `Content-Disposition` header names, or an empty string when
 * it names none.
 *
 * Both spellings are read. `filename*=UTF-8''...` is the one that survives a
 * name with anything outside ASCII in it -- a calendar called 家族 among them
 * -- so it wins wherever the server sent it; the quoted `filename="..."` is
 * what is left for everyone else. Any directory part is dropped: a downloaded
 * file is named, not placed.
 */
export function filenameFromDisposition(header: string | null): string {
  if (!header) return '';
  let name = '';
  const extended = header.match(/filename\*=\s*(?:UTF-8|utf-8)''([^;]+)/);
  if (extended?.[1]) {
    try {
      name = decodeURIComponent(extended[1].trim());
    } catch {
      // A percent-encoding the server got wrong is not a filename; the plain
      // form below is still worth reading.
    }
  }
  if (!name) {
    const quoted = header.match(/filename="([^"]*)"/i);
    name = (quoted?.[1] ?? header.match(/filename=([^;]+)/i)?.[1] ?? '').trim();
  }
  // A name is a name, not a path: keep only what follows the last separator.
  return name.slice(name.search(/[^/\\]*$/));
}

export const api = {
  get: <T>(path: string, skipAuthRedirect = false, signal?: AbortSignal) =>
    request<T>(path, { ...(signal ? { signal } : {}) }, skipAuthRedirect),
  /**
   * A GET that also reports the entity tag the server sent. An endpoint whose
   * updates are full replacements needs the caller to be able to say which
   * copy it is replacing, and that identity lives in the header rather than
   * in the body.
   */
  getWithRevision: async <T>(
    path: string,
    signal?: AbortSignal,
  ): Promise<{ data: T; revision: string | null }> => {
    const res = await requestRaw(path, { ...(signal ? { signal } : {}) });
    const revision = res.headers.get('ETag');
    return { data: (await res.json()) as T, revision };
  },
  /**
   * A GET of a capped listing, reporting whether the server cut it short.
   * A listing that stops at its cap looks complete unless the header is read,
   * and the reader takes the missing entries for an empty stretch of calendar.
   */
  getListing: async <T>(
    path: string,
    skipAuthRedirect = false,
    signal?: AbortSignal,
  ): Promise<{ data: T; truncated: boolean }> => {
    const res = await requestRaw(path, { ...(signal ? { signal } : {}) }, skipAuthRedirect);
    const truncated = res.headers.get('X-Result-Truncated') === 'true';
    return { data: (await res.json()) as T, truncated };
  },
  post: <T>(path: string, body?: unknown, signal?: AbortSignal) =>
    request<T>(path, {
      method: 'POST',
      ...(body != null ? { body: JSON.stringify(body) } : {}),
      ...(signal ? { signal } : {}),
    }),
  put: <T>(path: string, body?: unknown, headers?: Record<string, string>, signal?: AbortSignal) =>
    request<T>(path, {
      method: 'PUT',
      ...(headers ? { headers } : {}),
      ...(body != null ? { body: JSON.stringify(body) } : {}),
      ...(signal ? { signal } : {}),
    }),
  delete: <T>(path: string, signal?: AbortSignal) =>
    request<T>(path, { method: 'DELETE', ...(signal ? { signal } : {}) }),
  /**
   * Fetches a binary response through the central client (auth + 401
   * handling), with the name the server gave it.
   *
   * The name is part of the answer rather than decoration: an export is named
   * after the calendar it came from, and a caller that drops it saves every
   * calendar's export under one filename, over the last one. Likewise the
   * coverage: a body cut at the server's cap, or limited to a window, looks
   * like a whole backup unless the caller is told.
   */
  getBlob: async (
    path: string,
    signal?: AbortSignal,
  ): Promise<{ blob: Blob; filename: string; truncated: boolean; window: string }> => {
    const res = await requestRaw(path, { ...(signal ? { signal } : {}) });
    return {
      blob: await res.blob(),
      filename: filenameFromDisposition(res.headers.get('Content-Disposition')),
      truncated: res.headers.get('X-Result-Truncated') === 'true',
      window: res.headers.get('X-Export-Window') ?? '',
    };
  },
};
