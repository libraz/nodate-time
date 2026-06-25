import { DateTime } from 'luxon';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CustomSelect, DateTimeField } from '@/components/pickers';
import { type TranslationKey, useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import { canEdit, roleForCalendar } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { uploadViaPresign } from '@/lib/upload';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useUiStore } from '@/stores/ui-store';
import type {
  ChecklistItem,
  EventAttachment,
  RecurrencePreset,
  RecurrenceRule,
} from '@/types/calendar';
import {
  FALLBACK_LABEL_COLOR,
  NOTIFICATION_OFFSETS,
  normalizeNotificationOffset,
} from '@/types/calendar';

/** Renders a stored UTC instant as a `yyyy-MM-ddTHH:mm` string in the given zone. */
function toLocalDatetime(iso: string, zone: string): string {
  return DateTime.fromISO(iso, { zone }).toFormat("yyyy-MM-dd'T'HH:mm");
}

/** Interprets a wall-clock `yyyy-MM-ddTHH:mm` string as an instant in the given zone. */
function fromLocalDatetimeToISO(s: string, zone: string): string {
  return DateTime.fromISO(s, { zone }).toISO() ?? s;
}

interface Activity {
  id: string;
  content: string;
  userName: string;
  userIcon: string;
  userPublicId: string;
  createdAt: string;
}

// Format a relative time string according to locale
function formatRelativeTime(iso: string, locale: 'ja' | 'en'): string {
  const dt = DateTime.fromISO(iso);
  const diff = DateTime.now().diff(dt, ['days', 'hours', 'minutes']);
  if (locale === 'en') {
    if (diff.days >= 1) return `${Math.floor(diff.days)}d ago`;
    if (diff.hours >= 1) return `${Math.floor(diff.hours)}h ago`;
    if (diff.minutes >= 1) return `${Math.floor(diff.minutes)}m ago`;
    return 'just now';
  }
  if (diff.days >= 1) return `${Math.floor(diff.days)}\u65E5\u524D`;
  if (diff.hours >= 1) return `${Math.floor(diff.hours)}\u6642\u9593\u524D`;
  if (diff.minutes >= 1) return `${Math.floor(diff.minutes)}\u5206\u524D`;
  return '\u305F\u3063\u305F\u4ECA';
}

const WEEKDAY_CODES = ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'] as const;
const WEEKDAY_LABELS_JA = ['\u65E5', '\u6708', '\u706B', '\u6C34', '\u6728', '\u91D1', '\u571F'];
const WEEKDAY_LABELS_EN = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
];

function getWeekdayLabel(dt: DateTime, locale: 'ja' | 'en'): string {
  const idx = dt.weekday % 7; // Luxon: 1=Mon..7=Sun, %7 gives 0=Sun
  return locale === 'ja' ? (WEEKDAY_LABELS_JA[idx] ?? '') : (WEEKDAY_LABELS_EN[idx] ?? '');
}

function getNthWeekday(dt: DateTime): number {
  return Math.ceil(dt.day / 7);
}

function presetToRule(preset: RecurrencePreset, dt: DateTime): RecurrenceRule | null {
  const dayIdx = dt.weekday % 7;
  const dayCode = WEEKDAY_CODES[dayIdx] ?? 'SU';
  switch (preset) {
    case 'none':
      return null;
    case 'daily':
      return { freq: 'daily', interval: 1 };
    case 'weekly':
      return { freq: 'weekly', interval: 1, byDay: [dayCode] };
    case 'weekdays':
      return { freq: 'weekly', interval: 1, byDay: ['MO', 'TU', 'WE', 'TH', 'FR'] };
    case 'monthly_nth':
      return { freq: 'monthly', interval: 1, bySetPos: getNthWeekday(dt), byDay: [dayCode] };
    case 'monthly_date':
      return { freq: 'monthly', interval: 1, byMonthDay: dt.day };
    case 'yearly':
      return { freq: 'yearly', interval: 1 };
    case 'custom':
      return null;
  }
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
  if (next.freq === 'monthly' && next.byDay && !next.bySetPos) {
    next.byDay = undefined;
  }
  if (next.until && next.until.length === 10) {
    const end = DateTime.fromISO(next.until, { zone }).endOf('day');
    next.until = end.toISO() ?? next.until;
  }
  return next;
}

function ruleToPreset(rule: RecurrenceRule | null, dt: DateTime): RecurrencePreset {
  if (!rule) return 'none';
  const dayIdx = dt.weekday % 7;
  const dayCode = WEEKDAY_CODES[dayIdx] ?? 'SU';

  if (rule.freq === 'daily' && rule.interval === 1) return 'daily';
  if (rule.freq === 'yearly' && rule.interval === 1) return 'yearly';
  if (rule.freq === 'weekly' && rule.interval === 1) {
    if (
      rule.byDay?.length === 5 &&
      ['MO', 'TU', 'WE', 'TH', 'FR'].every((d) => rule.byDay?.includes(d))
    )
      return 'weekdays';
    if (rule.byDay?.length === 1 && rule.byDay[0] === dayCode) return 'weekly';
  }
  if (rule.freq === 'monthly' && rule.interval === 1) {
    if (rule.bySetPos && rule.byDay?.length === 1) return 'monthly_nth';
    if (rule.byMonthDay) return 'monthly_date';
  }
  return 'custom';
}

function recurrenceLabel(
  rule: RecurrenceRule | null,
  dt: DateTime,
  t: (key: TranslationKey, params?: Record<string, string | number>) => string,
  locale: 'ja' | 'en',
): string {
  if (!rule) return t('event.recurrenceNone');
  const preset = ruleToPreset(rule, dt);
  const dayLabel = getWeekdayLabel(dt, locale);
  switch (preset) {
    case 'daily':
      return t('event.recurrenceDaily');
    case 'weekly':
      return t('event.recurrenceWeekly', { day: dayLabel });
    case 'weekdays':
      return t('event.recurrenceWeekdays');
    case 'monthly_nth':
      return t('event.recurrenceMonthlyNth', { n: getNthWeekday(dt), day: dayLabel });
    case 'monthly_date':
      return t('event.recurrenceMonthlyDate', { date: dt.day });
    case 'yearly':
      return t('event.recurrenceYearly');
    default:
      // Custom: build a descriptive label
      if (rule.interval > 1) {
        const freqMap: Record<string, TranslationKey> = {
          daily: 'event.unitDay',
          weekly: 'event.unitWeek',
          monthly: 'event.unitMonth',
          yearly: 'event.unitYear',
        };
        const unitKey = freqMap[rule.freq] ?? 'event.unitDay';
        return `${rule.interval} ${t(unitKey)}`;
      }
      return t('event.recurrenceCustom');
  }
}

function CommentsSection({ calendarId, eventId }: { calendarId: string; eventId: string }) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const user = useAuthStore((s) => s.user);
  const [comments, setComments] = useState<Activity[]>([]);
  const [newComment, setNewComment] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [isSending, setIsSending] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editContent, setEditContent] = useState('');
  const listEndRef = useRef<HTMLDivElement>(null);

  const fetchComments = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await api.get<Activity[]>(
        `/calendars/${calendarId}/events/${eventId}/activities`,
      );
      setComments(data);
    } catch {
      // silently ignore fetch errors
    } finally {
      setIsLoading(false);
    }
  }, [calendarId, eventId]);

  useEffect(() => {
    fetchComments();
  }, [fetchComments]);

  const prevCountRef = useRef(0);
  useEffect(() => {
    if (comments.length > prevCountRef.current) {
      listEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
    prevCountRef.current = comments.length;
  });

  const handleSend = async () => {
    const content = newComment.trim();
    if (!content || isSending) return;
    setIsSending(true);
    try {
      await api.post(`/calendars/${calendarId}/events/${eventId}/activities`, { content });
      setNewComment('');
      await fetchComments();
    } catch {
      // silently ignore send errors
    } finally {
      setIsSending(false);
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
          <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
        </svg>
        <span className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('event.comments')}
        </span>
      </div>

      {isLoading && comments.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
          {t('common.loading')}
        </p>
      )}

      {!isLoading && comments.length === 0 && (
        <p className="py-2 text-center text-body text-[var(--color-text-tertiary)]">
          {t('event.noComments')}
        </p>
      )}

      {comments.length > 0 && (
        <div className="mb-3 max-h-[240px] space-y-3 overflow-y-auto">
          {comments.map((c) => (
            <div key={c.id} className="group flex gap-2">
              <span
                className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center bg-[var(--color-surface-inset)] text-default"
                style={{ borderRadius: 'var(--radius-sm)' }}
              >
                {c.userIcon || '\uD83D\uDC64'}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline gap-2">
                  <span className="text-body font-medium text-[var(--color-text-primary)]">
                    {c.userName}
                  </span>
                  <span className="text-caption text-[var(--color-text-tertiary)]">
                    {formatRelativeTime(c.createdAt, locale)}
                  </span>
                  {c.userPublicId === user?.id && editingId !== c.id && (
                    <span className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                      <button
                        type="button"
                        onClick={() => {
                          setEditingId(c.id);
                          setEditContent(c.content);
                        }}
                        className="text-caption text-[var(--color-text-tertiary)] hover:text-[var(--color-accent)]"
                      >
                        {t('event.editComment')}
                      </button>
                      <button
                        type="button"
                        onClick={async () => {
                          await api.delete(
                            `/calendars/${calendarId}/events/${eventId}/activities/${c.id}`,
                          );
                          fetchComments();
                        }}
                        className="text-caption text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
                      >
                        {t('event.deleteComment')}
                      </button>
                    </span>
                  )}
                </div>
                {editingId === c.id ? (
                  <div className="mt-1 flex gap-1">
                    <input
                      type="text"
                      value={editContent}
                      onChange={(e) => setEditContent(e.target.value)}
                      className="flex-1 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-body text-[var(--color-text-primary)] outline-none"
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          api
                            .put(`/calendars/${calendarId}/events/${eventId}/activities/${c.id}`, {
                              content: editContent,
                            })
                            .then(() => {
                              setEditingId(null);
                              fetchComments();
                            });
                        }
                        if (e.key === 'Escape') setEditingId(null);
                      }}
                    />
                    <button
                      type="button"
                      onClick={() => {
                        api
                          .put(`/calendars/${calendarId}/events/${eventId}/activities/${c.id}`, {
                            content: editContent,
                          })
                          .then(() => {
                            setEditingId(null);
                            fetchComments();
                          });
                      }}
                      className="bg-[var(--color-accent)] px-2 py-1 text-caption text-white"
                      style={{ borderRadius: 'var(--radius-sm)' }}
                    >
                      {t('event.saveComment')}
                    </button>
                  </div>
                ) : (
                  <p className="mt-0.5 whitespace-pre-wrap break-words text-default text-[var(--color-text-primary)]">
                    {c.content}
                  </p>
                )}
              </div>
            </div>
          ))}
          <div ref={listEndRef} />
        </div>
      )}

      <div className="flex items-center gap-2">
        <input
          type="text"
          value={newComment}
          onChange={(e) => setNewComment(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              handleSend();
            }
          }}
          placeholder={t('event.commentPlaceholder')}
          className="flex-1 border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-2 text-default text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--color-accent)]"
          style={{ borderRadius: 'var(--radius-sm)' }}
        />
        <button
          type="button"
          onClick={handleSend}
          disabled={!newComment.trim() || isSending}
          className="flex h-8 w-8 shrink-0 items-center justify-center bg-[var(--color-accent)] disabled:opacity-40"
          style={{ borderRadius: 'var(--radius-sm)' }}
          aria-label={t('event.send')}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="white">
            <path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
          </svg>
        </button>
      </div>
    </div>
  );
}

function ChecklistSection({ calendarId, eventId }: { calendarId: string; eventId: string }) {
  const t = useT();
  const [items, setItems] = useState<ChecklistItem[]>([]);
  const [newTitle, setNewTitle] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  const fetchItems = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await api.get<ChecklistItem[]>(
        `/calendars/${calendarId}/events/${eventId}/checklist`,
      );
      setItems(data);
    } catch {
      // ignore
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
      await api.post(`/calendars/${calendarId}/events/${eventId}/checklist`, {
        title,
        sortOrder: items.length,
      });
      setNewTitle('');
      await fetchItems();
    } catch {
      // ignore
    }
  };

  const handleToggle = async (item: ChecklistItem) => {
    try {
      await api.put(`/calendars/${calendarId}/events/${eventId}/checklist/${item.id}`, {
        title: item.title,
        done: !item.done,
        sortOrder: item.sortOrder,
      });
      await fetchItems();
    } catch {
      // ignore
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.delete(`/calendars/${calendarId}/events/${eventId}/checklist/${id}`);
      await fetchItems();
    } catch {
      // ignore
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

      {items.length > 0 && (
        <div className="mb-3 space-y-1">
          {items.map((item) => (
            <div key={item.id} className="group flex items-center gap-2">
              <button
                type="button"
                onClick={() => handleToggle(item)}
                className="flex h-5 w-5 shrink-0 items-center justify-center rounded border border-[var(--color-border)]"
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
                    stroke="white"
                    strokeWidth="3"
                  >
                    <path d="M20 6L9 17l-5-5" />
                  </svg>
                )}
              </button>
              <span
                className="flex-1 text-default"
                style={{
                  color: item.done ? 'var(--color-text-tertiary)' : 'var(--color-text-primary)',
                  textDecoration: item.done ? 'line-through' : 'none',
                }}
              >
                {item.title}
              </span>
              <button
                type="button"
                onClick={() => handleDelete(item.id)}
                className="opacity-0 group-hover:opacity-100 transition-opacity text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
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
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center gap-2">
        <input
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
          className="flex-1 border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-2 text-default text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--color-accent)]"
          style={{ borderRadius: 'var(--radius-sm)' }}
        />
      </div>
    </div>
  );
}

function AttachmentsSection({ calendarId, eventId }: { calendarId: string; eventId: string }) {
  const t = useT();
  const [attachments, setAttachments] = useState<EventAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const fetchAttachments = useCallback(async () => {
    try {
      const data = await api.get<EventAttachment[]>(
        `/calendars/${calendarId}/events/${eventId}/attachments`,
      );
      setAttachments(data);
    } catch {
      // ignore
    }
  }, [calendarId, eventId]);

  useEffect(() => {
    fetchAttachments();
  }, [fetchAttachments]);

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    const contentType = file.type || 'application/octet-stream';
    try {
      const presign = await uploadViaPresign<{ attachmentId: string; uploadUrl: string }>({
        kind: 'attachment',
        presignPath: `/calendars/${calendarId}/events/${eventId}/attachments/presign`,
        presignBody: { filename: file.name, contentType, byteSize: file.size },
        contentType,
        body: file,
        byteSize: file.size,
      });
      // The row is created disabled; confirm enables it once the object is stored.
      await api.post(
        `/calendars/${calendarId}/events/${eventId}/attachments/${presign.attachmentId}/confirm`,
      );
      await fetchAttachments();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setUploading(false);
      e.target.value = '';
    }
  };

  const handleDownload = async (att: EventAttachment) => {
    try {
      const { downloadUrl } = await api.get<{ downloadUrl: string }>(
        `/calendars/${calendarId}/events/${eventId}/attachments/${att.id}/download`,
      );
      window.open(downloadUrl, '_blank');
    } catch {
      // ignore
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.delete(`/calendars/${calendarId}/events/${eventId}/attachments/${id}`);
      await fetchAttachments();
    } catch {
      // ignore
    }
  };

  const formatSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
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
          <path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" />
        </svg>
        <span className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('event.attachments')}
        </span>
      </div>

      {attachments.length > 0 && (
        <div className="mb-3 space-y-2">
          {attachments.map((att) => (
            <div
              key={att.id}
              className="group flex items-center gap-2 bg-[var(--color-surface-inset)] px-3 py-2"
              style={{ borderRadius: 'var(--radius-sm)' }}
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="var(--color-text-tertiary)"
                strokeWidth="2"
                className="shrink-0"
              >
                <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
                <path d="M14 2v6h6" />
              </svg>
              <div className="min-w-0 flex-1">
                <p className="truncate text-body text-[var(--color-text-primary)]">
                  {att.filename}
                </p>
                <p className="text-caption text-[var(--color-text-tertiary)]">
                  {formatSize(att.byteSize)}
                </p>
              </div>
              <button
                type="button"
                onClick={() => handleDownload(att)}
                className="shrink-0 text-[var(--color-accent)] hover:underline text-footnote"
              >
                {t('event.download')}
              </button>
              <button
                type="button"
                onClick={() => handleDelete(att.id)}
                className="shrink-0 opacity-0 group-hover:opacity-100 transition-opacity text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
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
            </div>
          ))}
        </div>
      )}

      <input ref={fileInputRef} type="file" onChange={handleUpload} className="hidden" />
      <button
        type="button"
        onClick={() => fileInputRef.current?.click()}
        disabled={uploading}
        className="flex w-full items-center justify-center gap-2 border border-dashed border-[var(--color-border)] py-2.5 text-body text-[var(--color-text-tertiary)] hover:border-[var(--color-accent)] hover:text-[var(--color-accent)] transition-colors disabled:opacity-50"
        style={{ borderRadius: 'var(--radius-md)' }}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        >
          <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
          <polyline points="17 8 12 3 7 8" />
          <line x1="12" y1="3" x2="12" y2="15" />
        </svg>
        {uploading ? t('event.uploading') : t('event.uploadAttachment')}
      </button>
    </div>
  );
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
  assignedTo: string | null;
  recurrencePreset: RecurrencePreset;
  recurrenceRule: RecurrenceRule | null;
}

export function EventModal() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const showEventModal = useUiStore((s) => s.showEventModal);
  const editingEventId = useUiStore((s) => s.editingEventId);
  const closeEventModal = useUiStore((s) => s.closeEventModal);
  const selectedDate = useUiStore((s) => s.selectedDate);
  const eventDraftStart = useUiStore((s) => s.eventDraftStart);

  const calendars = useCalendarStore((s) => s.calendars);
  const activeCalendarIds = useCalendarStore((s) => s.activeCalendarIds);
  const events = useCalendarStore((s) => s.events);
  const addEvent = useCalendarStore((s) => s.addEvent);
  const updateEvent = useCalendarStore((s) => s.updateEvent);
  const deleteEvent = useCalendarStore((s) => s.deleteEvent);
  const membersMap = useCalendarStore((s) => s.membersMap);
  const labels = useCalendarStore((s) => s.labels);
  const me = useAuthStore((s) => s.user);

  const editingEvent = editingEventId ? events.find((e) => e.id === editingEventId) : null;
  const titleRef = useRef<HTMLTextAreaElement>(null);
  const [saving, setSaving] = useState(false);

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
    assignedTo: null,
    recurrencePreset: 'none',
    recurrenceRule: null,
  });
  const [showCustomRecurrence, setShowCustomRecurrence] = useState(false);
  // Optional fields (location/url/memo/people/reminder) collapse on create and
  // auto-expand when an existing event already uses any of them.
  const [showMore, setShowMore] = useState(false);

  // The current user's role for the event's calendar gates all mutating UI.
  const formCalendarId = form.calendarId || (editingEvent?.calendarId ?? '');
  const myRole = roleForCalendar(membersMap[formCalendarId], me?.email);
  const editable = canEdit(myRole);

  // Calendars a new event can be created in: active in the sidebar and writable.
  const postableCalendars = useMemo(
    () =>
      calendars.filter(
        (c) =>
          activeCalendarIds.includes(c.id) && canEdit(roleForCalendar(membersMap[c.id], me?.email)),
      ),
    [calendars, activeCalendarIds, membersMap, me?.email],
  );
  const defaultCalendarId = postableCalendars[0]?.id ?? calendars[0]?.id ?? '';

  useEffect(() => {
    if (!showEventModal) return;
    setShowCustomRecurrence(false);
    if (editingEvent) {
      const rule = editingEvent.recurrenceRule ?? null;
      const startDt = DateTime.fromISO(editingEvent.startAt, { zone: timezone });
      setForm({
        title: editingEvent.title,
        allDay: editingEvent.allDay,
        startAt: toLocalDatetime(editingEvent.startAt, timezone),
        endAt: toLocalDatetime(editingEvent.endAt, timezone),
        color: editingEvent.color,
        calendarId: editingEvent.calendarId,
        location: editingEvent.location,
        memo: editingEvent.memo,
        url: editingEvent.url ?? '',
        notificationOffset: editingEvent.notificationOffset ?? null,
        participants: editingEvent.participants ?? [],
        assignedTo: editingEvent.assignedTo ?? null,
        recurrenceRule: rule,
        recurrencePreset: ruleToPreset(rule, startDt),
      });
      setShowMore(
        !!(
          editingEvent.location ||
          editingEvent.url ||
          editingEvent.memo ||
          (editingEvent.participants?.length ?? 0) > 0 ||
          editingEvent.notificationOffset != null ||
          editingEvent.assignedTo
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
      const end = start.plus({ hours: 1 });
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
        assignedTo: null,
        recurrencePreset: 'none',
        recurrenceRule: null,
      });
      setShowMore(false);
    }
  }, [editingEvent, showEventModal, selectedDate, eventDraftStart, defaultCalendarId, timezone]);

  useEffect(() => {
    if (showEventModal) {
      setTimeout(() => titleRef.current?.focus(), 100);
    }
  }, [showEventModal]);

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
      let startIso = fromLocalDatetimeToISO(form.startAt, timezone);
      let endIso = fromLocalDatetimeToISO(form.endAt, timezone);
      if (form.allDay) {
        // All-day events are wall dates anchored at midnight in the selected zone.
        const startDay = DateTime.fromISO(form.startAt, { zone: timezone }).startOf('day');
        const endDay = DateTime.fromISO(form.endAt, { zone: timezone }).startOf('day');
        startIso = startDay.toISO() ?? startIso;
        // End is exclusive: add 1 day. Clamp end >= start.
        const effectiveEnd = endDay >= startDay ? endDay : startDay;
        endIso = effectiveEnd.plus({ days: 1 }).toISO() ?? endIso;
      } else {
        // For timed events, ensure end > start
        const startDt = DateTime.fromISO(startIso);
        const endDt = DateTime.fromISO(endIso);
        if (endDt <= startDt) {
          endIso = startDt.plus({ hours: 1 }).toISO() ?? endIso;
        }
      }
      const data = {
        title: form.title.trim(),
        allDay: form.allDay,
        startAt: startIso,
        endAt: endIso,
        timezone,
        color: form.color,
        location: form.location,
        memo: form.memo,
        url: form.url,
        notificationOffset: normalizeNotificationOffset(form.notificationOffset),
        participants: form.participants,
        assignedTo: form.assignedTo,
        recurrenceRule: normalizeRuleForApi(form.recurrenceRule, timezone),
      };
      setSaving(true);
      try {
        if (editingEvent) {
          await updateEvent(editingEvent.calendarId, editingEvent.id, data, scope);
        } else {
          await addEvent(form.calendarId, data);
        }
        setScopePrompt(null);
        closeEventModal();
      } catch (e) {
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
      closeEventModal,
      saving,
      editable,
      timezone,
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
        closeEventModal();
      } catch (e) {
        toast.error(errorMessage(e, t('error.deleteFailed')));
      } finally {
        setSaving(false);
      }
    },
    [editingEvent, isRecurringInstance, deleteEvent, closeEventModal, saving, editable, t],
  );

  if (!showEventModal) return null;

  // Shared form content used by both mobile and desktop layouts
  const formContent = (
    <>
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-3">
        <button
          type="button"
          onClick={closeEventModal}
          className="flex h-10 w-10 items-center justify-center bg-[var(--color-surface-secondary)] hover:bg-[var(--color-hover)] active:bg-[var(--color-active)]"
          style={{ borderRadius: 'var(--radius-sm)' }}
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
        </button>
        <span className="text-callout font-medium text-[var(--color-text-secondary)]">
          {editingEvent ? t('event.editEvent') : t('event.createEvent')}
        </span>
      </div>

      {/* Title */}
      <div className="px-6 pt-2 pb-4">
        <textarea
          ref={titleRef}
          value={form.title}
          onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
          placeholder={t('event.titlePlaceholder')}
          rows={1}
          className="w-full resize-none border-b-2 border-transparent bg-transparent text-display font-light text-[var(--color-text-primary)] outline-none transition-colors placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--color-accent)]"
        />
      </div>

      {/* Identity: which calendar + which color (event's visual identity) */}
      <div className="px-6 pb-3">
        <div className="card-section bg-[var(--color-surface-secondary)] p-4">
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
              >
                <rect x="3" y="4" width="18" height="18" rx="2" />
                <path d="M8 2v4M16 2v4M3 10h18" />
              </svg>
            );
            if (!editingEvent && postableCalendars.length > 1) {
              return (
                <>
                  <div className="flex items-center gap-3 py-1.5">
                    {calIcon}
                    <select
                      value={formCalendarId}
                      onChange={(e) => setForm((f) => ({ ...f, calendarId: e.target.value }))}
                      className="input-modern h-8 flex-1 text-sm"
                    >
                      {postableCalendars.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.name}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="my-1 border-t border-[var(--color-border)] opacity-50" />
                </>
              );
            }
            if (current && calendars.length > 1) {
              return (
                <>
                  <div className="flex items-center gap-3 py-1.5">
                    {calIcon}
                    <span
                      className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                      style={{ backgroundColor: current.color }}
                    />
                    <span className="text-callout font-medium text-[var(--color-text-primary)]">
                      {current.name}
                    </span>
                  </div>
                  <div className="my-1 border-t border-[var(--color-border)] opacity-50" />
                </>
              );
            }
            return null;
          })()}

          {/* Color */}
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
              <path d="M20.59 13.41l-7.17 7.17a2 2 0 01-2.83 0L2 12V2h10l8.59 8.59a2 2 0 010 2.82z" />
              <circle cx="7" cy="7" r="1" />
            </svg>
            <div className="flex flex-wrap gap-2.5">
              {labels.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => setForm((f) => ({ ...f, color: c.color }))}
                  className="color-dot h-7 w-7"
                  style={{
                    backgroundColor: c.color,
                    boxShadow:
                      form.color === c.color
                        ? `0 0 0 2px var(--color-surface), 0 0 0 4px ${c.color}`
                        : 'none',
                  }}
                  aria-label={t(c.nameKey as TranslationKey)}
                />
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Creator (read-only metadata, shown only for existing events) */}
      {editingEvent &&
        (() => {
          const member = (membersMap[editingEvent.calendarId] ?? []).find(
            (m) => m.id === editingEvent.createdBy,
          );
          const name = editingEvent.creatorName || member?.name;
          const icon = editingEvent.creatorIcon || member?.icon;
          const avatarUrl = editingEvent.creatorAvatarUrl;
          if (!name) return null;
          return (
            <div className="flex items-center gap-2 px-6 pb-3 text-caption text-[var(--color-text-secondary)]">
              <span>{t('event.createdBy')}</span>
              <span
                className="flex h-5 w-5 shrink-0 items-center justify-center overflow-hidden bg-[var(--color-surface-inset)] text-caption"
                style={{ borderRadius: 'var(--radius-sm)' }}
              >
                {avatarUrl ? (
                  <img src={avatarUrl} alt="" className="h-full w-full object-cover" />
                ) : (
                  (icon ?? name.slice(0, 1))
                )}
              </span>
              <span className="font-medium text-[var(--color-text-primary)]">{name}</span>
            </div>
          );
        })()}

      {/* Date & Time card */}
      <div className="px-6">
        <div className="card-section bg-[var(--color-surface-secondary)] p-4">
          <DateTimeField
            label={t('event.start')}
            dateValue={DateTime.fromISO(form.startAt)}
            timeValue={form.startAt.split('T')[1]?.slice(0, 5) ?? '09:00'}
            showTime={!form.allDay}
            onDateChange={(date) => {
              const time = form.startAt.split('T')[1] ?? '09:00';
              const newStart = `${date.toFormat('yyyy-MM-dd')}T${time}`;
              setForm((f) => {
                const oldStartDt = DateTime.fromISO(f.startAt);
                const oldEndDt = DateTime.fromISO(f.endAt);
                const newStartDt = DateTime.fromISO(newStart);
                // Preserve the original duration between start and end
                const durationMs = oldEndDt.diff(oldStartDt).milliseconds;
                const newEndDt = newStartDt.plus({ milliseconds: Math.max(durationMs, 0) });
                const newEnd = newEndDt.toFormat("yyyy-MM-dd'T'HH:mm");
                return { ...f, startAt: newStart, endAt: newEnd };
              });
            }}
            onTimeChange={(time) => {
              const datePart = form.startAt.split('T')[0] ?? '';
              setForm((f) => {
                const newStart = `${datePart}T${time}`;
                // If end is now before start on same day, push end forward by 1 hour
                const startDt = DateTime.fromISO(newStart);
                const endDt = DateTime.fromISO(f.endAt);
                if (endDt <= startDt) {
                  const newEnd = startDt.plus({ hours: 1 }).toFormat("yyyy-MM-dd'T'HH:mm");
                  return { ...f, startAt: newStart, endAt: newEnd };
                }
                return { ...f, startAt: newStart };
              });
            }}
          />
          <DateTimeField
            label={t('event.end')}
            dateValue={DateTime.fromISO(form.endAt)}
            timeValue={form.endAt.split('T')[1]?.slice(0, 5) ?? '10:00'}
            showTime={!form.allDay}
            onDateChange={(date) => {
              const time = form.endAt.split('T')[1] ?? '10:00';
              setForm((f) => {
                const newEnd = `${date.toFormat('yyyy-MM-dd')}T${time}`;
                // If end is before start, clamp to start date
                const startDt = DateTime.fromISO(f.startAt);
                const endDt = DateTime.fromISO(newEnd);
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
                const startDt = DateTime.fromISO(f.startAt);
                const endDt = DateTime.fromISO(newEnd);
                if (endDt <= startDt) return f;
                return { ...f, endAt: newEnd };
              });
            }}
            minDate={DateTime.fromISO(form.startAt)}
          />

          <div className="my-1 border-t border-[var(--color-border)] opacity-50" />

          {/* All day toggle */}
          <div className="flex items-center justify-between py-2.5">
            <span className="text-default text-[var(--color-text-primary)]">
              {t('calendar.allDay')}
            </span>
            <button
              type="button"
              onClick={() => setForm((f) => ({ ...f, allDay: !f.allDay }))}
              className="toggle-track relative h-7 w-12"
              style={{
                backgroundColor: form.allDay ? 'var(--color-accent)' : 'var(--color-surface-inset)',
              }}
            >
              <span
                className="toggle-knob absolute top-0.5 h-6 w-6"
                style={{ left: form.allDay ? '22px' : '2px' }}
              />
            </button>
          </div>

          {/* Recurrence */}
          <div className="flex items-center justify-between py-2.5">
            <span className="text-default text-[var(--color-text-primary)]">
              {t('event.recurrenceNone').split('/')[0] || 'Repeat'}
            </span>
            <CustomSelect
              value={form.recurrencePreset}
              options={(() => {
                const startDt = DateTime.fromISO(form.startAt);
                const dayLabel = getWeekdayLabel(startDt, locale);
                const nth = getNthWeekday(startDt);
                return [
                  { value: 'none', label: t('event.recurrenceNone') },
                  { value: 'daily', label: t('event.recurrenceDaily') },
                  { value: 'weekly', label: t('event.recurrenceWeekly', { day: dayLabel }) },
                  { value: 'weekdays', label: t('event.recurrenceWeekdays') },
                  {
                    value: 'monthly_nth',
                    label: t('event.recurrenceMonthlyNth', { n: nth, day: dayLabel }),
                  },
                  {
                    value: 'monthly_date',
                    label: t('event.recurrenceMonthlyDate', { date: startDt.day }),
                  },
                  { value: 'yearly', label: t('event.recurrenceYearly') },
                  { value: 'custom', label: t('event.recurrenceCustom') },
                ];
              })()}
              onChange={(val) => {
                const preset = val as RecurrencePreset;
                if (preset === 'custom') {
                  setShowCustomRecurrence(true);
                  setForm((f) => ({
                    ...f,
                    recurrencePreset: 'custom',
                    recurrenceRule: f.recurrenceRule ?? { freq: 'daily', interval: 1 },
                  }));
                  return;
                }
                setShowCustomRecurrence(false);
                const startDt = DateTime.fromISO(form.startAt);
                setForm((f) => ({
                  ...f,
                  recurrencePreset: preset,
                  recurrenceRule: presetToRule(preset, startDt),
                }));
              }}
            />
          </div>

          {/* Custom recurrence dialog */}
          {showCustomRecurrence && form.recurrenceRule && (
            <div className="card-section mt-2 space-y-3 bg-[var(--color-surface-inset)] p-4">
              <div className="space-y-1.5">
                <span className="block text-body text-[var(--color-text-secondary)]">
                  {t('event.recurrenceInterval')}
                </span>
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={1}
                    max={99}
                    value={form.recurrenceRule.interval}
                    onChange={(e) => {
                      const val = Math.max(1, Math.min(99, Number(e.target.value)));
                      setForm((f) => ({
                        ...f,
                        recurrenceRule: f.recurrenceRule
                          ? { ...f.recurrenceRule, interval: val }
                          : null,
                      }));
                    }}
                    style={{ width: '4rem' }}
                    className="input-modern shrink-0 text-center text-body"
                  />
                  <CustomSelect
                    value={form.recurrenceRule.freq}
                    options={[
                      { value: 'daily', label: t('event.unitDay') },
                      { value: 'weekly', label: t('event.unitWeek') },
                      { value: 'monthly', label: t('event.unitMonth') },
                      { value: 'yearly', label: t('event.unitYear') },
                    ]}
                    onChange={(val) => {
                      const freq = val as RecurrenceRule['freq'];
                      const startDt = DateTime.fromISO(form.startAt);
                      const dayCode = WEEKDAY_CODES[startDt.weekday % 7] ?? 'SU';
                      setForm((f) => {
                        if (!f.recurrenceRule) return f;
                        // Seed defaults so the rule stays valid for its frequency.
                        const base = {
                          ...f.recurrenceRule,
                          freq,
                          byDay: undefined as string[] | undefined,
                          byMonthDay: undefined as number | undefined,
                          bySetPos: undefined as number | undefined,
                        };
                        if (freq === 'weekly') base.byDay = [dayCode];
                        if (freq === 'monthly') base.byMonthDay = startDt.day;
                        return { ...f, recurrenceRule: base };
                      });
                    }}
                  />
                </div>
              </div>

              {/* Weekly: pick one or more weekdays */}
              {form.recurrenceRule.freq === 'weekly' && (
                <div className="space-y-1.5">
                  <span className="block text-body text-[var(--color-text-secondary)]">
                    {t('event.recurrenceWeekdaysLabel')}
                  </span>
                  <div className="flex flex-wrap gap-1.5">
                    {WEEKDAY_CODES.map((code, idx) => {
                      const selected = form.recurrenceRule?.byDay?.includes(code) ?? false;
                      return (
                        <button
                          key={code}
                          type="button"
                          onClick={() =>
                            setForm((f) => {
                              if (!f.recurrenceRule) return f;
                              const cur = f.recurrenceRule.byDay ?? [];
                              const next = cur.includes(code)
                                ? cur.filter((d) => d !== code)
                                : [...cur, code];
                              // Keep at least one weekday selected.
                              return {
                                ...f,
                                recurrenceRule: {
                                  ...f.recurrenceRule,
                                  byDay: next.length > 0 ? next : cur,
                                },
                              };
                            })
                          }
                          className="flex h-8 w-8 items-center justify-center rounded-full text-caption font-semibold transition-colors"
                          style={{
                            backgroundColor: selected
                              ? 'var(--color-accent)'
                              : 'var(--color-surface-secondary)',
                            color: selected ? '#fff' : 'var(--color-text-secondary)',
                          }}
                        >
                          {WEEKDAY_LABELS_JA[idx]}
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}

              {/* Monthly: by day-of-month or by nth weekday */}
              {form.recurrenceRule.freq === 'monthly' && (
                <div className="space-y-1.5">
                  <span className="block text-body text-[var(--color-text-secondary)]">
                    {t('event.recurrenceMonthlyMode')}
                  </span>
                  <CustomSelect
                    value={form.recurrenceRule.bySetPos ? 'weekday' : 'date'}
                    options={[
                      { value: 'date', label: t('event.recurrenceMonthlyByDate') },
                      { value: 'weekday', label: t('event.recurrenceMonthlyByWeekday') },
                    ]}
                    onChange={(mode) => {
                      const startDt = DateTime.fromISO(form.startAt);
                      setForm((f) => {
                        if (!f.recurrenceRule) return f;
                        if (mode === 'weekday') {
                          return {
                            ...f,
                            recurrenceRule: {
                              ...f.recurrenceRule,
                              bySetPos: getNthWeekday(startDt),
                              byDay: [WEEKDAY_CODES[startDt.weekday % 7] ?? 'SU'],
                              byMonthDay: undefined,
                            },
                          };
                        }
                        return {
                          ...f,
                          recurrenceRule: {
                            ...f.recurrenceRule,
                            byMonthDay: startDt.day,
                            bySetPos: undefined,
                            byDay: undefined,
                          },
                        };
                      });
                    }}
                  />
                  {form.recurrenceRule.bySetPos ? (
                    <div className="flex items-center gap-2">
                      <CustomSelect
                        value={String(form.recurrenceRule.bySetPos)}
                        options={[
                          { value: '1', label: t('event.recurrenceNthFirst') },
                          { value: '2', label: t('event.recurrenceNthSecond') },
                          { value: '3', label: t('event.recurrenceNthThird') },
                          { value: '4', label: t('event.recurrenceNthFourth') },
                          { value: '-1', label: t('event.recurrenceNthLast') },
                        ]}
                        onChange={(pos) =>
                          setForm((f) => ({
                            ...f,
                            recurrenceRule: f.recurrenceRule
                              ? { ...f.recurrenceRule, bySetPos: Number(pos) }
                              : null,
                          }))
                        }
                      />
                      <CustomSelect
                        value={form.recurrenceRule.byDay?.[0] ?? 'SU'}
                        options={WEEKDAY_CODES.map((code, idx) => ({
                          value: code,
                          label: WEEKDAY_LABELS_JA[idx] ?? code,
                        }))}
                        onChange={(day) =>
                          setForm((f) => ({
                            ...f,
                            recurrenceRule: f.recurrenceRule
                              ? { ...f.recurrenceRule, byDay: [day] }
                              : null,
                          }))
                        }
                      />
                    </div>
                  ) : (
                    <input
                      type="number"
                      min={1}
                      max={31}
                      value={form.recurrenceRule.byMonthDay ?? DateTime.fromISO(form.startAt).day}
                      onChange={(e) => {
                        const val = Math.max(1, Math.min(31, Number(e.target.value)));
                        setForm((f) => ({
                          ...f,
                          recurrenceRule: f.recurrenceRule
                            ? { ...f.recurrenceRule, byMonthDay: val }
                            : null,
                        }));
                      }}
                      style={{ width: '4rem' }}
                      className="input-modern shrink-0 text-center text-body"
                    />
                  )}
                </div>
              )}

              <div className="space-y-1.5">
                <span className="text-body text-[var(--color-text-secondary)]">
                  {t('event.recurrenceEndLabel')}
                </span>
                <div className="flex flex-col gap-1.5">
                  <label className="flex items-center gap-2">
                    <input
                      type="radio"
                      name="recEnd"
                      checked={!form.recurrenceRule.until && !form.recurrenceRule.count}
                      onChange={() =>
                        setForm((f) => ({
                          ...f,
                          recurrenceRule: f.recurrenceRule
                            ? { ...f.recurrenceRule, until: undefined, count: undefined }
                            : null,
                        }))
                      }
                      className="accent-[var(--color-accent)]"
                    />
                    <span className="text-body text-[var(--color-text-primary)]">
                      {t('event.recurrenceEndNever')}
                    </span>
                  </label>
                  <label className="flex flex-wrap items-center gap-2">
                    <input
                      type="radio"
                      name="recEnd"
                      checked={!!form.recurrenceRule.until}
                      onChange={() => {
                        const defaultEnd = DateTime.fromISO(form.startAt)
                          .plus({ months: 1 })
                          .toFormat('yyyy-MM-dd');
                        setForm((f) => ({
                          ...f,
                          recurrenceRule: f.recurrenceRule
                            ? { ...f.recurrenceRule, until: defaultEnd, count: undefined }
                            : null,
                        }));
                      }}
                      className="accent-[var(--color-accent)]"
                    />
                    <span className="shrink-0 whitespace-nowrap text-body text-[var(--color-text-primary)]">
                      {t('event.recurrenceEndDate')}:
                    </span>
                    {form.recurrenceRule.until && (
                      <DateTimeField
                        label=""
                        dateValue={DateTime.fromISO(form.recurrenceRule.until)}
                        timeValue="00:00"
                        showTime={false}
                        onDateChange={(date) =>
                          setForm((f) => ({
                            ...f,
                            recurrenceRule: f.recurrenceRule
                              ? { ...f.recurrenceRule, until: date.toFormat('yyyy-MM-dd') }
                              : null,
                          }))
                        }
                        onTimeChange={() => {}}
                      />
                    )}
                  </label>
                  <label className="flex flex-wrap items-center gap-2">
                    <input
                      type="radio"
                      name="recEnd"
                      checked={!!form.recurrenceRule.count}
                      onChange={() =>
                        setForm((f) => ({
                          ...f,
                          recurrenceRule: f.recurrenceRule
                            ? { ...f.recurrenceRule, count: 10, until: undefined }
                            : null,
                        }))
                      }
                      className="accent-[var(--color-accent)]"
                    />
                    <span className="shrink-0 whitespace-nowrap text-body text-[var(--color-text-primary)]">
                      {t('event.recurrenceEndCount')}:
                    </span>
                    {form.recurrenceRule.count && (
                      <input
                        type="number"
                        min={1}
                        max={365}
                        value={form.recurrenceRule.count}
                        onChange={(e) => {
                          const val = Math.max(1, Math.min(365, Number(e.target.value)));
                          setForm((f) => ({
                            ...f,
                            recurrenceRule: f.recurrenceRule
                              ? { ...f.recurrenceRule, count: val }
                              : null,
                          }));
                        }}
                        style={{ width: '4rem' }}
                        className="input-modern shrink-0 text-center text-body"
                      />
                    )}
                  </label>
                </div>
              </div>
            </div>
          )}

          {/* Recurrence label for editing recurring events */}
          {editingEvent?.isRecurrence && form.recurrenceRule && (
            <p className="mt-1 text-footnote text-[var(--color-text-tertiary)]">
              {recurrenceLabel(form.recurrenceRule, DateTime.fromISO(form.startAt), t, locale)}
            </p>
          )}
        </div>
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
                  value={form.memo}
                  onChange={(e) => setForm((f) => ({ ...f, memo: e.target.value }))}
                  placeholder={t('event.memo')}
                  rows={2}
                  className="flex-1 resize-none bg-transparent text-callout text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
                />
              </div>
            </div>
          </div>

          {/* Participants & Notification card */}
          <div className="mt-3 px-6">
            <div className="card-section bg-[var(--color-surface-secondary)] p-4">
              {/* Participants */}
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
                  <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2" />
                  <circle cx="9" cy="7" r="4" />
                  <path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75" />
                </svg>
                <div className="flex flex-wrap gap-1.5">
                  {(membersMap[form.calendarId] ?? []).map((m) => {
                    const isSelected = form.participants.includes(m.id);
                    return (
                      <button
                        key={m.id}
                        type="button"
                        onClick={() =>
                          setForm((f) => ({
                            ...f,
                            participants: isSelected
                              ? f.participants.filter((id) => id !== m.id)
                              : [...f.participants, m.id],
                          }))
                        }
                        title={m.name}
                        className="flex h-8 w-8 items-center justify-center rounded-full text-default transition-opacity"
                        style={{
                          backgroundColor: m.color,
                          opacity: isSelected ? 1 : 0.3,
                          outline: isSelected ? `2px solid ${m.color}` : 'none',
                          outlineOffset: '2px',
                        }}
                      >
                        {m.icon || m.name[0]}
                      </button>
                    );
                  })}
                  {form.participants.length === 0 && (
                    <span className="text-default text-[var(--color-text-tertiary)]">
                      {t('event.selectParticipants')}
                    </span>
                  )}
                </div>
              </div>

              <div className="my-1 border-t border-[var(--color-border)] opacity-50" />

              {/* Notification */}
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

              <div className="my-1 border-t border-[var(--color-border)] opacity-50" />

              {/* Assignee */}
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
                  <path d="M16 21v-2a4 4 0 00-4-4H6a4 4 0 00-4 4v2" />
                  <circle cx="9" cy="7" r="4" />
                  <path d="M22 11l-3 3-1.5-1.5" />
                </svg>
                <CustomSelect
                  value={form.assignedTo ?? 'none'}
                  options={[
                    { value: 'none', label: t('event.assigneeNone') },
                    ...(membersMap[form.calendarId] ?? []).map((m) => ({
                      value: m.id,
                      label: m.name,
                    })),
                  ]}
                  onChange={(v) => {
                    setForm((f) => ({ ...f, assignedTo: v === 'none' ? null : v }));
                  }}
                  className="flex-1"
                />
              </div>
            </div>
          </div>
        </>
      )}

      {/* Checklist (edit mode only) */}
      {editingEvent && (
        <ChecklistSection calendarId={editingEvent.calendarId} eventId={editingEvent.id} />
      )}

      {/* Attachments (edit mode only) */}
      {editingEvent && (
        <AttachmentsSection calendarId={editingEvent.calendarId} eventId={editingEvent.id} />
      )}

      {/* Comments (edit mode only) */}
      {editingEvent && (
        <CommentsSection calendarId={editingEvent.calendarId} eventId={editingEvent.id} />
      )}

      {/* Delete (edit mode, editors only) */}
      {editingEvent && editable && (
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
      {/* Mobile: bottom sheet */}
      <div className="sm:hidden">
        <button
          type="button"
          aria-label={t('common.close')}
          className="modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)]"
          onClick={closeEventModal}
        />
        <div className="glass-surface-heavy bottom-sheet fixed inset-x-0 bottom-0 z-50 flex max-h-[92vh] flex-col overflow-hidden">
          <div className="drag-handle mx-auto mt-2 mb-1 h-1 w-10 rounded-full bg-[var(--color-text-tertiary)] opacity-30" />
          <div className="flex-1 overflow-y-auto">{formContent}</div>
          {/* Sticky action bar */}
          <div className="flex gap-3 border-t border-[var(--color-border)] px-6 py-4">
            <button
              type="button"
              onClick={closeEventModal}
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

      {/* Desktop: centered modal */}
      <div className="hidden sm:contents">
        <button
          type="button"
          aria-label={t('common.close')}
          className="modal-backdrop fixed inset-0 z-50 bg-[var(--color-overlay)]"
          onClick={closeEventModal}
        />
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 pointer-events-none">
          <div className="glass-surface-heavy modal-panel pointer-events-auto flex w-full max-w-[480px] max-h-[90vh] flex-col overflow-hidden ring-1 ring-[var(--color-border)]">
            <div className="flex-1 overflow-y-auto">{formContent}</div>
            {/* Action bar */}
            <div className="flex gap-3 border-t border-[var(--color-border)] px-6 py-4">
              <button
                type="button"
                onClick={closeEventModal}
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
