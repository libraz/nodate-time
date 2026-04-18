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
  ) {
    super(detail);
  }
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
      window.location.href = '/login';
      throw new ApiError(401, 'Unauthorized');
    }
    let detail = res.statusText;
    try {
      const body = await res.json();
      detail = body.detail ?? body.message ?? detail;
    } catch {
      // ignore
    }
    throw new ApiError(res.status, detail);
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
};
