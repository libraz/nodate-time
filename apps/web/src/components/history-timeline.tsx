import { useEffect, useState } from 'react';
import { type TranslationKey, useT } from '@/i18n';
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
  id: number;
  action: 'create' | 'update' | 'delete';
  summary: string;
  createdAt: string;
  actor: HistoryActor | null;
}

const ACTION_LABEL: Record<HistoryItem['action'], TranslationKey> = {
  create: 'history.created',
  update: 'history.updated',
  delete: 'history.deleted',
};

const ACTION_COLOR: Record<HistoryItem['action'], string> = {
  create: 'var(--color-accent)',
  update: 'var(--color-text-tertiary)',
  delete: 'var(--color-danger)',
};

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
        if (!cancelled) setItems(data);
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
            style={{ backgroundColor: ACTION_COLOR[item.action] }}
          />
          <span
            className="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden bg-[var(--color-surface-inset)] text-default"
            style={{ borderRadius: 'var(--radius-sm)' }}
          >
            {item.actor?.avatarUrl ? (
              <img src={item.actor.avatarUrl} alt="" className="h-full w-full object-cover" />
            ) : (
              (item.actor?.icon ?? '👤')
            )}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-x-2">
              <span className="text-body font-medium text-[var(--color-text-primary)]">
                {item.actor?.name ?? t('history.deletedUser')}
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
  );
}
