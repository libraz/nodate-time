import { useCallback, useEffect, useSyncExternalStore } from 'react';
import { api, errorMessage } from '@/lib/api';

export type OAuthProvider = 'google' | 'line';

interface AuthConfigResponse {
  providers: OAuthProvider[];
  passwordEnabled: boolean;
  mailEnabled: boolean;
}

type Snapshot =
  | { status: 'loading' }
  | { status: 'error'; detail: string }
  | ({ status: 'ready' } & AuthConfigResponse);

/**
 * What the public auth-providers endpoint says about this deployment.
 *
 * Until `status` is `ready`, `providers` is empty and both flags are false:
 * a caller renders what depends on them only once the config is known.
 */
export interface AuthConfig {
  status: Snapshot['status'];
  providers: OAuthProvider[];
  passwordEnabled: boolean;
  mailEnabled: boolean;
  /** The failure's own message when `status` is `error`. */
  detail: string | null;
  /** Fetches again after a failure; resolves when the attempt has finished. */
  retry: () => Promise<void>;
}

// One fetch per page load, shared by every screen that reads the config.
let snapshot: Snapshot = { status: 'loading' };
let inFlight: Promise<void> | null = null;
const listeners = new Set<() => void>();

function publish(next: Snapshot): void {
  snapshot = next;
  for (const l of listeners) l();
}

function load(): Promise<void> {
  if (inFlight) return inFlight;
  inFlight = (async () => {
    try {
      const r = await api.get<AuthConfigResponse>('/auth/oauth/providers', true);
      publish({
        status: 'ready',
        providers: r.providers,
        passwordEnabled: r.passwordEnabled,
        mailEnabled: r.mailEnabled,
      });
    } catch (e) {
      publish({ status: 'error', detail: errorMessage(e) });
    } finally {
      inFlight = null;
    }
  })();
  return inFlight;
}

function retry(): Promise<void> {
  if (snapshot.status === 'error') publish({ status: 'loading' });
  return load();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Forgets the cached config, so the next reader fetches afresh. */
export function resetAuthConfig(): void {
  inFlight = null;
  snapshot = { status: 'loading' };
}

/** Reads the public auth config, fetching it on first use. */
export function useAuthConfig(): AuthConfig {
  const snap = useSyncExternalStore(subscribe, () => snapshot);

  useEffect(() => {
    if (snapshot.status === 'loading') void load();
  }, []);

  const retryFetch = useCallback(retry, []);
  if (snap.status === 'ready') return { ...snap, detail: null, retry: retryFetch };
  return {
    status: snap.status,
    providers: [],
    passwordEnabled: false,
    mailEnabled: false,
    detail: snap.status === 'error' ? snap.detail : null,
    retry: retryFetch,
  };
}
