import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => String(e),
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  rightPanel: 'album' as string | null,
  toggleRightPanel: vi.fn(),
  timezone: 'Asia/Tokyo',
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

/**
 * Mutable so a test can say what the reader is allowed to do. Most of this
 * file reads an album it cannot edit; the event picker only exists for a
 * calendar the reader can write to.
 */
const calendarState = {
  calendars: [{ id: 'cal-1' }] as { id: string; role?: string }[],
  activeCalendarIds: ['cal-1'],
  membersMap: {} as Record<string, never[]>,
  // A photo is offered the events of the day it was taken, so the store has to
  // carry one for the picker to have anything to show.
  events: [
    { id: 'evt-1', calendarId: 'cal-1', title: 'Sports day', startAt: '2026-04-20T09:00:00+09:00' },
  ],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
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
  vi.restoreAllMocks();
  uiState.rightPanel = 'album';
  document.body.style.overflow = '';
});

/** jsdom reports no layout, so focusable elements have to look painted. */
function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

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

/**
 * The album table has carried an event link and a capture time since it was
 * created, and the API accepts both, but nothing on screen could set either --
 * while the README advertised attaching photos to an event as a feature.
 *
 * The picker is offered the events of the day the photo was taken, which is
 * what makes it a list somebody can pick from rather than every event loaded.
 */
describe('AlbumPanel event link', () => {
  beforeEach(() => {
    calendarState.calendars = [{ id: 'cal-1', role: 'owner' }];
  });

  afterEach(() => {
    calendarState.calendars = [{ id: 'cal-1' }];
  });

  it('offers the events of the day the photo was taken', async () => {
    mockApi.get.mockReset();
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });

    render(<AlbumPanel />);
    fireEvent.click(await screen.findByAltText('a photo'));

    // The dropdown renders through a portal, so it is queried from the
    // document rather than from the render container.
    const trigger = await screen.findByText('album.noEvent');
    fireEvent.click(trigger);

    expect(await screen.findByText('Sports day')).toBeInTheDocument();
  });

  it('attaches the photo to the event that was chosen', async () => {
    mockApi.get.mockReset();
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });
    mockApi.put.mockResolvedValue({ ...photo('https://storage/one'), eventId: 'evt-1' });

    render(<AlbumPanel />);
    fireEvent.click(await screen.findByAltText('a photo'));
    fireEvent.click(await screen.findByText('album.noEvent'));
    fireEvent.click(await screen.findByText('Sports day'));

    await waitFor(() =>
      expect(mockApi.put).toHaveBeenCalledWith('/calendars/cal-1/albums/photo-1', {
        caption: 'a photo',
        eventId: 'evt-1',
      }),
    );
  });

  it('detaches a photo without deleting it', async () => {
    mockApi.get.mockReset();
    mockApi.get.mockResolvedValue({
      items: [{ ...photo('https://storage/one'), eventId: 'evt-1' }],
    });
    mockApi.put.mockResolvedValue({ ...photo('https://storage/one'), eventId: '' });

    render(<AlbumPanel />);
    fireEvent.click(await screen.findByAltText('a photo'));
    fireEvent.click(await screen.findByText('Sports day'));
    fireEvent.click(await screen.findByText('album.noEvent'));

    // An empty string is how the API is told to clear the link; omitting the
    // field would leave the photo attached.
    await waitFor(() =>
      expect(mockApi.put).toHaveBeenCalledWith('/calendars/cal-1/albums/photo-1', {
        caption: 'a photo',
        eventId: '',
      }),
    );
  });
});

// The panel covers the page but had none of what that owes the keyboard: Tab
// walked into the calendar behind it and Escape did nothing.
describe('AlbumPanel keyboard', () => {
  it('announces itself as a modal dialog', async () => {
    mockApi.get.mockResolvedValue({ items: [] });

    render(<AlbumPanel />);

    const panel = await screen.findByRole('dialog', { name: 'panel.album' });
    expect(panel).toHaveAttribute('aria-modal', 'true');
  });

  it('locks the page behind it and closes on Escape', async () => {
    mockApi.get.mockResolvedValue({ items: [] });

    render(<AlbumPanel />);

    expect(document.body.style.overflow).toBe('hidden');

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(uiState.toggleRightPanel).toHaveBeenCalledWith('album');
  });

  it('keeps Tab inside the panel', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    mockApi.get.mockResolvedValue({
      items: [photo('https://storage/one')],
      nextCursor: 'c1',
    });

    render(<AlbumPanel />);

    const panel = await screen.findByRole('dialog', { name: 'panel.album' });
    await screen.findByText('album.loadMore');
    const focusable = Array.from(panel.querySelectorAll<HTMLElement>('button,input'));
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    expect(focusable.length).toBeGreaterThan(1);
    if (!first || !last) return;

    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it('returns focus to whatever opened it', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    mockApi.get.mockResolvedValue({ items: [] });
    uiState.rightPanel = null;

    function Harness() {
      return (
        <>
          <button type="button" data-testid="trigger">
            open
          </button>
          <AlbumPanel />
        </>
      );
    }
    const { rerender } = render(<Harness />);
    const trigger = screen.getByTestId('trigger');
    trigger.focus();

    uiState.rightPanel = 'album';
    rerender(<Harness />);
    await waitFor(() => expect(document.activeElement).not.toBe(trigger));

    uiState.rightPanel = null;
    rerender(<Harness />);

    expect(document.activeElement).toBe(trigger);
  });
});

/**
 * The lightbox is a dialog over the panel, so it carries its own trap: the
 * question it puts on screen is the one the keyboard should be answering.
 *
 * Its event picker portals its dropdown to the body, which puts the open list
 * outside the lightbox's own subtree on purpose. The trap only intercepts Tab
 * at the edges of its container, so a portalled list keeps the keyboard; and
 * Escape is answered by the innermost open surface, which is the list.
 */
describe('AlbumPanel lightbox keyboard', () => {
  beforeEach(() => {
    calendarState.calendars = [{ id: 'cal-1', role: 'owner' }];
  });

  afterEach(() => {
    calendarState.calendars = [{ id: 'cal-1' }];
  });

  it('announces itself as a modal dialog of its own', async () => {
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });

    render(<AlbumPanel />);
    fireEvent.click(await screen.findByAltText('a photo'));

    const lightbox = await screen.findByRole('dialog', { name: 'album.photo' });
    expect(lightbox).toHaveAttribute('aria-modal', 'true');
  });

  it('closes on Escape and leaves the panel behind it open', async () => {
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });

    render(<AlbumPanel />);
    fireEvent.click(await screen.findByAltText('a photo'));
    await screen.findByRole('dialog', { name: 'album.photo' });

    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'album.photo' })).not.toBeInTheDocument(),
    );
    expect(screen.getByRole('dialog', { name: 'panel.album' })).toBeInTheDocument();
    expect(uiState.toggleRightPanel).not.toHaveBeenCalled();
  });

  it('keeps Tab inside the lightbox', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });

    render(<AlbumPanel />);
    fireEvent.click(await screen.findByAltText('a photo'));

    const lightbox = await screen.findByRole('dialog', { name: 'album.photo' });
    const focusable = Array.from(lightbox.querySelectorAll<HTMLElement>('button,input'));
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    expect(focusable.length).toBeGreaterThan(1);
    if (!first || !last) return;

    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it('returns focus to the thumbnail that opened it', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });

    render(<AlbumPanel />);
    const thumbnail = (await screen.findByAltText('a photo')).closest('button');
    expect(thumbnail).not.toBeNull();
    if (!thumbnail) return;
    thumbnail.focus();
    fireEvent.click(thumbnail);

    await screen.findByRole('dialog', { name: 'album.photo' });
    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => expect(document.activeElement).toBe(thumbnail));
  });

  it('hands Escape and the keyboard to an open event picker, not to itself', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    mockApi.get.mockResolvedValue({ items: [photo('https://storage/one')] });

    render(<AlbumPanel />);
    fireEvent.click(await screen.findByAltText('a photo'));
    fireEvent.click(await screen.findByText('album.noEvent'));

    const option = await screen.findByText('Sports day');
    // The list is portalled to the body, so it sits outside the lightbox's own
    // subtree. Tab there must be left alone rather than pulled back inside.
    const optionButton = option.closest('button');
    expect(optionButton).not.toBeNull();
    if (!optionButton) return;
    optionButton.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(optionButton);

    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => expect(screen.queryByText('Sports day')).not.toBeInTheDocument());
    expect(screen.getByRole('dialog', { name: 'album.photo' })).toBeInTheDocument();
  });
});
