import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Avatar } from '@/components/avatar';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { CustomSelect } from '@/components/custom-select';
import { useT } from '@/i18n';
import { type AlbumPhoto, formatAlbumMoment } from '@/lib/album';
import { fromISOInZone } from '@/lib/date-utils';
import { seriesId } from '@/lib/event-move';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { eventStartDay } from '@/lib/week-layout';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

/** How far a horizontal drag has to travel before it turns the page. */
const SWIPE_PAGE_PX = 50;
/** How far a vertical drag has to travel to open the details or dismiss. */
const SWIPE_SHEET_PX = 70;
/** Pages left before the end at which the next page of the album is fetched. */
const PREFETCH_REMAINING = 3;

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
 * The full-screen photo viewer: one photo at a time on black, paged with the
 * arrow keys or a swipe, with its bars hidden by a tap. The caption, the event
 * link and who added it sit in a details sheet, so the photo keeps the screen.
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
  const events = useCalendarStore((s) => s.events);
  const [chromeHidden, setChromeHidden] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [captionDraft, setCaptionDraft] = useState('');
  const [savingCaption, setSavingCaption] = useState(false);
  const [linkingEvent, setLinkingEvent] = useState(false);
  const [dragX, setDragX] = useState(0);
  const dragRef = useRef<{ x: number; y: number; id: number } | null>(null);

  const index = photos.findIndex((p) => p.id === photoId);
  const photo = photos[index];
  const prev = index > 0 ? photos[index - 1] : undefined;
  const next = index >= 0 ? photos[index + 1] : undefined;

  // The innermost open surface owns the keyboard: a confirmation first, then
  // the details sheet, then the viewer. The event picker portals its list out
  // of this subtree and closes itself on Escape, so the viewer stands down
  // while one is open -- asked for by class name, since a portalled list is
  // not in this subtree to ask any other way.
  const dismiss = () => {
    if (document.querySelector('.dropdown-panel')) return;
    if (confirmingDelete) setConfirmingDelete(false);
    else if (infoOpen) setInfoOpen(false);
    else onClose();
  };
  const dialogRef = useModalA11y<HTMLDivElement>(true, dismiss);

  useEffect(() => {
    if (photo) setCaptionDraft(photo.caption);
  }, [photo?.id, photo?.caption, photo]);

  useEffect(() => {
    if (hasMore && index >= 0 && photos.length - index <= PREFETCH_REMAINING) onLoadMore();
  }, [hasMore, index, photos.length, onLoadMore]);

  const goTo = useCallback(
    (target: AlbumPhoto | undefined) => {
      if (!target) return;
      setConfirmingDelete(false);
      onNavigate(target.id);
    },
    [onNavigate],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
      const el = e.target;
      if (el instanceof Element && el.closest('input,textarea,[contenteditable="true"]')) return;
      if (document.querySelector('.dropdown-panel')) return;
      if (confirmingDelete) return;
      e.preventDefault();
      goTo(e.key === 'ArrowLeft' ? prev : next);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [goTo, prev, next, confirmingDelete]);

  /**
   * Events the photo could belong to: the ones on the day it was taken.
   *
   * Offering every loaded event would be a list of hundreds ordered by nothing
   * the viewer can see. A photo belongs to what was happening when it was
   * taken, and the capture time is read off the file precisely so this list
   * can be short enough to pick from.
   */
  const linkableEvents = useMemo(() => {
    if (!photo) return [];
    const day = fromISOInZone(photo.takenAt, timezone).toISODate();
    // A photo links to the series, so an occurrence is offered under its
    // series id, once.
    const offered = new Map<string, { id: string; title: string; startAt: string }>();
    for (const evt of events) {
      if (evt.calendarId !== calendarId) continue;
      if (eventStartDay(evt, timezone).toISODate() !== day) continue;
      const id = seriesId(evt.id);
      if (!offered.has(id)) offered.set(id, { id, title: evt.title, startAt: evt.startAt });
    }
    return [...offered.values()].sort((a, b) => a.startAt.localeCompare(b.startAt));
  }, [photo, events, calendarId, timezone]);

  if (!photo) return null;

  const moment = formatAlbumMoment(photo.takenAt, timezone, locale);
  const linkedTitle = linkableEvents.find((evt) => evt.id === photo.eventId)?.title;

  const handlePointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    dragRef.current = { x: e.clientX, y: e.clientY, id: e.pointerId };
  };
  const handlePointerMove = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag || drag.id !== e.pointerId) return;
    const dx = e.clientX - drag.x;
    if (Math.abs(dx) > Math.abs(e.clientY - drag.y)) setDragX(dx);
  };
  const handlePointerUp = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    dragRef.current = null;
    setDragX(0);
    if (!drag || drag.id !== e.pointerId) return;
    const dx = e.clientX - drag.x;
    const dy = e.clientY - drag.y;
    if (Math.abs(dx) > Math.abs(dy) && Math.abs(dx) >= SWIPE_PAGE_PX) {
      // Dragging the photo leftwards brings in the one after it.
      goTo(dx < 0 ? next : prev);
    } else if (dy <= -SWIPE_SHEET_PX) {
      setInfoOpen(true);
      setChromeHidden(false);
    } else if (dy >= SWIPE_SHEET_PX) {
      if (infoOpen) setInfoOpen(false);
      else onClose();
    } else if (Math.abs(dx) < 6 && Math.abs(dy) < 6) {
      if (infoOpen) setInfoOpen(false);
      else setChromeHidden((v) => !v);
    }
  };

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

  const handleSaveCaption = async () => {
    setSavingCaption(true);
    try {
      await onSaveCaption(photo.id, captionDraft);
    } catch {
      // The failure is reported by the caller; the typed caption stays.
    } finally {
      setSavingCaption(false);
    }
  };

  const handleLinkEvent = async (eventId: string) => {
    setLinkingEvent(true);
    try {
      await onLinkEvent(photo.id, eventId);
    } catch {
      // The failure is reported by the caller.
    } finally {
      setLinkingEvent(false);
    }
  };

  const chromeClass = `transition-opacity duration-200 ${
    chromeHidden ? 'pointer-events-none opacity-0' : 'opacity-100'
  }`;

  return createPortal(
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-label={t('album.photo')}
      className="album-viewer fixed inset-0 z-50 flex flex-col bg-black text-white"
    >
      {/* Top bar: close, and when the photo was taken */}
      <div
        className={`absolute inset-x-0 top-0 z-10 flex items-center gap-3 bg-gradient-to-b from-black/70 to-transparent px-3 pt-[max(env(safe-area-inset-top),0.75rem)] pb-6 ${chromeClass}`}
        aria-hidden={chromeHidden}
      >
        <button
          type="button"
          onClick={onClose}
          aria-label={t('common.close')}
          className="album-viewer-btn h-10 w-10"
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </button>
        <div className="min-w-0 flex-1 text-center">
          <p className="truncate text-callout font-semibold">{moment.date}</p>
          <p className="truncate text-caption text-white/70 tabular-nums">{moment.time}</p>
        </div>
        <span className="h-10 w-10 shrink-0" aria-hidden="true" />
      </div>

      {/* The photo */}
      <div
        className="relative flex flex-1 touch-none select-none items-center justify-center overflow-hidden"
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={() => {
          dragRef.current = null;
          setDragX(0);
        }}
      >
        <img
          key={photo.id}
          src={photo.imageUrl}
          alt={photo.caption}
          draggable={false}
          onError={() => onImageError(photo.id)}
          className="album-viewer-image max-h-full max-w-full object-contain"
          style={{
            transform: dragX ? `translateX(${dragX}px)` : undefined,
            transition: dragX ? 'none' : undefined,
          }}
        />
        {prev && (
          <button
            type="button"
            onClick={() => goTo(prev)}
            onPointerDown={(e) => e.stopPropagation()}
            onPointerUp={(e) => e.stopPropagation()}
            aria-label={t('album.prev')}
            className={`album-viewer-btn album-viewer-nav left-4 hidden sm:flex ${chromeClass}`}
          >
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>
        )}
        {next && (
          <button
            type="button"
            onClick={() => goTo(next)}
            onPointerDown={(e) => e.stopPropagation()}
            onPointerUp={(e) => e.stopPropagation()}
            aria-label={t('album.next')}
            className={`album-viewer-btn album-viewer-nav right-4 hidden sm:flex ${chromeClass}`}
          >
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d="M9 18l6-6-6-6" />
            </svg>
          </button>
        )}
      </div>

      {/* Details sheet: caption, event, who added it */}
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
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                aria-hidden="true"
              >
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </button>
          </div>

          <div className="mt-4">
            {editable ? (
              <div className="flex items-center gap-2">
                <input
                  data-1p-ignore
                  type="text"
                  value={captionDraft}
                  onChange={(e) => setCaptionDraft(e.target.value)}
                  placeholder={t('album.captionPlaceholder')}
                  aria-label={t('album.caption')}
                  className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-white/10 px-3 py-2.5 text-body text-white outline-none placeholder:text-white/45 focus:bg-white/15"
                />
                <button
                  type="button"
                  onClick={handleSaveCaption}
                  disabled={savingCaption || captionDraft === photo.caption}
                  className="btn-primary btn-sm shrink-0 disabled:opacity-40"
                >
                  {savingCaption ? t('profile.saving') : t('album.saveCaption')}
                </button>
              </div>
            ) : (
              photo.caption && <p className="text-body text-white">{photo.caption}</p>
            )}
          </div>

          <dl className="mt-4 divide-y divide-white/10 rounded-[var(--radius-sm)] bg-white/[0.06] text-footnote">
            <div className="flex items-center gap-3 px-3.5 py-2.5">
              <dt className="w-16 shrink-0 text-white/60">{t('album.event')}</dt>
              <dd className="flex min-w-0 flex-1 items-center gap-2">
                {editable ? (
                  <CustomSelect
                    value={photo.eventId ?? ''}
                    onChange={handleLinkEvent}
                    ariaLabel={t('album.event')}
                    className="min-w-0 flex-1"
                    triggerClassName="rounded-[var(--radius-control)] bg-white/10 px-3 py-1.5 text-footnote text-white hover:bg-white/15"
                    options={[
                      { value: '', label: t('album.noEvent') },
                      ...linkableEvents.map((evt) => ({ value: evt.id, label: evt.title })),
                    ]}
                  />
                ) : (
                  <span className="truncate">{linkedTitle ?? t('album.noEvent')}</span>
                )}
                {linkingEvent && (
                  <span className="shrink-0 text-white/60">{t('profile.saving')}</span>
                )}
              </dd>
            </div>
            <div className="flex items-center gap-3 px-3.5 py-2.5">
              <dt className="w-16 shrink-0 text-white/60">{t('album.addedBy')}</dt>
              <dd className="flex min-w-0 items-center gap-2">
                <Avatar
                  src={photo.uploaderAvatarUrl}
                  name={photo.uploaderName}
                  size="h-5 w-5"
                  round
                />
                <span className="truncate">{photo.uploaderName}</span>
              </dd>
            </div>
          </dl>
        </section>
      )}

      {/* Bottom toolbar */}
      <div
        className={`absolute inset-x-0 bottom-0 z-10 bg-gradient-to-t from-black/70 to-transparent px-6 pt-8 pb-[max(env(safe-area-inset-bottom),1rem)] ${chromeClass}`}
        aria-hidden={chromeHidden}
      >
        <div className="mx-auto flex max-w-[420px] items-center justify-between">
          <button
            type="button"
            onClick={() => onDownload(photo)}
            aria-label={t('album.download')}
            title={t('album.download')}
            className="album-viewer-btn h-11 w-11"
          >
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M12 4v11M7.5 10.5L12 15l4.5-4.5M5 19h14" />
            </svg>
          </button>
          <button
            type="button"
            onClick={() => setInfoOpen((v) => !v)}
            aria-label={t('album.info')}
            aria-pressed={infoOpen}
            title={t('album.info')}
            className="album-viewer-btn h-11 w-11"
          >
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill={infoOpen ? 'currentColor' : 'none'}
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <circle cx="12" cy="12" r="9" />
              <path d="M12 11v5" stroke={infoOpen ? 'black' : 'currentColor'} />
              <circle
                cx="12"
                cy="7.8"
                r="0.6"
                fill={infoOpen ? 'black' : 'currentColor'}
                stroke={infoOpen ? 'black' : 'currentColor'}
              />
            </svg>
          </button>
          {editable && (
            <button
              type="button"
              onClick={() => setConfirmingDelete(true)}
              aria-label={t('common.delete')}
              title={t('common.delete')}
              className="album-viewer-btn h-11 w-11"
            >
              <TrashIcon />
            </button>
          )}
        </div>
      </div>

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

export function TrashIcon() {
  return (
    <svg
      width="22"
      height="22"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.9"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M4 7h16M9 7V4.8A.8.8 0 019.8 4h4.4a.8.8 0 01.8.8V7M6.5 7l.8 12.2A1.9 1.9 0 009.2 21h5.6a1.9 1.9 0 001.9-1.8L17.5 7M10 11v6M14 11v6" />
    </svg>
  );
}
