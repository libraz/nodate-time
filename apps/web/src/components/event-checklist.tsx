import { useCallback, useEffect, useState } from 'react';
import { LabelledButton } from '@/components/labelled-button';
import { useT } from '@/i18n';
import { client, errorMessage, isStaleError } from '@/lib/client';
import { isSubmitEnter } from '@/lib/keys';
import { toast } from '@/lib/toast';
import type { ChecklistItem } from '@/types/calendar';

/** A checklist item of an event not created yet, kept until the event is saved. */
export interface DraftChecklistItem {
  key: string;
  title: string;
  done: boolean;
}

/** The checklist of an event not created yet, including text typed but not added. */
export interface DraftChecklist {
  items: DraftChecklistItem[];
  pending: string;
}

export const EMPTY_DRAFT_CHECKLIST: DraftChecklist = { items: [], pending: '' };

/**
 * Writes a new event's draft checklist, in order, once the event exists. Text
 * still in the field is added too: pressing Save is how a phone user finishes.
 * Reports whether every item was written.
 */
export async function saveDraftChecklist(
  calendarId: string,
  eventId: string,
  draft: DraftChecklist,
): Promise<boolean> {
  const pending = draft.pending.trim();
  const items = pending ? [...draft.items, { key: '', title: pending, done: false }] : draft.items;
  try {
    for (const [sortOrder, item] of items.entries()) {
      const { data: created } = await client.POST(
        '/calendars/{calendarId}/events/{eventId}/checklist',
        { params: { path: { calendarId, eventId } }, body: { title: item.title, sortOrder } },
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
    }
    return true;
  } catch {
    return false;
  }
}

type ChecklistSectionProps =
  | { calendarId: string; eventId: string; editable: boolean; draft?: undefined }
  | { draft: DraftChecklist; onDraftChange: (draft: DraftChecklist) => void };

/**
 * An event's checklist. A saved event's items are written as they change; a
 * new event's are held as a draft and written by `saveDraftChecklist` once it
 * has been created.
 */
export function ChecklistSection(props: ChecklistSectionProps) {
  if (props.draft) return <DraftChecklistSection {...props} />;
  return <SavedChecklistSection {...props} />;
}

function DraftChecklistSection({
  draft,
  onDraftChange,
}: {
  draft: DraftChecklist;
  onDraftChange: (draft: DraftChecklist) => void;
}) {
  const add = () => {
    const title = draft.pending.trim();
    if (!title) return;
    onDraftChange({
      items: [...draft.items, { key: crypto.randomUUID(), title, done: false }],
      pending: '',
    });
  };
  return (
    <ChecklistView
      rows={draft.items.map((i) => ({ id: i.key, title: i.title, done: i.done }))}
      editable
      newTitle={draft.pending}
      onNewTitleChange={(pending) => onDraftChange({ ...draft, pending })}
      onAdd={add}
      onToggle={(id) =>
        onDraftChange({
          ...draft,
          items: draft.items.map((i) => (i.key === id ? { ...i, done: !i.done } : i)),
        })
      }
      onDelete={(id) => onDraftChange({ ...draft, items: draft.items.filter((i) => i.key !== id) })}
    />
  );
}

function SavedChecklistSection({
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

  const byId = (id: string) => items.find((i) => i.id === id);
  return (
    <ChecklistView
      rows={items}
      editable={editable}
      loading={isLoading}
      loadError={loadError}
      newTitle={newTitle}
      onNewTitleChange={setNewTitle}
      onAdd={handleAdd}
      onToggle={(id) => {
        const item = byId(id);
        if (item) handleToggle(item);
      }}
      onDelete={(id) => {
        const item = byId(id);
        if (item) handleDelete(item);
      }}
    />
  );
}

interface ChecklistRow {
  id: string;
  title: string;
  done: boolean;
}

interface ChecklistViewProps {
  rows: ChecklistRow[];
  editable: boolean;
  loading?: boolean;
  loadError?: string | null;
  newTitle: string;
  onNewTitleChange: (title: string) => void;
  onAdd: () => void;
  onToggle: (id: string) => void;
  onDelete: (id: string) => void;
}

function ChecklistView({
  rows: items,
  editable,
  loading: isLoading = false,
  loadError = null,
  newTitle,
  onNewTitleChange: setNewTitle,
  onAdd: handleAdd,
  onToggle,
  onDelete,
}: ChecklistViewProps) {
  const t = useT();
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
                onClick={() => onToggle(item.id)}
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
                  onClick={() => onDelete(item.id)}
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
              if (!isSubmitEnter(e)) return;
              e.preventDefault();
              handleAdd();
            }}
            placeholder={t('event.checklistPlaceholder')}
            className="input-modern h-10 min-w-0 flex-1 text-default"
          />
          {/* A software keyboard's return key is not something users reach for. */}
          <LabelledButton
            label={t('checklist.add')}
            onClick={handleAdd}
            disabled={!newTitle.trim()}
            className="icon-btn icon-btn-accent h-10 w-10 shrink-0 disabled:opacity-40"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
            >
              <path d="M12 5v14M5 12h14" />
            </svg>
          </LabelledButton>
        </div>
      )}
    </div>
  );
}
