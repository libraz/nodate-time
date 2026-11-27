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

/**
 * The desktop gallery: a toolbar over the photo, arrows beside it, a
 * filmstrip of the album beneath, and the details in a sidebar that stays
 * open while the reader pages through.
 */
export function AlbumViewerDesktop({
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
  const { index, prev, next, goTo } = nav;

  return (
    <div className="flex min-h-0 flex-1 bg-black">
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-16 shrink-0 items-center gap-4 px-5">
          <div className="min-w-0 flex-1">
            <p className="truncate text-callout font-semibold">{moment.date}</p>
            <p className="truncate text-caption text-white/60 tabular-nums">
              {moment.time}
              <span className="mx-2 text-white/30" aria-hidden="true">
                ·
              </span>
              {index + 1} / {photos.length}
              {hasMore && '+'}
            </p>
          </div>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={onDownload}
              aria-label={t('album.download')}
              title={t('album.download')}
              className="album-viewer-btn h-10 w-10"
            >
              <DownloadIcon size={20} />
            </button>
            <button
              type="button"
              onClick={() => setInfoOpen(!infoOpen)}
              aria-label={t('album.info')}
              aria-pressed={infoOpen}
              title={t('album.info')}
              className="album-viewer-btn h-10 w-10"
            >
              <InfoIcon filled={infoOpen} size={20} />
            </button>
            {editable && (
              <button
                type="button"
                onClick={onRequestDelete}
                aria-label={t('common.delete')}
                title={t('common.delete')}
                className="album-viewer-btn h-10 w-10"
              >
                <TrashIcon size={20} />
              </button>
            )}
            <span className="mx-1.5 h-5 w-px bg-white/15" aria-hidden="true" />
            <button
              type="button"
              onClick={onClose}
              aria-label={t('common.close')}
              title={t('common.close')}
              className="album-viewer-btn h-10 w-10"
            >
              <CloseIcon />
            </button>
          </div>
        </header>

        <div className="relative flex min-h-0 flex-1 select-none items-center justify-center px-20">
          <img
            key={photo.id}
            src={photo.imageUrl}
            alt={photo.caption}
            draggable={false}
            onError={() => onImageError(photo.id)}
            className="album-viewer-image max-h-full max-w-full rounded-[var(--radius-sm)] object-contain"
          />
          {prev && (
            <button
              type="button"
              onClick={() => goTo(prev)}
              aria-label={t('album.prev')}
              title={t('album.prev')}
              className="album-viewer-btn album-viewer-nav left-5"
            >
              <ChevronIcon direction="left" />
            </button>
          )}
          {next && (
            <button
              type="button"
              onClick={() => goTo(next)}
              aria-label={t('album.next')}
              title={t('album.next')}
              className="album-viewer-btn album-viewer-nav right-5"
            >
              <ChevronIcon direction="right" />
            </button>
          )}
        </div>

        <AlbumFilmstrip
          variant="strip"
          photos={photos}
          activeId={photo.id}
          hasMore={hasMore}
          onSelect={goTo}
          onLoadMore={onLoadMore}
          onImageError={onImageError}
        />
      </div>

      {infoOpen && (
        <aside
          aria-label={t('album.info')}
          className="album-viewer-aside flex w-[340px] shrink-0 flex-col overflow-y-auto border-l border-white/10 bg-neutral-900 px-5 pb-5"
        >
          <div className="flex h-16 shrink-0 items-center justify-between gap-3">
            <h2 className="text-callout font-semibold">{t('album.info')}</h2>
            <button
              type="button"
              onClick={() => setInfoOpen(false)}
              aria-label={t('common.close')}
              className="album-viewer-btn h-8 w-8"
            >
              <CloseIcon size={16} />
            </button>
          </div>
          {details}
        </aside>
      )}
    </div>
  );
}
