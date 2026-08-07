import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => String(e),
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { rightPanel: string; toggleRightPanel: () => void }) => unknown) =>
    selector({ rightPanel: 'album', toggleRightPanel: () => {} }),
}));

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (
    selector: (s: {
      calendars: { id: string }[];
      activeCalendarIds: string[];
      membersMap: Record<string, never[]>;
    }) => unknown,
  ) => selector({ calendars: [{ id: 'cal-1' }], activeCalendarIds: ['cal-1'], membersMap: {} }),
}));

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: { user: null }) => unknown) => selector({ user: null }),
}));

import { api } from '@/lib/api';
import { AlbumPanel } from './album-panel';

const mockApi = vi.mocked(api);

function photo(imageUrl: string) {
  return {
    id: 'photo-1',
    caption: 'a photo',
    imageUrl,
    createdAt: '2026-04-20T10:00:00+09:00',
    takenAt: '2026-04-20T10:00:00+09:00',
    uploadedBy: { id: 'u1', name: 'User 1' },
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('AlbumPanel', () => {
  // An image URL is signed for a limited time and thumbnails load as they are
  // scrolled to, so one can expire while the panel is open. Nothing reports
  // that as a failed request -- the browser just shows a broken image -- which
  // is why the recovery has to hang off the image's own error.
  it('re-lists the album when an image URL has expired', async () => {
    mockApi.get
      .mockResolvedValueOnce({ items: [photo('https://storage/expired')] })
      .mockResolvedValueOnce({ items: [photo('https://storage/fresh')] });

    render(<AlbumPanel />);

    const image = await screen.findByAltText('a photo');
    expect(image).toHaveAttribute('src', 'https://storage/expired');

    fireEvent.error(image);

    await waitFor(() =>
      expect(screen.getByAltText('a photo')).toHaveAttribute('src', 'https://storage/fresh'),
    );
  });

  // A photo whose object is genuinely gone fails again on the fresh URL, and
  // an unconditional retry would ask for another listing every time.
  it('gives a photo one retry, not an endless loop', async () => {
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/gone')] });

    render(<AlbumPanel />);

    const image = await screen.findByAltText('a photo');
    fireEvent.error(image);
    await waitFor(() => expect(mockApi.get).toHaveBeenCalledTimes(2));

    fireEvent.error(screen.getByAltText('a photo'));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(mockApi.get).toHaveBeenCalledTimes(2);
  });
});
