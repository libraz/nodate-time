import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useInvites } from './use-invites';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn() },
}));

const fetchMock = vi.fn();

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const created = {
  id: 'inv-1',
  token: 'tok-1',
  role: 'editor',
  useCount: 0,
  isPublic: false,
  createdAt: '2026-08-01T00:00:00Z',
  maxUses: 1,
};

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('useInvites against the wire', () => {
  it('takes the 201 answer of a create as the new link, limits as given', async () => {
    fetchMock.mockImplementation(async (request: Request) =>
      request.method === 'POST' ? json(created, 201) : json([]),
    );
    const { result } = renderHook(() => useInvites('cal-1'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await expect(
        result.current.createInvite({ role: 'editor', expiryHours: 168, maxUses: 1 }),
      ).resolves.toBe(true);
    });

    const post = fetchMock.mock.calls
      .map((c) => c[0] as Request)
      .find((request) => request.method === 'POST');
    expect(new URL(post?.url ?? '').pathname).toBe('/calendars/cal-1/invites');
    expect(await post?.json()).toEqual({ role: 'editor', expiresInHours: 168, maxUses: 1 });
    expect(result.current.joinInvites).toEqual([{ ...created, maxUses: 1, expiresAt: null }]);
  });

  it('reads a limit the server leaves out as no limit', async () => {
    const { maxUses: _maxUses, ...unlimited } = created;
    fetchMock.mockResolvedValue(json([unlimited]));
    const { result } = renderHook(() => useInvites('cal-1'));

    await waitFor(() => expect(result.current.invites).toHaveLength(1));

    expect(result.current.invites[0]).toMatchObject({ maxUses: null, expiresAt: null });
  });
});
