import { useEffect, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { useT } from '@/i18n';
import { activityColor, activityLabelKey } from '@/lib/activity';
import { api } from '@/lib/api';
import { formatRelativeTime } from '@/lib/date-utils';
import { useUiStore } from '@/stores/ui-store';

export interface HistoryActor {
  id: string;
  name: string;
  icon: string;
  avatarUrl?: string;
}

export interface HistoryItem {
  id: string;
  /** The server's dotted name, e.g. `calendar.event.updated`. Read through the
   *  helpers in lib/activity rather than matched literally: matching on a
   *  vocabulary the server does not use leaves every row unlabelled. */
  action: string;
  summary: string;
  createdAt: string;
  actor: HistoryActor | null;
}

interface HistoryTimelineProps {
  kind: 'event' | 'memo';
  calendarId: string;
  entityId: string;
}

/** Renders a compact oldest-to-newest history timeline for an event or memo. */
export function HistoryTimeline({ kind, calendarId, entityId }: HistoryTimelineProps) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const [items, setItems] = useState<HistoryItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    const path =
      kind === 'event'
        ? `/calendars/${calendarId}/events/${entityId}/history`
        : `/calendars/${calendarId}/memos/${entityId}/history`;
    (async () => {
      try {
        const data = await api.get<HistoryItem[]>(path);
        // The API returns newest-first; reverse for the oldest-to-newest timeline.
        if (!cancelled) setItems([...data].reverse());
      } catch {
        if (!cancelled) setItems([]);
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [kind, calendarId, entityId]);

  if (isLoading) {
    return (
      <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
        {t('history.loading')}
      </p>
    );
  }

  if (items.length === 0) {
    return (
      <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
        {t('history.empty')}
      </p>
    );
  }

  return (
    <div className="space-y-3">
      {items.map((item) => (
        <div key={item.id} className="flex gap-2">
          <span
            className="mt-1.5 h-2 w-2 shrink-0 rounded-full"
            style={{ backgroundColor: activityColor(item.action) }}
          />
          <Avatar src={item.actor?.avatarUrl} name={item.actor?.name ?? t('history.deletedUser')} />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-x-2">
              <span className="text-body font-medium text-[var(--color-text-primary)]">
                {item.actor?.name ?? t('history.deletedUser')}
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
    </div>
  );
}
