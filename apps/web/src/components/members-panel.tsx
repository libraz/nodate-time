import { useT } from '@/i18n';
import { ApiError, api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type { Member } from '@/types/calendar';
import { useEffect } from 'react';

export function MembersPanel() {
  const t = useT();
  const rightPanel = useUiStore((s) => s.rightPanel);
  const toggleRightPanel = useUiStore((s) => s.toggleRightPanel);
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const me = useAuthStore((s) => s.user);

  const calendarId = activeCalendarIds[0] ?? calendars[0]?.id ?? '';
  const calendar = calendars.find((c) => c.id === calendarId);
  const members = (calendarId && membersMap[calendarId]) || [];
  const myMembership = members.find((m) => m.email === me?.email);
  const isAdmin = myMembership?.role === 'admin';
  const adminCount = members.filter((m) => m.role === 'admin').length;

  useEffect(() => {
    if (rightPanel === 'members' && calendarId) {
      fetchMembers(calendarId);
    }
  }, [rightPanel, calendarId, fetchMembers]);

  if (rightPanel !== 'members') return null;

  const handleRoleChange = async (member: Member, role: string) => {
    if (member.role === 'admin' && role !== 'admin' && adminCount <= 1) {
      toast.error(t('members.lastAdmin'));
      return;
    }
    try {
      await api.put(`/calendars/${calendarId}/members/${member.id}/role`, { role });
      await fetchMembers(calendarId);
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : 'Error');
    }
  };

  const handleRemove = async (member: Member) => {
    if (!confirm(t('members.removeConfirm'))) return;
    try {
      await api.delete(`/calendars/${calendarId}/members/${member.id}`);
      await fetchMembers(calendarId);
      toast.success(t('panel.updated'));
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : t('members.lastAdmin'));
    }
  };

  return (
    <>
      <div
        className="fixed inset-0 z-40 bg-[var(--color-overlay)]"
        onClick={() => toggleRightPanel('members')}
        onKeyDown={undefined}
      />
      <div className="glass-surface-heavy fixed right-0 top-0 z-40 flex h-full w-full max-w-[420px] flex-col border-l border-[var(--color-border)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-5 py-4">
          <div className="min-w-0">
            <h2 className="truncate text-[16px] font-semibold">{t('panel.members')}</h2>
            {calendar && (
              <p className="truncate text-[12px] text-[var(--color-text-secondary)]">
                {calendar.name}
              </p>
            )}
          </div>
          <div className="flex items-center gap-2">
            {isAdmin && (
              <button
                type="button"
                onClick={() => toggleRightPanel('share')}
                className="btn-primary px-3 py-1.5 text-[12px]"
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

        <div className="flex-1 overflow-y-auto">
          {!calendarId ? (
            <p className="py-10 text-center text-[13px] text-[var(--color-text-tertiary)]">—</p>
          ) : members.length === 0 ? (
            <p className="py-10 text-center text-[13px] text-[var(--color-text-tertiary)]">—</p>
          ) : (
            <ul className="divide-y divide-[var(--color-separator)]">
              {members.map((m) => {
                const isMe = m.email === me?.email;
                const lastAdmin = m.role === 'admin' && adminCount <= 1;
                const canChangeRole = isAdmin && !lastAdmin;
                const canRemove = (isAdmin || isMe) && !(lastAdmin && isMe);
                return (
                  <li key={m.id} className="flex items-center gap-3 px-5 py-3">
                    <span
                      aria-hidden
                      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-[14px] font-bold text-white"
                      style={{ backgroundColor: m.color }}
                    >
                      {m.icon || m.name.slice(0, 1)}
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[14px] font-semibold text-[var(--color-text-primary)]">
                        {m.name}
                        {isMe && (
                          <span className="ml-2 rounded-full bg-[var(--color-accent-bg)] px-2 py-0.5 text-[11px] font-medium text-[var(--color-accent)]">
                            you
                          </span>
                        )}
                      </p>
                      <p className="truncate text-[12px] text-[var(--color-text-secondary)]">
                        {m.email}
                      </p>
                    </div>
                    {canChangeRole ? (
                      <select
                        value={m.role}
                        onChange={(e) => handleRoleChange(m, e.target.value)}
                        className="input-modern shrink-0 text-[12px]"
                      >
                        <option value="admin">{t('members.roleAdmin')}</option>
                        <option value="member">{t('members.roleMember')}</option>
                        <option value="viewer">{t('members.roleViewer')}</option>
                      </select>
                    ) : (
                      <span className="shrink-0 rounded-full bg-[var(--color-surface-inset)] px-3 py-1 text-[12px] text-[var(--color-text-secondary)]">
                        {m.role === 'admin'
                          ? t('members.roleAdmin')
                          : m.role === 'viewer'
                            ? t('members.roleViewer')
                            : t('members.roleMember')}
                      </span>
                    )}
                    {canRemove && (
                      <button
                        type="button"
                        onClick={() => handleRemove(m)}
                        className="flex h-8 w-8 shrink-0 items-center justify-center text-[var(--color-text-tertiary)] hover:bg-[var(--color-danger-bg)] hover:text-[var(--color-danger)]"
                        style={{ borderRadius: 'var(--radius-sm)' }}
                        aria-label={t('common.delete')}
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
