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

function photo(imageUrl: string, id = 'photo-1') {
  return {
    id,
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

  // The signature expires for every page on screen, not just the first, so a
  // reader who paged further down and then came back would be left with half
  // the album on dead URLs if only the first page were re-listed.
  it('re-lists every page on screen, not only the first', async () => {
    mockApi.get.mockReset();
    mockApi.get
      .mockResolvedValueOnce({
        items: [photo('https://storage/one-expired', 'photo-1')],
        nextCursor: 'c1',
      })
      .mockResolvedValueOnce({ items: [photo('https://storage/two-expired', 'photo-2')] })
      .mockResolvedValueOnce({
        items: [photo('https://storage/one-fresh', 'photo-1')],
        nextCursor: 'c1',
      })
      .mockResolvedValueOnce({ items: [photo('https://storage/two-fresh', 'photo-2')] });

    render(<AlbumPanel />);

    fireEvent.click(await screen.findByText('album.loadMore'));
    await waitFor(() => expect(screen.getAllByAltText('a photo')).toHaveLength(2));

    fireEvent.error(screen.getAllByAltText('a photo')[0] as HTMLElement);

    await waitFor(() =>
      expect(screen.getAllByAltText('a photo').map((img) => img.getAttribute('src'))).toEqual([
        'https://storage/one-fresh',
        'https://storage/two-fresh',
      ]),
    );
    expect(mockApi.get).toHaveBeenNthCalledWith(3, '/calendars/cal-1/albums');
    expect(mockApi.get).toHaveBeenNthCalledWith(4, '/calendars/cal-1/albums?cursor=c1');
  });

  // The refresh runs behind a broken image the reader never asked about, so a
  // failure there is not theirs to see -- and it must not take the album they
  // are looking at down with it.
  it('leaves the album on screen when the refresh itself fails', async () => {
    mockApi.get.mockReset();
    mockApi.get
      .mockResolvedValueOnce({ items: [photo('https://storage/expired')] })
      .mockRejectedValueOnce(new Error('listing failed'));

    render(<AlbumPanel />);

    fireEvent.error(await screen.findByAltText('a photo'));

    await waitFor(() => expect(mockApi.get).toHaveBeenCalledTimes(2));
    expect(screen.getByAltText('a photo')).toHaveAttribute('src', 'https://storage/expired');
    expect(screen.queryByText('Error: listing failed')).not.toBeInTheDocument();
  });

  // A listing that fails leaves the grid empty, which on its own reads as an
  // album nobody has put anything in yet.
  it('says why the album could not be listed', async () => {
    mockApi.get.mockReset();
    mockApi.get.mockRejectedValue(new Error('album unavailable'));

    render(<AlbumPanel />);

    expect(await screen.findByText('Error: album unavailable')).toBeInTheDocument();
    expect(screen.getByText('panel.noPhotos')).toBeInTheDocument();
  });

  it('shows the empty state for an album with no photos', async () => {
    mockApi.get.mockReset();
    mockApi.get.mockResolvedValue({ items: [] });

    render(<AlbumPanel />);

    expect(await screen.findByText('panel.noPhotos')).toBeInTheDocument();
  });

  it('opens a photo full size and closes it again', async () => {
    mockApi.get.mockReset();
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });

    render(<AlbumPanel />);

    fireEvent.click(await screen.findByAltText('a photo'));
    await waitFor(() => expect(screen.getAllByAltText('a photo')).toHaveLength(2));
    // A viewer without edit rights reads the caption rather than editing it.
    expect(screen.getByText('a photo')).toBeInTheDocument();
    expect(screen.queryByText('album.saveCaption')).not.toBeInTheDocument();

    const closers = screen.getAllByLabelText('common.close');
    fireEvent.click(closers[closers.length - 1] as HTMLElement);

    await waitFor(() => expect(screen.getAllByAltText('a photo')).toHaveLength(1));
  });
});
