import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Calendar } from '@/types/calendar';
import type { InviteData } from '@/types/invite';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const uiState = {
  rightPanel: 'share' as string | null,
  toggleRightPanel: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

function calendar(id: string, name: string): Calendar {
  return {
    id,
    name,
    color: '#000',
    coverUrl: '',
    createdAt: '',
    publicShared: false,
    role: 'owner',
    memberColor: '#000',
  };
}

const calendarState = {
  calendars: [calendar('cal-1', 'A')],
  activeCalendarIds: ['cal-1'],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { SharePanel } from './share-panel';

const apiGet = vi.mocked(api.get);
const apiPost = vi.mocked(api.post);
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

/**
 * The panel and the settings tab offer the same operations on a calendar's
 * links and share one implementation of them, so what holds here has to hold
 * on both screens.
 */
describe('SharePanel invites', () => {
  it('asks a bounded link for the role and the limits that were chosen', async () => {
    apiPost.mockResolvedValue(invite('inv-1', 'token-1'));

    render(<SharePanel />);

    fireEvent.click(await screen.findByRole('button', { name: 'share.createInvite' }));

    await act(async () => {});
    expect(apiPost).toHaveBeenCalledWith('/calendars/cal-1/invites', {
      role: 'editor',
      expiresInHours: 168,
      maxUses: 1,
    });
  });

  /**
   * A public link is handed to people who are not members and cannot be taken
   * back from wherever it is forwarded, so it reads and only reads.
   */
  it('creates the public link read-only, and only once confirmed', async () => {
    apiPost.mockResolvedValue(invite('inv-public', 'token-public', true));
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);

    render(<SharePanel />);

    const create = await screen.findByRole('button', { name: 'share.createPublic' });
    fireEvent.click(create);
    await act(async () => {});
    expect(apiPost).not.toHaveBeenCalled();

    confirm.mockReturnValue(true);
    fireEvent.click(create);
    await act(async () => {});
    expect(apiPost).toHaveBeenCalledWith('/calendars/cal-1/invites', {
      role: 'viewer',
      isPublic: true,
    });

    confirm.mockRestore();
  });

  /**
   * The API gives a join link seven days when the request states no expiry,
   * and takes nothing that means "never", so the picker must not offer one:
   * the option produced a link that died in a week under a label promising it
   * would not.
   */
  it('offers no expiry that outlives what the server will grant', async () => {
    render(<SharePanel />);

    // The trigger shows the current choice; the list arrives through a portal.
    fireEvent.click(await screen.findByRole('button', { name: 'invites.expiry7d' }));

    expect(await screen.findByRole('button', { name: 'invites.expiry30d' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'invites.expiryNever' })).toBeNull();
  });

  /**
   * A link that could hand out management would let whoever holds it widen its
   * own reach, so the API accepts editor and viewer and nothing else.
   */
  it('offers only the roles an invite link may grant', async () => {
    render(<SharePanel />);

    // The dropdown renders through a portal, so it is queried from the
    // document rather than from the render container.
    fireEvent.click(await screen.findByRole('button', { name: 'members.roleEditor' }));
    expect(await screen.findByRole('button', { name: 'members.roleViewer' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'members.roleOwner' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'members.roleManager' })).toBeNull();
  });

  it('does not let a superseded listing land under another calendar', async () => {
    calendarState.calendars = [calendar('cal-1', 'A'), calendar('cal-2', 'B')];
    calendarState.activeCalendarIds = ['cal-1', 'cal-2'];

    let landFirst: (invites: InviteData[]) => void = () => {};
    apiGet.mockImplementation((path) => {
      if (String(path).startsWith('/calendars/cal-1/invites')) {
        return new Promise((resolve) => {
          landFirst = resolve;
        });
      }
      if (String(path).startsWith('/calendars/cal-2/invites')) {
        return Promise.resolve([invite('inv-b', 'token-b')]);
      }
      return Promise.resolve([]);
    });

    render(<SharePanel />);

    fireEvent.change(await screen.findByRole('combobox'), { target: { value: 'cal-2' } });
    expect(await screen.findByDisplayValue('http://localhost:3000/share/token-b')).toBeTruthy();

    await act(async () => {
      landFirst([invite('inv-a', 'token-a')]);
    });

    expect(screen.queryByDisplayValue('http://localhost:3000/share/token-a')).toBeNull();
  });

  it('says why the listing failed in the language the reader chose', async () => {
    apiGet.mockRejectedValue(new Error('error.serverUnavailable'));

    render(<SharePanel />);

    await act(async () => {});
    expect(toastError).toHaveBeenCalledWith('error.serverUnavailable');
  });
});
