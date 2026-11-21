import { useEffect, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { TipCard } from '@/components/guide';
import { MemberAdder } from '@/components/member-adder';
import { MemberColorEditor } from '@/components/member-color-editor';
import { PanelCalendarPicker } from '@/components/panel-calendar-picker';
import { CustomSelect } from '@/components/pickers';
import { SidePanel } from '@/components/side-panel';
import { useT } from '@/i18n';
import { memberRowRules } from '@/lib/member-row';
import { roleLabelKey } from '@/lib/permissions';
import { useCalendarMembers, useMembersState } from '@/lib/use-calendar-members';
import { usePanelCalendar } from '@/lib/use-panel-calendar';
import { usePresence } from '@/lib/use-presence';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { Member } from '@/types/calendar';

const JUST_ADDED_MS = 2400;

export function MembersPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const openPanelFor = useUiStore((s) => s.openPanelFor);
  const presence = usePresence(rightPanel === 'members');
  const me = useAuthStore((s) => s.user);
  const { options, target, setTargetId } = usePanelCalendar();
  const [editingColorOf, setEditingColorOf] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [pendingRemove, setPendingRemove] = useState<Member | null>(null);
  const [removing, setRemoving] = useState(false);
  // Rows just added stay tinted for a moment, so the reader sees where they landed.
  const [justAdded, setJustAdded] = useState<ReadonlySet<string>>(new Set());
  const request = useUiStore((s) => s.panelCalendarRequest);
  const calendarId = target?.id ?? '';
  // Nothing is asked of the server while the panel is closed. It stays asked
  // through the exit, so the list does not empty out as the sheet slides away.
  const {
    members,
    canManageMembers,
    amOwner,
    roleOptions,
    ownerCount,
    changeRole,
    removeMember,
    changeColor,
  } = useCalendarMembers(presence.mounted ? calendarId : '');
  const { loading, error: loadError } = useMembersState(calendarId);
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const myRow = members.find((m) => m.id === me?.id);

  const confirmRemove = async (member: Member) => {
    setRemoving(true);
    await removeMember(member);
    setRemoving(false);
    setPendingRemove(null);
  };

  // The picker is a step inside the panel, not a place it reopens on.
  useEffect(() => {
    if (!presence.mounted) setAdding(false);
  }, [presence.mounted]);

  useEffect(() => {
    if (request?.addMembers) setAdding(true);
  }, [request]);

  useEffect(() => {
    if (justAdded.size === 0) return;
    const timer = setTimeout(() => setJustAdded(new Set()), JUST_ADDED_MS);
    return () => clearTimeout(timer);
  }, [justAdded]);

  if (!presence.mounted) return null;
  const picking = adding && canManageMembers && calendarId !== '';

  return (
    <SidePanel
      presence={presence}
      onClose={() => toggleRightPanel('members')}
      title={t('panel.members')}
      helpTopic="calendars"
      actions={
        canManageMembers &&
        !picking && (
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
            {t('members.addShort')}
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
          {!picking && (
            <>
              <p className="mt-2 text-caption text-[var(--color-text-secondary)]">
                {t('members.layerHint')}
              </p>
              <TipCard id="members" topic="calendars" className="mt-3">
                {t('tip.members')}
              </TipCard>
            </>
          )}
        </div>
      )}

      {picking ? (
        <MemberAdder
          key={calendarId}
          calendarId={calendarId}
          onDone={(ids) => {
            setAdding(false);
            setJustAdded(new Set(ids));
          }}
          onInviteByLink={() => openPanelFor('share', calendarId)}
        />
      ) : (
        <div className="flex-1 overflow-y-auto">
          {!calendarId ? (
            <p className="py-10 text-center text-body text-[var(--color-text-tertiary)]">—</p>
          ) : loading ? (
            <ul className="divide-y divide-[var(--color-separator)]" aria-busy="true">
              {[0, 1, 2].map((i) => (
                <li key={i} className="flex animate-pulse items-center gap-3 px-5 py-3">
                  <span className="h-9 w-9 shrink-0 rounded-full bg-[var(--color-surface-inset)]" />
                  <span className="h-4 flex-1 rounded bg-[var(--color-surface-inset)]" />
                </li>
              ))}
            </ul>
          ) : loadError && members.length === 0 ? (
            <div className="flex flex-col items-center gap-3 px-5 py-10 text-center">
              <p className="text-body text-[var(--color-danger)]">{t('members.loadFailed')}</p>
              <button
                type="button"
                onClick={() => fetchMembers(calendarId)}
                className="btn-secondary btn-sm"
              >
                {t('error.retry')}
              </button>
            </div>
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
                  <li
                    key={m.id}
                    className={`px-5 py-3 transition-colors duration-1000 ${
                      justAdded.has(m.id) ? 'reveal-in bg-[var(--color-accent-bg)]' : ''
                    }`}
                  >
                    <div className="flex items-center gap-3">
                      {canManageMembers || isMe ? (
                        <button
                          type="button"
                          onClick={() => setEditingColorOf(editingColorOf === m.id ? null : m.id)}
                          aria-expanded={editingColorOf === m.id}
                          aria-label={`${t('members.color')}: ${m.name}`}
                          title={t('members.color')}
                          className="member-color-trigger relative shrink-0 rounded-full"
                        >
                          <Avatar
                            src={m.avatarUrl}
                            name={m.name}
                            size="h-9 w-9"
                            color={m.color}
                            round
                          />
                          <span
                            aria-hidden
                            className="absolute -right-0.5 -bottom-0.5 h-3.5 w-3.5 rounded-full border-2 border-[var(--color-surface-elevated)]"
                            style={{ backgroundColor: m.color }}
                          />
                        </button>
                      ) : (
                        <Avatar
                          src={m.avatarUrl}
                          name={m.name}
                          size="h-9 w-9"
                          color={m.color}
                          round
                        />
                      )}
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-default font-semibold text-[var(--color-text-primary)]">
                          {m.name}
                          {isMe && (
                            <span className="ml-2 rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-caption font-medium text-[var(--color-accent-ink)]">
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
                          ariaLabel={t('members.roleFor', { name: m.name })}
                          className="shrink-0"
                          triggerClassName="rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
                          options={roleOptions.map((r) => ({
                            value: r,
                            label: t(roleLabelKey(r)),
                          }))}
                        />
                      ) : (
                        <span className="shrink-0 rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-footnote text-[var(--color-text-secondary)]">
                          {t(roleLabelKey(m.role))}
                        </span>
                      )}
                      {canRemove && (
                        <button
                          type="button"
                          onClick={() => setPendingRemove(m)}
                          className="icon-btn icon-btn-danger h-8 w-8"
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
                    </div>
                    {editingColorOf === m.id && (canManageMembers || isMe) && (
                      <MemberColorEditor
                        member={m}
                        others={members.filter((o) => o.id !== m.id)}
                        onCommit={(color) => changeColor(m, color)}
                      />
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          {myRow &&
            memberRowRules(myRow, { meId: me?.id, canManageMembers, amOwner, ownerCount })
              .canRemove && (
              <div className="px-5 py-4">
                <button
                  type="button"
                  onClick={() => setPendingRemove(myRow)}
                  className="btn-secondary btn-sm btn-danger w-full"
                >
                  {t('members.leaveButton')}
                </button>
              </div>
            )}
          {members.length === 1 && (
            <p className="px-5 py-4 text-caption text-[var(--color-text-tertiary)]">
              {t('members.soloHint')}
            </p>
          )}
        </div>
      )}

      <ConfirmSheet
        open={pendingRemove !== null}
        title={
          pendingRemove?.id === me?.id
            ? t('confirm.leaveTitle')
            : t('confirm.removeMember', { name: pendingRemove?.name ?? '' })
        }
        hint={pendingRemove?.id === me?.id ? t('members.leaveConfirm') : t('members.removeConfirm')}
        confirmLabel={pendingRemove?.id === me?.id ? t('members.leave') : t('confirm.remove')}
        busy={removing}
        onConfirm={() => pendingRemove && confirmRemove(pendingRemove)}
        onCancel={() => setPendingRemove(null)}
      />
    </SidePanel>
  );
}
