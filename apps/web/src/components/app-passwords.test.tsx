import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '@/lib/api-schema';

vi.mock('@/i18n', () => ({
  useT: () => (key: string, params?: Record<string, string>) =>
    params ? `${key}:${Object.values(params).join(',')}` : key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
  client: { GET: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));

vi.mock('@/lib/clipboard', () => ({ copyText: vi.fn() }));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

const uiState = { locale: 'en', timezone: 'Asia/Tokyo' };

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { ApiError, client } from '@/lib/client';
import { copyText } from '@/lib/clipboard';
import { toast } from '@/lib/toast';
import { AppPasswords } from './app-passwords';

type AppPassword = components['schemas']['AppPasswordResponse'];

const apiGet = vi.mocked(client.GET);
const apiPost = vi.mocked(client.POST);
const apiDelete = vi.mocked(client.DELETE);

function row(n: number, over: Partial<AppPassword> = {}): AppPassword {
  return {
    id: `id-${n}`,
    name: `Device ${n}`,
    prefix: `ndt_AB${String(n).padStart(2, '0')}`,
    createdAt: '2026-11-01T00:00:00Z',
    lastUsedAt: null,
    ...over,
  };
}

const created = {
  id: 'id-new',
  name: 'iPhone',
  prefix: 'ndt_ABCD',
  createdAt: '2026-11-22T00:00:00Z',
  password: 'ndt_ABCDEFGHIJKLMNOP',
  serverHost: 'time.example.com',
  username: 'me@example.com',
};

function listing(items: AppPassword[]) {
  apiGet.mockResolvedValue({ data: items } as never);
}

/** Renders the section and waits until the list it was given is on screen. */
async function renderLoaded(items: AppPassword[] = []) {
  listing(items);
  render(<AppPasswords />);
  if (items.length === 0) await screen.findByText('appPasswords.empty');
  else await screen.findAllByRole('button', { name: 'appPasswords.revoke' });
}

function createNamed(name: string) {
  fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: name } });
  fireEvent.click(screen.getByRole('button', { name: 'appPasswords.create' }));
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  apiDelete.mockReset();
  vi.mocked(copyText).mockReset();
  vi.mocked(toast.error).mockReset();
  vi.mocked(toast.success).mockReset();
});

afterEach(cleanup);

describe('AppPasswords list', () => {
  it('shows name, prefix, last use and creation date for each password', async () => {
    await renderLoaded([row(1), row(2, { lastUsedAt: '2026-11-20T10:00:00Z' })]);
    await screen.findByText('Device 1');
    expect(screen.getByText('ndt_AB01…')).toBeInTheDocument();
    expect(screen.getByText('Device 2')).toBeInTheDocument();
    expect(screen.getAllByText(/appPasswords\.neverUsed/)).toHaveLength(1);
    expect(screen.getAllByText(/appPasswords\.lastUsed/)).toHaveLength(1);
    expect(screen.getAllByText(/appPasswords\.createdOn/)).toHaveLength(2);
  });

  it('shows the empty state when there are none', async () => {
    await renderLoaded([]);
    expect(await screen.findByText('appPasswords.empty')).toBeInTheDocument();
  });

  it('offers a retry when the list cannot be read, and recovers', async () => {
    apiGet.mockRejectedValue(new Error('network'));
    render(<AppPasswords />);
    expect(await screen.findByText('appPasswords.loadFailed')).toBeInTheDocument();
    expect(screen.queryByText('appPasswords.empty')).toBeNull();
    expect(screen.getByRole('button', { name: 'appPasswords.create' })).toBeDisabled();

    listing([row(1)]);
    fireEvent.click(screen.getByText('error.retry'));
    expect(await screen.findByText('Device 1')).toBeInTheDocument();
    expect(screen.queryByText('appPasswords.loadFailed')).toBeNull();
  });
});

describe('AppPasswords create', () => {
  it('shows the plaintext in groups of four, once, with the connection details', async () => {
    await renderLoaded([]);
    apiPost.mockResolvedValue({ data: created } as never);
    createNamed('  iPhone ');

    expect(await screen.findByText('ndt_ ABCD EFGH IJKL MNOP')).toBeInTheDocument();
    expect(apiPost).toHaveBeenCalledWith('/user/app-passwords', { body: { name: 'iPhone' } });
    expect(screen.getAllByText('time.example.com').length).toBeGreaterThan(0);
    expect(screen.getByText('me@example.com')).toBeInTheDocument();
    expect(screen.getByText('appPasswords.shownOnce')).toBeInTheDocument();
    // The new password joins the list without a second request for it.
    expect(screen.getByText('ndt_ABCD…')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'common.close' }));
    expect(screen.queryByText('ndt_ ABCD EFGH IJKL MNOP')).toBeNull();
    expect(screen.queryByText(/ABCDEFGH/)).toBeNull();
  });

  it('copies the password without the grouping', async () => {
    await renderLoaded([]);
    apiPost.mockResolvedValue({ data: created } as never);
    vi.mocked(copyText).mockResolvedValue(true);
    createNamed('iPhone');
    fireEvent.click(await screen.findByRole('button', { name: 'appPasswords.copyPassword' }));

    expect(copyText).toHaveBeenCalledWith('ndt_ABCDEFGHIJKLMNOP');
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('common.copied'));
  });

  it('reports a copy the browser refused', async () => {
    await renderLoaded([]);
    apiPost.mockResolvedValue({ data: created } as never);
    vi.mocked(copyText).mockResolvedValue(false);
    createNamed('iPhone');
    fireEvent.click(await screen.findByRole('button', { name: 'appPasswords.copyPassword' }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('common.copyFailed'));
  });

  it('does not submit an empty name', async () => {
    await renderLoaded([]);
    expect(screen.getByRole('button', { name: 'appPasswords.create' })).toBeDisabled();
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: '  ' } });
    expect(screen.getByRole('button', { name: 'appPasswords.create' })).toBeDisabled();
  });

  it('disables create at the cap and says why', async () => {
    await renderLoaded(Array.from({ length: 20 }, (_, i) => row(i + 1)));
    await screen.findByText('Device 20');
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: 'x' } });
    expect(screen.getByRole('button', { name: 'appPasswords.create' })).toBeDisabled();
    expect(screen.getByText('appPasswords.limitReached:20')).toBeInTheDocument();
  });

  it('allows create just under the cap', async () => {
    await renderLoaded(Array.from({ length: 19 }, (_, i) => row(i + 1)));
    await screen.findByText('Device 19');
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: 'x' } });
    expect(screen.getByRole('button', { name: 'appPasswords.create' })).toBeEnabled();
    expect(screen.queryByText(/appPasswords\.limitReached/)).toBeNull();
  });

  it.each(['APP_PASSWORD.EMAIL_UNVERIFIED', 'APP_PASSWORD.MFA_ENABLED'])(
    'shows the reason for a 403 %s',
    async (code) => {
      await renderLoaded([]);
      apiPost.mockRejectedValue(new ApiError(403, 'forbidden', code));
      createNamed('iPhone');

      expect(await screen.findByRole('alert')).toHaveTextContent(`apiError.${code}`);
      expect(screen.queryByText('appPasswords.createdTitle')).toBeNull();
    },
  );
});

describe('AppPasswords connection steps', () => {
  it('is always present and names this host before anything is created', async () => {
    await renderLoaded([]);
    expect(screen.getByText('appPasswords.stepsTitle')).toBeInTheDocument();
    expect(screen.getByText('appPasswords.stepsIphonePath')).toBeInTheDocument();
    expect(screen.getByText('appPasswords.stepsMacPath')).toBeInTheDocument();
    expect(
      screen.getByText(`appPasswords.stepsServer:${window.location.host}`),
    ).toBeInTheDocument();
  });

  it('switches to the server the API reported, and keeps it after the card closes', async () => {
    await renderLoaded([]);
    apiPost.mockResolvedValue({ data: created } as never);
    createNamed('iPhone');
    await screen.findByText('appPasswords.createdTitle');
    fireEvent.click(screen.getByRole('button', { name: 'common.close' }));

    expect(screen.getByText('appPasswords.stepsServer:time.example.com')).toBeInTheDocument();
  });

  it('lists what does not sync', async () => {
    await renderLoaded([]);
    for (const key of ['Hidden', 'Viewer', 'Others', 'Move', 'Repeat']) {
      expect(screen.getByText(`appPasswords.limits${key}`)).toBeInTheDocument();
    }
  });
});

describe('AppPasswords revoke', () => {
  it('asks first and revokes only on confirmation', async () => {
    await renderLoaded([row(1), row(2)]);
    await screen.findByText('Device 1');
    const [first] = screen.getAllByRole('button', { name: 'appPasswords.revoke' });
    fireEvent.click(first as HTMLElement);

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('appPasswords.confirmRevoke:Device 1');
    expect(apiDelete).not.toHaveBeenCalled();

    apiDelete.mockResolvedValue({} as never);
    fireEvent.click(within(dialog).getByText('appPasswords.revoke'));

    await waitFor(() => expect(screen.queryByText('Device 1')).toBeNull());
    expect(apiDelete).toHaveBeenCalledWith('/user/app-passwords/{appPasswordId}', {
      params: { path: { appPasswordId: 'id-1' } },
    });
    expect(screen.getByText('Device 2')).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith('appPasswords.revoked');
  });

  it('keeps the password when the confirmation is cancelled', async () => {
    await renderLoaded([row(1)]);
    await screen.findByText('Device 1');
    fireEvent.click(screen.getByRole('button', { name: 'appPasswords.revoke' }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByText('common.cancel'));

    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(apiDelete).not.toHaveBeenCalled();
    expect(screen.getByText('Device 1')).toBeInTheDocument();
  });

  it('keeps the row and reports when the revoke fails', async () => {
    await renderLoaded([row(1)]);
    await screen.findByText('Device 1');
    fireEvent.click(screen.getByRole('button', { name: 'appPasswords.revoke' }));
    apiDelete.mockRejectedValue(new ApiError(404, 'gone', 'APP_PASSWORD.NOT_FOUND'));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByText('appPasswords.revoke'));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('apiError.APP_PASSWORD.NOT_FOUND'),
    );
    expect(screen.getByText('Device 1')).toBeInTheDocument();
  });
});
