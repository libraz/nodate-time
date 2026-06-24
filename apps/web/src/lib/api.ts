import { getT } from '@/i18n';
import { toast } from '@/lib/toast';

const API_BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';

function getToken(): string | null {
  return localStorage.getItem('tt_token');
}

export function setToken(token: string): void {
  localStorage.setItem('tt_token', token);
}

export function clearToken(): void {
  localStorage.removeItem('tt_token');
}

export function hasToken(): boolean {
  return getToken() !== null;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public detail: string,
    /** Machine-readable error code from the API envelope (e.g. `CALENDAR.ROLE_REQUIRED`). */
    public code = '',
  ) {
    super(detail);
  }
}

/** Maps known API error codes to localized message keys for user-facing toasts. */
function localizeError(err: ApiError): string {
  const t = getT();
  switch (err.code) {
    case 'CALENDAR.ROLE_REQUIRED':
    case 'CALENDAR.ACCESS_DENIED':
      return t('error.noPermission');
    case 'AUTH.TOKEN_INVALID':
      return t('error.sessionExpired');
    default:
      if (err.status === 403) return t('error.noPermission');
      return err.detail;
  }
}

/** Returns a localized, user-facing message for any thrown error. */
export function errorMessage(e: unknown, fallback?: string): string {
  if (e instanceof ApiError) return localizeError(e);
  return fallback ?? getT()('error.generic');
}

async function buildError(res: Response): Promise<ApiError> {
  let detail = res.statusText;
  let code = '';
  try {
    const body = await res.json();
    detail = body.detail ?? body.message ?? detail;
    code = typeof body.code === 'string' ? body.code : '';
  } catch {
    // ignore non-JSON bodies
  }
  return new ApiError(res.status, detail, code);
}

async function request<T>(
  path: string,
  options: RequestInit = {},
  skipAuthRedirect = false,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((options.headers as Record<string, string>) ?? {}),
  };

  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(`${API_BASE}${path}`, { ...options, headers });

  if (!res.ok) {
    if (res.status === 401 && !skipAuthRedirect) {
      clearToken();
      toast.error(getT()('error.sessionExpired'));
      window.location.href = '/login';
      throw new ApiError(401, 'Unauthorized', 'AUTH.TOKEN_INVALID');
    }
    throw await buildError(res);
  }

  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const api = {
  get: <T>(path: string, skipAuthRedirect = false) => request<T>(path, {}, skipAuthRedirect),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', ...(body != null ? { body: JSON.stringify(body) } : {}) }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', ...(body != null ? { body: JSON.stringify(body) } : {}) }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
  /** Fetches a binary response through the central client (auth + 401 handling). */
  getBlob: async (path: string): Promise<Blob> => {
    const token = getToken();
    const headers: Record<string, string> = {};
    if (token) headers.Authorization = `Bearer ${token}`;
    const res = await fetch(`${API_BASE}${path}`, { headers });
    if (!res.ok) {
      if (res.status === 401) {
        clearToken();
        toast.error(getT()('error.sessionExpired'));
        window.location.href = '/login';
        throw new ApiError(401, 'Unauthorized', 'AUTH.TOKEN_INVALID');
      }
      throw await buildError(res);
    }
    return res.blob();
  },
};
