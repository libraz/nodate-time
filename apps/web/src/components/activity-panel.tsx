import { useEffect } from 'react';
import { Avatar } from '@/components/avatar';
import { EmptyState, GUIDE_ICONS } from '@/components/guide';
import type { HistoryItem } from '@/components/history-timeline';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { SidePanel } from '@/components/side-panel';
import { type TranslationKey, useT } from '@/i18n';
import { activityColor, activityLabelKey } from '@/lib/activity';
import { formatRelativeTime } from '@/lib/date-utils';
import { toast } from '@/lib/toast';
import { useCalendarPages } from '@/lib/use-calendar-pages';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { usePresence } from '@/lib/use-presence';
import { useUiStore } from '@/stores/ui-store';

interface FeedItem extends HistoryItem {
  entityType: string;
  entityId: string;
}

const ENTITY_LABEL: Record<string, TranslationKey> = {
  event: 'activity.entityEvent',
  memo: 'activity.entityMemo',
  member: 'activity.entityMember',
  attendee: 'activity.entityAttendee',
  invite: 'activity.entityInvite',
  comment: 'activity.entityComment',
  checklist: 'activity.entityChecklist',
  attachment: 'activity.entityAttachment',
  photo: 'activity.entityPhoto',
  calendar: 'activity.entityCalendar',
  rsvp: 'activity.entityRsvp',
  subscription: 'activity.entitySubscription',
};

/** An unrecognised entity still names something, so it renders as itself. */
function entityLabel(entityType: string): TranslationKey | null {
  return ENTITY_LABEL[entityType] ?? null;
}

interface ActivityPanelProps {
  open: boolean;
  onClose: () => void;
}

/** Calendar-wide activity feed listing recent event/memo changes, including deletions. */
export function ActivityPanel({ open, onClose }: ActivityPanelProps) {
  const t = useT();
  const presence = usePresence(open);
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
  } = useCalendarPages<FeedItem>(calendarId, 'activity?limit=50', presence.mounted);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);

  if (!presence.mounted) return null;

  return (
    <SidePanel presence={presence} onClose={onClose} title={t('activity.title')} helpTopic="faq">
      <div className="flex-1 overflow-y-auto px-5 py-4">
        {!target ? (
          <p className="rounded-[var(--radius-md)] bg-[var(--color-surface-inset)] px-4 py-6 text-center text-body text-[var(--color-text-secondary)]">
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
              <EmptyState
                icon={GUIDE_ICONS.activity}
                title={t('empty.activityTitle')}
                description={t('empty.activityDesc')}
              />
            )}

            {items.length > 0 && (
              <div className="space-y-3">
                {items.map((item) => {
                  const actorName =
                    item.actorName ??
                    t(item.systemActor ? 'history.systemActor' : 'history.deletedUser');
                  return (
                    <div key={item.id} className="flex gap-2">
                      <Avatar src={item.actorAvatarUrl} name={actorName} />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-baseline gap-x-2">
                          <span className="text-body font-medium text-[var(--color-text-primary)]">
                            {actorName}
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
                  );
                })}
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
    </SidePanel>
  );
}
