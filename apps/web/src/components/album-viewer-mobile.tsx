import { useState } from 'react';
import { AlbumFilmstrip } from '@/components/album-filmstrip';
import {
  ChevronIcon,
  CloseIcon,
  DownloadIcon,
  InfoIcon,
  TrashIcon,
} from '@/components/album-viewer-icons';
import type { AlbumViewerLayoutProps } from '@/components/album-viewer-layout';
import { useT } from '@/i18n';
import type { AlbumPhoto } from '@/lib/album';
import { PAGE_GAP_PX, SETTLE_MS, usePhotoGestures } from '@/lib/use-photo-gestures';

/** Downward drag over which the backdrop fades out entirely. */
const DISMISS_FADE_PX = 360;
/** Smallest a photo shrinks to while it is dragged away. */
const DISMISS_MIN_SCALE = 0.7;

/**
 * The phone viewer, modelled on a camera roll: the neighbouring photos ride
 * along as one is dragged sideways, dragging down carries the photo off as
 * the backdrop fades, pinch or double tap zooms, and a tap hides the bars.
 * A scrubber of the album sits above the toolbar; swiping up opens the details.
 */
export function AlbumViewerMobile({
  photos,
  photo,
  nav,
  hasMore,
  moment,
  editable,
  infoOpen,
  setInfoOpen,
  details,
  onClose,
  onDownload,
  onRequestDelete,
  onLoadMore,
  onImageError,
}: AlbumViewerLayoutProps) {
  const t = useT();
  const [chromeHidden, setChromeHidden] = useState(false);
  const { prev, next, goTo } = nav;

  const gestures = usePhotoGestures({
    photoKey: photo.id,
    canPrev: Boolean(prev),
    canNext: Boolean(next),
    dismissable: !infoOpen,
    onPage: (direction) => goTo(direction === 1 ? next : prev),
    onDismiss: onClose,
    onSwipeUp: () => {
      setInfoOpen(true);
      setChromeHidden(false);
    },
    onSwipeDown: () => setInfoOpen(false),
    onTap: () => {
      if (infoOpen) setInfoOpen(false);
      else setChromeHidden((v) => !v);
    },
  });
  const { pageX, dismiss, zoom, settling } = gestures;
  const dismissing = dismiss.y > 0;
  const transition = settling ? `transform ${SETTLE_MS}ms var(--ease-out)` : 'none';

  const slots: { item: AlbumPhoto; pos: -1 | 0 | 1 }[] = [
    ...(prev ? [{ item: prev, pos: -1 as const }] : []),
    { item: photo, pos: 0 },
    ...(next ? [{ item: next, pos: 1 as const }] : []),
  ];

  const currentTransform = dismissing
    ? `translate3d(${dismiss.x}px, ${dismiss.y}px, 0) scale(${Math.max(
        DISMISS_MIN_SCALE,
        1 - dismiss.y / (DISMISS_FADE_PX * 3),
      )})`
    : zoom.scale !== 1 || zoom.x || zoom.y
      ? `translate3d(${zoom.x}px, ${zoom.y}px, 0) scale(${zoom.scale})`
      : undefined;

  const hideChrome = chromeHidden || dismissing || zoom.scale > 1;
  const chromeClass = `transition-opacity duration-200 ${
    hideChrome ? 'pointer-events-none opacity-0' : 'opacity-100'
  }`;

  return (
    <>
      <div
        className="absolute inset-0 bg-black"
        style={{
          opacity: 1 - Math.min(1, dismiss.y / DISMISS_FADE_PX),
          transition: settling ? `opacity ${SETTLE_MS}ms var(--ease-out)` : 'none',
        }}
        aria-hidden="true"
      />

      {/* The photo track */}
      <div
        ref={gestures.stageRef}
        className="relative flex-1 touch-none select-none overflow-hidden"
        {...gestures.handlers}
      >
        <div
          className="absolute inset-0"
          style={{ transform: `translate3d(${pageX}px, 0, 0)`, transition }}
        >
          {slots.map(({ item, pos }) => (
            <div
              key={item.id}
              className="absolute inset-0 flex items-center justify-center"
              style={{
                transform: pos
                  ? `translateX(calc(${pos * 100}% + ${pos * PAGE_GAP_PX}px))`
                  : undefined,
              }}
              aria-hidden={pos !== 0}
            >
              <img
                src={item.imageUrl}
                alt={pos === 0 ? item.caption : ''}
                draggable={false}
                onError={() => onImageError(item.id)}
                className="max-h-full max-w-full object-contain will-change-transform"
                style={pos === 0 ? { transform: currentTransform, transition } : undefined}
              />
            </div>
          ))}
        </div>
      </div>

      {/* Top bar: back, and when the photo was taken */}
      <div
        className={`album-viewer-bar absolute inset-x-0 top-0 z-10 flex items-center gap-2 px-2 pt-[max(env(safe-area-inset-top),0.5rem)] pb-2 ${chromeClass}`}
        aria-hidden={hideChrome}
      >
        <button
          type="button"
          onClick={onClose}
          aria-label={t('common.close')}
          className="album-viewer-btn h-10 w-10"
        >
          <ChevronIcon direction="left" size={26} />
        </button>
        <div className="min-w-0 flex-1 text-center">
          <p className="truncate text-callout font-semibold">{moment.date}</p>
          <p className="truncate text-caption text-white/70 tabular-nums">{moment.time}</p>
        </div>
        <span className="h-10 w-10 shrink-0" aria-hidden="true" />
      </div>

      {/* Details sheet */}
      {infoOpen && (
        <section
          aria-label={t('album.info')}
          className="album-viewer-sheet absolute inset-x-0 bottom-0 z-20 mx-auto max-h-[70vh] w-full max-w-[560px] overflow-y-auto rounded-t-[var(--radius-lg)] bg-neutral-900/95 px-5 pt-3 pb-[max(env(safe-area-inset-bottom),1.25rem)] backdrop-blur-xl"
        >
          <div className="mx-auto mb-3 h-1 w-9 rounded-full bg-white/25" aria-hidden="true" />
          <div className="flex items-center justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate text-callout font-semibold">{moment.date}</p>
              <p className="text-caption text-white/60 tabular-nums">{moment.time}</p>
            </div>
            <button
              type="button"
              onClick={() => setInfoOpen(false)}
              aria-label={t('common.close')}
              className="album-viewer-btn h-8 w-8"
            >
              <CloseIcon size={16} />
            </button>
          </div>
          <div className="mt-4">{details}</div>
        </section>
      )}

      {/* Bottom: scrubber over the toolbar */}
      <div
        className={`album-viewer-bar absolute inset-x-0 bottom-0 z-10 pb-[max(env(safe-area-inset-bottom),0.5rem)] ${chromeClass}`}
        aria-hidden={hideChrome}
      >
        {photos.length > 1 && (
          <AlbumFilmstrip
            variant="scrubber"
            photos={photos}
            activeId={photo.id}
            hasMore={hasMore}
            onSelect={goTo}
            onLoadMore={onLoadMore}
            onImageError={onImageError}
          />
        )}
        <div className="mx-auto flex max-w-[420px] items-center justify-between px-4 pt-1">
          <button
            type="button"
            onClick={onDownload}
            aria-label={t('album.download')}
            title={t('album.download')}
            className="album-viewer-btn h-11 w-11"
          >
            <DownloadIcon />
          </button>
          <button
            type="button"
            onClick={() => setInfoOpen(!infoOpen)}
            aria-label={t('album.info')}
            aria-pressed={infoOpen}
            title={t('album.info')}
            className="album-viewer-btn h-11 w-11"
          >
            <InfoIcon filled={infoOpen} />
          </button>
          {editable ? (
            <button
              type="button"
              onClick={onRequestDelete}
              aria-label={t('common.delete')}
              title={t('common.delete')}
              className="album-viewer-btn h-11 w-11"
            >
              <TrashIcon />
            </button>
          ) : (
            <span className="h-11 w-11" aria-hidden="true" />
          )}
        </div>
      </div>
    </>
  );
}
