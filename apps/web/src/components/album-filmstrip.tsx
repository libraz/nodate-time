import { useEffect, useRef } from 'react';
import { useT } from '@/i18n';
import type { AlbumPhoto } from '@/lib/album';

/** How far before the strip's end the next page of the album starts loading. */
const LOAD_AHEAD_MARGIN = '0px 400px 0px 0px';

/**
 * `strip` is the desktop row of square thumbnails with the current one lit;
 * `scrubber` is the phone's slim row in which the current photo widens.
 */
export type AlbumFilmstripVariant = 'strip' | 'scrubber';

const VARIANT_CLASS: Record<
  AlbumFilmstripVariant,
  { nav: string; tile: string; active: string; idle: string }
> = {
  strip: {
    nav: 'gap-1.5 px-4 py-3',
    tile: 'h-14 w-14 rounded-[6px] transition-opacity',
    active: 'opacity-100 ring-2 ring-white',
    idle: 'opacity-50 hover:opacity-85',
  },
  scrubber: {
    // Padded by half the width so the first and last photos can sit centred.
    nav: 'gap-[2px] px-[50%] py-1.5',
    tile: 'h-[42px] rounded-[2px] transition-[width,margin] duration-200',
    active: 'mx-1.5 w-[56px]',
    idle: 'w-[30px]',
  },
};

interface AlbumFilmstripProps {
  variant: AlbumFilmstripVariant;
  photos: AlbumPhoto[];
  activeId: string;
  hasMore: boolean;
  onSelect: (photo: AlbumPhoto) => void;
  onLoadMore: () => void;
  onImageError: (photoId: string) => void;
}

/**
 * A row of thumbnails under the photo, the current one marked and kept in view.
 * Scrolling to its end reads the next page, as the grid does.
 */
export function AlbumFilmstrip({
  variant,
  photos,
  activeId,
  hasMore,
  onSelect,
  onLoadMore,
  onImageError,
}: AlbumFilmstripProps) {
  const t = useT();
  const cls = VARIANT_CLASS[variant];
  const activeRef = useRef<HTMLButtonElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: re-centre whenever the active photo changes
  useEffect(() => {
    const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
    activeRef.current?.scrollIntoView?.({
      block: 'nearest',
      inline: 'center',
      behavior: reduced ? 'auto' : 'smooth',
    });
  }, [activeId]);

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel || !hasMore) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) onLoadMore();
      },
      { root: sentinel.parentElement, rootMargin: LOAD_AHEAD_MARGIN },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, onLoadMore]);

  return (
    <nav
      aria-label={t('album.thumbnails')}
      className={`album-filmstrip flex shrink-0 overflow-x-auto ${cls.nav}`}
    >
      {photos.map((p, i) => {
        const active = p.id === activeId;
        return (
          <button
            key={p.id}
            ref={active ? activeRef : undefined}
            type="button"
            onClick={() => onSelect(p)}
            aria-label={t('album.photoNumber', { n: i + 1 })}
            aria-current={active ? 'true' : undefined}
            className={`album-filmstrip-tile shrink-0 overflow-hidden bg-white/10 ${cls.tile} ${
              active ? cls.active : cls.idle
            }`}
          >
            <img
              src={p.thumbnailUrl ?? p.imageUrl}
              alt=""
              loading="lazy"
              draggable={false}
              onError={() => onImageError(p.id)}
              className="h-full w-full object-cover"
            />
          </button>
        );
      })}
      {hasMore && <div ref={sentinelRef} className="w-px shrink-0" aria-hidden="true" />}
    </nav>
  );
}
