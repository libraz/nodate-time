import { useEffect, useMemo, useState } from 'react';
import type { HistoryActor } from '@/components/history-timeline';
import { type TranslationKey, useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { formatRelativeTime } from '@/lib/date-utils';
import { roleForCalendar } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';

interface FeedItem {
  id: number;
  action: 'create' | 'update' | 'delete';
  summary: string;
  createdAt: string;
  actor: HistoryActor | null;
  entityType: 'event' | 'memo';
  entityId: string;
}

const ACTION_LABEL: Record<FeedItem['action'], TranslationKey> = {
  create: 'history.created',
  update: 'history.updated',
  delete: 'history.deleted',
};

const ACTION_COLOR: Record<FeedItem['action'], string> = {
  create: 'var(--color-accent)',
  update: 'var(--color-text-tertiary)',
  delete: 'var(--color-danger)',
};

interface ActivityPanelProps {
  onClose: () => void;
}

/** Calendar-wide activity feed listing recent event/memo changes, including deletions. */
export function ActivityPanel({ onClose }: ActivityPanelProps) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const me = useAuthStore((s) => s.user);

  // Only calendars the user is a member of and is currently viewing.
  const memberCalendars = useMemo(
    () =>
      calendars.filter(
        (c) =>
          activeCalendarIds.includes(c.id) &&
          roleForCalendar(membersMap[c.id], me?.email) !== undefined,
      ),
    [calendars, activeCalendarIds, membersMap, me?.email],
  );

  const [targetId, setTargetId] = useState('');
  const target = memberCalendars.find((c) => c.id === targetId) ?? memberCalendars[0] ?? null;
  const calendarId = target?.id ?? '';

  const [items, setItems] = useState<FeedItem[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!calendarId) return;
    let cancelled = false;
    setIsLoading(true);
    (async () => {
      try {
        const data = await api.get<FeedItem[]>(`/calendars/${calendarId}/activity?limit=50`);
        if (!cancelled) setItems(data);
      } catch (e) {
        if (!cancelled) toast.error(errorMessage(e));
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [calendarId]);

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={onClose}
      />
      <div className="glass-surface-heavy side-panel fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]">
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
              {/* Target calendar */}
              <div className="space-y-1.5">
                <span className="block text-caption font-medium text-[var(--color-text-secondary)]">
                  {t('activity.targetCalendar')}
                </span>
                {memberCalendars.length > 1 ? (
                  <select
                    value={target.id}
                    onChange={(e) => setTargetId(e.target.value)}
                    className="input-modern h-10 w-full text-sm"
                  >
                    {memberCalendars.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <div className="flex items-center gap-2">
                    <span
                      className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                      style={{ backgroundColor: target.color }}
                    />
                    <span className="truncate text-callout font-medium text-[var(--color-text-primary)]">
                      {target.name}
                    </span>
                  </div>
                )}
              </div>

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
                      <span
                        className="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden bg-[var(--color-surface-inset)] text-default"
                        style={{ borderRadius: 'var(--radius-sm)' }}
                      >
                        {item.actor?.avatarUrl ? (
                          <img
                            src={item.actor.avatarUrl}
                            alt=""
                            className="h-full w-full object-cover"
                          />
                        ) : (
                          (item.actor?.icon ?? '👤')
                        )}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-baseline gap-x-2">
                          <span className="text-body font-medium text-[var(--color-text-primary)]">
                            {item.actor?.name ?? t('history.deletedUser')}
                          </span>
                          <span className="rounded-full bg-[var(--color-surface-inset)] px-1.5 py-0.5 text-micro font-medium text-[var(--color-text-secondary)]">
                            {item.entityType === 'event'
                              ? t('activity.entityEvent')
                              : t('activity.entityMemo')}
                          </span>
                          <span
                            className="text-caption font-medium"
                            style={{ color: ACTION_COLOR[item.action] }}
                          >
                            {t(ACTION_LABEL[item.action])}
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
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  );
}
