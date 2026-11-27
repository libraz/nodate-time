import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { AlbumPhotoDetails } from '@/components/album-photo-details';
import { AlbumViewerDesktop } from '@/components/album-viewer-desktop';
import type { AlbumViewerLayoutProps } from '@/components/album-viewer-layout';
import { AlbumViewerMobile } from '@/components/album-viewer-mobile';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { useT } from '@/i18n';
import { type AlbumPhoto, formatAlbumMoment } from '@/lib/album';
import { useAlbumNavigation } from '@/lib/use-album-navigation';
import { useIsDesktop } from '@/lib/use-is-desktop';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useUiStore } from '@/stores/ui-store';

interface AlbumViewerProps {
  photos: AlbumPhoto[];
  photoId: string;
  calendarId: string;
  calendarName: string;
  editable: boolean;
  hasMore: boolean;
  onLoadMore: () => void;
  onNavigate: (photoId: string) => void;
  onClose: () => void;
  onImageError: (photoId: string) => void;
  onDownload: (photo: AlbumPhoto) => void;
  onDelete: (photoId: string) => Promise<boolean>;
  onSaveCaption: (photoId: string, caption: string) => Promise<void>;
  onLinkEvent: (photoId: string, eventId: string) => Promise<void>;
}

/**
 * The full-screen photo viewer. It owns where the reader is, the details
 * toggle and the delete flow; the phone and desktop layouts only arrange them.
 * On a phone the details are a sheet over the photo, on a desktop a sidebar
 * beside it that starts open.
 */
export function AlbumViewer({
  photos,
  photoId,
  calendarId,
  calendarName,
  editable,
  hasMore,
  onLoadMore,
  onNavigate,
  onClose,
  onImageError,
  onDownload,
  onDelete,
  onSaveCaption,
  onLinkEvent,
}: AlbumViewerProps) {
  const t = useT();
  const timezone = useUiStore((s) => s.timezone);
  const locale = useUiStore((s) => s.locale);
  const isDesktop = useIsDesktop();
  const [infoOpen, setInfoOpen] = useState(isDesktop);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => setInfoOpen(isDesktop), [isDesktop]);

  const nav = useAlbumNavigation({
    photos,
    photoId,
    hasMore,
    onLoadMore,
    onNavigate,
    keyboard: !confirmingDelete,
  });
  const { photo, prev, next } = nav;

  // The innermost open surface owns the keyboard: a confirmation first, then
  // the phone's details sheet, then the viewer. The desktop sidebar sits
  // beside the photo rather than over it, so Escape passes it by. The event
  // picker portals its list out of this subtree and closes itself on Escape,
  // so the viewer stands down while one is open -- asked for by class name,
  // since a portalled list is not in this subtree to ask any other way.
  const dismiss = () => {
    if (document.querySelector('.dropdown-panel')) return;
    if (confirmingDelete) setConfirmingDelete(false);
    else if (infoOpen && !isDesktop) setInfoOpen(false);
    else onClose();
  };
  const dialogRef = useModalA11y<HTMLDivElement>(true, dismiss);

  if (!photo) return null;

  const handleDelete = async () => {
    setDeleting(true);
    // The photo after it takes its place, or the one before at the end.
    const successor = next ?? prev;
    const removed = await onDelete(photo.id);
    setDeleting(false);
    setConfirmingDelete(false);
    if (!removed) return;
    if (successor) onNavigate(successor.id);
    else onClose();
  };

  const layout: AlbumViewerLayoutProps = {
    photos,
    photo,
    nav,
    hasMore,
    moment: formatAlbumMoment(photo.takenAt, timezone, locale),
    editable,
    infoOpen,
    setInfoOpen,
    details: (
      <AlbumPhotoDetails
        key={photo.id}
        photo={photo}
        calendarId={calendarId}
        editable={editable}
        onSaveCaption={onSaveCaption}
        onLinkEvent={onLinkEvent}
      />
    ),
    onClose,
    onDownload: () => onDownload(photo),
    onRequestDelete: () => setConfirmingDelete(true),
    onLoadMore,
    onImageError,
  };

  return createPortal(
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-label={t('album.photo')}
      className="album-viewer fixed inset-0 z-50 flex flex-col text-white"
    >
      {isDesktop ? <AlbumViewerDesktop {...layout} /> : <AlbumViewerMobile {...layout} />}

      <ConfirmSheet
        open={confirmingDelete}
        title={t('album.deleteOne')}
        hint={t('album.deleteHint', { name: calendarName })}
        confirmLabel={t('common.delete')}
        busy={deleting}
        tone="dark"
        onConfirm={handleDelete}
        onCancel={() => setConfirmingDelete(false)}
      />
    </div>,
    document.body,
  );
}
