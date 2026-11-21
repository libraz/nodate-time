import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({ getT: () => (key: string) => key }));
vi.mock('@/lib/api', () => ({ errorMessage: () => 'boom' }));
// biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
vi.mock('@/lib/client', () => ({ client: { GET: vi.fn() } }));

import { client } from '@/lib/client';
import { resetAuthConfig, useAuthConfig } from './auth-config';

const apiGet = vi.mocked(client.GET);
const CONFIG = { providers: ['google'], passwordEnabled: true, mailEnabled: false };

beforeEach(() => {
  apiGet.mockReset();
  resetAuthConfig();
});
afterEach(cleanup);

describe('useAuthConfig', () => {
  it('reports loading, with nothing enabled, until the server answers', () => {
    apiGet.mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(() => useAuthConfig());
    expect(result.current.status).toBe('loading');
    expect(result.current.providers).toEqual([]);
    expect(result.current.passwordEnabled).toBe(false);
    expect(result.current.mailEnabled).toBe(false);
  });

  it('reports the server values once ready', async () => {
    apiGet.mockResolvedValue({ data: CONFIG } as never);
    const { result } = renderHook(() => useAuthConfig());
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.providers).toEqual(['google']);
    expect(result.current.passwordEnabled).toBe(true);
    expect(result.current.mailEnabled).toBe(false);
    expect(apiGet).toHaveBeenCalledWith('/auth/oauth/providers');
  });

  it('fetches once however many readers and mounts there are', async () => {
    apiGet.mockResolvedValue({ data: CONFIG } as never);
    const first = renderHook(() => useAuthConfig());
    const second = renderHook(() => useAuthConfig());
    await waitFor(() => expect(first.result.current.status).toBe('ready'));
    await waitFor(() => expect(second.result.current.status).toBe('ready'));
    first.unmount();
    const third = renderHook(() => useAuthConfig());
    expect(third.result.current.status).toBe('ready');
    expect(apiGet).toHaveBeenCalledTimes(1);
  });

  it('reports an error, and a retry fetches again', async () => {
    apiGet.mockRejectedValueOnce(new Error('network'));
    const { result } = renderHook(() => useAuthConfig());
    await waitFor(() => expect(result.current.status).toBe('error'));
    expect(result.current.detail).toBe('boom');

    apiGet.mockResolvedValue({ data: CONFIG } as never);
    await act(() => result.current.retry());
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(apiGet).toHaveBeenCalledTimes(2);
  });
});
