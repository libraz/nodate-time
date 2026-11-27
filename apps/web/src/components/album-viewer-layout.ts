import type { ReactNode } from 'react';
import type { AlbumPhoto } from '@/lib/album';
import type { AlbumNavigation } from '@/lib/use-album-navigation';

/**
 * What the viewer hands its phone and desktop layouts. The state and the
 * writes live in `AlbumViewer`; a layout only decides where things go.
 */
export interface AlbumViewerLayoutProps {
  photos: AlbumPhoto[];
  photo: AlbumPhoto;
  nav: AlbumNavigation;
  hasMore: boolean;
  moment: { date: string; time: string };
  editable: boolean;
  infoOpen: boolean;
  setInfoOpen: (open: boolean) => void;
  /** The caption, event and uploader block, placed wherever the layout keeps details. */
  details: ReactNode;
  onClose: () => void;
  onDownload: () => void;
  onRequestDelete: () => void;
  onLoadMore: () => void;
  onImageError: (photoId: string) => void;
}
