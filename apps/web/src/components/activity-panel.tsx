import { useEffect } from 'react';
import { Avatar } from '@/components/avatar';
import type { HistoryActor } from '@/components/history-timeline';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { type TranslationKey, useT } from '@/i18n';
import { activityColor, activityLabelKey } from '@/lib/activity';
import { formatRelativeTime } from '@/lib/date-utils';
import { toast } from '@/lib/toast';
import { useCalendarPages } from '@/lib/use-calendar-pages';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { useUiStore } from '@/stores/ui-store';

interface FeedItem {
  id: string;
  /** The server's dotted name, e.g. `calendar.event.created`. Read through
   *  the helpers in lib/activity rather than matched literally. */
  action: string;
  summary: string;
  createdAt: string;
  actor: HistoryActor | null;
  entityType: string;
  entityId: string;
}

const ENTITY_LABEL: Record<string, TranslationKey> = {
  event: 'activity.entityEvent',
  memo: 'activity.entityMemo',
  member: 'activity.entityMember',
  invite: 'activity.entityInvite',
  comment: 'activity.entityComment',
  checklist: 'activity.entityChecklist',
  attachment: 'activity.entityAttachment',
  photo: 'activity.entityPhoto',
  calendar: 'activity.entityCalendar',
};

/** An unrecognised entity still names something, so it renders as itself. */
function entityLabel(entityType: string): TranslationKey | null {
  return ENTITY_LABEL[entityType] ?? null;
}

interface ActivityPanelProps {
  onClose: () => void;
}

/** Calendar-wide activity feed listing recent event/memo changes, including deletions. */
export function ActivityPanel({ onClose }: ActivityPanelProps) {
  const t = useT();
  // Mounted only while the feed is open, so the trap is live for as long as it is.
  const panelRef = useModalA11y<HTMLDivElement>(true, onClose);
  const locale = useUiStore((s) => s.locale);
  // The calendar list the server returns is already scoped to the caller's
  // memberships, so every shown calendar has a feed they may read.
  const { options, target, setTargetId } = usePanelCalendar();
  const calendarId = target?.id ?? '';

  const {
    items,
    nextCursor,
    loading: isLoading,
    loadingMore: isLoadingMore,
    error,
    loadMore,
  } = useCalendarPages<FeedItem>(calendarId, 'activity?limit=50', true);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={onClose}
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={t('activity.title')}
        className="glass-surface-heavy side-panel fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]"
      >
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="truncate text-subhead font-semibold">{t('activity.title')}</h2>
          <button
            type="button"
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
            style={{ borderRadius: 'var(--radius-sm)' }}
            aria-label={t('common.close')}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4">
          {!target ? (
            <p className="rounded-xl bg-[var(--color-surface-inset)] px-4 py-6 text-center text-body text-[var(--color-text-secondary)]">
              {t('activity.empty')}
            </p>
          ) : (
            <div className="space-y-4">
              <PanelCalendarPicker
                label={t('panel.targetCalendar')}
                options={options}
                target={target}
                onChange={setTargetId}
              />

              {isLoading && items.length === 0 && (
                <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
                  {t('history.loading')}
                </p>
              )}

              {!isLoading && items.length === 0 && (
                <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
                  {t('activity.empty')}
                </p>
              )}

              {items.length > 0 && (
                <div className="space-y-3">
                  {items.map((item) => (
                    <div key={item.id} className="flex gap-2">
                      <Avatar
                        src={item.actor?.avatarUrl}
                        name={item.actor?.name ?? t('history.deletedUser')}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-baseline gap-x-2">
                          <span className="text-body font-medium text-[var(--color-text-primary)]">
                            {item.actor?.name ?? t('history.deletedUser')}
                          </span>
                          <span className="rounded-full bg-[var(--color-surface-inset)] px-1.5 py-0.5 text-micro font-medium text-[var(--color-text-secondary)]">
                            {entityLabel(item.entityType)
                              ? t(entityLabel(item.entityType) as TranslationKey)
                              : item.entityType}
                          </span>
                          <span
                            className="text-caption font-medium"
                            style={{ color: activityColor(item.action) }}
                          >
                            {t(activityLabelKey(item.action))}
                          </span>
                          <span className="text-caption text-[var(--color-text-tertiary)]">
                            {formatRelativeTime(item.createdAt, locale)}
                          </span>
                        </div>
                        {item.summary && (
                          <p className="mt-0.5 break-words text-caption text-[var(--color-text-secondary)]">
                            {item.summary}
                          </p>
                        )}
                      </div>
                    </div>
                  ))}
                  {nextCursor && (
                    <button
                      type="button"
                      onClick={loadMore}
                      disabled={isLoadingMore}
                      className="btn-secondary w-full text-body disabled:opacity-50"
                    >
                      {isLoadingMore ? t('history.loading') : t('activity.loadMore')}
                    </button>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  );
}
