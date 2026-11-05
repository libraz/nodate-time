import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({ getT: () => (key: string) => key }));
vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
  errorMessage: () => 'boom',
}));

import { api } from '@/lib/api';
import { resetAuthConfig, useAuthConfig } from './auth-config';

const apiGet = vi.mocked(api.get);
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
    apiGet.mockResolvedValue(CONFIG);
    const { result } = renderHook(() => useAuthConfig());
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.providers).toEqual(['google']);
    expect(result.current.passwordEnabled).toBe(true);
    expect(result.current.mailEnabled).toBe(false);
    expect(apiGet).toHaveBeenCalledWith('/auth/oauth/providers', true);
  });

  it('fetches once however many readers and mounts there are', async () => {
    apiGet.mockResolvedValue(CONFIG);
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

    apiGet.mockResolvedValue(CONFIG);
    await act(() => result.current.retry());
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(apiGet).toHaveBeenCalledTimes(2);
  });
});
