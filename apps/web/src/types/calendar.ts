import type { components } from '@/lib/api-schema';

type Schemas = components['schemas'];

/** A response schema without the JSON Schema pointer the server adds. */
type Wire<K extends keyof Schemas> = Omit<Schemas[K], '$schema'>;

export type Calendar = Wire<'CalendarResponse'>;

export type Member = Wire<'MemberResponse'>;

export type RecurrenceRule = Omit<Wire<'RecurrenceRuleResponse'>, 'freq' | 'wkst'> & {
  freq: 'daily' | 'weekly' | 'monthly' | 'yearly';
  /** First day of the week; absent reads as MO. Matters for weekly rules with interval > 1. */
  wkst?: 'MO' | 'SU' | undefined;
};

export type RecurrencePreset =
  | 'none'
  | 'daily'
  | 'weekly'
  | 'weekdays'
  | 'monthly_nth'
  | 'monthly_date'
  | 'yearly'
  | 'custom';

export type ChecklistItem = Wire<'ChecklistItemResponse'>;

export type EventAttachment = Wire<'AttachmentResponse'>;

/** Comment response returned by the event activities API. */
export type EventComment = Wire<'CommentResponse'>;

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

/** Whether the time reads as taken. The iCalendar TRANSP axis, and what an
 *  external free/busy consumer sees. */
export type ShowAs = Schemas['EventResponse']['showAs'];

/** Whether the commitment could move. A separate question from whether the
 *  time is taken: a meeting its owner would gladly reschedule and one that
 *  cannot move are both busy, which is why this is its own axis. */
export type Flexibility = Schemas['EventResponse']['flexibility'];

/**
 * Who may see an event's details. `default` follows the calendar, `private`
 * publishes the time with no details, and `confidential` is not published at
 * all.
 */
export type Visibility = Schemas['EventResponse']['visibility'];

/** What an event row is: a normal event, a labelled block, a free window, or a point in time. */
export type EventKind = Schemas['EventResponse']['kind'];

/** The option sets shown in pickers, in the order they read best. */
export const VISIBILITY_OPTIONS: Visibility[] = ['default', 'public', 'private', 'confidential'];
export const SHOW_AS_OPTIONS: ShowAs[] = ['busy', 'tentative', 'free', 'oof'];

export type Rsvp = Schemas['AttendeeResponse']['rsvp'];

/** Per-participant state: their answer, and whether the owner has trusted them to change the event. */
export type Attendee = Wire<'AttendeeResponse'>;

/**
 * An event as the list and detail endpoints return it. An all-day event
 * carries `startDate`/`endDate` (`YYYY-MM-DD`, `endDate` exclusive) alongside
 * `startAt`/`endAt`. `etag` is the version of the row `id` points at and
 * `seriesEtag` the master's, both sent as If-Match on a write. When
 * `detailsHidden` is set the server has withheld the details.
 */
export type CalendarEvent = Omit<
  Wire<'EventResponse'>,
  'recurrenceRule' | 'recurrenceDate' | 'attendees'
> & {
  /** Absent on events read from a public share. */
  attendees?: Attendee[] | null;
  recurrenceRule: RecurrenceRule | null;
  recurrenceDate?: string | null;
};

export type Memo = Wire<'MemoResponse'>;

export type CalendarView = 'month' | 'week' | 'list' | 'year';

/** A named colour a calendar's events can be filed under. */
export type Label = Wire<'LabelResponse'>;

export const FALLBACK_LABEL_COLOR = '#47B2F7';

/**
 * The palette a calendar's layers are drawn from.
 *
 * The same ten colours as the server's member palette (`memberpalette`). They
 * are repeated here because the first calendar is created before there is one
 * to ask, and the two lists are pinned by a test on each side: a palette that
 * drifts hands out a colour the server's list does not contain.
 */
export const MEMBER_COLORS = [
  '#47B2F7',
  '#F35F8C',
  '#B38BDC',
  '#FDC02D',
  '#E73B3B',
  '#2ECC87',
  '#F5A623',
  '#8F8F8F',
  '#42A5F5',
  '#FF7043',
] as const;

/**
 * The colour editor's palette: ten hue columns in spectrum order (the last
 * neutral), each in a soft, a standard and a deep tone, row by row. The
 * standard row keeps the member palette's colours.
 */
export const COLOR_SWATCH_ROWS = [
  [
    '#F59A9A',
    '#FFAA8C',
    '#F9CB7A',
    '#FEE08A',
    '#8EE3BC',
    '#8ADAD6',
    '#9BD3FB',
    '#D6C2EE',
    '#F9A9C1',
    '#C9C9CE',
  ],
  [
    '#E73B3B',
    '#FF7043',
    '#F5A623',
    '#FDC02D',
    '#2ECC87',
    '#26B5B0',
    '#47B2F7',
    '#B38BDC',
    '#F35F8C',
    '#8F8F8F',
  ],
  [
    '#B3261E',
    '#D5501F',
    '#C47A06',
    '#C99A00',
    '#18935E',
    '#157F7B',
    '#2273C9',
    '#7C55B5',
    '#C23A66',
    '#55555A',
  ],
] as const;

export const COLOR_SWATCHES: readonly string[] = COLOR_SWATCH_ROWS.flat();
