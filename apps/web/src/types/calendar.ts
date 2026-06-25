export interface Calendar {
  id: string;
  name: string;
  color: string;
  coverUrl: string;
  createdAt: string;
  /** True when an active public (read-only embed) link exposes this calendar externally. */
  publicShared: boolean;
}

export interface Member {
  id: string;
  name: string;
  email: string;
  role: string;
  color: string;
  icon: string;
}

export interface RecurrenceRule {
  freq: 'daily' | 'weekly' | 'monthly' | 'yearly';
  interval: number;
  byDay?: string[] | undefined;
  byMonthDay?: number | undefined;
  bySetPos?: number | undefined;
  until?: string | undefined;
  count?: number | undefined;
}

export type RecurrencePreset =
  | 'none'
  | 'daily'
  | 'weekly'
  | 'weekdays'
  | 'monthly_nth'
  | 'monthly_date'
  | 'yearly'
  | 'custom';

export interface ChecklistItem {
  id: string;
  title: string;
  done: boolean;
  sortOrder: number;
  createdAt: string;
}

export interface EventAttachment {
  id: string;
  filename: string;
  contentType: string;
  byteSize: number;
  createdAt: string;
}

export const NOTIFICATION_OFFSETS = [
  { label: 'none', value: null },
  { label: 'atTime', value: 0 },
  { label: '5min', value: 5 },
  { label: '10min', value: 10 },
  { label: '15min', value: 15 },
  { label: '30min', value: 30 },
  { label: '1hour', value: 60 },
  { label: '2hours', value: 120 },
  { label: '1day', value: 1440 },
  { label: '2days', value: 2880 },
] as const;

/** The set of notification offsets (in minutes) the API accepts. */
export const ALLOWED_NOTIFICATION_OFFSETS: ReadonlySet<number> = new Set(
  NOTIFICATION_OFFSETS.flatMap((o) => (o.value === null ? [] : [o.value as number])),
);

/** Returns `offset` if it is an accepted notification value, otherwise `null`. */
export function normalizeNotificationOffset(offset: number | null | undefined): number | null {
  if (offset == null) return null;
  return ALLOWED_NOTIFICATION_OFFSETS.has(offset) ? offset : null;
}

export interface CalendarEvent {
  id: string;
  calendarId: string;
  title: string;
  allDay: boolean;
  startAt: string;
  endAt: string;
  timezone?: string;
  color: string;
  assignedTo: string | null;
  location: string;
  memo: string;
  url: string;
  notificationOffset: number | null;
  participants: string[];
  recurrenceRule: RecurrenceRule | null;
  isRecurrence: boolean;
  recurrenceDate: string | null;
  createdBy?: string;
  creatorName?: string;
  creatorIcon?: string;
  creatorAvatarUrl?: string;
  createdAt: string;
  updatedAt: string;
}

export interface Memo {
  id: string;
  calendarId: string;
  title: string;
  done: boolean;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
}

export type CalendarView = 'month' | 'week' | 'list' | 'year';

export interface Label {
  id: string;
  nameKey: string;
  color: string;
}

export const FALLBACK_LABEL_COLOR = '#47B2F7';

export const MEMBER_COLORS = [
  '#47B2F7',
  '#F35F8C',
  '#B38BDC',
  '#FDC02D',
  '#E73B3B',
  '#2ECC87',
  '#F5A623',
  '#26C6DA',
  '#8F8F8F',
  '#26A69A',
] as const;
