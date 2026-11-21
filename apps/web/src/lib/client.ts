import createOpenapiFetch, { type Client, type Middleware } from 'openapi-fetch';
import { getT } from '@/i18n';
import {
  ApiError,
  clearToken,
  getRefreshToken,
  isTokenExpired,
  SESSION_EXPIRED_EVENT,
  setToken,
} from '@/lib/api';
import { isSafeRedirect } from '@/lib/safe-redirect';
import { toast } from '@/lib/toast';

const API_BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';

/** An access token and the refresh token issued beside it. */
interface TokenPair {
  token: string;
  refreshToken?: string;
}

function getToken(): string | null {
  const token = localStorage.getItem('tt_token');
  if (token !== null && isTokenExpired(token)) {
    localStorage.removeItem('tt_token');
    return null;
  }
  return token;
}

/** Whether a request speaks for the signed-in session; the rest of /auth/* obtains one. */
function carriesSession(schemaPath: string): boolean {
  return !schemaPath.startsWith('/auth/') || schemaPath === '/auth/verify-email/confirm';
}

/**
 * The application's own `{status, code, message}` envelope and the validator's
 * `{title, status, detail, errors[]}` read into one error shape.
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

/** Resolves null only when the server definitively refuses the token; anything else throws. */
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
    // Another tab rotated the pair while this one waited; the old token would be refused.
    if (current !== started) {
      const token = getToken();
      if (token) return { token, refreshToken: current };
    }
    const pair = await exchangeRefreshToken(current);
    // A sign-out that landed meanwhile must not be undone by storing the new pair.
    if (pair && getRefreshToken() === current) setToken(pair.token, pair.refreshToken);
    return pair;
  });
}

// Renewal rotates the refresh token, so concurrent 401s must share one exchange.
let refreshInFlight: Promise<TokenPair | null> | null = null;

function renewSession(): Promise<TokenPair | null> {
  if (!refreshInFlight) {
    refreshInFlight = refreshSession().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

/** The token to retry with after the server refused `failed`, or null if unrenewable. */
async function renewedToken(failed: string | null): Promise<string | null> {
  const stored = getToken();
  if (stored && stored !== failed) return stored;
  const pair = await renewSession();
  if (pair) return pair.token;
  const rotated = getToken();
  return rotated && rotated !== failed ? rotated : null;
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

/** Ends the session after the server refused `failed`, unless another tab stored a newer pair. */
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
