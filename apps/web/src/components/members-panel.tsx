import { useEffect, useRef, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { CustomSelect } from '@/components/pickers';
import { useT } from '@/i18n';
import { memberRowRules } from '@/lib/member-row';
import { roleLabelKey } from '@/lib/permissions';
import { useCalendarMembers } from '@/lib/use-calendar-members';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { useAuthStore } from '@/stores/auth-store';
import { useUiStore } from '@/stores/ui-store';
import type { Member } from '@/types/calendar';

/**
 * The colour picker on a member's row. The browser reports every step of a
 * drag through the picker; only the colour the picker is closed on is sent.
 */
function MemberColorInput({
  member,
  label,
  onCommit,
}: {
  member: Member;
  label: string;
  onCommit: (color: string) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState(member.color);
  const commitRef = useRef(onCommit);
  useEffect(() => {
    commitRef.current = onCommit;
  }, [onCommit]);

  useEffect(() => {
    setDraft(member.color);
  }, [member.color]);

  // React's onChange is the DOM input event; the native change event is the
  // one that fires once, when the choice is made.
  useEffect(() => {
    const input = inputRef.current;
    if (!input) return;
    const commit = () => commitRef.current(input.value);
    input.addEventListener('change', commit);
    return () => input.removeEventListener('change', commit);
  }, []);

  return (
    <label className="relative shrink-0" aria-label={label}>
      <Avatar src={member.avatar} name={member.name} size="h-9 w-9" color={draft} round />
      <input
        ref={inputRef}
        type="color"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        className="absolute inset-0 h-9 w-9 cursor-pointer opacity-0"
        aria-label={label}
      />
    </label>
  );
}

export function MembersPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const open = rightPanel === 'members';
  const panelRef = useModalA11y<HTMLDivElement>(open, () => toggleRightPanel('members'));
  const me = useAuthStore((s) => s.user);
  const { options, target, setTargetId } = usePanelCalendar();
  const calendarId = target?.id ?? '';
  // Nothing is asked of the server while the panel is closed.
  const {
    members,
    canManageMembers,
    amOwner,
    roleOptions,
    ownerCount,
    changeRole,
    removeMember,
    changeColor,
  } = useCalendarMembers(open ? calendarId : '');

  if (!open) return null;

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className="modal-backdrop fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={() => toggleRightPanel('members')}
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={t('panel.members')}
        className="glass-surface-heavy side-panel fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]"
      >
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <div className="min-w-0">
            <h2 className="truncate text-subhead font-semibold">{t('panel.members')}</h2>
          </div>
          <div className="flex items-center gap-2">
            {canManageMembers && (
              <button
                type="button"
                onClick={() => toggleRightPanel('share')}
                className="btn-primary px-3 py-1.5 text-footnote"
              >
                {t('invites.create')}
              </button>
            )}
            <button
              type="button"
              onClick={() => toggleRightPanel('members')}
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
        </div>

        {target && (
          <div className="border-b border-[var(--color-border)] px-5 py-3">
            <PanelCalendarPicker
              label={t('panel.targetCalendar')}
              options={options}
              target={target}
              onChange={setTargetId}
            />
          </div>
        )}

        <div className="flex-1 overflow-y-auto">
          {!calendarId ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">—</p>
          ) : members.length === 0 ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">—</p>
          ) : (
            <ul className="divide-y divide-[var(--color-separator)]">
              {members.map((m) => {
                const { isMe, canChangeRole, canRemove } = memberRowRules(m, {
                  meId: me?.id,
                  canManageMembers,
                  amOwner,
                  ownerCount,
                });
                return (
                  <li key={m.id} className="flex items-center gap-3 px-5 py-3">
                    {canManageMembers || isMe ? (
                      <MemberColorInput
                        member={m}
                        label={t('members.color')}
                        onCommit={(color) => changeColor(m, color)}
                      />
                    ) : (
                      <Avatar src={m.avatar} name={m.name} size="h-9 w-9" color={m.color} round />
                    )}
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-default font-semibold text-[var(--color-text-primary)]">
                        {m.name}
                        {isMe && (
                          <span className="ml-2 rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-caption font-medium text-[var(--color-accent)]">
                            {t('members.you')}
                          </span>
                        )}
                      </p>
                      <p className="truncate text-footnote text-[var(--color-text-secondary)]">
                        {m.email}
                      </p>
                    </div>
                    {canChangeRole ? (
                      <CustomSelect
                        value={m.role}
                        onChange={(v) => changeRole(m, v)}
                        className="shrink-0"
                        triggerClassName="rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
                        options={roleOptions.map((r) => ({ value: r, label: t(roleLabelKey(r)) }))}
                      />
                    ) : (
                      <span className="shrink-0 rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)]">
                        {t(roleLabelKey(m.role))}
                      </span>
                    )}
                    {canRemove && (
                      <button
                        type="button"
                        onClick={() => removeMember(m)}
                        className="flex h-8 w-8 shrink-0 items-center justify-center text-[var(--color-text-tertiary)] hover:bg-[var(--color-danger-bg)] hover:text-[var(--color-danger)]"
                        style={{ borderRadius: 'var(--radius-sm)' }}
                        aria-label={isMe ? t('members.leave') : t('common.delete')}
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
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </>
  );
}
