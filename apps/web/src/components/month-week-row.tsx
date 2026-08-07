import type { DateTime } from 'luxon';
import { memo, type PointerEvent as ReactPointerEvent, useMemo } from 'react';
import { useT } from '@/i18n';
import { fromISOInZone, isToday, jsDayOfWeek } from '@/lib/date-utils';
import { getHoliday } from '@/lib/holidays';
import {
  eventStartDay,
  isMultiDay,
  layoutDayCell,
  layoutWeek,
  MAX_VISIBLE_TRACKS,
} from '@/lib/week-layout';
import type { CalendarEvent } from '@/types/calendar';

/** Landing range of the event currently being dragged. */
export interface DragLanding {
  event: CalendarEvent;
  start: DateTime;
  end: DateTime;
}

/**
 * Vertical rhythm of one week row, in px.
 *
 * A day cell stacks its chips in fixed slots while multi-day bars are drawn in
 * an overlay above the cells, so the two only line up while both are measured
 * from the same numbers.
 *
 * `overflowH` is reserved whether or not a cell shows a "+N". A badge added to
 * a body sized for the tracks alone compresses every slot under it, which walks
 * the chips off the bars they belong with; and sizing the body per cell would
 * make a row change height as its events arrive, moving every row below it in
 * the mobile scroller.
 */
interface WeekRowMetrics {
  /** Space above the day number. */
  padTop: number;
  /** Height of the day-number row. */
  dateRowH: number;
  /** Vertical pitch of one event track. */
  slotH: number;
  /** Height of the "+N" row kept free below the tracks. */
  overflowH: number;
}

/** Gap between tracks: a chip or bar is drawn this much shorter than its slot. */
const TRACK_GAP = 1;

/** Horizontal inset of a chip inside its cell (cell px-1 plus chip mx-0.5). */
const CHIP_INSET = 6;

/** Names the track slots so an empty one keeps its identity across renders. */
const TRACK_SLOTS = Array.from({ length: MAX_VISIBLE_TRACKS }, (_, track) => `t${track}`);

/** Comfortable is the pointer-sized desktop grid, compact the mobile scroller. */
export type WeekRowDensity = 'comfortable' | 'compact';

interface DensityStyle {
  metrics: WeekRowMetrics;
  dayBackdrop: string;
  dayNumber: string;
  chip: string;
  chipHover: string;
  dot: string;
  bar: string;
  overflow: string;
  cursorMovable: string;
  cursorFixed: string;
}

const DENSITY: Record<WeekRowDensity, DensityStyle> = {
  comfortable: {
    metrics: { padTop: 6, dateRowH: 28, slotH: 21, overflowH: 16 },
    dayBackdrop:
      'transition-colors hover:bg-[var(--color-hover)] focus-visible:bg-[var(--color-hover)] focus-visible:outline-none',
    dayNumber: 'h-7 w-7 text-callout',
    chip: 'gap-1 rounded-[5px] px-1.5 text-caption',
    chipHover: 'hover:brightness-95',
    dot: 'h-1.5 w-1.5',
    bar: 'gap-1 px-2 text-caption',
    overflow: 'text-caption',
    cursorMovable: 'cursor-grab active:cursor-grabbing',
    cursorFixed: 'cursor-pointer',
  },
  compact: {
    metrics: { padTop: 4, dateRowH: 24, slotH: 15, overflowH: 13 },
    dayBackdrop: 'touch-manipulation transition-colors active:bg-[var(--color-active)]',
    dayNumber: 'h-6 w-6 text-body',
    chip: 'gap-0.5 rounded-[4px] px-1 text-micro',
    chipHover: '',
    dot: 'h-1 w-1',
    bar: 'gap-1 px-2 text-micro',
    overflow: 'text-micro',
    cursorMovable: 'active:cursor-grabbing',
    cursorFixed: '',
  },
};

export interface MonthWeekRowProps {
  /** Sunday the row starts on. */
  weekStart: DateTime;
  /** The events touching this week, as grouped by `bucketEventsByWeek`. */
  events: CalendarEvent[];
  zone: string;
  holidaysCountry: string | null;
  selectedDate: DateTime;
  /**
   * Changes when the optional holiday chunk lands. The row reads that data
   * through `getHoliday`, which no prop of its own would ever report, so a
   * memoised row needs the loader's revision to know it has more to draw.
   */
  holidayRevision?: number;
  density: WeekRowDensity;
  /** Month the grid is paging; days outside it are dimmed. Null draws them all alike. */
  pagedMonth?: { year: number; month: number } | null;
  /** Holiday names beside the day number, for cells wide enough to read them. */
  showHolidayName?: boolean;
  /** Event under an active drag; drawn faded while its ghost follows the pointer. */
  draggingEventId?: string | null;
  /** Landing preview, already narrowed to the rows it crosses. */
  dragLanding?: DragLanding | null;
  onDayClick: (date: DateTime) => void;
  onDayDoubleClick?: (date: DateTime) => void;
  /** Given, the "+N" is a button; otherwise it is a badge the cell tap covers. */
  onOverflowClick?: (date: DateTime) => void;
  onEventClick: (eventId: string) => void;
  onEventPointerDown: (evt: CalendarEvent, e: ReactPointerEvent) => void;
  canMoveEvent: (evt: CalendarEvent) => boolean;
}

function dateColor(dow: number, isHoliday: boolean): string {
  if (isHoliday || dow === 0) return 'var(--color-sunday)';
  if (dow === 6) return 'var(--color-saturday)';
  return 'var(--color-text-primary)';
}

function barRadius(continuesLeft: boolean, continuesRight: boolean): string {
  if (continuesLeft && continuesRight) return '0';
  if (continuesLeft) return '0 5px 5px 0';
  if (continuesRight) return '5px 0 0 5px';
  return '5px';
}

/**
 * One Sunday-aligned week of the month view: seven day cells with their
 * single-day chips, the multi-day bars drawn across them, and the drag ghost.
 *
 * Both month surfaces render this. They differ in density and in what a tap
 * does, not in how a week is laid out, and keeping one implementation is what
 * stops a fix landing on one surface and not the other.
 */
function MonthWeekRowImpl({
  weekStart,
  events,
  zone,
  holidaysCountry,
  selectedDate,
  density,
  pagedMonth = null,
  showHolidayName = false,
  draggingEventId = null,
  dragLanding = null,
  onDayClick,
  onDayDoubleClick,
  onOverflowClick,
  onEventClick,
  onEventPointerDown,
  canMoveEvent,
}: MonthWeekRowProps) {
  const t = useT();
  const { metrics, ...style } = DENSITY[density];
  const barH = metrics.slotH - TRACK_GAP;

  const week = useMemo(
    () => Array.from({ length: 7 }, (_, i) => weekStart.plus({ days: i })),
    [weekStart],
  );

  const positioned = useMemo(() => layoutWeek(weekStart, events, zone), [weekStart, events, zone]);

  // Single-day events grouped by yyyy-MM-dd within this week.
  const singleDayMap = useMemo(() => {
    const map = new Map<string, CalendarEvent[]>();
    for (const evt of events) {
      if (isMultiDay(evt, zone)) continue;
      const startDt = eventStartDay(evt, zone);
      const inWeek = week.find((d) => d.hasSame(startDt, 'day'));
      if (!inWeek) continue;
      const key = inWeek.toFormat('yyyy-MM-dd');
      const arr = map.get(key) ?? [];
      arr.push(evt);
      map.set(key, arr);
    }
    return map;
  }, [events, week, zone]);

  const cells = useMemo(
    () =>
      week.map((dt) =>
        layoutDayCell(
          jsDayOfWeek(dt),
          positioned,
          singleDayMap.get(dt.toFormat('yyyy-MM-dd')) ?? [],
        ),
      ),
    [week, positioned, singleDayMap],
  );

  // Segment of the drag ghost that falls inside this week (grid-aligned, spans
  // the event's real length and wraps across weeks).
  const previewSeg = useMemo(() => {
    if (!dragLanding) return null;
    const weekEnd = weekStart.plus({ days: 6 });
    if (dragLanding.end < weekStart || dragLanding.start > weekEnd) return null;
    const segStart = dragLanding.start < weekStart ? weekStart : dragLanding.start;
    const segEnd = dragLanding.end > weekEnd ? weekEnd : dragLanding.end;
    return {
      startCol: jsDayOfWeek(segStart),
      span: Math.round(segEnd.diff(segStart, 'days').days) + 1,
    };
  }, [dragLanding, weekStart]);

  const bodyH = MAX_VISIBLE_TRACKS * metrics.slotH + metrics.overflowH;
  const overlayTop = metrics.padTop + metrics.dateRowH;
  const colLeft = (col: number) => `calc(${(col * 100) / 7}% + ${CHIP_INSET}px)`;
  const colWidth = (span: number) => `calc(${(span * 100) / 7}% - ${CHIP_INSET * 2}px)`;

  return (
    <div className="relative grid grid-cols-7" data-week={weekStart.toFormat('yyyy-MM-dd')}>
      {week.map((dt, dIdx) => {
        const today = isToday(dt, zone);
        const isoDate = dt.toFormat('yyyy-MM-dd');
        const dow = jsDayOfWeek(dt);
        const holiday = getHoliday(holidaysCountry, isoDate);
        const { reserved = [], singleSlots = [], overflow = 0 } = cells[dIdx] ?? {};
        const isSelected = dt.hasSame(selectedDate, 'day');
        const outsideMonth =
          pagedMonth !== null && (dt.month !== pagedMonth.month || dt.year !== pagedMonth.year);

        return (
          <div
            key={isoDate}
            data-day={isoDate}
            className={`group relative flex flex-col items-start overflow-hidden border-b border-r border-[var(--color-separator)] px-1 pb-1 ${
              isSelected ? 'day-selected' : ''
            }`}
            style={{ paddingTop: metrics.padTop, opacity: outsideMonth ? 0.4 : undefined }}
          >
            {/* Background target: takes the day's own click, and on the surfaces
                that have one the double-click that starts a new event. */}
            <button
              type="button"
              onClick={() => onDayClick(dt)}
              onDoubleClick={onDayDoubleClick ? () => onDayDoubleClick(dt) : undefined}
              className={`absolute inset-0 z-0 ${style.dayBackdrop}`}
              aria-label={`${isoDate}${holiday ? ` (${holiday.name})` : ''}`}
            />

            {/* Content passes pointer events through to the day button, except event chips. */}
            <div className="pointer-events-none relative z-10 flex w-full flex-col">
              <div
                className="flex w-full items-center justify-between pl-0.5"
                style={{ height: metrics.dateRowH }}
              >
                {today ? (
                  <span
                    className={`today-badge flex items-center justify-center rounded-full bg-[var(--color-accent)] font-medium tabular-nums text-white ${style.dayNumber}`}
                  >
                    {dt.day}
                  </span>
                ) : (
                  <span
                    className={`flex items-center justify-center font-medium tabular-nums ${style.dayNumber}`}
                    style={{ color: dateColor(dow, !!holiday) }}
                  >
                    {dt.day}
                  </span>
                )}
                {showHolidayName && holiday && (
                  <span
                    className="ml-1 truncate rounded-full bg-[var(--color-sunday-bg,rgba(244,67,54,0.12))] px-1.5 text-micro font-medium text-[var(--color-sunday)]"
                    title={holiday.name}
                  >
                    {holiday.name}
                  </span>
                )}
              </div>

              <div className="flex w-full flex-col" style={{ height: bodyH }}>
                {TRACK_SLOTS.map((slotKey, slot) => {
                  const filler = reserved.includes(slot)
                    ? undefined
                    : singleSlots.find((s) => s.track === slot);
                  if (!filler) {
                    return (
                      <div
                        key={`${isoDate}-${slotKey}`}
                        style={{ height: barH, marginBottom: TRACK_GAP }}
                      />
                    );
                  }
                  const evt = filler.evt;
                  const start = fromISOInZone(evt.startAt, zone);
                  return (
                    <button
                      key={evt.id}
                      type="button"
                      onPointerDown={(e) => onEventPointerDown(evt, e)}
                      onClick={() => onEventClick(evt.id)}
                      className={`pointer-events-auto mx-0.5 flex items-center text-left font-semibold tabular-nums ${style.chip} ${style.chipHover} ${
                        canMoveEvent(evt) ? style.cursorMovable : style.cursorFixed
                      }`}
                      style={{
                        height: barH,
                        lineHeight: `${barH}px`,
                        marginBottom: TRACK_GAP,
                        backgroundColor: `${evt.color}1f`,
                        color: evt.color,
                        opacity: draggingEventId === evt.id ? 0.4 : undefined,
                      }}
                    >
                      <span
                        aria-hidden
                        className={`shrink-0 rounded-full ${style.dot}`}
                        style={{ backgroundColor: evt.color }}
                      />
                      <span className="truncate">
                        {evt.allDay ? '' : `${start.toFormat('H:mm')} `}
                        {evt.title}
                      </span>
                    </button>
                  );
                })}

                {/* The cell draws three tracks, so the rest of the day is reached
                    through the day detail. Where the badge is a button it has to
                    opt back into pointer events: its container hands them to the
                    day button behind it. */}
                <div
                  className="flex w-full items-center justify-center"
                  style={{ height: metrics.overflowH }}
                >
                  {overflow > 0 &&
                    (onOverflowClick ? (
                      <button
                        type="button"
                        onClick={() => onOverflowClick(dt)}
                        aria-label={t('calendar.moreEvents', { count: overflow })}
                        className={`pointer-events-auto font-medium text-[var(--color-accent)] hover:underline ${style.overflow}`}
                      >
                        +{overflow}
                      </button>
                    ) : (
                      <span className={`font-medium text-[var(--color-accent)] ${style.overflow}`}>
                        +{overflow}
                      </span>
                    ))}
                </div>
              </div>
            </div>
          </div>
        );
      })}

      {/* Multi-day bar overlay */}
      <div
        className="pointer-events-none absolute inset-x-0 grid grid-cols-7"
        style={{ top: overlayTop }}
      >
        {positioned.map((p) => {
          if (p.track >= MAX_VISIBLE_TRACKS) return null;
          const start = fromISOInZone(p.event.startAt, zone);
          return (
            <button
              key={`${p.event.id}-${p.startCol}`}
              type="button"
              onPointerDown={(e) => onEventPointerDown(p.event, e)}
              onClick={() => onEventClick(p.event.id)}
              className={`event-bar pointer-events-auto absolute flex items-center truncate font-semibold tabular-nums text-white ${style.bar} ${style.chipHover} ${
                canMoveEvent(p.event) ? style.cursorMovable : style.cursorFixed
              }`}
              style={{
                left: colLeft(p.startCol),
                width: colWidth(p.span),
                top: p.track * metrics.slotH,
                height: barH,
                lineHeight: `${barH}px`,
                backgroundColor: p.event.color,
                borderRadius: barRadius(p.continuesLeft, p.continuesRight),
                opacity: draggingEventId === p.event.id ? 0.4 : undefined,
              }}
              title={p.event.title}
            >
              {p.continuesLeft && (
                <span aria-hidden className="opacity-80">
                  ‹
                </span>
              )}
              <span className="truncate">
                {!p.event.allDay && !p.continuesLeft && (
                  <span className="mr-1 opacity-90">{start.toFormat('H:mm')}</span>
                )}
                {p.event.title}
              </span>
              {p.continuesRight && (
                <span aria-hidden className="ml-auto opacity-80">
                  ›
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Grid-aligned drag ghost: real-width preview at the landing spot. */}
      {previewSeg && dragLanding && (
        <div
          className={`pointer-events-none absolute z-20 flex items-center truncate font-semibold text-white shadow-lg ${style.bar}`}
          style={{
            top: overlayTop,
            left: colLeft(previewSeg.startCol),
            width: colWidth(previewSeg.span),
            height: barH,
            lineHeight: `${barH}px`,
            backgroundColor: dragLanding.event.color,
            borderRadius: '5px',
            opacity: 0.85,
          }}
        >
          {dragLanding.event.title}
        </div>
      )}
    </div>
  );
}

/**
 * Memoised: the mobile scroller keeps months of weeks mounted at once, and a
 * drag samples the pointer every frame. Rows only re-render when their own
 * events, day state or landing preview change.
 */
export const MonthWeekRow = memo(MonthWeekRowImpl);
