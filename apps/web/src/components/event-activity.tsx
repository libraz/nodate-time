import { type KeyboardEvent, useId, useRef, useState } from 'react';
import { CommentsSection } from '@/components/event-comments';
import { HistoryTimeline } from '@/components/history-timeline';
import { useT } from '@/i18n';

const TABS = ['comments', 'history'] as const;
type Tab = (typeof TABS)[number];

/**
 * Comments and the change history of an event, one tab at a time. History is
 * something to look up, so it waits behind a tab instead of sitting beside the
 * conversation. Both panels stay mounted: a half-written comment survives a
 * look at the history.
 */
export function EventActivity({
  calendarId,
  eventId,
  seriesId,
  canPost,
  moderatorRole,
}: {
  calendarId: string;
  eventId: string;
  /** The series the event belongs to; history is kept per series. */
  seriesId: string;
  canPost: boolean;
  moderatorRole: string | undefined;
}) {
  const t = useT();
  const [tab, setTab] = useState<Tab>('comments');
  const baseId = useId();
  const tabRefs = useRef<Record<Tab, HTMLButtonElement | null>>({ comments: null, history: null });
  const label: Record<Tab, string> = {
    comments: t('event.comments'),
    history: t('history.title'),
  };

  // Arrow keys move between tabs, as a tablist does.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const next = TABS[(TABS.indexOf(tab) + 1) % TABS.length] as Tab;
    setTab(next);
    tabRefs.current[next]?.focus();
  };

  return (
    <div className="card-section mx-6 mt-2 bg-[var(--color-surface-secondary)] p-4">
      <div
        role="tablist"
        aria-label={t('event.commentsAndHistory')}
        onKeyDown={onKeyDown}
        className="segmented-control mb-3 w-full"
      >
        {TABS.map((id) => (
          <button
            key={id}
            ref={(el) => {
              tabRefs.current[id] = el;
            }}
            type="button"
            role="tab"
            id={`${baseId}-${id}-tab`}
            aria-selected={tab === id}
            aria-controls={`${baseId}-${id}`}
            tabIndex={tab === id ? 0 : -1}
            data-active={tab === id}
            onClick={() => setTab(id)}
          >
            {label[id]}
          </button>
        ))}
      </div>
      <div
        role="tabpanel"
        id={`${baseId}-comments`}
        aria-labelledby={`${baseId}-comments-tab`}
        hidden={tab !== 'comments'}
      >
        <CommentsSection
          calendarId={calendarId}
          eventId={eventId}
          canPost={canPost}
          moderatorRole={moderatorRole}
        />
      </div>
      <div
        role="tabpanel"
        id={`${baseId}-history`}
        aria-labelledby={`${baseId}-history-tab`}
        hidden={tab !== 'history'}
      >
        <HistoryTimeline kind="event" calendarId={calendarId} entityId={seriesId} />
      </div>
    </div>
  );
}
