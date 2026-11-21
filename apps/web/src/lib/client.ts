import createOpenapiFetch, { type Client, type Middleware } from 'openapi-fetch';
import { getT } from '@/i18n';
import type { paths } from '@/lib/api-schema';
import { errorMessageKey } from '@/lib/error-codes';
import { isSafeRedirect } from '@/lib/safe-redirect';
import {
  clearToken,
  getRefreshToken,
  getToken,
  isTokenExpired,
  SESSION_EXPIRED_EVENT,
  setToken,
} from '@/lib/session';
import { toast } from '@/lib/toast';

const API_BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';

/** An access token and the refresh token issued beside it. */
interface TokenPair {
  token: string;
  refreshToken?: string;
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

/** Whether a request speaks for the signed-in session; the rest of /auth/* obtains one. */
function carriesSession(schemaPath: string): boolean {
  return !schemaPath.startsWith('/auth/') || schemaPath === '/auth/verify-email/confirm';
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
async function toApiError(res: Response): Promise<ApiError> {
  let detail = '';
  let code = '';
  let issues: { message?: string; location?: string }[] = [];
  try {
    const body = await res.json();
    detail = body.message ?? body.detail ?? '';
    code = typeof body.code === 'string' ? body.code : '';
    if (Array.isArray(body.errors)) issues = body.errors;
  } catch {
    // A body that is not JSON says nothing beyond the status line.
  }
  if (!code && issues.length > 0) code = 'REQUEST.INVALID';
  if (!detail) detail = res.statusText || `HTTP ${res.status}`;
  return new ApiError(res.status, detail, code, issues);
}

/**
 * Trades a refresh token for a new pair without storing it.
 *
 * Resolves null only when the server definitively refuses the token (401/403).
 * A network error, a 5xx or a malformed body throws instead: the session may
 * be perfectly alive, and treating the failure as a refusal would sign the
 * user out of it.
 */
async function exchangeRefreshToken(refreshToken: string): Promise<TokenPair | null> {
  const res = await globalThis.fetch(`${API_BASE}/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refreshToken }),
  });
  if (res.status === 401 || res.status === 403) return null;
  if (!res.ok) throw await toApiError(res);
  const body = (await res.json().catch(() => null)) as TokenPair | null;
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

/**
 * The renewal currently in flight, if any.
 *
 * One at a time, deliberately. Renewal rotates the refresh token -- the old
 * one stops working the moment the new one is issued -- so two requests that
 * both noticed a 401 and both tried would race, and the loser would present a
 * token that had just been retired and be signed out for it.
 */
let refreshInFlight: Promise<TokenPair | null> | null = null;

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
  const logout = (token: string) =>
    globalThis.fetch(`${API_BASE}/auth/logout`, {
      method: 'POST',
      // biome-ignore lint/style/useNamingConvention: HTTP header name
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    });
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

function navigateToLogin(): void {
  if (window.location.pathname === '/login') return;
  const { pathname, search, hash } = window.location;
  const target = `${pathname}${search}${hash}`;
  const next = isSafeRedirect(target) ? `/login?redirect=${encodeURIComponent(target)}` : '/login';
  if (`${pathname}${search}` === next) return;
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

interface Sent {
  /** The request before the Authorization header was attached, kept for a retry. */
  pristine: Request;
  token: string | null;
}

/**
 * Attaches the bearer token, renews an expired session before sending, retries
 * once after a 401, and turns every failed response into an `ApiError`.
 */
function sessionMiddleware(): Middleware {
  const sent = new WeakMap<Request, Sent>();
  return {
    async onRequest({ request, schemaPath }) {
      if (!getToken() && getRefreshToken() && carriesSession(schemaPath)) await renewSession();
      const token = getToken();
      sent.set(request, { pristine: request.clone(), token });
      if (token) request.headers.set('Authorization', `Bearer ${token}`);
      return request;
    },
    async onResponse({ request, response, schemaPath }) {
      let res = response;
      const entry = sent.get(request);
      let used = entry?.token ?? null;
      if (res.status === 401 && entry && carriesSession(schemaPath)) {
        const next = await renewedToken(used);
        if (next) {
          used = next;
          const retry = new Request(entry.pristine);
          retry.headers.set('Authorization', `Bearer ${next}`);
          res = await globalThis.fetch(retry);
        }
      }
      if (res.ok) return res;
      if (res.status === 401 && !schemaPath.startsWith('/auth/')) expireSession(used);
      throw await toApiError(res);
    },
  };
}

/**
 * Builds a typed client over the generated `paths`. Failed responses reject
 * with `ApiError`; successful ones resolve to `{ data, response }`.
 */
export function createApiClient<Paths extends {}>(baseUrl: string = API_BASE): Client<Paths> {
  const client = createOpenapiFetch<Paths>({
    baseUrl,
    // Resolved per call so a replaced global fetch is honoured.
    fetch: (request) => globalThis.fetch(request),
  });
  client.use(sessionMiddleware());
  return client;
}

/** The application's client over the generated API document. */
export const client = createApiClient<paths>();

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

/** Unwraps a response body the client has already checked for failure. */
export function present<T>(data: T | undefined): T {
  if (data === undefined) throw new ApiError(502, 'Empty response');
  return data;
}

/** Leaves `signal` out when absent, as exactOptionalPropertyTypes requires. */
export const withSignal = (signal?: AbortSignal) => (signal ? { signal } : {});

/** Whether a write was refused because the item changed after it was read: every such refusal is a 412. */
export function isStaleError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 412;
}
