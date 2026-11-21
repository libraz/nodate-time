import { useCallback, useEffect, useState } from 'react';
import { LabelledButton } from '@/components/labelled-button';
import { useT } from '@/i18n';
import { client, errorMessage, isStaleError } from '@/lib/client';
import { toast } from '@/lib/toast';
import type { ChecklistItem } from '@/types/calendar';

export function ChecklistSection({
  calendarId,
  eventId,
  editable,
}: {
  calendarId: string;
  eventId: string;
  editable: boolean;
}) {
  const t = useT();
  const [items, setItems] = useState<ChecklistItem[]>([]);
  const [newTitle, setNewTitle] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  // A list that could not be read says so where the list would be, rather than
  // passing for an event that simply has no checklist.
  const [loadError, setLoadError] = useState<string | null>(null);

  const fetchItems = useCallback(async () => {
    setIsLoading(true);
    try {
      const { data } = await client.GET('/calendars/{calendarId}/events/{eventId}/checklist', {
        params: { path: { calendarId, eventId } },
      });
      setItems(data ?? []);
      setLoadError(null);
    } catch (err) {
      setLoadError(errorMessage(err));
    } finally {
      setIsLoading(false);
    }
  }, [calendarId, eventId]);

  useEffect(() => {
    fetchItems();
  }, [fetchItems]);

  const handleAdd = async () => {
    const title = newTitle.trim();
    if (!title) return;
    try {
      await client.POST('/calendars/{calendarId}/events/{eventId}/checklist', {
        params: { path: { calendarId, eventId } },
        body: { title, sortOrder: items.length },
      });
      setNewTitle('');
      await fetchItems();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const handleToggle = async (item: ChecklistItem) => {
    try {
      // Only done: a title or position read earlier would revert another
      // member's rename or reorder made since.
      await client.PATCH('/calendars/{calendarId}/events/{eventId}/checklist/{itemId}', {
        params: {
          path: { calendarId, eventId, itemId: item.id },
          header: { 'If-Match': item.etag },
        },
        body: { done: !item.done },
      });
      await fetchItems();
    } catch (err) {
      await failWrite(err);
    }
  };

  // A stale item is never retried with a newer version; the list is read again.
  const failWrite = async (err: unknown) => {
    if (isStaleError(err)) {
      toast.error(t('conflict.stale'));
      await fetchItems();
      return;
    }
    toast.error(errorMessage(err));
  };

  const handleDelete = async (item: ChecklistItem) => {
    try {
      await client.DELETE('/calendars/{calendarId}/events/{eventId}/checklist/{itemId}', {
        params: {
          path: { calendarId, eventId, itemId: item.id },
          header: { 'If-Match': item.etag },
        },
      });
      await fetchItems();
      // The item comes back as a new row carrying the same title, state and
      // sort order; its id and creation time are the server's to assign.
      toast.info(t('checklist.deleted'), {
        label: t('common.undo'),
        run: () => {
          void (async () => {
            try {
              const { data: created } = await client.POST(
                '/calendars/{calendarId}/events/{eventId}/checklist',
                {
                  params: { path: { calendarId, eventId } },
                  body: { title: item.title, sortOrder: item.sortOrder },
                },
              );
              if (created && item.done) {
                await client.PATCH('/calendars/{calendarId}/events/{eventId}/checklist/{itemId}', {
                  params: {
                    path: { calendarId, eventId, itemId: created.id },
                    header: { 'If-Match': created.etag },
                  },
                  body: { done: true },
                });
              }
              await fetchItems();
            } catch (err) {
              toast.error(errorMessage(err));
            }
          })();
        },
      });
    } catch (err) {
      await failWrite(err);
    }
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
          <path d="M9 11l3 3L22 4" />
          <path d="M21 12v7a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2h11" />
        </svg>
        <span className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('event.checklist')}
        </span>
      </div>

      {isLoading && items.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
          {t('common.loading')}
        </p>
      )}

      {loadError && items.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-danger)]">{loadError}</p>
      )}

      {items.length > 0 && (
        <div className="mb-3 space-y-1">
          {items.map((item) => (
            <div key={item.id} className="group flex items-center gap-2">
              <LabelledButton
                label={item.title}
                pressed={item.done}
                onClick={() => handleToggle(item)}
                disabled={!editable}
                className="flex h-5 w-5 shrink-0 items-center justify-center rounded border border-[var(--color-border)] disabled:cursor-default"
                style={{
                  backgroundColor: item.done ? 'var(--color-accent)' : 'transparent',
                  borderColor: item.done ? 'var(--color-accent)' : undefined,
                }}
              >
                {item.done && (
                  <svg
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="var(--color-text-on-accent)"
                    strokeWidth="3"
                  >
                    <path d="M20 6L9 17l-5-5" />
                  </svg>
                )}
              </LabelledButton>
              <span
                className="flex-1 text-default"
                style={{
                  color: item.done ? 'var(--color-text-tertiary)' : 'var(--color-text-primary)',
                  textDecoration: item.done ? 'line-through' : 'none',
                }}
              >
                {item.title}
              </span>
              {editable && (
                <LabelledButton
                  label={t('common.delete')}
                  onClick={() => handleDelete(item)}
                  className="reveal-on-hover text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
                >
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M18 6L6 18M6 6l12 12" />
                  </svg>
                </LabelledButton>
              )}
            </div>
          ))}
        </div>
      )}

      {editable && (
        <div className="flex items-center gap-2">
          <input
            data-1p-ignore
            type="text"
            value={newTitle}
            onChange={(e) => setNewTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                handleAdd();
              }
            }}
            placeholder={t('event.checklistPlaceholder')}
            className="input-modern h-10 min-w-0 flex-1 text-default"
          />
        </div>
      )}
    </div>
  );
}
