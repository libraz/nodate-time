import { useCallback, useEffect, useRef, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { LabelledButton } from '@/components/labelled-button';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/api';
import { client } from '@/lib/client';
import { formatRelativeTime } from '@/lib/date-utils';
import { canDeleteComment, canEditComment } from '@/lib/permissions';
import { isStaleError } from '@/lib/stale';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { useUiStore } from '@/stores/ui-store';
import type { EventComment } from '@/types/calendar';

export function eventCommentText(comment: EventComment): string {
  return comment.body;
}

export function CommentsSection({
  calendarId,
  eventId,
  canPost,
  moderatorRole,
}: {
  calendarId: string;
  eventId: string;
  /**
   * Whether the viewer may add to the thread. This follows writing to the
   * calendar, not editing this event: commenting on somebody else's plan is
   * what a shared calendar is for, so it is deliberately wider than the
   * event-level grant that governs the form above.
   */
  canPost: boolean;
  /** The viewer's role on the calendar, which decides who may touch a comment. */
  moderatorRole: string | undefined;
}) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const user = useAuthStore((s) => s.user);
  const [comments, setComments] = useState<EventComment[]>([]);
  const [newComment, setNewComment] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [isSending, setIsSending] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editContent, setEditContent] = useState('');
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  // A thread that could not be read is reported in place of the list: a toast
  // would be gone by the time the reader reaches this far down the modal, and
  // an empty section otherwise reads as "no comments yet".
  const [loadError, setLoadError] = useState<string | null>(null);
  // Set while there are older comments than the ones on screen. They are
  // fetched on request rather than up front: a long-running event's thread is
  // unbounded, and the newest is what someone opening the event is after.
  const [olderCursor, setOlderCursor] = useState('');
  const [loadingOlder, setLoadingOlder] = useState(false);
  const listEndRef = useRef<HTMLDivElement>(null);
  const mayEdit = (c: EventComment) => canEditComment(c, moderatorRole, user?.id);
  const mayDelete = (c: EventComment) => canDeleteComment(c, moderatorRole, user?.id);

  const fetchComments = useCallback(async () => {
    setIsLoading(true);
    try {
      // The thread is paged: the first page is the newest run of comments,
      // already oldest-first within itself, and the cursor reaches the older
      // ones before it.
      const { data: page } = await client.GET(
        '/calendars/{calendarId}/events/{eventId}/activities',
        { params: { path: { calendarId, eventId } } },
      );
      setComments(page?.items ?? []);
      setOlderCursor(page?.nextCursor ?? '');
      setLoadError(null);
    } catch (err) {
      setLoadError(errorMessage(err));
    } finally {
      setIsLoading(false);
    }
  }, [calendarId, eventId]);

  // Older comments go in front of what is already shown, because that is where
  // they belong in the reading order.
  const loadOlderComments = useCallback(async () => {
    if (!olderCursor || loadingOlder) return;
    setLoadingOlder(true);
    try {
      const { data: page } = await client.GET(
        '/calendars/{calendarId}/events/{eventId}/activities',
        { params: { path: { calendarId, eventId }, query: { cursor: olderCursor } } },
      );
      setComments((cur) => [...(page?.items ?? []), ...cur]);
      setOlderCursor(page?.nextCursor ?? '');
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setLoadingOlder(false);
    }
  }, [calendarId, eventId, olderCursor, loadingOlder]);

  useEffect(() => {
    fetchComments();
  }, [fetchComments]);

  const prevCountRef = useRef(0);
  useEffect(() => {
    if (comments.length > prevCountRef.current) {
      listEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
    prevCountRef.current = comments.length;
  });

  const handleSend = async () => {
    const content = newComment.trim();
    if (!content || isSending) return;
    setIsSending(true);
    try {
      await client.POST('/calendars/{calendarId}/events/{eventId}/activities', {
        params: { path: { calendarId, eventId } },
        body: { body: content },
      });
      setNewComment('');
      await fetchComments();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsSending(false);
    }
  };

  // A stale comment is never retried with a newer version; the thread is read again.
  const failWrite = async (err: unknown) => {
    if (isStaleError(err)) {
      toast.error(t('conflict.stale'));
      setEditingId(null);
      await fetchComments();
      return;
    }
    toast.error(errorMessage(err));
  };

  const saveEdit = async (commentId: string) => {
    const comment = comments.find((c) => c.id === commentId);
    if (!comment) return;
    try {
      await client.PATCH('/calendars/{calendarId}/events/{eventId}/activities/{commentId}', {
        params: { path: { calendarId, eventId, commentId }, header: { 'If-Match': comment.etag } },
        body: { body: editContent },
      });
    } catch (err) {
      await failWrite(err);
      return;
    }
    setEditingId(null);
    await fetchComments();
  };

  const deleteComment = async (commentId: string) => {
    const comment = comments.find((c) => c.id === commentId);
    if (!comment) {
      setPendingDeleteId(null);
      return;
    }
    setDeleting(true);
    try {
      await client.DELETE('/calendars/{calendarId}/events/{eventId}/activities/{commentId}', {
        params: { path: { calendarId, eventId, commentId }, header: { 'If-Match': comment.etag } },
      });
    } catch (err) {
      await failWrite(err);
      return;
    } finally {
      setDeleting(false);
      setPendingDeleteId(null);
    }
    fetchComments();
  };

  return (
    <div className="card-section mx-6 mt-3 bg-[var(--color-surface-secondary)] p-4">
      <div className="mb-3 flex items-center gap-2">
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--color-text-secondary)"
          strokeWidth="2"
        >
          <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
        </svg>
        <span className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('event.comments')}
        </span>
      </div>

      {isLoading && comments.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
          {t('common.loading')}
        </p>
      )}

      {loadError && comments.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-danger)]">{loadError}</p>
      )}

      {!isLoading && !loadError && comments.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
          {t('event.noComments')}
        </p>
      )}

      {comments.length > 0 && (
        <div className="mb-3 max-h-[240px] space-y-3 overflow-y-auto">
          {olderCursor && (
            <button
              type="button"
              onClick={loadOlderComments}
              disabled={loadingOlder}
              className="w-full py-1 text-center text-footnote text-[var(--color-text-secondary)]"
            >
              {loadingOlder ? t('common.loading') : t('event.loadEarlierComments')}
            </button>
          )}
          {comments.map((c) => (
            <div key={c.id} className="group flex gap-2">
              <Avatar src={c.authorAvatarUrl} name={c.authorName} className="mt-0.5" />
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline gap-2">
                  <span className="text-body font-medium text-[var(--color-text-primary)]">
                    {c.authorName}
                  </span>
                  <span className="text-caption text-[var(--color-text-tertiary)]">
                    {formatRelativeTime(c.createdAt, locale)}
                  </span>
                  {editingId !== c.id && (mayEdit(c) || mayDelete(c)) && (
                    <span className="reveal-on-hover flex gap-1">
                      {mayEdit(c) && (
                        <button
                          type="button"
                          onClick={() => {
                            setEditingId(c.id);
                            setEditContent(eventCommentText(c));
                          }}
                          className="text-caption text-[var(--color-text-tertiary)] hover:text-[var(--color-accent-ink)]"
                        >
                          {t('event.editComment')}
                        </button>
                      )}
                      {mayDelete(c) && (
                        <button
                          type="button"
                          onClick={() => setPendingDeleteId(c.id)}
                          className="text-caption text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
                        >
                          {t('event.deleteComment')}
                        </button>
                      )}
                    </span>
                  )}
                </div>
                {editingId === c.id ? (
                  <div className="mt-1 flex gap-1">
                    <input
                      data-1p-ignore
                      type="text"
                      value={editContent}
                      onChange={(e) => setEditContent(e.target.value)}
                      className="flex-1 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-body text-[var(--color-text-primary)] outline-none"
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') saveEdit(c.id);
                        if (e.key === 'Escape') {
                          // Leaving the edit box is all this Escape means; let
                          // it reach the modal and the whole editor would close.
                          e.stopPropagation();
                          setEditingId(null);
                        }
                      }}
                    />
                    <button
                      type="button"
                      onClick={() => saveEdit(c.id)}
                      className="bg-[var(--color-accent)] px-2 py-1 text-caption text-[var(--color-text-on-accent)]"
                      style={{ borderRadius: 'var(--radius-sm)' }}
                    >
                      {t('event.saveComment')}
                    </button>
                  </div>
                ) : (
                  <p className="mt-0.5 whitespace-pre-wrap break-words text-default text-[var(--color-text-primary)]">
                    {eventCommentText(c)}
                  </p>
                )}
              </div>
            </div>
          ))}
          <div ref={listEndRef} />
        </div>
      )}

      {canPost && (
        <div className="flex items-center gap-2">
          <input
            data-1p-ignore
            type="text"
            value={newComment}
            onChange={(e) => setNewComment(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder={t('event.commentPlaceholder')}
            className="h-10 min-w-0 flex-1 border border-[var(--color-border)] bg-[var(--color-surface)] px-4 text-default text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--color-accent)]"
            style={{ borderRadius: 'var(--radius-sm)' }}
          />
          <LabelledButton
            label={t('event.send')}
            onClick={handleSend}
            disabled={!newComment.trim() || isSending}
            className="icon-btn h-10 w-10 shrink-0 bg-[var(--color-accent)] disabled:opacity-40"
            style={{ borderRadius: 'var(--radius-sm)' }}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="white">
              <path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
            </svg>
          </LabelledButton>
        </div>
      )}

      <ConfirmSheet
        open={pendingDeleteId !== null}
        title={t('confirm.deleteComment')}
        confirmLabel={t('common.delete')}
        busy={deleting}
        onConfirm={() => pendingDeleteId && deleteComment(pendingDeleteId)}
        onCancel={() => setPendingDeleteId(null)}
      />
    </div>
  );
}
