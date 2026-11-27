import { useCallback, useEffect } from 'react';
import type { AlbumPhoto } from '@/lib/album';

/** Pages left before the end at which the next page of the album is fetched. */
const PREFETCH_REMAINING = 3;

interface AlbumNavigationOptions {
  photos: AlbumPhoto[];
  photoId: string;
  hasMore: boolean;
  onLoadMore: () => void;
  onNavigate: (photoId: string) => void;
  /** Off while a surface above the viewer owns the arrow keys. */
  keyboard: boolean;
}

export interface AlbumNavigation {
  index: number;
  photo: AlbumPhoto | undefined;
  prev: AlbumPhoto | undefined;
  next: AlbumPhoto | undefined;
  goTo: (target: AlbumPhoto | undefined) => void;
}

/**
 * Where the viewer is in the album and how it moves: the arrow keys page, and
 * the next page of the album is read before the reader runs out of photos.
 */
export function useAlbumNavigation({
  photos,
  photoId,
  hasMore,
  onLoadMore,
  onNavigate,
  keyboard,
}: AlbumNavigationOptions): AlbumNavigation {
  const index = photos.findIndex((p) => p.id === photoId);
  const photo = photos[index];
  const prev = index > 0 ? photos[index - 1] : undefined;
  const next = index >= 0 ? photos[index + 1] : undefined;

  useEffect(() => {
    if (hasMore && index >= 0 && photos.length - index <= PREFETCH_REMAINING) onLoadMore();
  }, [hasMore, index, photos.length, onLoadMore]);

  const goTo = useCallback(
    (target: AlbumPhoto | undefined) => {
      if (target) onNavigate(target.id);
    },
    [onNavigate],
  );

  useEffect(() => {
    if (!keyboard) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
      const el = e.target;
      if (el instanceof Element && el.closest('input,textarea,[contenteditable="true"]')) return;
      // A portalled event picker list is open and takes the arrows itself.
      if (document.querySelector('.dropdown-panel')) return;
      e.preventDefault();
      goTo(e.key === 'ArrowLeft' ? prev : next);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [keyboard, goTo, prev, next]);

  return { index, photo, prev, next, goTo };
}
