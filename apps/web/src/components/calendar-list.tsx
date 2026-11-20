import { useEffect, useId, useRef, useState } from 'react';
import { CalendarCoverImage } from '@/components/calendar-cover-image';
import { EmptyState, GUIDE_ICONS } from '@/components/guide';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/api';
import { canManage, canOwn } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import { MEMBER_COLORS } from '@/types/calendar';

/**
 * Calendar list with per-calendar visibility toggles, a select-all control, and
 * an inline create form. Shared by the desktop left sidebar and the mobile menu.
 */
export function CalendarList() {
  const t = useT();
  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const toggleCalendarFilter = useCalendarStore((s) => s.toggleCalendarFilter);
  const setActiveCalendarIds = useCalendarStore((s) => s.setActiveCalendarIds);
  const addCalendar = useCalendarStore((s) => s.addCalendar);
  const deleteCalendar = useCalendarStore((s) => s.deleteCalendar);
  const openPanelFor = useUiStore((s) => s.openPanelFor);
  const [showNewForm, setShowNewForm] = useState(false);
  const [newName, setNewName] = useState('');
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null);
  // A calendar is only shared once someone is invited, so the one just made
  // offers that next step in place.
  const [justCreatedId, setJustCreatedId] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const newHintId = useId();

  const allActive = calendars.every((c) => activeCalendarIds.includes(c.id));

  const handleToggleAll = () => {
    if (allActive) {
      setActiveCalendarIds([]);
    } else {
      setActiveCalendarIds(calendars.map((c) => c.id));
    }
  };

  const handleCreateCalendar = async () => {
    if (!newName.trim()) return;
    const request = addCalendar({
      name: newName.trim(),
      color: MEMBER_COLORS[calendars.length % MEMBER_COLORS.length] ?? '#47B2F7',
    });
    setNewName('');
    setShowNewForm(false);
    try {
      const created = await request;
      if (created) setJustCreatedId(created.id);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const handleInvite = (id: string) => {
    setJustCreatedId(null);
    openPanelFor('share', id);
  };

  // Deleting takes the calendar's events, memos and album with it for every
  // member, so the trash icon only arms the confirmation -- the second press
  // is the one that deletes.
  const handleDeleteCalendar = async (id: string) => {
    setPendingDeleteId(null);
    try {
      await deleteCalendar(id);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  useEffect(() => {
    if (showNewForm) inputRef.current?.focus();
  }, [showNewForm]);

  return (
    <div className="flex min-h-0 flex-col" data-tour="calendars">
      <div className="flex items-center justify-between px-4 pt-4 pb-1">
        <span className="text-caption font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">
          {t('calendar.calendarList')}
        </span>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={handleToggleAll}
            className="icon-btn h-7 w-7 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
            aria-label={t('calendar.selectAll')}
          >
            {allActive ? (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                <rect
                  className="checkbox-box"
                  x="3"
                  y="3"
                  width="18"
                  height="18"
                  rx="3"
                  fill="var(--color-accent)"
                  stroke="var(--color-accent)"
                  strokeWidth="2"
                />
                <path
                  d="M8 12l3 3 5-5"
                  stroke="var(--color-text-on-accent)"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            ) : (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                <rect
                  className="checkbox-box"
                  x="3"
                  y="3"
                  width="18"
                  height="18"
                  rx="3"
                  stroke="var(--color-text-tertiary)"
                  strokeWidth="2"
                />
              </svg>
            )}
          </button>
          <button
            type="button"
            onClick={() => setShowNewForm(true)}
            className="icon-btn h-7 w-7 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
            aria-label={t('calendar.newCalendar')}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <line x1="12" y1="5" x2="12" y2="19" />
              <line x1="5" y1="12" x2="19" y2="12" />
            </svg>
          </button>
        </div>
      </div>
      <p className="px-4 pb-1 text-caption text-[var(--color-text-tertiary)]">
        {t('calendar.listHint')}
      </p>

      <div className="overflow-y-auto px-2 py-1">
        {calendars.length === 0 && !showNewForm && (
          <EmptyState
            compact
            icon={GUIDE_ICONS.calendar}
            title={t('empty.calendarsTitle')}
            description={t('empty.calendarsDesc')}
            action={{ label: t('calendar.newCalendar'), onClick: () => setShowNewForm(true) }}
          />
        )}
        {calendars.map((cal) => {
          const isActive = activeCalendarIds.includes(cal.id);
          const confirming = pendingDeleteId === cal.id;
          const offerInvite = justCreatedId === cal.id && canManage(cal.role);
          return (
            <div key={cal.id}>
              <div className="group flex w-full items-center rounded-lg transition-colors hover:bg-[var(--color-hover)]">
                <button
                  type="button"
                  onClick={() => toggleCalendarFilter(cal.id)}
                  className="flex min-w-0 flex-1 items-center gap-3 px-3 py-2 text-left"
                >
                  <CalendarCoverImage
                    calendar={cal}
                    className="h-7 w-7 shrink-0 rounded-md object-cover transition-opacity duration-200"
                    fallbackClassName="h-3 w-3 shrink-0 rounded-full transition-[background-color,opacity,transform] duration-200 group-active:scale-90"
                    fallbackColor={isActive ? cal.color : 'var(--color-text-tertiary)'}
                    inactive={!isActive}
                  />
                  <span
                    className="flex-1 truncate text-default"
                    style={{
                      color: isActive ? 'var(--color-text-primary)' : 'var(--color-text-tertiary)',
                    }}
                  >
                    {cal.name}
                  </span>
                  {cal.publicShared && (
                    <span
                      role="img"
                      title={t('calendar.publicShared')}
                      aria-label={t('calendar.publicShared')}
                      className="shrink-0 text-[var(--color-danger)]"
                    >
                      <svg
                        width="14"
                        height="14"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                      >
                        <circle cx="12" cy="12" r="10" />
                        <path d="M2 12h20" />
                        <path d="M12 2a15.3 15.3 0 014 10 15.3 15.3 0 01-4 10 15.3 15.3 0 01-4-10 15.3 15.3 0 014-10z" />
                      </svg>
                    </span>
                  )}
                  {isActive && (
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      className="pop-in shrink-0"
                      aria-hidden="true"
                    >
                      <path
                        d="M5 12l5 5L19 7"
                        stroke="var(--color-accent)"
                        strokeWidth="2.5"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      />
                    </svg>
                  )}
                </button>
                {canOwn(cal.role) && (
                  <button
                    type="button"
                    onClick={() => setPendingDeleteId(confirming ? null : cal.id)}
                    aria-label={t('calendar.deleteCalendar')}
                    aria-expanded={confirming}
                    title={t('calendar.deleteCalendar')}
                    className={`icon-btn icon-btn-danger mr-1 h-7 w-7 text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)] ${
                      confirming ? '' : 'reveal-on-hover'
                    }`}
                  >
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6" />
                    </svg>
                  </button>
                )}
              </div>

              {confirming && (
                <div className="reveal-in mx-2 mb-1 rounded-[var(--radius-md)] bg-[var(--color-surface-inset)] px-3 py-2">
                  <p className="text-caption text-[var(--color-text-secondary)]">
                    {t('calendar.deleteWarning', { name: cal.name })}
                  </p>
                  <div className="mt-2 flex gap-2">
                    <button
                      type="button"
                      onClick={() => setPendingDeleteId(null)}
                      className="btn-secondary h-8 flex-1 text-footnote"
                    >
                      {t('common.cancel')}
                    </button>
                    <button
                      type="button"
                      onClick={() => handleDeleteCalendar(cal.id)}
                      className="h-8 flex-1 rounded-[var(--radius-sm)] bg-[var(--color-danger-bg)] text-footnote font-medium text-[var(--color-danger)]"
                    >
                      {t('common.delete')}
                    </button>
                  </div>
                </div>
              )}

              {offerInvite && (
                <div className="callout reveal-in mx-2 mb-1 flex-col gap-0 px-3 py-2">
                  <p className="callout-title text-footnote">
                    {t('calendar.createdTitle', { name: cal.name })}
                  </p>
                  <p className="mt-0.5 text-caption text-[var(--color-text-secondary)]">
                    {t('calendar.createdInviteHint')}
                  </p>
                  <div className="mt-2 flex w-full gap-2">
                    <button
                      type="button"
                      onClick={() => setJustCreatedId(null)}
                      className="btn-secondary h-8 flex-1 text-footnote"
                    >
                      {t('calendar.inviteLater')}
                    </button>
                    <button
                      type="button"
                      onClick={() => handleInvite(cal.id)}
                      className="btn-primary h-8 flex-1 text-footnote"
                    >
                      {t('calendar.inviteMembers')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          );
        })}

        {showNewForm && (
          <div className="reveal-in px-2 py-2">
            <input
              data-1p-ignore
              ref={inputRef}
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleCreateCalendar();
                if (e.key === 'Escape') setShowNewForm(false);
              }}
              placeholder={t('calendar.calendarName')}
              className="input-modern h-9 w-full text-sm"
              aria-describedby={newHintId}
            />
            <p id={newHintId} className="mt-1.5 text-caption text-[var(--color-text-tertiary)]">
              {t('calendar.newCalendarHint')}
            </p>
            <div className="mt-2 flex gap-2">
              <button
                type="button"
                onClick={() => setShowNewForm(false)}
                className="btn-secondary h-8 flex-1 text-footnote"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                onClick={handleCreateCalendar}
                className="btn-primary h-8 flex-1 text-footnote"
              >
                {t('common.create')}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
