import { DateTime } from 'luxon';
import {
  type KeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
import { Avatar } from '@/components/avatar';
import { Callout } from '@/components/callout';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { CustomSelect } from '@/components/custom-select';
import { DateTimeRangeField } from '@/components/date-time-fields';
import { AttachmentsSection } from '@/components/event-attachments';
import {
  ChecklistSection,
  type DraftChecklist,
  EMPTY_DRAFT_CHECKLIST,
  saveDraftChecklist,
} from '@/components/event-checklist';
import { CommentsSection } from '@/components/event-comments';
import { EventPeople, ShowAsPicker } from '@/components/event-people';
import { RecurrenceEditor } from '@/components/event-recurrence';
import { TipCard } from '@/components/guide';
import { HistoryTimeline } from '@/components/history-timeline';
import { LabelledButton } from '@/components/labelled-button';
import { Switch } from '@/components/switch';
import { type TranslationKey, useT } from '@/i18n';
import { toAllDayInclusiveEndInput, toLocalDatetimeInput } from '@/lib/all-day';
import { ApiError, client, errorMessage, isStaleError } from '@/lib/client';
import { writableVisibleCalendars } from '@/lib/create-event';
import { seriesId } from '@/lib/event-move';
import {
  eventTimesForSave,
  recurrenceAnchorStart,
  shiftStartKeepingDuration,
} from '@/lib/event-times';
import { isSubmitEnter } from '@/lib/keys';
import {
  canEdit,
  canEditEvent,
  canManage,
  roleOnCalendar,
  visibilityLabelKey,
} from '@/lib/permissions';
import { rebuildPresetRule, ruleToPreset } from '@/lib/recurrence-preset';
import { toast } from '@/lib/toast';
import { useIsDesktop } from '@/lib/use-is-desktop';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useSheetDrag } from '@/lib/use-sheet-drag';
import { useUnsavedGuard } from '@/lib/use-unsaved-guard';
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
import { FALLBACK_LABEL_COLOR, NOTIFICATION_OFFSETS, VISIBILITY_OPTIONS } from '@/types/calendar';

const VISIBILITY_HINT_KEY: Record<Visibility, TranslationKey> = {
  default: 'event.visibilityDefaultHint',
  public: 'event.visibilityPublicHint',
  private: 'event.visibilityPrivateHint',
  confidential: 'event.visibilityConfidentialHint',
};

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
    delete next.byDay;
  }
  if (next.until && next.until.length === 10) {
    const end = DateTime.fromISO(next.until, { zone }).endOf('day');
    next.until = end.toISO() ?? next.until;
  }
  return next;
}

/** The i18n key and count naming an offset that is not one of the presets. */
function customOffsetLabel(minutes: number): { key: TranslationKey; n: number } {
  if (minutes > 0 && minutes % 1440 === 0)
    return { key: 'notification.customDays', n: minutes / 1440 };
  if (minutes > 0 && minutes % 60 === 0)
    return { key: 'notification.customHours', n: minutes / 60 };
  return { key: 'notification.customMinutes', n: minutes };
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
  participantIds: string[];
  ownerId: string | null;
  labelId: string | null;
  visibility: Visibility;
  showAs: ShowAs;
  flexibility: Flexibility;
  recurrencePreset: RecurrencePreset;
  recurrenceRule: RecurrenceRule | null;
}

/** Structural equality over plain data; a key holding `undefined` counts as absent. */
function sameData(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false;
  if (Array.isArray(a) || Array.isArray(b)) {
    return (
      Array.isArray(a) &&
      Array.isArray(b) &&
      a.length === b.length &&
      a.every((v, i) => sameData(v, b[i]))
    );
  }
  const ra = a as Record<string, unknown>;
  const rb = b as Record<string, unknown>;
  const keys = new Set([...Object.keys(ra), ...Object.keys(rb)]);
  return [...keys].every((k) => sameData(ra[k], rb[k]));
}

/** Whether two forms say the same thing; participants are a set, not a sequence. */
function formEqual(a: FormState, b: FormState): boolean {
  const { participantIds: pa, ...restA } = a;
  const { participantIds: pb, ...restB } = b;
  const setB = new Set(pb);
  return pa.length === setB.size && pa.every((id) => setB.has(id)) && sameData(restA, restB);
}

/** The form an existing event opens on, with its dates read in the zone they are edited in. */
function eventToForm(ev: CalendarEvent, timezone: string): FormState {
  const anchorZone = ev.timezone || timezone;
  const rule = ev.recurrenceRule ?? null;
  const startDt = DateTime.fromISO(ev.startAt, { zone: anchorZone });
  return {
    title: ev.title,
    allDay: ev.allDay,
    startAt: ev.allDay
      ? ev.startDate
        ? `${ev.startDate}T00:00`
        : toLocalDatetime(ev.startAt, anchorZone)
      : toLocalDatetime(ev.startAt, timezone),
    endAt: ev.allDay
      ? ev.endDate
        ? DateTime.fromISO(ev.endDate).minus({ days: 1 }).toFormat("yyyy-MM-dd'T'00:00")
        : toAllDayInclusiveEndInput(ev.endAt, anchorZone)
      : toLocalDatetime(ev.endAt, timezone),
    color: ev.color,
    calendarId: ev.calendarId,
    location: ev.location,
    memo: ev.memo,
    url: ev.url ?? '',
    notificationOffset: ev.notificationOffset ?? null,
    participantIds: ev.participantIds ?? [],
    ownerId: ev.ownerId ?? null,
    labelId: ev.labelId ?? null,
    visibility: ev.visibility ?? 'default',
    showAs: ev.showAs ?? 'busy',
    flexibility: ev.flexibility ?? 'fixed',
    recurrenceRule: rule,
    recurrencePreset: ruleToPreset(rule, startDt),
  };
}

/**
 * The event editor, mounted once. On a phone it is a modal bottom sheet; on a
 * desktop only its panel is portalled into `desktopSlot`, beside the calendar
 * and non-modal, so the calendar stays usable while it is open.
 */
export function EventModal({ desktopSlot = null }: { desktopSlot?: HTMLElement | null }) {
  const t = useT();
  const timezone = useUiStore((s) => s.timezone);
  const showEventModal = useUiStore((s) => s.showEventModal);
  const editingEventId = useUiStore((s) => s.editingEventId);
  const closeEventModal = useUiStore((s) => s.closeEventModal);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const eventDraftStart = useUiStore((s) => s.eventDraftStart);
  const eventDraftEnd = useUiStore((s) => s.eventDraftEnd);
  const eventDraftDay = useUiStore((s) => s.eventDraftDay);
  const eventOpenSeq = useUiStore((s) => s.eventOpenSeq);
  const pendingEventOpen = useUiStore((s) => s.pendingEventOpen);
  const applyPendingEventOpen = useUiStore((s) => s.applyPendingEventOpen);
  const clearPendingEventOpen = useUiStore((s) => s.clearPendingEventOpen);

  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const events = useCalendarStore((s) => s.events);
  const addEvent = useCalendarStore((s) => s.addEvent);
  const updateEvent = useCalendarStore((s) => s.updateEvent);
  const deleteEvent = useCalendarStore((s) => s.deleteEvent);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const labelsMap = useCalendarStore((s) => s.labelsMap);
  const labelErrors = useCalendarStore((s) => s.labelErrors);
  const fetchLabels = useCalendarStore((s) => s.fetchLabels);
  const fetchEvents = useCalendarStore((s) => s.fetchEvents);
  const visibleRange = useCalendarStore((s) => s.visibleRange);
  const me = useAuthStore((s) => s.user);

  // The event as the form was built from it. Moving to another month drops the
  // event from the store; without this a save would then create a duplicate.
  const [openedEvent, setOpenedEvent] = useState<CalendarEvent | null>(null);
  const editingEvent = editingEventId
    ? (events.find((e) => e.id === editingEventId) ??
      (openedEvent?.id === editingEventId ? openedEvent : null))
    : null;
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
    setConfirming(null);
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
    participantIds: [],
    ownerId: null,
    labelId: null,
    visibility: 'default',
    showAs: 'busy',
    flexibility: 'fixed',
    recurrencePreset: 'none',
    recurrenceRule: null,
  });
  // The form as it was opened or last saved; an edit is whatever differs from it.
  const initialForm = useRef<FormState | null>(null);
  // A new event's checklist, written once the event has been created.
  const [draftChecklist, setDraftChecklist] = useState<DraftChecklist>(EMPTY_DRAFT_CHECKLIST);
  // Which confirmation is up: discarding unsaved edits, or deleting the event.
  const [confirming, setConfirming] = useState<null | 'discard' | 'delete'>(null);
  // Optional fields (location/url/memo/reminder/visibility) collapse on create and
  // auto-expand when an existing event already uses any of them.
  const [showMore, setShowMore] = useState(false);
  // The versions this editor was opened on. A refetch replaces the stored event
  // but not these, so a save made after someone else's is refused, not merged.
  const openedEtags = useRef<{ etag: string; seriesEtag: string | undefined } | null>(null);
  // Grants changed in this session, shown until the stored copy catches up.
  const [grants, setGrants] = useState<Record<string, boolean>>({});

  // The current user's role for the event's calendar gates all mutating UI,
  // and for an existing event so does whose layer it sits on: a shared
  // calendar is not a licence to rewrite everyone else's plans.
  const formCalendarId = form.calendarId || (editingEvent?.calendarId ?? '');
  const myRole = roleOnCalendar(calendars, formCalendarId);
  const editable = editingEvent ? canEditEvent(editingEvent, myRole, me?.id) : canEdit(myRole);
  // A task projects this event's title, dates and recurrence; the server refuses
  // changes to them, so the form shows them without offering to edit.
  const taskOwned = editingEvent?.taskOwned === true;
  // The server withheld location, memo, url and the like; the form has nothing
  // true to show for them and must not send them back.
  const detailsHidden = editingEvent?.detailsHidden === true;
  // An end before the start is refused; an end equal to it is a zero-length
  // event, which the API accepts.
  const endError =
    !form.allDay &&
    DateTime.fromISO(form.endAt, { zone: timezone }) <
      DateTime.fromISO(form.startAt, { zone: timezone });
  const hasDraftChecklist =
    !editingEvent && (draftChecklist.items.length > 0 || draftChecklist.pending.trim() !== '');
  const isDirty =
    editable &&
    initialForm.current !== null &&
    (!formEqual(form, initialForm.current) || hasDraftChecklist);
  // Every way out of the editor asks first when it would throw edits away.
  const guardedClose = () => {
    if (closeTimer.current) return;
    if (isDirty) {
      setConfirming('discard');
      return;
    }
    requestClose();
  };
  useUnsavedGuard(isDirty, false);
  // The sheet snaps back before dismissing, so a discard question is asked over a sheet at rest.
  const sheetDrag = useSheetDrag(guardedClose);
  const sheetDragY = sheetDrag.style.transform;
  // Mirrors the API: on create any writer may file the plan under anyone; on
  // edit only a manager or the event's owner may move it, and only they may
  // take off a participant trusted to edit it.
  const canChangeOwner = editingEvent
    ? canManage(myRole) || editingEvent.ownerId === me?.id
    : canEdit(myRole);
  // A non-manager who files a new plan under someone else cannot edit it after.
  const otherOwner =
    !editingEvent && !canManage(myRole) && form.ownerId && form.ownerId !== me?.id
      ? membersMap[formCalendarId]?.find((m) => m.id === form.ownerId)
      : undefined;
  const ownerHint = otherOwner ? t('event.ownerOtherHint', { name: otherOwner.name }) : undefined;

  // An offset outside the presets stays selectable, so opening and saving never resets it.
  const notificationOptions = useMemo(() => {
    const options = NOTIFICATION_OFFSETS.map(({ label, value }) => ({
      value: String(value ?? 'none'),
      label: t(`event.notification_${label}` as TranslationKey),
    }));
    const current = form.notificationOffset;
    if (current == null || NOTIFICATION_OFFSETS.some((o) => o.value === current)) return options;
    const { key, n } = customOffsetLabel(current);
    return [...options, { value: String(current), label: t(key, { n }) }];
  }, [form.notificationOffset, t]);

  // Calendars a new event can be created in: active in the sidebar and writable.
  const postableCalendars = useMemo(
    () => writableVisibleCalendars(calendars, activeCalendarIds),
    [calendars, activeCalendarIds],
  );
  const defaultCalendarId = postableCalendars[0]?.id ?? '';

  // The participant picker names the people on the calendar being edited, so
  // that one list is fetched when the modal opens rather than every
  // calendar's at startup. Switching the calendar in the picker fetches the
  // new one; a list already held is not asked for again.
  useEffect(() => {
    if (!showEventModal || !formCalendarId) return;
    if (membersMap[formCalendarId]) return;
    fetchMembers(formCalendarId);
  }, [showEventModal, formCalendarId, membersMap, fetchMembers]);

  // The label picker names the labels of the calendar being edited, asked for
  // the same way; a failed read is not retried on every render.
  useEffect(() => {
    if (!showEventModal || !formCalendarId) return;
    if (labelsMap[formCalendarId] || labelErrors[formCalendarId]) return;
    void fetchLabels(formCalendarId);
  }, [showEventModal, formCalendarId, labelsMap, labelErrors, fetchLabels]);
  const calendarLabels = (formCalendarId && labelsMap[formCalendarId]) || [];

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
      await client.PUT('/calendars/{calendarId}/events/{eventId}/rsvp', {
        params: { path: { calendarId: editingEvent.calendarId, eventId: editingEvent.id } },
        body: { rsvp: next },
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

  // A grant is its own request, made on the spot rather than with the save.
  const setCanEdit = async (userId: string, next: boolean) => {
    if (!editingEvent) return;
    const previous = grants[userId];
    setGrants((g) => ({ ...g, [userId]: next }));
    try {
      await client.PUT('/calendars/{calendarId}/events/{eventId}/attendees/{userId}', {
        params: {
          path: { calendarId: editingEvent.calendarId, eventId: editingEvent.id, userId },
        },
        body: { canEdit: next },
      });
    } catch (e) {
      setGrants((g) => {
        const { [userId]: _dropped, ...rest } = g;
        return previous === undefined ? rest : { ...rest, [userId]: previous };
      });
      toast.error(errorMessage(e));
      return;
    }
    const { start, end } = visibleRange();
    void fetchEvents(start, end);
  };

  // The form is built once per opening (or per event being edited), never from
  // a refetch: the store replaces the event object whenever the month reloads,
  // and rebuilding then would throw away what the user has typed.
  const populatedFor = useRef<string | null>(null);
  const populateKey = `${editingEventId ?? 'new'}:${eventOpenSeq}`;
  const [remoteChanged, setRemoteChanged] = useState(false);
  useEffect(() => {
    if (!showEventModal) {
      populatedFor.current = null;
      setOpenedEvent(null);
      return;
    }
    const key = populateKey;
    if (populatedFor.current === key) return;
    if (editingEventId) {
      if (!editingEvent) return;
      populatedFor.current = key;
      setOpenedEvent(editingEvent);
      openedEtags.current = { etag: editingEvent.etag, seriesEtag: editingEvent.seriesEtag };
      setGrants({});
      const opened = eventToForm(editingEvent, timezone);
      setForm(opened);
      initialForm.current = opened;
      setRemoteChanged(false);
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
      return;
    }
    // Calendars are known and none can take a new event: there is no form to offer.
    if (calendars.length > 0 && !defaultCalendarId) {
      toast.error(t('event.noWritableCalendar'));
      closeEventModal();
      return;
    }
    populatedFor.current = key;
    // A draft start (e.g. a clicked weekly slot) creates a timed event at that
    // hour; otherwise default to an all-day event on the selected day.
    const timed = eventDraftStart != null;
    const base = (eventDraftStart ?? eventDraftDay ?? selectedDate).setZone(timezone, {
      keepLocalTime: true,
    });
    const start = timed
      ? base.set({ second: 0, millisecond: 0 })
      : base.set({ hour: 9, minute: 0, second: 0, millisecond: 0 });
    const draftEnd = eventDraftEnd?.setZone(timezone, { keepLocalTime: true });
    const end = timed && draftEnd && draftEnd > start ? draftEnd : start.plus({ hours: 1 });
    const opened: FormState = {
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
      participantIds: [],
      ownerId: null,
      labelId: null,
      visibility: 'default',
      showAs: 'busy',
      flexibility: 'fixed',
      recurrencePreset: 'none',
      recurrenceRule: null,
    };
    setForm(opened);
    initialForm.current = opened;
    setDraftChecklist(EMPTY_DRAFT_CHECKLIST);
    setShowMore(false);
  }, [
    editingEvent,
    editingEventId,
    populateKey,
    showEventModal,
    selectedDate,
    eventDraftStart,
    eventDraftEnd,
    eventDraftDay,
    defaultCalendarId,
    calendars.length,
    closeEventModal,
    timezone,
    t,
  ]);

  // Calendars that arrive after a new event's form opened still fill in its calendar.
  useEffect(() => {
    if (!showEventModal || editingEventId || !defaultCalendarId) return;
    setForm((f) => (f.calendarId ? f : { ...f, calendarId: defaultCalendarId }));
    if (initialForm.current && !initialForm.current.calendarId) {
      initialForm.current = { ...initialForm.current, calendarId: defaultCalendarId };
    }
  }, [showEventModal, editingEventId, defaultCalendarId]);

  // The stored copy of the event being edited changed under the editor. Untouched
  // forms follow it; one with unsaved edits is left alone and offers a reload.
  const dirtyRef = useRef(false);
  dirtyRef.current = isDirty;
  useEffect(() => {
    if (!showEventModal || !editingEvent || populatedFor.current !== populateKey) return;
    const base = initialForm.current;
    if (!base) return;
    const fresh = eventToForm(editingEvent, timezone);
    if (formEqual(fresh, base)) return;
    if (dirtyRef.current) {
      setRemoteChanged(true);
      return;
    }
    setForm(fresh);
    initialForm.current = fresh;
    openedEtags.current = { etag: editingEvent.etag, seriesEtag: editingEvent.seriesEtag };
    setOpenedEvent(editingEvent);
  }, [showEventModal, editingEvent, populateKey, timezone]);

  const reloadRemote = () => {
    if (!editingEvent) return;
    const fresh = eventToForm(editingEvent, timezone);
    setForm(fresh);
    initialForm.current = fresh;
    openedEtags.current = { etag: editingEvent.etag, seriesEtag: editingEvent.seriesEtag };
    setOpenedEvent(editingEvent);
    setRemoteChanged(false);
  };

  // An open asked for while the editor is up: a closing editor takes it at once,
  // unsaved edits are asked about first, and the event already open is a no-op.
  useEffect(() => {
    if (!pendingEventOpen) return;
    if (closing) {
      if (closeTimer.current) {
        window.clearTimeout(closeTimer.current);
        closeTimer.current = null;
      }
      setClosing(false);
      applyPendingEventOpen();
      return;
    }
    if (pendingEventOpen.eventId !== null && pendingEventOpen.eventId === editingEventId) {
      clearPendingEventOpen();
      return;
    }
    if (dirtyRef.current) {
      setConfirming('discard');
      return;
    }
    applyPendingEventOpen();
  }, [pendingEventOpen, closing, editingEventId, applyPendingEventOpen, clearPendingEventOpen]);

  // Recurring events are always opened as an expanded instance (composite id),
  // so editing or deleting one must ask whether it applies to just this
  // occurrence or the whole series.
  const isRecurringInstance =
    !!editingEvent && (editingEvent.isRecurrence || editingEvent.id.includes('_'));
  const [scopePrompt, setScopePrompt] = useState<null | 'save' | 'delete'>(null);

  const handleSave = useCallback(
    async (scope?: 'this' | 'all') => {
      if (!form.title.trim() || saving || !editable || endError) return;
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
        ...(form.allDay
          ? {
              startDate: DateTime.fromISO(startIso, { zone: anchorZone }).toISODate() ?? startIso,
              endDate: DateTime.fromISO(endIso, { zone: anchorZone }).toISODate() ?? endIso,
            }
          : { startAt: startIso, endAt: endIso }),
        timezone: anchorZone,
        // What the server withheld from this reader is not theirs to write back.
        ...(detailsHidden
          ? {}
          : {
              location: form.location,
              memo: form.memo,
              url: form.url,
              participantIds: form.participantIds,
              ownerId: form.ownerId,
              visibility: form.visibility,
              // An edit names the label only when it changed, so a label
              // deleted meanwhile cannot fail an unrelated save.
              ...((editingEvent ? form.labelId !== initialForm.current?.labelId : form.labelId)
                ? { labelId: form.labelId }
                : {}),
            }),
        notificationOffset: form.notificationOffset,
        showAs: form.showAs,
        flexibility: form.flexibility,
        recurrenceRule: normalizeRuleForApi(form.recurrenceRule, anchorZone),
      };
      setSaving(true);
      try {
        if (editingEvent) {
          const opened = openedEtags.current;
          // A scope over the whole series is checked against the master's version.
          const etag = scope === 'all' ? (opened?.seriesEtag ?? opened?.etag) : opened?.etag;
          await updateEvent(editingEvent.calendarId, editingEvent.id, data, scope, etag);
        } else {
          const createdId = await addEvent(form.calendarId, data);
          if (!(await saveDraftChecklist(form.calendarId, createdId, draftChecklist))) {
            toast.error(t('checklist.saveFailed'));
          }
          setDraftChecklist(EMPTY_DRAFT_CHECKLIST);
        }
        initialForm.current = form;
        setScopePrompt(null);
        requestClose();
      } catch (e) {
        // A refused save means someone else's version is the current one.
        // Leaving the editor open with the rejected text in it would invite
        // the same save again, so the event is reloaded and the form is
        // rebuilt from what is actually stored.
        if (isStaleError(e)) {
          toast.error(t('conflict.stale'));
          setScopePrompt(null);
          const { start, end } = visibleRange();
          // The refusal has already been reported; a reload that also fails
          // leaves the grid as it was rather than raising a second complaint.
          await fetchEvents(start, end).catch(() => {});
          return;
        }
        if (e instanceof ApiError && e.code === 'EVENT.LABEL_UNKNOWN') {
          // The label was deleted meanwhile: read the list again and drop the choice.
          void fetchLabels(form.calendarId || editingEvent?.calendarId || '');
          setForm((f) => ({ ...f, labelId: null }));
          setScopePrompt(null);
        }
        toast.error(errorMessage(e, t('error.saveFailed')));
      } finally {
        setSaving(false);
      }
    },
    [
      form,
      draftChecklist,
      fetchLabels,
      editingEvent,
      isRecurringInstance,
      addEvent,
      updateEvent,
      requestClose,
      saving,
      editable,
      endError,
      timezone,
      anchorZone,
      detailsHidden,
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
        await deleteEvent(editingEvent, scope);
        setScopePrompt(null);
        requestClose();
      } catch (e) {
        toast.error(errorMessage(e, t('error.deleteFailed')));
      } finally {
        setSaving(false);
        setConfirming(null);
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
    guardedClose();
  };
  // A desktop panel without its slot is not on screen, so it does not join the stack.
  const panelShown = showEventModal && (!isDesktop || desktopSlot !== null);
  const panelRef = useModalA11y<HTMLDivElement>(panelShown, dismiss, titleRef, {
    modal: !isDesktop,
  });
  const scopeRef = useModalA11y<HTMLDivElement>(scopePrompt !== null, () => setScopePrompt(null));
  // The chooser is a dialog over a dialog, so it carries its own trap: without
  // one its buttons sit outside the modal's container and Tab walks straight
  // out of the page rather than around the question being asked. The two traps
  // do not fight — each only intercepts Tab at the edge of its own container,
  // and the containers are disjoint — and on a phone the outer one keeps the
  // scroll lock and the return-focus target for the whole time the chooser is up.

  // The memo grows with its text; the CSS cap (12 lines) turns the rest into a scroll.
  const memoRef = useRef<HTMLTextAreaElement>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: the text and the field appearing are the triggers
  useLayoutEffect(() => {
    const el = memoRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  }, [form.memo, showEventModal, showMore, detailsHidden, isDesktop, desktopSlot]);

  if (!showEventModal && !closing) return null;
  if (isDesktop && !desktopSlot) return null;

  // The body of the modal. It is mounted once, in whichever layout is shown, so every
  // section below asks the server for its contents once rather than per layout.
  const formContent = (
    <>
      {/* Header */}
      <div className="grid grid-cols-[2.5rem_1fr_2.5rem] items-center gap-3 px-6 py-3">
        <LabelledButton
          label={t('common.close')}
          onClick={guardedClose}
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
          onKeyDown={(e) => {
            // Enter confirms the title instead of breaking the line.
            if (!isSubmitEnter(e) || e.metaKey || e.ctrlKey) return;
            e.preventDefault();
            void handleSave();
          }}
          readOnly={!editable || taskOwned}
          placeholder={t('event.titlePlaceholder')}
          rows={1}
          className="w-full resize-none border-b-2 border-transparent bg-transparent text-display font-light text-[var(--color-text-primary)] outline-none transition-colors placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--color-accent)]"
        />
      </div>

      {remoteChanged && editingEvent && (
        <div
          role="status"
          className="mx-6 mb-3 flex items-center justify-between gap-3 bg-[var(--color-surface-secondary)] px-4 py-2.5 text-footnote text-[var(--color-text-secondary)]"
          style={{ borderRadius: 'var(--radius-md)' }}
        >
          <span>{t('event.remoteChanged')}</span>
          <button
            type="button"
            onClick={reloadRemote}
            className="shrink-0 font-medium text-[var(--color-accent)]"
          >
            {t('event.reloadRemote')}
          </button>
        </div>
      )}

      {/* Identity: which calendar + which color (event's visual identity) */}
      {/* An event is drawn in its label's colour, else its owner's layer colour,
          which the whole calendar has agreed on and the members list changes.
          With one calendar there is nothing to choose or name, so the card is
          left out. */}
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
                onChange={(v) =>
                  setForm((f) => ({ ...f, calendarId: v, ownerId: null, labelId: null }))
                }
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

      {/* Label: the colour the event is drawn in instead of its owner's. Left
          out where the calendar has none to offer. */}
      {!detailsHidden && calendarLabels.length > 0 && (
        <div className="px-6 pb-3">
          <div className="card-section flex items-center gap-3 bg-[var(--color-surface-secondary)] px-4 py-2.5">
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-text-tertiary)"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="shrink-0"
              aria-hidden="true"
            >
              <path d="M20.6 13.4l-7.2 7.2a2 2 0 0 1-2.8 0L3 13V3h10l7.6 7.6a2 2 0 0 1 0 2.8z" />
              <circle cx="7.5" cy="7.5" r="1.2" />
            </svg>
            <span className="flex-1 text-default text-[var(--color-text-primary)]">
              {t('event.label')}
            </span>
            <CustomSelect
              value={form.labelId ?? ''}
              onChange={(v) => setForm((f) => ({ ...f, labelId: v || null }))}
              ariaLabel={t('event.label')}
              disabled={!editable}
              options={[
                { value: '', label: t('event.labelNone') },
                ...calendarLabels.map((l) => ({ value: l.id, label: l.name, color: l.color })),
              ]}
            />
          </div>
        </div>
      )}

      {/* Creator (read-only metadata, shown only for existing events) */}
      {editingEvent &&
        (() => {
          const member = (membersMap[editingEvent.calendarId] ?? []).find(
            (m) => m.id === editingEvent.creatorId,
          );
          const name = editingEvent.creatorName || member?.name;
          if (!name) return null;
          return (
            <div className="flex items-center gap-2 px-6 pb-3 text-caption text-[var(--color-text-secondary)]">
              <span>{t('event.createdBy')}</span>
              <Avatar
                src={editingEvent.creatorAvatarUrl}
                name={name}
                color={member?.color}
                round
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
      {!detailsHidden && (membersMap[formCalendarId]?.length ?? 0) > 1 && (
        <div className="px-6 pb-3">
          <EventPeople
            members={membersMap[formCalendarId] ?? []}
            myId={me?.id}
            ownerId={form.ownerId}
            participants={form.participantIds}
            readOnly={!editable}
            canChangeOwner={canChangeOwner}
            canRemoveEditors={canChangeOwner}
            ownerHint={ownerHint}
            attendees={editingEvent?.attendees?.map((a) =>
              a.userId in grants ? { ...a, canEdit: grants[a.userId] ?? a.canEdit } : a,
            )}
            onCanEditChange={editingEvent && canChangeOwner ? setCanEdit : undefined}
            onOwnerChange={(id) => setForm((f) => ({ ...f, ownerId: id }))}
            onParticipantsChange={(ids) => setForm((f) => ({ ...f, participantIds: ids }))}
          />
        </div>
      )}

      {detailsHidden && (
        <p className="px-6 pb-3 text-footnote text-[var(--color-text-secondary)]">
          {t('event.detailsHidden')}
        </p>
      )}

      {/* Date & Time card */}
      <div className="px-6">
        {taskOwned && <Callout className="mb-2">{t('event.taskOwnedNote')}</Callout>}
        <div className="card-section bg-[var(--color-surface-secondary)] p-4">
          <fieldset disabled={!editable || taskOwned} className="contents">
            <DateTimeRangeField
              showTime={!form.allDay}
              endInvalid={endError}
              start={{
                label: t('event.start'),
                date: DateTime.fromISO(form.startAt, { zone: timezone }),
                time: form.startAt.split('T')[1]?.slice(0, 5) ?? '09:00',
                onDateChange: (date) => {
                  setForm((f) => {
                    const moved = shiftStartKeepingDuration(f, date, timezone);
                    return withPresetRule({ ...f, startAt: moved.startAt, endAt: moved.endAt });
                  });
                },
                onTimeChange: (time) => {
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
                },
              }}
              end={{
                label: t('event.end'),
                date: DateTime.fromISO(form.endAt, { zone: timezone }),
                time: form.endAt.split('T')[1]?.slice(0, 5) ?? '10:00',
                onDateChange: (date) => {
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
                },
                onTimeChange: (time) => {
                  const datePart = form.endAt.split('T')[0] ?? '';
                  // Applied as chosen: an end before the start is reported below
                  // and blocks the save rather than being silently ignored.
                  setForm((f) => ({ ...f, endAt: `${datePart}T${time}` }));
                },
              }}
            />
            {endError && (
              <p role="alert" className="pb-1 text-footnote text-[var(--color-danger)]">
                {t('event.endBeforeStart')}
              </p>
            )}

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
        {/* No "learn more" link: the help page is another route, and
            leaving for it would throw away the plan being written. */}
        {!editingEvent && (membersMap[formCalendarId]?.length ?? 0) > 1 && (
          <TipCard id="event" className="mt-3">
            {t('tip.event')}
          </TipCard>
        )}
      </div>

      {/* Optional fields: collapsed by default on create, expanded when used */}
      <div className="mt-3 px-6">
        <button
          type="button"
          onClick={() => setShowMore((v) => !v)}
          aria-expanded={showMore}
          className="flex w-full items-center justify-between rounded-[var(--radius-md)] px-4 py-2 text-default font-medium text-[var(--color-text-secondary)] transition-colors hover:text-[var(--color-text-primary)]"
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
          {!detailsHidden && (
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
                    readOnly={!editable}
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
                    readOnly={!editable}
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
                    ref={memoRef}
                    value={form.memo}
                    onChange={(e) => setForm((f) => ({ ...f, memo: e.target.value }))}
                    readOnly={!editable}
                    placeholder={t('event.memo')}
                    maxLength={16000}
                    rows={2}
                    className="max-h-[12lh] flex-1 resize-none overflow-y-auto bg-transparent text-callout text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
                  />
                </div>
              </div>
            </div>
          )}

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
                    ariaLabel={t('event.notification')}
                    disabled={!editable}
                    options={notificationOptions}
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
                    {form.allDay ? t('notification.deliveryNote') : t('event.notification_hint')}
                  </p>
                )}
              </div>

              {!detailsHidden && (
                <>
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
                      ariaLabel={t('event.visibility')}
                      disabled={!editable}
                      options={VISIBILITY_OPTIONS.map((v) => ({
                        value: v,
                        label: t(visibilityLabelKey(v)),
                      }))}
                      onChange={(v) => setForm((f) => ({ ...f, visibility: v as Visibility }))}
                      className="flex-1"
                    />
                  </div>
                  <p className="pb-1.5 pl-[30px] text-footnote text-[var(--color-text-secondary)]">
                    {t(VISIBILITY_HINT_KEY[form.visibility])}
                  </p>
                </>
              )}
              <div className="flex flex-wrap items-center justify-between gap-2 py-1.5">
                <span className="text-body text-[var(--color-text-secondary)]">
                  {t('event.flexibility')}
                </span>
                <CustomSelect
                  ariaLabel={t('event.flexibility')}
                  disabled={!editable}
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

      {/* A new event's checklist is part of the form, saved along with it. */}
      {!editingEvent && editable && (
        <ChecklistSection draft={draftChecklist} onDraftChange={setDraftChecklist} />
      )}

      {/* The sections below write as they change, unlike the form above. */}
      {editingEvent && (
        <div className="mx-6 mt-5 flex items-center gap-3">
          <span className="h-px flex-1 bg-[var(--color-border)]" aria-hidden="true" />
          <h3 className="shrink-0 text-footnote font-medium text-[var(--color-text-secondary)]">
            {t('event.savedImmediately')}
          </h3>
          <span className="h-px flex-1 bg-[var(--color-border)]" aria-hidden="true" />
        </div>
      )}

      {/* Keyed by event, so switching events in the open panel starts each section afresh. */}
      <div data-saves-immediately key={editingEvent?.id}>
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
      </div>

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
            key={editingEvent.id}
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
            onClick={() => (isRecurringInstance ? handleDelete() : setConfirming('delete'))}
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

  const panelLabel = editingEvent ? t('event.editEvent') : t('event.createEvent');
  const onPanelKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!isSubmitEnter(e) || !(e.metaKey || e.ctrlKey)) return;
    // The sections that save as they change keep Enter for themselves.
    if ((e.target as HTMLElement).closest('[data-saves-immediately]')) return;
    e.preventDefault();
    void handleSave();
  };
  const panelBody = (
    <>
      {/* Grab zone: drag down to hold/dismiss. Generous height for easy grab. */}
      <div
        className="shrink-0 cursor-grab touch-none pt-3 pb-2.5 active:cursor-grabbing sm:hidden"
        onPointerDown={sheetDrag.onPointerDown}
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
          onClick={guardedClose}
          className="btn-secondary flex-1 py-3 text-default font-medium"
        >
          {editable ? t('common.cancel') : t('common.close')}
        </button>
        {editable && (
          <button
            type="button"
            onClick={() => handleSave()}
            disabled={saving || !form.title.trim() || endError}
            className="btn-primary flex-1 py-3 text-default font-medium disabled:opacity-50"
          >
            {saving ? t('common.saving') : t('common.save')}
          </button>
        )}
      </div>
    </>
  );

  return (
    <>
      {isDesktop && desktopSlot ? (
        // Only the panel moves into the slot: the scope chooser and the
        // confirmations stay here, since the panel's transform would capture
        // their fixed positioning. Overlaid on the calendar below 1284px of row,
        // beside it (pushing it narrower) from there up.
        createPortal(
          <div
            ref={panelRef}
            role="dialog"
            aria-label={panelLabel}
            onKeyDown={onPanelKeyDown}
            className={`glass-surface-heavy side-panel flex h-full w-[400px] shrink-0 flex-col border-l border-[var(--color-border)] absolute inset-y-0 right-16 z-30 shadow-xl @min-[1284px]:relative @min-[1284px]:inset-auto @min-[1284px]:right-auto @min-[1284px]:z-auto @min-[1284px]:shadow-none ${
              closing ? 'side-panel-exit' : ''
            }`}
          >
            {panelBody}
          </div>,
          desktopSlot,
        )
      ) : (
        <>
          <button
            type="button"
            aria-label={t('common.close')}
            className={`modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)] ${
              closing ? 'backdrop-out' : ''
            }`}
            onClick={guardedClose}
          />
          {/* The phone sheet rests on the bottom edge. */}
          <div className="pointer-events-none fixed inset-0 z-50 flex flex-col justify-end">
            <div
              ref={panelRef}
              role="dialog"
              aria-modal="true"
              aria-label={panelLabel}
              onKeyDown={onPanelKeyDown}
              className="glass-surface-heavy pointer-events-auto flex w-full flex-col overflow-hidden bottom-sheet event-sheet max-h-[92dvh]"
              style={{
                // At rest, drop the transform entirely: a lingering one makes
                // this a containing block, and anything fixed inside the sheet
                // would then be positioned against the sheet, not the viewport.
                transform: closing
                  ? 'translateY(100%)'
                  : sheetDragY
                    ? sheetDragY
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
              }}
            >
              {panelBody}
            </div>
          </div>
        </>
      )}

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
                style={scopePrompt === 'delete' ? { color: 'var(--color-danger)' } : undefined}
              >
                {scopePrompt === 'delete' ? t('event.scopeAllDelete') : t('event.scopeAll')}
              </button>
              <button
                type="button"
                onClick={() => setScopePrompt(null)}
                className="py-2 text-default text-[var(--color-text-secondary)]"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </>
      )}

      <ConfirmSheet
        open={confirming === 'discard'}
        title={t('common.discardTitle')}
        confirmLabel={t('common.discard')}
        cancelLabel={t('common.keepEditing')}
        onConfirm={() => {
          setConfirming(null);
          // Asked over a queued open: discarding switches rather than closes.
          if (pendingEventOpen) applyPendingEventOpen();
          else requestClose();
        }}
        onCancel={() => {
          setConfirming(null);
          if (pendingEventOpen) clearPendingEventOpen();
        }}
      />
      <ConfirmSheet
        open={confirming === 'delete'}
        title={t('event.deleteConfirmTitle')}
        confirmLabel={t('common.delete')}
        busy={saving}
        onConfirm={() => handleDelete()}
        onCancel={() => setConfirming(null)}
      />
    </>
  );
}
