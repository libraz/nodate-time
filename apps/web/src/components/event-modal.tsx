import { DateTime } from 'luxon';
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { AttachmentsSection } from '@/components/event-attachments';
import { ChecklistSection } from '@/components/event-checklist';
import { CommentsSection } from '@/components/event-comments';
import { EventPeople, ShowAsPicker } from '@/components/event-people';
import { RecurrenceEditor } from '@/components/event-recurrence';
import { TipCard } from '@/components/guide';
import { HistoryTimeline } from '@/components/history-timeline';
import { LabelledButton } from '@/components/labelled-button';
import { CustomSelect, DateTimeField } from '@/components/pickers';
import { Switch } from '@/components/switch';
import { type TranslationKey, useT } from '@/i18n';
import { toAllDayInclusiveEndInput, toLocalDatetimeInput } from '@/lib/all-day';
import { ApiError, api, errorMessage } from '@/lib/api';
import { seriesId } from '@/lib/event-move';
import {
  eventTimesForSave,
  recurrenceAnchorStart,
  shiftStartKeepingDuration,
} from '@/lib/event-times';
import { canEdit, canEditEvent, roleOnCalendar, visibilityLabelKey } from '@/lib/permissions';
import { rebuildPresetRule, ruleToPreset } from '@/lib/recurrence-preset';
import { toast } from '@/lib/toast';
import { useIsDesktop } from '@/lib/use-is-desktop';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useAuthStore } from '@/stores/auth-store';
import { type EventInput, useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type {
  CalendarEvent,
  Flexibility,
  RecurrencePreset,
  RecurrenceRule,
  Rsvp,
  ShowAs,
  Visibility,
} from '@/types/calendar';
import {
  FALLBACK_LABEL_COLOR,
  NOTIFICATION_OFFSETS,
  normalizeNotificationOffset,
  VISIBILITY_OPTIONS,
} from '@/types/calendar';

/** Renders a stored UTC instant as a `yyyy-MM-ddTHH:mm` string in the given zone. */
function toLocalDatetime(iso: string, zone: string): string {
  return toLocalDatetimeInput(iso, zone);
}

/**
 * Produces an API-valid recurrence rule:
 * - drops a monthly `byDay` that lacks `bySetPos` (the server rejects it),
 * - serializes a date-only `until` to an RFC3339 instant at the end of that
 *   day in the event timezone so non-UTC users don't lose the final occurrence.
 */
function normalizeRuleForApi(rule: RecurrenceRule | null, zone: string): RecurrenceRule | null {
  if (!rule) return null;
  const next: RecurrenceRule = { ...rule };
  if (next.freq === 'monthly' && next.byDay && !next.bySetPos?.length) {
    next.byDay = undefined;
  }
  if (next.until && next.until.length === 10) {
    const end = DateTime.fromISO(next.until, { zone }).endOf('day');
    next.until = end.toISO() ?? next.until;
  }
  return next;
}

interface FormState {
  title: string;
  allDay: boolean;
  startAt: string;
  endAt: string;
  color: string;
  calendarId: string;
  location: string;
  memo: string;
  url: string;
  notificationOffset: number | null;
  participants: string[];
  ownerId: string | null;
  visibility: Visibility;
  showAs: ShowAs;
  flexibility: Flexibility;
  recurrencePreset: RecurrencePreset;
  recurrenceRule: RecurrenceRule | null;
}

export function EventModal() {
  const t = useT();
  const timezone = useUiStore((s) => s.timezone);
  const showEventModal = useUiStore((s) => s.showEventModal);
  const editingEventId = useUiStore((s) => s.editingEventId);
  const closeEventModal = useUiStore((s) => s.closeEventModal);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const eventDraftStart = useUiStore((s) => s.eventDraftStart);
  const eventDraftEnd = useUiStore((s) => s.eventDraftEnd);

  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const events = useCalendarStore((s) => s.events);
  const addEvent = useCalendarStore((s) => s.addEvent);
  const updateEvent = useCalendarStore((s) => s.updateEvent);
  const deleteEvent = useCalendarStore((s) => s.deleteEvent);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const fetchEvents = useCalendarStore((s) => s.fetchEvents);
  const visibleRange = useCalendarStore((s) => s.visibleRange);
  const me = useAuthStore((s) => s.user);

  const editingEvent = editingEventId ? events.find((e) => e.id === editingEventId) : null;
  // The zone an event's dates are anchored in. An existing event keeps the one
  // it was created in: its all-day dates and its recurrence grid are both
  // measured there, so re-anchoring it to whoever happens to have the modal
  // open would move the dates and shift every later occurrence across a DST
  // boundary. A new event takes the zone its author is working in.
  const anchorZone = editingEvent?.timezone || timezone;
  // The start as the server expands the rule: in the anchor zone, whichever zone
  // the fields are typed in.
  const recurrenceStart = (f: Pick<FormState, 'allDay' | 'startAt'>) =>
    recurrenceAnchorStart(f, timezone, anchorZone);
  // A preset follows the start it describes; a custom rule is the user's own.
  const withPresetRule = (f: FormState): FormState =>
    f.recurrencePreset !== 'custom' && f.recurrenceRule
      ? {
          ...f,
          recurrenceRule: rebuildPresetRule(
            f.recurrencePreset,
            recurrenceStart(f),
            f.recurrenceRule,
          ),
        }
      : f;
  const titleRef = useRef<HTMLTextAreaElement>(null);
  const [saving, setSaving] = useState(false);

  // Animated dismissal: play the exit transition, then unmount via the store.
  const EXIT_MS = 260;
  const [closing, setClosing] = useState(false);
  const closeTimer = useRef<number | null>(null);
  const requestClose = useCallback(() => {
    if (closeTimer.current) return;
    setClosing(true);
    closeTimer.current = window.setTimeout(() => {
      closeTimer.current = null;
      setClosing(false);
      closeEventModal();
    }, EXIT_MS);
  }, [closeEventModal]);
  useEffect(
    () => () => {
      if (closeTimer.current) window.clearTimeout(closeTimer.current);
    },
    [],
  );

  // Swipe-down-to-dismiss for the mobile bottom sheet, driven from its top handle.
  // The sheet tracks the finger 1:1 while held, then resolves on release. Window
  // listeners (not pointer capture) guarantee the release is caught even when the
  // finger drifts off the handle, so the sheet never rests at a partial offset.
  const [sheetDragY, setSheetDragY] = useState(0);
  const onSheetDown = (e: React.PointerEvent) => {
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    const start = { startY: e.clientY, lastY: e.clientY, lastT: e.timeStamp, vy: 0 };
    const ctrl = new AbortController();
    const move = (ev: globalThis.PointerEvent) => {
      const dt = ev.timeStamp - start.lastT;
      if (dt > 0) start.vy = (ev.clientY - start.lastY) / dt;
      start.lastY = ev.clientY;
      start.lastT = ev.timeStamp;
      const dy = ev.clientY - start.startY;
      setSheetDragY(dy > 0 ? dy : 0);
    };
    const end = () => {
      ctrl.abort();
      const dy = start.lastY - start.startY;
      // Dismiss on a long pull or a quick downward flick; otherwise snap back.
      if (dy > 160 || (dy > 48 && start.vy > 0.55)) requestClose();
      setSheetDragY(0);
    };
    window.addEventListener('pointermove', move, { signal: ctrl.signal });
    window.addEventListener('pointerup', end, { signal: ctrl.signal });
    window.addEventListener('pointercancel', end, { signal: ctrl.signal });
  };

  // Drive the sheet's spring entrance: mount off-screen, then settle on next frame.
  const [sheetIn, setSheetIn] = useState(false);
  useEffect(() => {
    if (!showEventModal) {
      setSheetIn(false);
      return;
    }
    // Opening (or re-opening mid-dismiss) must cancel any pending close, otherwise
    // the scheduled closeEventModal() would immediately shut the reopened modal.
    if (closeTimer.current) {
      clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
    setClosing(false);
    setSheetIn(false);
    const id = requestAnimationFrame(() => setSheetIn(true));
    return () => cancelAnimationFrame(id);
  }, [showEventModal]);
  const reduceMotion =
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
  // Layout is CSS's job; motion is not: the sheet is dragged by an inline
  // transform while the desktop panel springs in on a keyframe.
  const isDesktop = useIsDesktop();

  const [form, setForm] = useState<FormState>({
    title: '',
    allDay: true,
    startAt: '',
    endAt: '',
    color: FALLBACK_LABEL_COLOR,
    calendarId: '',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participants: [],
    ownerId: null,
    visibility: 'default',
    showAs: 'busy',
    flexibility: 'fixed',
    recurrencePreset: 'none',
    recurrenceRule: null,
  });
  // Optional fields (location/url/memo/reminder/visibility) collapse on create and
  // auto-expand when an existing event already uses any of them.
  const [showMore, setShowMore] = useState(false);
  // The revision this editor was opened on, as the server named it.
  const [revision, setRevision] = useState<string | null>(null);

  // The current user's role for the event's calendar gates all mutating UI,
  // and for an existing event so does whose layer it sits on: a shared
  // calendar is not a licence to rewrite everyone else's plans.
  const formCalendarId = form.calendarId || (editingEvent?.calendarId ?? '');
  const myRole = roleOnCalendar(calendars, formCalendarId);
  const editable = editingEvent ? canEditEvent(editingEvent, myRole, me?.id) : canEdit(myRole);
  // A task projects this event's title, dates and recurrence; the server refuses
  // changes to them, so the form shows them without offering to edit.
  const taskOwned = editingEvent?.taskOwned === true;

  // Calendars a new event can be created in: active in the sidebar and writable.
  const postableCalendars = useMemo(
    () => calendars.filter((c) => activeCalendarIds.includes(c.id) && canEdit(c.role)),
    [calendars, activeCalendarIds],
  );
  const defaultCalendarId = postableCalendars[0]?.id ?? calendars[0]?.id ?? '';

  // The participant picker names the people on the calendar being edited, so
  // that one list is fetched when the modal opens rather than every
  // calendar's at startup. Switching the calendar in the picker fetches the
  // new one; a list already held is not asked for again.
  useEffect(() => {
    if (!showEventModal || !formCalendarId) return;
    if (membersMap[formCalendarId]) return;
    fetchMembers(formCalendarId);
  }, [showEventModal, formCalendarId, membersMap, fetchMembers]);

  // Answering an invitation is the invitee's own to give and is not editing,
  // so the control appears for a participant of a saved event regardless of
  // whether they may change it.
  const myAttendance = editingEvent?.attendees?.find((a) => a.userId === me?.id);
  const [rsvp, setRsvp] = useState<Rsvp | null>(null);
  useEffect(() => {
    setRsvp(myAttendance?.rsvp ?? null);
  }, [myAttendance?.rsvp]);

  const answerRsvp = async (next: Rsvp) => {
    if (!editingEvent) return;
    const previous = rsvp;
    setRsvp(next);
    try {
      await api.put(`/calendars/${editingEvent.calendarId}/events/${editingEvent.id}/rsvp`, {
        rsvp: next,
      });
    } catch (e) {
      setRsvp(previous);
      toast.error(errorMessage(e));
      return;
    }
    // The answer lands on the series row, or on an edited occurrence's own, so
    // reading the range back is what puts it on every copy the store holds;
    // without it the modal reopens on the old answer.
    const { start, end } = visibleRange();
    void fetchEvents(start, end);
  };

  useEffect(() => {
    if (!showEventModal) return;
    if (editingEvent) {
      const rule = editingEvent.recurrenceRule ?? null;
      const startDt = DateTime.fromISO(editingEvent.startAt, { zone: anchorZone });
      const preset = ruleToPreset(rule, startDt);
      setForm({
        title: editingEvent.title,
        allDay: editingEvent.allDay,
        startAt: editingEvent.allDay
          ? toLocalDatetime(editingEvent.startAt, anchorZone)
          : toLocalDatetime(editingEvent.startAt, timezone),
        endAt: editingEvent.allDay
          ? toAllDayInclusiveEndInput(editingEvent.endAt, anchorZone)
          : toLocalDatetime(editingEvent.endAt, timezone),
        color: editingEvent.color,
        calendarId: editingEvent.calendarId,
        location: editingEvent.location,
        memo: editingEvent.memo,
        url: editingEvent.url ?? '',
        notificationOffset: editingEvent.notificationOffset ?? null,
        participants: editingEvent.participants ?? [],
        ownerId: editingEvent.ownerId ?? null,
        visibility: editingEvent.visibility ?? 'default',
        showAs: editingEvent.showAs ?? 'busy',
        flexibility: editingEvent.flexibility ?? 'fixed',
        recurrenceRule: rule,
        recurrencePreset: preset,
      });
      setShowMore(
        !!(
          editingEvent.location ||
          editingEvent.url ||
          editingEvent.memo ||
          editingEvent.notificationOffset != null ||
          (editingEvent.visibility && editingEvent.visibility !== 'default') ||
          (editingEvent.flexibility && editingEvent.flexibility !== 'fixed')
        ),
      );
    } else {
      // A draft start (e.g. a clicked weekly slot) creates a timed event at that
      // hour; otherwise default to an all-day event on the selected day.
      const timed = eventDraftStart != null;
      const base = (eventDraftStart ?? selectedDate).setZone(timezone, { keepLocalTime: true });
      const start = timed
        ? base.set({ second: 0, millisecond: 0 })
        : base.set({ hour: 9, minute: 0, second: 0, millisecond: 0 });
      const draftEnd = eventDraftEnd?.setZone(timezone, { keepLocalTime: true });
      const end = timed && draftEnd && draftEnd > start ? draftEnd : start.plus({ hours: 1 });
      setForm({
        title: '',
        allDay: !timed,
        startAt: start.toFormat("yyyy-MM-dd'T'HH:mm"),
        endAt: end.toFormat("yyyy-MM-dd'T'HH:mm"),
        color: FALLBACK_LABEL_COLOR,
        calendarId: defaultCalendarId,
        location: '',
        memo: '',
        url: '',
        notificationOffset: null,
        participants: [],
        ownerId: null,
        visibility: 'default',
        showAs: 'busy',
        flexibility: 'fixed',
        recurrencePreset: 'none',
        recurrenceRule: null,
      });
      setShowMore(false);
    }
  }, [
    editingEvent,
    showEventModal,
    selectedDate,
    eventDraftStart,
    eventDraftEnd,
    defaultCalendarId,
    timezone,
    anchorZone,
  ]);

  // Ask the server which revision this edit starts from. Saving is a full
  // replace, so without it two people with the editor open overwrite each
  // other and neither is told. A revision we could not read is left null,
  // which saves unconditionally -- the behaviour before this existed.
  useEffect(() => {
    if (!showEventModal || !editingEvent) {
      setRevision(null);
      return;
    }
    let cancelled = false;
    api
      .getWithRevision<CalendarEvent>(
        `/calendars/${editingEvent.calendarId}/events/${editingEvent.id}`,
      )
      .then(({ revision: rev }) => {
        if (!cancelled) setRevision(rev);
      })
      .catch(() => {
        if (!cancelled) setRevision(null);
      });
    return () => {
      cancelled = true;
    };
  }, [showEventModal, editingEvent]);

  // Recurring events are always opened as an expanded instance (composite id),
  // so editing or deleting one must ask whether it applies to just this
  // occurrence or the whole series.
  const isRecurringInstance =
    !!editingEvent && (editingEvent.isRecurrence || editingEvent.id.includes('_'));
  const [scopePrompt, setScopePrompt] = useState<null | 'save' | 'delete'>(null);

  const handleSave = useCallback(
    async (scope?: 'this' | 'all') => {
      if (!form.title.trim() || saving || !editable) return;
      // Editing an occurrence: ask this-vs-series before mutating anything.
      if (editingEvent && isRecurringInstance && !scope) {
        setScopePrompt('save');
        return;
      }
      const { startAt: startIso, endAt: endIso } = eventTimesForSave(form, timezone, anchorZone);
      // Annotated so TypeScript's excess-property check rejects any field the
      // API does not accept: the body schema forbids unknown properties, so a
      // stray key is a 422 on every save rather than a value the server ignores.
      const data: EventInput = {
        title: form.title.trim(),
        allDay: form.allDay,
        startAt: startIso,
        endAt: endIso,
        timezone: anchorZone,
        location: form.location,
        memo: form.memo,
        url: form.url,
        notificationOffset: normalizeNotificationOffset(form.notificationOffset),
        participants: form.participants,
        ownerId: form.ownerId,
        visibility: form.visibility,
        showAs: form.showAs,
        flexibility: form.flexibility,
        recurrenceRule: normalizeRuleForApi(form.recurrenceRule, anchorZone),
      };
      setSaving(true);
      try {
        if (editingEvent) {
          await updateEvent(editingEvent.calendarId, editingEvent.id, data, scope, revision);
        } else {
          await addEvent(form.calendarId, data);
        }
        setScopePrompt(null);
        requestClose();
      } catch (e) {
        // A refused save means someone else's version is the current one.
        // Leaving the editor open with the rejected text in it would invite
        // the same save again, so the event is reloaded and the form is
        // rebuilt from what is actually stored.
        if (e instanceof ApiError && e.code === 'EVENT.STALE') {
          toast.error(errorMessage(e));
          setScopePrompt(null);
          const { start, end } = visibleRange();
          // The refusal has already been reported; a reload that also fails
          // leaves the grid as it was rather than raising a second complaint.
          await fetchEvents(start, end).catch(() => {});
          return;
        }
        toast.error(errorMessage(e, t('error.saveFailed')));
      } finally {
        setSaving(false);
      }
    },
    [
      form,
      editingEvent,
      isRecurringInstance,
      addEvent,
      updateEvent,
      requestClose,
      saving,
      editable,
      timezone,
      anchorZone,
      revision,
      fetchEvents,
      visibleRange,
      t,
    ],
  );

  const handleDelete = useCallback(
    async (scope?: 'this' | 'all') => {
      if (!editingEvent || saving || !editable) return;
      if (isRecurringInstance && !scope) {
        setScopePrompt('delete');
        return;
      }
      setSaving(true);
      try {
        await deleteEvent(editingEvent.calendarId, editingEvent.id, scope);
        setScopePrompt(null);
        requestClose();
      } catch (e) {
        toast.error(errorMessage(e, t('error.deleteFailed')));
      } finally {
        setSaving(false);
      }
    },
    [editingEvent, isRecurringInstance, deleteEvent, requestClose, saving, editable, t],
  );

  // The innermost open surface owns the keyboard. The pickers portal their
  // dropdowns out of this subtree and close themselves on Escape, so the modal
  // stands down while one is up; the scope chooser is answered first too.
  const dismiss = () => {
    if (document.querySelector('.dropdown-panel')) return;
    if (scopePrompt) {
      setScopePrompt(null);
      return;
    }
    requestClose();
  };
  const panelRef = useModalA11y<HTMLDivElement>(showEventModal, dismiss, titleRef);
  const scopeRef = useModalA11y<HTMLDivElement>(scopePrompt !== null, () => setScopePrompt(null));
  // The chooser is a dialog over a dialog, so it carries its own trap: without
  // one its buttons sit outside the modal's container and Tab walks straight
  // out of the page rather than around the question being asked. The two traps
  // do not fight — each only intercepts Tab at the edge of its own container,
  // and the containers are disjoint — and the outer one keeps the scroll lock
  // and the return-focus target for the whole time the chooser is up.

  if (!showEventModal && !closing) return null;

  // The body of the modal. It is mounted once and placed by CSS, so every
  // section below asks the server for its contents once rather than per layout.
  const formContent = (
    <>
      {/* Header */}
      <div className="grid grid-cols-[2.5rem_1fr_2.5rem] items-center gap-3 px-6 py-3">
        <LabelledButton
          label={t('common.close')}
          onClick={requestClose}
          className="icon-btn h-10 w-10 bg-[var(--color-surface-secondary)] hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-text-secondary)"
            strokeWidth="2.5"
          >
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </LabelledButton>
        <span className="truncate text-center text-callout font-semibold text-[var(--color-text-primary)]">
          {editingEvent ? t('event.editEvent') : t('event.createEvent')}
        </span>
      </div>

      {/* Title */}
      <div className="px-6 pt-2 pb-4">
        <textarea
          data-1p-ignore
          ref={titleRef}
          value={form.title}
          onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
          readOnly={taskOwned}
          placeholder={t('event.titlePlaceholder')}
          rows={1}
          className="w-full resize-none border-b-2 border-transparent bg-transparent text-display font-light text-[var(--color-text-primary)] outline-none transition-colors placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--color-accent)]"
        />
      </div>

      {/* Identity: which calendar + which color (event's visual identity) */}
      {/* An event has no colour of its own: it sits on its owner's layer, and
          the layer is what carries a colour the whole calendar has agreed on.
          Changing it is done in the members list. With one calendar there is
          nothing to choose or name, so the card is left out. */}
      {(() => {
        const current = calendars.find((c) => c.id === formCalendarId);
        const calIcon = (
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-text-tertiary)"
            strokeWidth="2"
            className="shrink-0"
            aria-hidden="true"
          >
            <rect x="3" y="4" width="18" height="18" rx="2" />
            <path d="M8 2v4M16 2v4M3 10h18" />
          </svg>
        );
        let row: ReactNode = null;
        if (!editingEvent && postableCalendars.length > 1) {
          row = (
            <>
              {calIcon}
              <CustomSelect
                value={formCalendarId}
                onChange={(v) => setForm((f) => ({ ...f, calendarId: v }))}
                ariaLabel={t('event.calendar')}
                className="min-w-0 flex-1"
                triggerClassName="input-modern"
                options={postableCalendars.map((c) => ({
                  value: c.id,
                  label: c.name,
                  color: c.color,
                }))}
              />
            </>
          );
        } else if (current && calendars.length > 1) {
          row = (
            <>
              {calIcon}
              <span
                className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                style={{ backgroundColor: current.color }}
              />
              <span className="text-callout font-medium text-[var(--color-text-primary)]">
                {current.name}
              </span>
            </>
          );
        }
        if (!row) return null;
        return (
          <div className="px-6 pb-3">
            <div className="card-section flex items-center gap-3 bg-[var(--color-surface-secondary)] px-4 py-2.5">
              {row}
            </div>
          </div>
        );
      })()}

      {/* Creator (read-only metadata, shown only for existing events) */}
      {editingEvent &&
        (() => {
          const member = (membersMap[editingEvent.calendarId] ?? []).find(
            (m) => m.id === editingEvent.createdBy,
          );
          const name = editingEvent.creatorName || member?.name;
          if (!name) return null;
          return (
            <div className="flex items-center gap-2 px-6 pb-3 text-caption text-[var(--color-text-secondary)]">
              <span>{t('event.createdBy')}</span>
              <Avatar
                src={editingEvent.creatorAvatarUrl}
                name={name}
                size="h-5 w-5"
                className="text-caption"
              />
              <span className="font-medium text-[var(--color-text-primary)]">{name}</span>
            </div>
          );
        })()}

      {/* The reader's own answer to an invitation: theirs to give whether or
          not they may edit the plan. */}
      {myAttendance && (
        <div className="px-6 pb-3">
          <div className="card-section flex items-center justify-between gap-3 bg-[var(--color-surface-secondary)] px-4 py-3">
            <span className="text-footnote text-[var(--color-text-secondary)]">
              {t('event.yourRsvp')}
            </span>
            <div className="flex gap-1.5">
              {(['accepted', 'tentative', 'declined'] as const).map((answer) => (
                <button
                  key={answer}
                  type="button"
                  onClick={() => answerRsvp(answer)}
                  aria-pressed={rsvp === answer}
                  className="rounded-full px-3 py-1 text-footnote transition"
                  style={{
                    backgroundColor:
                      rsvp === answer ? 'var(--color-accent-bg)' : 'var(--color-surface-inset)',
                    color: rsvp === answer ? 'var(--color-accent)' : 'var(--color-text-secondary)',
                  }}
                >
                  {t(
                    answer === 'accepted'
                      ? 'event.rsvpAccepted'
                      : answer === 'tentative'
                        ? 'event.rsvpTentative'
                        : 'event.rsvpDeclined',
                  )}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Who: whose plan it is and who takes part. Left out on a calendar
          nobody else is on, where both answers are the author. */}
      {(membersMap[formCalendarId]?.length ?? 0) > 1 && (
        <div className="px-6 pb-3">
          {/* No "learn more" link: the help page is another route, and
              leaving for it would throw away the plan being written. */}
          {!editingEvent && (
            <TipCard id="event" className="mb-3">
              {t('tip.event')}
            </TipCard>
          )}
          <EventPeople
            members={membersMap[formCalendarId] ?? []}
            myId={me?.id}
            ownerId={form.ownerId}
            participants={form.participants}
            disabled={!editable}
            onOwnerChange={(id) => setForm((f) => ({ ...f, ownerId: id }))}
            onParticipantsChange={(ids) => setForm((f) => ({ ...f, participants: ids }))}
          />
        </div>
      )}

      {/* Date & Time card */}
      <div className="px-6">
        {taskOwned && (
          <p className="pb-2 text-footnote text-[var(--color-text-tertiary)]">
            {t('event.taskOwnedNote')}
          </p>
        )}
        <div className="card-section bg-[var(--color-surface-secondary)] p-4">
          <fieldset disabled={taskOwned} className="contents">
            <DateTimeField
              label={t('event.start')}
              dateValue={DateTime.fromISO(form.startAt, { zone: timezone })}
              timeValue={form.startAt.split('T')[1]?.slice(0, 5) ?? '09:00'}
              showTime={!form.allDay}
              onDateChange={(date) => {
                setForm((f) => {
                  const moved = shiftStartKeepingDuration(f, date, timezone);
                  return withPresetRule({ ...f, startAt: moved.startAt, endAt: moved.endAt });
                });
              }}
              onTimeChange={(time) => {
                const datePart = form.startAt.split('T')[0] ?? '';
                setForm((f) => {
                  const newStart = `${datePart}T${time}`;
                  // If end is now before start on same day, push end forward by 1 hour
                  const startDt = DateTime.fromISO(newStart, { zone: timezone });
                  const endDt = DateTime.fromISO(f.endAt, { zone: timezone });
                  if (endDt <= startDt) {
                    const newEnd = startDt.plus({ hours: 1 }).toFormat("yyyy-MM-dd'T'HH:mm");
                    return withPresetRule({ ...f, startAt: newStart, endAt: newEnd });
                  }
                  return withPresetRule({ ...f, startAt: newStart });
                });
              }}
            />
            <DateTimeField
              label={t('event.end')}
              dateValue={DateTime.fromISO(form.endAt, { zone: timezone })}
              timeValue={form.endAt.split('T')[1]?.slice(0, 5) ?? '10:00'}
              showTime={!form.allDay}
              onDateChange={(date) => {
                const time = form.endAt.split('T')[1] ?? '10:00';
                setForm((f) => {
                  const newEnd = `${date.toFormat('yyyy-MM-dd')}T${time}`;
                  // If end is before start, clamp to start date
                  const startDt = DateTime.fromISO(f.startAt, { zone: timezone });
                  const endDt = DateTime.fromISO(newEnd, { zone: timezone });
                  if (endDt < startDt) {
                    const startTime = f.startAt.split('T')[1] ?? '09:00';
                    return { ...f, endAt: `${date.toFormat('yyyy-MM-dd')}T${startTime}` };
                  }
                  return { ...f, endAt: newEnd };
                });
              }}
              onTimeChange={(time) => {
                const datePart = form.endAt.split('T')[0] ?? '';
                setForm((f) => {
                  const newEnd = `${datePart}T${time}`;
                  // If end is before start, don't allow it — keep old value
                  const startDt = DateTime.fromISO(f.startAt, { zone: timezone });
                  const endDt = DateTime.fromISO(newEnd, { zone: timezone });
                  if (endDt <= startDt) return f;
                  return { ...f, endAt: newEnd };
                });
              }}
              minDate={DateTime.fromISO(form.startAt, { zone: timezone })}
            />

            <div className="my-1 border-t border-[var(--color-border)] opacity-50" />

            {/* All day toggle */}
            <div className="flex items-center justify-between py-2.5">
              <span className="text-default text-[var(--color-text-primary)]">
                {t('calendar.allDay')}
              </span>
              <Switch
                label={t('calendar.allDay')}
                checked={form.allDay}
                onChange={() => setForm((f) => withPresetRule({ ...f, allDay: !f.allDay }))}
              />
            </div>

            <RecurrenceEditor
              preset={form.recurrencePreset}
              rule={form.recurrenceRule}
              start={recurrenceStart(form)}
              zone={anchorZone}
              showSummary={!!editingEvent?.isRecurrence}
              onChange={(update) =>
                setForm((f) => {
                  const next = update({ preset: f.recurrencePreset, rule: f.recurrenceRule });
                  return { ...f, recurrencePreset: next.preset, recurrenceRule: next.rule };
                })
              }
            />
          </fieldset>
        </div>
      </div>

      <div className="mt-3 px-6">
        <ShowAsPicker
          value={form.showAs}
          disabled={!editable}
          onChange={(v) => setForm((f) => ({ ...f, showAs: v }))}
        />
      </div>

      {/* Optional fields: collapsed by default on create, expanded when used */}
      <div className="mt-3 px-6">
        <button
          type="button"
          onClick={() => setShowMore((v) => !v)}
          aria-expanded={showMore}
          className="flex w-full items-center justify-between rounded-[var(--radius-md)] px-1 py-2 text-callout font-medium text-[var(--color-text-secondary)] transition-colors hover:text-[var(--color-text-primary)]"
        >
          <span>{t('event.moreOptions')}</span>
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            className="shrink-0 transition-transform duration-200"
            style={{ transform: showMore ? 'rotate(180deg)' : 'none' }}
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
      </div>

      {showMore && (
        <>
          {/* Details card (location + memo) */}
          <div className="mt-1 px-6">
            <div className="card-section bg-[var(--color-surface-secondary)] p-4">
              <div className="flex items-center gap-3 py-1">
                <svg
                  width="18"
                  height="18"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--color-text-tertiary)"
                  strokeWidth="2"
                  className="shrink-0"
                >
                  <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0118 0z" />
                  <circle cx="12" cy="10" r="3" />
                </svg>
                <input
                  data-1p-ignore
                  type="text"
                  value={form.location}
                  onChange={(e) => setForm((f) => ({ ...f, location: e.target.value }))}
                  placeholder={t('event.location')}
                  className="flex-1 bg-transparent text-callout text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
                />
              </div>
              <div className="my-1 border-t border-[var(--color-border)] opacity-50" />
              <div className="flex items-center gap-3 py-1">
                <svg
                  width="18"
                  height="18"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--color-text-tertiary)"
                  strokeWidth="2"
                  className="shrink-0"
                >
                  <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71" />
                  <path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71" />
                </svg>
                <input
                  data-1p-ignore
                  type="url"
                  value={form.url}
                  onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
                  placeholder={t('event.urlPlaceholder')}
                  className="flex-1 bg-transparent text-callout text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
                />
              </div>
              <div className="my-1 border-t border-[var(--color-border)] opacity-50" />
              <div className="flex items-start gap-3 py-1">
                <svg
                  width="18"
                  height="18"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--color-text-tertiary)"
                  strokeWidth="2"
                  className="mt-0.5 shrink-0"
                >
                  <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
                  <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" />
                </svg>
                <textarea
                  data-1p-ignore
                  value={form.memo}
                  onChange={(e) => setForm((f) => ({ ...f, memo: e.target.value }))}
                  placeholder={t('event.memo')}
                  maxLength={16000}
                  rows={2}
                  className="flex-1 resize-none bg-transparent text-callout text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
                />
              </div>
            </div>
          </div>

          {/* Reminder, visibility and flexibility card */}
          <div className="mt-3 px-6">
            <div className="card-section bg-[var(--color-surface-secondary)] p-4">
              {/* Notification. The reminder travels with the event as an
                  iCalendar alarm; the app itself does not deliver one, so the
                  hint names what will. */}
              <div className="py-1.5">
                <div className="flex items-center gap-3">
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="var(--color-text-tertiary)"
                    strokeWidth="2"
                    className="shrink-0"
                  >
                    <path d="M18 8A6 6 0 006 8c0 7-3 9-3 9h18s-3-2-3-9" />
                    <path d="M13.73 21a2 2 0 01-3.46 0" />
                  </svg>
                  <CustomSelect
                    value={String(form.notificationOffset ?? 'none')}
                    options={NOTIFICATION_OFFSETS.map(({ label, value }) => ({
                      value: String(value ?? 'none'),
                      label: t(`event.notification_${label}` as TranslationKey),
                    }))}
                    onChange={(v) => {
                      setForm((f) => ({
                        ...f,
                        notificationOffset: v === 'none' ? null : Number(v),
                      }));
                    }}
                    className="flex-1"
                  />
                </div>
                {form.notificationOffset != null && (
                  <p className="mt-1 pl-[30px] text-footnote text-[var(--color-text-tertiary)]">
                    {t('event.notification_hint')}
                  </p>
                )}
              </div>

              <div className="my-1 border-t border-[var(--color-border)] opacity-50" />

              {/* Visibility on a shared link */}
              <div className="flex items-center gap-3 py-1.5">
                <svg
                  width="18"
                  height="18"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--color-text-tertiary)"
                  strokeWidth="2"
                  className="shrink-0"
                >
                  <rect x="3" y="11" width="18" height="11" rx="2" />
                  <path d="M7 11V7a5 5 0 0110 0v4" />
                </svg>
                <CustomSelect
                  value={form.visibility}
                  options={VISIBILITY_OPTIONS.map((v) => ({
                    value: v,
                    label: t(visibilityLabelKey(v)),
                  }))}
                  onChange={(v) => setForm((f) => ({ ...f, visibility: v as Visibility }))}
                  className="flex-1"
                />
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2 py-1.5">
                <span className="text-body text-[var(--color-text-secondary)]">
                  {t('event.flexibility')}
                </span>
                <CustomSelect
                  ariaLabel={t('event.flexibility')}
                  value={form.flexibility}
                  options={[
                    { value: 'fixed', label: t('event.flexibilityFixed') },
                    { value: 'negotiable', label: t('event.flexibilityNegotiable') },
                    { value: 'conditional', label: t('event.flexibilityConditional') },
                  ]}
                  onChange={(value) =>
                    setForm((current) => ({ ...current, flexibility: value as Flexibility }))
                  }
                  className="min-w-36"
                />
              </div>
            </div>
          </div>
        </>
      )}

      {/* Checklist (edit mode only) */}
      {editingEvent && (
        <ChecklistSection
          calendarId={editingEvent.calendarId}
          eventId={editingEvent.id}
          editable={editable}
        />
      )}

      {/* Attachments (edit mode only) */}
      {editingEvent && (
        <AttachmentsSection
          calendarId={editingEvent.calendarId}
          eventId={editingEvent.id}
          editable={editable}
        />
      )}

      {/* Comments (edit mode only) */}
      {editingEvent && (
        <CommentsSection
          calendarId={editingEvent.calendarId}
          eventId={editingEvent.id}
          canPost={canEdit(myRole)}
          moderatorRole={myRole}
        />
      )}

      {/* History (edit mode only) */}
      {editingEvent && (
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
              <path d="M12 8v4l3 3" />
              <circle cx="12" cy="12" r="9" />
            </svg>
            <span className="text-default font-semibold text-[var(--color-text-primary)]">
              {t('history.title')}
            </span>
          </div>
          <HistoryTimeline
            kind="event"
            calendarId={editingEvent.calendarId}
            entityId={seriesId(editingEvent.id)}
          />
        </div>
      )}

      {/* Delete (edit mode, editors only; a task's event goes with the task) */}
      {editingEvent && editable && !taskOwned && (
        <div className="px-6 pt-4">
          <button
            type="button"
            onClick={() => handleDelete()}
            disabled={saving}
            className="w-full bg-[var(--color-danger-bg)] py-3 text-center text-default font-medium text-[var(--color-danger)] disabled:opacity-50"
            style={{ borderRadius: 'var(--radius-md)' }}
          >
            {editingEvent?.recurrenceRule ? t('event.deleteRecurring') : t('event.deleteEvent')}
          </button>
        </div>
      )}

      {/* Spacer so content doesn't hide behind sticky action bar on mobile */}
      <div className="h-20 sm:h-0" />
    </>
  );

  return (
    <>
      <button
        type="button"
        aria-label={t('common.close')}
        className={`modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)] ${
          closing ? 'backdrop-out' : ''
        }`}
        onClick={requestClose}
      />
      {/* One panel for both layouts: CSS rests it on the bottom edge as a sheet
          and centres it as a dialog from `sm` up. */}
      <div className="pointer-events-none fixed inset-0 z-50 flex flex-col justify-end sm:items-center sm:justify-center sm:p-4">
        <div
          ref={panelRef}
          role="dialog"
          aria-modal="true"
          aria-label={editingEvent ? t('event.editEvent') : t('event.createEvent')}
          className={`glass-surface-heavy pointer-events-auto flex w-full flex-col overflow-hidden ${
            isDesktop
              ? `modal-panel ${
                  closing ? 'modal-exit' : 'modal-enter'
                } max-h-[90dvh] max-w-[480px] ring-1 ring-[var(--color-border)]`
              : 'bottom-sheet event-sheet max-h-[92dvh]'
          }`}
          style={
            isDesktop
              ? undefined
              : {
                  // At rest, drop the transform entirely: a lingering one makes
                  // this a containing block, and anything fixed inside the sheet
                  // would then be positioned against the sheet, not the viewport.
                  transform: closing
                    ? 'translateY(100%)'
                    : sheetDragY
                      ? `translateY(${sheetDragY}px)`
                      : sheetIn
                        ? 'none'
                        : 'translateY(100%)',
                  transition: sheetDragY
                    ? 'none'
                    : reduceMotion
                      ? 'none'
                      : closing
                        ? 'transform 0.24s cubic-bezier(0.4, 0, 1, 1)'
                        : 'transform 0.42s cubic-bezier(0.34, 1.5, 0.5, 1)',
                }
          }
        >
          {/* Grab zone: drag down to hold/dismiss. Generous height for easy grab. */}
          <div
            className="shrink-0 cursor-grab touch-none pt-3 pb-2.5 active:cursor-grabbing sm:hidden"
            onPointerDown={onSheetDown}
          >
            <div className="drag-handle mx-auto h-1.5 w-11 rounded-full bg-[var(--color-text-tertiary)] opacity-40" />
          </div>
          <div className="flex-1 overflow-y-auto">{formContent}</div>
          {/* Sticky action bar (clears the home-indicator safe area, which is
              zero on a desktop viewport and leaves a plain 1rem there) */}
          <div
            className="flex gap-3 border-t border-[var(--color-border)] px-6 pt-4"
            style={{ paddingBottom: 'calc(env(safe-area-inset-bottom) + 1rem)' }}
          >
            <button
              type="button"
              onClick={requestClose}
              className="btn-secondary flex-1 py-3 text-default font-medium"
            >
              {editable ? t('common.cancel') : t('common.close')}
            </button>
            {editable && (
              <button
                type="button"
                onClick={() => handleSave()}
                disabled={saving || !form.title.trim()}
                className="btn-primary flex-1 py-3 text-default font-medium disabled:opacity-50"
              >
                {saving ? t('common.saving') : t('common.save')}
              </button>
            )}
          </div>
        </div>
      </div>

      {/* This-vs-series scope chooser for recurring occurrences */}
      {scopePrompt && (
        <>
          <button
            type="button"
            aria-label={t('common.cancel')}
            className="fixed inset-0 z-[60] bg-[var(--color-overlay)]"
            onClick={() => setScopePrompt(null)}
          />
          <div className="fixed inset-0 z-[60] flex items-center justify-center p-4 pointer-events-none">
            <div
              ref={scopeRef}
              role="dialog"
              aria-modal="true"
              aria-label={
                scopePrompt === 'delete' ? t('event.scopeDeleteTitle') : t('event.scopeEditTitle')
              }
              className="glass-surface-heavy pointer-events-auto flex w-full max-w-[360px] flex-col gap-3 p-6 ring-1 ring-[var(--color-border)]"
              style={{ borderRadius: 'var(--radius-lg)' }}
            >
              <p className="text-default font-semibold">
                {scopePrompt === 'delete' ? t('event.scopeDeleteTitle') : t('event.scopeEditTitle')}
              </p>
              <button
                type="button"
                disabled={saving}
                onClick={() =>
                  scopePrompt === 'delete' ? handleDelete('this') : handleSave('this')
                }
                className="btn-secondary py-3 text-default font-medium disabled:opacity-50"
              >
                {t('event.scopeThis')}
              </button>
              <button
                type="button"
                disabled={saving}
                onClick={() => (scopePrompt === 'delete' ? handleDelete('all') : handleSave('all'))}
                className="btn-secondary py-3 text-default font-medium disabled:opacity-50"
              >
                {t('event.scopeAll')}
              </button>
              <button
                type="button"
                onClick={() => setScopePrompt(null)}
                className="py-2 text-sm text-[var(--color-text-secondary)]"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </>
      )}
    </>
  );
}
