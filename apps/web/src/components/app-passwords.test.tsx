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
  fireEvent.click(screen.getByRole('radio', { name: 'appPasswords.targetManual' }));
  fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: name } });
  fireEvent.click(screen.getByRole('button', { name: 'appPasswords.actionManual' }));
}

/** The submit button of the default target on a browser that is no Apple device. */
const showQr = () => screen.getByRole('button', { name: 'appPasswords.actionQr' });

/** Pretends the browser runs on the device the user agent names. */
function onDevice(userAgent: string, maxTouchPoints = 0) {
  vi.spyOn(window.navigator, 'userAgent', 'get').mockReturnValue(userAgent);
  Object.defineProperty(window.navigator, 'maxTouchPoints', {
    value: maxTouchPoints,
    configurable: true,
  });
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  apiDelete.mockReset();
  vi.mocked(copyText).mockReset();
  vi.mocked(toast.error).mockReset();
  vi.mocked(toast.success).mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  Object.defineProperty(window.navigator, 'maxTouchPoints', { value: 0, configurable: true });
});

describe('AppPasswords list', () => {
  it('shows name, last sync and when each device was added, without the secret prefix', async () => {
    await renderLoaded([row(1), row(2, { lastUsedAt: '2026-11-20T10:00:00Z' })]);
    await screen.findByText('Device 1');
    expect(screen.queryByText(/ndt_AB01/)).toBeNull();
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
    expect(showQr()).toBeDisabled();

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
    expect(screen.getByText(created.name)).toBeInTheDocument();

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
    expect(showQr()).toBeEnabled();
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: '  ' } });
    expect(showQr()).toBeDisabled();
  });

  it('disables create at the cap and says why', async () => {
    await renderLoaded(Array.from({ length: 20 }, (_, i) => row(i + 1)));
    await screen.findByText('Device 20');
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: 'x' } });
    expect(showQr()).toBeDisabled();
    expect(screen.getByText('appPasswords.limitReached:20')).toBeInTheDocument();
  });

  it('allows create just under the cap', async () => {
    await renderLoaded(Array.from({ length: 19 }, (_, i) => row(i + 1)));
    await screen.findByText('Device 19');
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: 'x' } });
    expect(showQr()).toBeEnabled();
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

describe('AppPasswords profile', () => {
  const link = {
    url: 'https://time.example.com/api/auth/app-password-profiles/abc.def',
    expiresAt: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
  };

  function profileNamed(name: string) {
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), { target: { value: name } });
    fireEvent.click(showQr());
  }

  it('shows a QR code for an iPhone set up from a computer, with no link to open here', async () => {
    await renderLoaded([]);
    apiPost.mockResolvedValue({ data: link } as never);
    profileNamed('  iPhone ');

    const qr = await screen.findByRole('img', { name: 'appPasswords.profileQrLabel' });
    expect(apiPost).toHaveBeenCalledWith('/user/app-passwords/profile-links', {
      body: { name: 'iPhone' },
    });
    expect(screen.queryByRole('link', { name: 'appPasswords.profileOpen' })).toBeNull();
    expect(screen.getByText('appPasswords.profileStepScan')).toBeInTheDocument();
    expect(screen.getByText('appPasswords.profileStepInstallIos')).toBeInTheDocument();
    expect(qr.tagName.toLowerCase()).toBe('svg');
    expect(qr.querySelectorAll('rect').length).toBeGreaterThan(100);
    expect(screen.getByText('appPasswords.profileWarning')).toBeInTheDocument();
    expect(screen.getByText(/appPasswords\.profileRemaining/)).toBeInTheDocument();
    expect(screen.getByText('appPasswords.profileWaiting')).toBeInTheDocument();
    // No password is made or shown on this path.
    expect(apiPost).not.toHaveBeenCalledWith('/user/app-passwords', expect.anything());
    expect(screen.queryByText('appPasswords.createdTitle')).toBeNull();
  });

  it.each(['APP_PASSWORD.LIMIT_REACHED', 'APP_PASSWORD.MFA_ENABLED'])(
    'shows the reason for a refused link %s',
    async (code) => {
      await renderLoaded([]);
      apiPost.mockRejectedValue(new ApiError(403, 'forbidden', code));
      profileNamed('iPhone');

      expect(await screen.findByRole('alert')).toHaveTextContent(`apiError.${code}`);
      expect(screen.queryByText('appPasswords.profileWaiting')).toBeNull();
    },
  );

  it('reloads the list when the card is closed, so the row made on first open shows', async () => {
    await renderLoaded([]);
    apiPost.mockResolvedValue({ data: link } as never);
    profileNamed('iPhone');
    await screen.findByText('appPasswords.profileWaiting');
    expect(apiGet).toHaveBeenCalledTimes(1);

    listing([row(1, { name: 'iPhone' })]);
    fireEvent.click(screen.getByRole('button', { name: 'common.close' }));

    expect(await screen.findByText('iPhone')).toBeInTheDocument();
    expect(apiGet).toHaveBeenCalledTimes(2);
    expect(screen.queryByText('appPasswords.profileWaiting')).toBeNull();
  });
});

describe('AppPasswords profile progress', () => {
  const link = {
    url: 'https://time.example.com/api/auth/app-password-profiles/abc.def',
    expiresAt: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
  };

  afterEach(() => vi.useRealTimers());

  it('follows the device from waiting to opened to synced', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    await renderLoaded([row(1, { name: 'iPhone' })]);
    apiPost.mockResolvedValue({ data: link } as never);
    fireEvent.click(showQr());
    await screen.findByText('appPasswords.profileWaiting');

    // An older row with the same name is not the device this link set up.
    listing([row(2, { name: 'iPhone' }), row(1, { name: 'iPhone' })]);
    await vi.advanceTimersByTimeAsync(4000);
    expect(await screen.findByText('appPasswords.profileOpened')).toBeInTheDocument();

    listing([
      row(2, { name: 'iPhone', lastUsedAt: new Date().toISOString() }),
      row(1, { name: 'iPhone' }),
    ]);
    await vi.advanceTimersByTimeAsync(4000);
    expect(await screen.findByText('appPasswords.profileSynced')).toBeInTheDocument();
    expect(screen.queryByText('appPasswords.profileWarning')).toBeNull();
  });

  it('offers a new link once this one has expired unopened', async () => {
    await renderLoaded([]);
    apiPost.mockResolvedValue({
      data: { ...link, expiresAt: new Date(Date.now() - 1000).toISOString() },
    } as never);
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), {
      target: { value: 'iPad' },
    });
    fireEvent.click(showQr());

    expect(await screen.findByText('appPasswords.profileExpired')).toBeInTheDocument();
    expect(screen.queryByText('appPasswords.profileWaiting')).toBeNull();

    apiPost.mockResolvedValue({ data: link } as never);
    fireEvent.click(screen.getByRole('button', { name: 'appPasswords.profileRecreate' }));
    expect(await screen.findByText('appPasswords.profileWaiting')).toBeInTheDocument();
    expect(apiPost).toHaveBeenLastCalledWith('/user/app-passwords/profile-links', {
      body: { name: 'iPad' },
    });
  });
});

describe('AppPasswords on each device', () => {
  const link = {
    url: 'https://time.example.com/api/auth/app-password-profiles/abc.def',
    expiresAt: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
  };
  const targets = () => screen.getAllByRole('radio').map((r) => r.textContent);

  it('sets up an iPhone on the iPhone itself, with no QR code', async () => {
    onDevice('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15');
    await renderLoaded([]);
    expect(targets()).toEqual(['appPasswords.targetHere:iPhone', 'appPasswords.targetManual']);
    expect(screen.getByLabelText('appPasswords.nameLabel')).toHaveValue('iPhone');

    apiPost.mockResolvedValue({ data: link } as never);
    fireEvent.click(screen.getByRole('button', { name: 'appPasswords.actionHere:iPhone' }));
    const open = await screen.findByRole('link', { name: 'appPasswords.profileOpen' });
    expect(open).toHaveAttribute('href', link.url);
    expect(screen.queryByRole('img', { name: 'appPasswords.profileQrLabel' })).toBeNull();
    expect(screen.getByText('appPasswords.profileStepOpenHere')).toBeInTheDocument();
    expect(screen.getByText('appPasswords.profileStepInstallIos')).toBeInTheDocument();
  });

  it('tells an iPad from the Mac it claims to be', async () => {
    onDevice('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15', 5);
    await renderLoaded([]);
    expect(targets()[0]).toBe('appPasswords.targetHere:iPad');
  });

  it('sets up this Mac by link, and an iPhone from it by QR code', async () => {
    onDevice('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15');
    await renderLoaded([]);
    expect(targets()).toEqual([
      'appPasswords.targetHere:Mac',
      'appPasswords.targetQr',
      'appPasswords.targetManual',
    ]);
    apiPost.mockResolvedValue({ data: link } as never);
    fireEvent.click(screen.getByRole('button', { name: 'appPasswords.actionHere:Mac' }));
    await screen.findByRole('link', { name: 'appPasswords.profileOpen' });
    expect(screen.getByText('appPasswords.profileStepOpenMac')).toBeInTheDocument();
    expect(screen.getByText('appPasswords.profileStepInstallMac')).toBeInTheDocument();
    expect(screen.queryByRole('img', { name: 'appPasswords.profileQrLabel' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'common.close' }));

    fireEvent.click(await screen.findByRole('radio', { name: 'appPasswords.targetQr' }));
    expect(screen.getByLabelText('appPasswords.nameLabel')).toHaveValue('iPhone');
    fireEvent.click(showQr());
    expect(
      await screen.findByRole('img', { name: 'appPasswords.profileQrLabel' }),
    ).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'appPasswords.profileOpen' })).toBeNull();
  });

  it('defaults to a manual password on Android', async () => {
    onDevice('Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Mobile');
    await renderLoaded([]);
    expect(targets()).toEqual(['appPasswords.targetHere:Android', 'appPasswords.targetQr']);
    expect(screen.getByLabelText('appPasswords.nameLabel')).toHaveValue('Android');

    apiPost.mockResolvedValue({ data: { ...created, name: 'Android' } } as never);
    fireEvent.click(screen.getByRole('button', { name: 'appPasswords.actionManual' }));
    expect(await screen.findByText('appPasswords.createdTitle')).toBeInTheDocument();
    expect(apiPost).toHaveBeenCalledWith('/user/app-passwords', { body: { name: 'Android' } });
  });

  it('sends a Mac to the Mac instead of making a link it cannot use', async () => {
    await renderLoaded([]);
    fireEvent.click(screen.getByRole('radio', { name: 'appPasswords.targetMac' }));
    expect(screen.getByText('appPasswords.hintMac')).toBeInTheDocument();
    expect(screen.queryByLabelText('appPasswords.nameLabel')).toBeNull();
    expect(screen.queryByRole('button', { name: 'appPasswords.actionQr' })).toBeNull();
  });

  it('keeps a typed name when the target changes', async () => {
    await renderLoaded([]);
    fireEvent.change(screen.getByLabelText('appPasswords.nameLabel'), {
      target: { value: 'Work phone' },
    });
    fireEvent.click(screen.getByRole('radio', { name: 'appPasswords.targetManual' }));
    expect(screen.getByLabelText('appPasswords.nameLabel')).toHaveValue('Work phone');
  });
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
