import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import type { InviteData } from '@/types/invite';
import { useInvites } from './use-invites';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
  errorMessage: (error: unknown) => (error instanceof Error ? error.message : 'error'),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn() },
}));

const apiGet = vi.mocked(api.get);
const apiPost = vi.mocked(api.post);
const apiDelete = vi.mocked(api.delete);
const toastError = vi.mocked(toast.error);

function invite(id: string, token?: string, isPublic = false): InviteData {
  return {
    id,
    ...(token ? { token } : {}),
    role: 'editor',
    maxUses: 1,
    useCount: 0,
    isPublic,
    expiresAt: null,
    createdAt: '2026-08-01T00:00:00Z',
  };
}

function listedInvite(id: string, isPublic = false): InviteData {
  return invite(id, undefined, isPublic);
}

function deferred<T>() {
  let resolve: (value: T) => void = () => {};
  let reject: (error: unknown) => void = () => {};
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const terms = { role: 'editor' as const, expiryHours: 168, maxUses: 1 };

beforeEach(() => {
  apiGet.mockReset().mockResolvedValue([] as never);
  apiPost.mockReset();
  apiDelete.mockReset();
  toastError.mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('useInvites activation races', () => {
  it('hides the previous calendar rows as soon as the selected calendar changes', async () => {
    const nextCalendar = deferred<InviteData[]>();
    apiGet
      .mockResolvedValueOnce([invite('invite-a', 'token-a')] as never)
      .mockReturnValueOnce(nextCalendar.promise as never);

    const { result, rerender } = renderHook(({ id }) => useInvites(id), {
      initialProps: { id: 'calendar-a' },
    });
    await waitFor(() => expect(result.current.invites.map((row) => row.id)).toEqual(['invite-a']));

    rerender({ id: 'calendar-b' });

    expect(result.current.invites).toEqual([]);
    expect(result.current.loading).toBe(true);
    await act(async () => nextCalendar.resolve([invite('invite-b', 'token-b')]));
    expect(result.current.invites.map((row) => row.id)).toEqual(['invite-b']);
  });

  it('ignores an old create completion without clearing the current create busy state', async () => {
    const staleCreate = deferred<InviteData>();
    const currentCreate = deferred<InviteData>();
    apiPost
      .mockReturnValueOnce(staleCreate.promise as never)
      .mockReturnValueOnce(currentCreate.promise as never);
    const { result, rerender } = renderHook(({ id }) => useInvites(id), {
      initialProps: { id: 'calendar-a' },
    });
    await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(result.current.loading).toBe(false));

    let staleResult!: Promise<boolean>;
    act(() => {
      staleResult = result.current.createInvite(terms);
    });
    rerender({ id: 'calendar-b' });
    await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.loading).toBe(false));

    let currentResult!: Promise<boolean>;
    act(() => {
      currentResult = result.current.createInvite(terms);
    });
    expect(result.current.busy).toBe('invite');

    await act(async () => {
      staleCreate.resolve(invite('stale-a', 'token-a'));
      await expect(staleResult).resolves.toBe(true);
    });
    expect(result.current.busy).toBe('invite');
    expect(result.current.invites).toEqual([]);

    await act(async () => {
      currentCreate.resolve(invite('current-b', 'token-b'));
      await expect(currentResult).resolves.toBe(true);
    });
    expect(result.current.invites.map((row) => row.id)).toEqual(['current-b']);
    expect(apiPost).toHaveBeenNthCalledWith(1, '/calendars/calendar-a/invites', {
      role: 'editor',
      expiresInHours: 168,
      maxUses: 1,
    });
  });

  it('ignores stale public-link and revoke completions after switching calendars', async () => {
    const stalePublic = deferred<InviteData>();
    const staleRevoke = deferred<void>();
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    apiPost.mockReturnValueOnce(stalePublic.promise as never);
    apiDelete.mockReturnValueOnce(staleRevoke.promise as never);
    const { result, rerender } = renderHook(({ id }) => useInvites(id), {
      initialProps: { id: 'calendar-a' },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));

    let publicResult!: Promise<boolean>;
    let revokeResult!: Promise<boolean>;
    act(() => {
      publicResult = result.current.createPublicLink();
      revokeResult = result.current.revokeInvite('old-link');
    });
    rerender({ id: 'calendar-b' });
    await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      staleRevoke.resolve();
      await expect(revokeResult).resolves.toBe(true);
      stalePublic.resolve(invite('public-a', 'token-public-a', true));
      await expect(publicResult).resolves.toBe(true);
    });

    expect(confirm).toHaveBeenCalledWith('share.publicConfirm');
    expect(apiPost).toHaveBeenCalledWith('/calendars/calendar-a/invites', {
      role: 'viewer',
      isPublic: true,
    });
    expect(result.current.invites).toEqual([]);
    expect(result.current.publicLink).toBeNull();
  });

  it('suppresses errors from a create that fails after its activation ends', async () => {
    const staleFailure = deferred<InviteData>();
    apiPost.mockReturnValueOnce(staleFailure.promise as never);
    const { result, rerender } = renderHook(({ id }) => useInvites(id), {
      initialProps: { id: 'calendar-a' },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));

    let staleResult!: Promise<boolean>;
    act(() => {
      staleResult = result.current.createInvite(terms);
    });
    rerender({ id: 'calendar-b' });
    await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      staleFailure.reject(new Error('stale failure'));
      await expect(staleResult).resolves.toBe(false);
    });
    expect(toastError).not.toHaveBeenCalled();
    expect(result.current.invites).toEqual([]);
  });

  it('falls back to the newest still-pending create when another finishes', async () => {
    const olderCreate = deferred<InviteData>();
    const newerCreate = deferred<InviteData>();
    apiPost
      .mockReturnValueOnce(olderCreate.promise as never)
      .mockReturnValueOnce(newerCreate.promise as never);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const { result } = renderHook(() => useInvites('calendar-a'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    let olderResult!: Promise<boolean>;
    let newerResult!: Promise<boolean>;
    act(() => {
      olderResult = result.current.createInvite(terms);
      newerResult = result.current.createPublicLink();
    });
    expect(result.current.busy).toBe('public');

    await act(async () => {
      newerCreate.resolve(invite('public-a', 'token-public', true));
      await expect(newerResult).resolves.toBe(true);
    });
    expect(result.current.busy).toBe('invite');

    await act(async () => {
      olderCreate.resolve(invite('join-a', 'token-join'));
      await expect(olderResult).resolves.toBe(true);
    });
    expect(result.current.busy).toBeNull();
  });

  it('does not call the API or ask for confirmation while disabled', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const { result } = renderHook(() => useInvites('calendar-a', false));

    await act(async () => {
      await expect(result.current.createInvite(terms)).resolves.toBe(false);
      await expect(result.current.createPublicLink()).resolves.toBe(false);
      await expect(result.current.revokeInvite('invite-a')).resolves.toBe(false);
    });

    expect(apiGet).not.toHaveBeenCalled();
    expect(apiPost).not.toHaveBeenCalled();
    expect(apiDelete).not.toHaveBeenCalled();
    expect(confirm).not.toHaveBeenCalled();
    expect(result.current).toMatchObject({ invites: [], loading: false, busy: null });
  });

  it.each(['switch', 'disable'] as const)(
    'rejects captured callbacks after a calendar %s',
    async (transition) => {
      const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
      const { result, rerender } = renderHook(({ id, enabled }) => useInvites(id, enabled), {
        initialProps: { id: 'calendar-a', enabled: true },
      });
      await waitFor(() => expect(result.current.loading).toBe(false));
      const captured = result.current;

      rerender({
        id: transition === 'switch' ? 'calendar-b' : 'calendar-a',
        enabled: transition !== 'disable',
      });

      await act(async () => {
        await expect(captured.createInvite(terms)).resolves.toBe(false);
        await expect(captured.createPublicLink()).resolves.toBe(false);
        await expect(captured.revokeInvite('invite-a')).resolves.toBe(false);
      });

      expect(apiPost).not.toHaveBeenCalled();
      expect(apiDelete).not.toHaveBeenCalled();
      expect(confirm).not.toHaveBeenCalled();
    },
  );

  it('keeps shown-once tokens when the same calendar is disabled and enabled again', async () => {
    const created = invite('created-a', 'token-a');
    apiGet
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([listedInvite('created-a')] as never);
    apiPost.mockResolvedValueOnce(created as never);
    const { result, rerender } = renderHook(({ enabled }) => useInvites('calendar-a', enabled), {
      initialProps: { enabled: true },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await expect(result.current.createInvite(terms)).resolves.toBe(true);
    });
    expect(result.current.invites[0]?.token).toBe('token-a');

    rerender({ enabled: false });
    expect(result.current).toMatchObject({ invites: [], loading: false, busy: null });
    rerender({ enabled: true });
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.invites).toEqual([created]);
  });

  it('rejects an old A activation after switching A to B and back to A', async () => {
    const staleCreate = deferred<InviteData>();
    apiPost.mockReturnValueOnce(staleCreate.promise as never);
    const { result, rerender } = renderHook(({ id }) => useInvites(id), {
      initialProps: { id: 'calendar-a' },
    });
    await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(result.current.loading).toBe(false));

    let staleResult!: Promise<boolean>;
    act(() => {
      staleResult = result.current.createInvite(terms);
    });
    rerender({ id: 'calendar-b' });
    await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
    rerender({ id: 'calendar-a' });
    await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(3));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      staleCreate.resolve(invite('stale-a', 'token-stale'));
      await expect(staleResult).resolves.toBe(true);
    });
    expect(result.current.invites).toEqual([]);
  });

  it.each([false, true])(
    'reconciles mutations when the initial listing contains the new join: %s',
    async (alreadyListed) => {
      const firstListing = deferred<InviteData[]>();
      const createdJoin = invite('created-join', 'token-join');
      const createdPublic = invite('created-public', 'token-public', true);
      apiGet.mockReturnValueOnce(firstListing.promise as never);
      apiPost
        .mockResolvedValueOnce(createdJoin as never)
        .mockResolvedValueOnce(createdPublic as never);
      apiDelete.mockResolvedValueOnce(undefined as never);
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      const { result } = renderHook(() => useInvites('calendar-a'));

      expect(result.current.loading).toBe(true);
      await act(async () => {
        await expect(result.current.createInvite(terms)).resolves.toBe(true);
        await expect(result.current.createPublicLink()).resolves.toBe(true);
        await expect(result.current.revokeInvite('revoked-before-list')).resolves.toBe(true);
      });

      await act(async () => {
        firstListing.resolve([
          ...(alreadyListed ? [listedInvite('created-join')] : []),
          listedInvite('other-listed'),
          listedInvite('old-public', true),
          listedInvite('revoked-before-list'),
        ]);
      });
      await waitFor(() => expect(result.current.loading).toBe(false));

      expect(result.current.invites).toEqual([
        createdPublic,
        createdJoin,
        listedInvite('other-listed'),
      ]);
      expect(result.current.invites.filter((row) => row.id === 'created-join')).toHaveLength(1);
      expect(apiDelete).toHaveBeenCalledWith('/calendars/calendar-a/invites/revoked-before-list');
    },
  );

  it('reports a failure from the active create and returns false', async () => {
    apiPost.mockRejectedValueOnce(new Error('invite failed'));
    const { result } = renderHook(() => useInvites('calendar-a'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await expect(result.current.createInvite(terms)).resolves.toBe(false);
    });

    expect(toastError).toHaveBeenCalledWith('invite failed');
    expect(result.current.busy).toBeNull();
  });
});
