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

export function getToken(): string | null {
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
