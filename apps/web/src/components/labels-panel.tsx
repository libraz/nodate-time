import { useEffect, useId, useState } from 'react';
import { ColorEditor } from '@/components/color-editor';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { LabelledButton } from '@/components/labelled-button';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { SidePanel } from '@/components/side-panel';
import { useT } from '@/i18n';
import { errorMessage, isStaleError } from '@/lib/client';
import { isSubmitEnter } from '@/lib/keys';
import { canManage } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { usePresence } from '@/lib/use-presence';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import { type Label, MEMBER_COLORS } from '@/types/calendar';

const NAME_MAX = 50;

/** The first palette colour no label carries yet, else the first. */
function freshColor(labels: Label[]): string {
  const used = new Set(labels.map((l) => l.color.toUpperCase()));
  return MEMBER_COLORS.find((c) => !used.has(c)) ?? MEMBER_COLORS[0];
}

/** Name and colour of one label, for both adding and editing. */
function LabelForm({
  initial,
  others,
  submitLabel,
  busy,
  onSubmit,
  onCancel,
}: {
  initial: { name: string; color: string };
  /** The calendar's other labels, to mark the colours already in use. */
  others: Label[];
  submitLabel: string;
  busy: boolean;
  onSubmit: (value: { name: string; color: string }) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const nameId = useId();
  const [name, setName] = useState(initial.name);
  const [color, setColor] = useState(initial.color);
  const trimmed = name.trim();
  const submit = () => {
    if (trimmed && !busy) onSubmit({ name: trimmed, color });
  };

  return (
    <div className="space-y-3">
      <div>
        <label
          htmlFor={nameId}
          className="mb-2 block text-caption font-medium text-[var(--color-text-secondary)]"
        >
          {t('labels.name')}
        </label>
        <input
          id={nameId}
          data-1p-ignore
          type="text"
          autoComplete="off"
          maxLength={NAME_MAX}
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (!isSubmitEnter(e)) return;
            e.preventDefault();
            submit();
          }}
          placeholder={t('labels.namePlaceholder')}
          className="input-modern input-compact h-10 sm:h-[34px]"
        />
      </div>
      <ColorEditor
        value={color}
        onChange={setColor}
        inUse={others.map((l) => ({ color: l.color, name: l.name }))}
        legend={t('labels.color')}
        inUseHint={t('labels.colorInUseHint')}
      />
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="btn-secondary btn-sm">
          {t('common.cancel')}
        </button>
        <button
          type="button"
          onClick={submit}
          disabled={!trimmed || busy}
          className="btn-primary btn-sm"
        >
          {submitLabel}
        </button>
      </div>
    </div>
  );
}

/**
 * The named colours a calendar's events can be filed under. Everyone on the
 * calendar sees the list; owners and managers also add, edit and delete.
 */
export function LabelsPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const presence = usePresence(rightPanel === 'labels');
  const { options, target, setTargetId } = usePanelCalendar();
  const [adding, setAdding] = useState(false);
  const calendarId = target?.id ?? '';
  const manages = target ? canManage(target.role) : false;

  // The add form belongs to one calendar and one opening of the panel.
  // biome-ignore lint/correctness/useExhaustiveDependencies: calendarId is the trigger
  useEffect(() => {
    setAdding(false);
  }, [calendarId, presence.mounted]);

  if (!presence.mounted) return null;

  return (
    <SidePanel
      presence={presence}
      onClose={() => toggleRightPanel('labels')}
      title={t('panel.labels')}
      actions={
        manages &&
        !adding && (
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="btn-primary btn-sm inline-flex items-center gap-1 pr-3 pl-2.5"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.4"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d="M12 5v14M5 12h14" />
            </svg>
            {t('labels.add')}
          </button>
        )
      }
    >
      {target && (
        <div className="border-b border-[var(--color-border)] px-5 py-3">
          <PanelCalendarPicker
            label={t('panel.targetCalendar')}
            options={options}
            target={target}
            onChange={setTargetId}
          />
          <p className="mt-2 text-caption text-[var(--color-text-secondary)]">{t('labels.hint')}</p>
        </div>
      )}
      <div className="flex-1 overflow-y-auto">
        {calendarId ? (
          <LabelList
            calendarId={calendarId}
            canManage={manages}
            adding={adding}
            onAddingChange={setAdding}
          />
        ) : (
          <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">—</p>
        )}
      </div>
    </SidePanel>
  );
}

/** One calendar's labels, with the add form above them while `adding`. */
export function LabelList({
  calendarId,
  canManage,
  adding,
  onAddingChange,
}: {
  calendarId: string;
  canManage: boolean;
  adding: boolean;
  onAddingChange: (adding: boolean) => void;
}) {
  const t = useT();
  const labels = useCalendarStore((s) => s.labelsMap[calendarId]);
  const loadError = useCalendarStore((s) => s.labelErrors[calendarId]);
  const fetchLabels = useCalendarStore((s) => s.fetchLabels);
  const addLabel = useCalendarStore((s) => s.addLabel);
  const updateLabel = useCalendarStore((s) => s.updateLabel);
  const deleteLabel = useCalendarStore((s) => s.deleteLabel);
  // The label whose edit form is open.
  const [editing, setEditing] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<Label | null>(null);
  const [busy, setBusy] = useState(false);

  // Another calendar's form is not this one's.
  useEffect(() => {
    setEditing(null);
    setPendingDelete(null);
    if (calendarId) void fetchLabels(calendarId);
  }, [calendarId, fetchLabels]);

  // Opening the add form closes any edit form.
  useEffect(() => {
    if (adding) setEditing(null);
  }, [adding]);

  const loading = labels === undefined && !loadError;
  const list = labels ?? [];

  // A refused write may mean the list is out of date, so it is read again.
  const run = async (action: () => Promise<void>): Promise<boolean> => {
    setBusy(true);
    try {
      await action();
      return true;
    } catch (e) {
      toast.error(errorMessage(e));
      if (isStaleError(e)) void fetchLabels(calendarId);
      return false;
    } finally {
      setBusy(false);
    }
  };

  const confirmDelete = async (label: Label) => {
    await run(() => deleteLabel(calendarId, label.id));
    setPendingDelete(null);
  };

  return (
    <section className="px-5 py-2">
      {adding && canManage && (
        <div className="py-3">
          <LabelForm
            initial={{ name: '', color: freshColor(list) }}
            others={list}
            submitLabel={t('labels.add')}
            busy={busy}
            onCancel={() => onAddingChange(false)}
            onSubmit={async (value) => {
              if (await run(() => addLabel(calendarId, value))) onAddingChange(false);
            }}
          />
        </div>
      )}

      {loading ? (
        <ul className="space-y-2 py-2" aria-busy="true">
          {[0, 1].map((i) => (
            <li key={i} className="flex animate-pulse items-center gap-3 py-2">
              <span className="h-3 w-3 shrink-0 rounded-full bg-[var(--color-surface-inset)]" />
              <span className="h-4 flex-1 rounded bg-[var(--color-surface-inset)]" />
            </li>
          ))}
        </ul>
      ) : loadError && labels === undefined ? (
        <div className="flex flex-col items-center gap-3 py-6 text-center">
          <p className="text-body text-[var(--color-danger)]">{t('labels.loadFailed')}</p>
          <button
            type="button"
            onClick={() => fetchLabels(calendarId)}
            className="btn-secondary btn-sm"
          >
            {t('error.retry')}
          </button>
        </div>
      ) : list.length === 0 ? (
        !adding && (
          <p className="py-4 text-center text-footnote text-[var(--color-text-tertiary)]">
            {t('labels.empty')}
          </p>
        )
      ) : (
        <ul className="divide-y divide-[var(--color-separator)]">
          {list.map((label) => (
            <li key={label.id} className="py-2.5">
              {editing === label.id && canManage ? (
                <LabelForm
                  initial={{ name: label.name, color: label.color }}
                  others={list.filter((l) => l.id !== label.id)}
                  submitLabel={t('common.save')}
                  busy={busy}
                  onCancel={() => setEditing(null)}
                  onSubmit={async (value) => {
                    const changed = {
                      ...(value.name !== label.name ? { name: value.name } : {}),
                      ...(value.color.toUpperCase() !== label.color.toUpperCase()
                        ? { color: value.color }
                        : {}),
                    };
                    if (
                      Object.keys(changed).length === 0 ||
                      (await run(() => updateLabel(calendarId, label.id, changed)))
                    ) {
                      setEditing(null);
                    }
                  }}
                />
              ) : (
                <div className="flex items-center gap-3">
                  <span
                    aria-hidden
                    className="h-3.5 w-3.5 shrink-0 rounded-full"
                    style={{ backgroundColor: label.color }}
                  />
                  <span className="min-w-0 flex-1 truncate text-default text-[var(--color-text-primary)]">
                    {label.name}
                  </span>
                  {canManage && (
                    <>
                      <LabelledButton
                        label={t('labels.edit', { name: label.name })}
                        onClick={() => {
                          onAddingChange(false);
                          setEditing(label.id);
                        }}
                        className="icon-btn h-8 w-8"
                      >
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          aria-hidden="true"
                        >
                          <path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" />
                        </svg>
                      </LabelledButton>
                      <LabelledButton
                        label={t('labels.delete', { name: label.name })}
                        onClick={() => setPendingDelete(label)}
                        className="icon-btn icon-btn-danger h-8 w-8"
                      >
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                          aria-hidden="true"
                        >
                          <path d="M18 6L6 18M6 6l12 12" />
                        </svg>
                      </LabelledButton>
                    </>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      <ConfirmSheet
        open={pendingDelete !== null}
        title={t('labels.deleteTitle', { name: pendingDelete?.name ?? '' })}
        hint={t('labels.deleteConfirm')}
        confirmLabel={t('common.delete')}
        busy={busy}
        onConfirm={() => pendingDelete && confirmDelete(pendingDelete)}
        onCancel={() => setPendingDelete(null)}
      />
    </section>
  );
}
