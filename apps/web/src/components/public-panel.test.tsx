import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@tanstack/react-router', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

import type { Calendar } from '@/types/calendar';
import type { InviteData } from '@/types/invite';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

// The typed client is replaced by an adapter over three stubs that see the
// resolved path and body, so the assertions read as the requests made.
const raw = vi.hoisted(() => {
  const fill = (path: string, init?: { params?: { path?: Record<string, string> } }) =>
    path.replace(/\{(\w+)\}/g, (_, key: string) => init?.params?.path?.[key] ?? '');
  return { fill, get: vi.fn(), post: vi.fn(), delete: vi.fn() };
});

type Init = { params?: { path?: Record<string, string> }; body?: unknown };

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),

  client: {
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    GET: async (path: string, init?: Init) => ({ data: await raw.get(raw.fill(path, init)) }),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    POST: async (path: string, init?: Init) => ({
      data: await raw.post(raw.fill(path, init), init?.body),
    }),
    // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
    DELETE: async (path: string, init?: Init) => ({ data: await raw.delete(raw.fill(path, init)) }),
  },
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const uiState = {
  rightPanel: 'public' as string | null,
  toggleRightPanel: vi.fn(),
  openPanelFor: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

function calendar(id: string, name: string): Calendar {
  return {
    id,
    name,
    color: '#000',
    createdAt: '',
    publicShared: false,
    memberCount: 1,
    role: 'owner',
    memberColor: '#000',
    coverUploadAllowed: false,
    etag: 'etag-1',
  };
}

const calendarState = {
  calendars: [calendar('cal-1', 'A')],
  activeCalendarIds: ['cal-1'],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { toast } from '@/lib/toast';
import { PublicPanel } from './public-panel';

const apiGet = raw.get;
const apiPost = raw.post;
const toastError = vi.mocked(toast.error);

function invite(id: string, token: string, isPublic = false): InviteData {
  return {
    id,
    token,
    role: isPublic ? 'viewer' : 'editor',
    maxUses: isPublic ? null : 1,
    useCount: 0,
    isPublic,
    expiresAt: null,
    createdAt: '2026-08-01T00:00:00Z',
  };
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  toastError.mockReset();
  apiGet.mockResolvedValue([]);
  calendarState.calendars = [calendar('cal-1', 'A')];
  calendarState.activeCalendarIds = ['cal-1'];
});

afterEach(cleanup);

describe('PublicPanel', () => {
  /**
   * A public link is handed to people who are not members and cannot be taken
   * back from wherever it is forwarded, so it reads and only reads.
   */
  it('creates the public link read-only, and only once confirmed', async () => {
    apiPost.mockResolvedValue(invite('inv-public', 'token-public', true));

    render(<PublicPanel />);

    const create = await screen.findByRole('button', { name: 'share.createPublic' });
    fireEvent.click(create);
    await act(async () => {});
    expect(screen.getByRole('alertdialog')).toBeTruthy();
    expect(apiPost).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'common.cancel' }));
    await act(async () => {});
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(apiPost).not.toHaveBeenCalled();

    fireEvent.click(create);
    await act(async () => {});
    fireEvent.click(screen.getByRole('button', { name: 'common.create' }));
    await act(async () => {});
    expect(apiPost).toHaveBeenCalledWith('/calendars/cal-1/invites', {
      role: 'viewer',
      isPublic: true,
    });
  });

  it('offers nothing about members or invite links', async () => {
    render(<PublicPanel />);
    await act(async () => {});
    expect(screen.queryByRole('button', { name: 'share.createInvite' })).toBeNull();
    expect(screen.queryByText('members.addSection')).toBeNull();
  });
});

describe('PublicPanel copy', () => {
  const setClipboard = (writeText: (text: string) => Promise<void>) =>
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });

  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
  });

  it('acknowledges the copy only once the clipboard write has resolved', async () => {
    apiGet.mockResolvedValue([invite('inv-p', 'tok', true)]);
    let finish: () => void = () => {};
    setClipboard(() => new Promise<void>((resolve) => (finish = resolve)));

    render(<PublicPanel />);
    fireEvent.click(await screen.findByRole('button', { name: 'share.copyUrl' }));
    await act(async () => {});
    expect(screen.queryByRole('button', { name: 'common.copied' })).toBeNull();

    await act(async () => finish());
    expect(screen.getByRole('button', { name: 'common.copied' })).toBeTruthy();
  });

  it('shows an error and no acknowledgement when the clipboard write is rejected', async () => {
    apiGet.mockResolvedValue([invite('inv-p', 'tok', true)]);
    setClipboard(() => Promise.reject(new DOMException('denied', 'NotAllowedError')));

    render(<PublicPanel />);
    fireEvent.click(await screen.findByRole('button', { name: 'share.copyUrl' }));
    await act(async () => {});

    expect(toastError).toHaveBeenCalledWith('common.copyFailed');
    expect(screen.queryByRole('button', { name: 'common.copied' })).toBeNull();
  });

  it('shows an error when there is no clipboard API', async () => {
    apiGet.mockResolvedValue([invite('inv-p', 'tok', true)]);

    render(<PublicPanel />);
    fireEvent.click(await screen.findByRole('button', { name: 'share.copyUrl' }));
    await act(async () => {});

    expect(toastError).toHaveBeenCalledWith('common.copyFailed');
  });
});
