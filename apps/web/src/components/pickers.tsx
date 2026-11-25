import { DateTime } from 'luxon';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Avatar } from '@/components/avatar';
import { useT } from '@/i18n';
import { formatMonthYear, formatYear, getWeekdayLabel, isToday } from '@/lib/date-utils';
import { getHoliday } from '@/lib/holidays';
import { isSubmitEnter } from '@/lib/keys';
import { useHolidayLoader } from '@/lib/use-holidays';
import { useUiStore } from '@/stores/ui-store';

/* ============================================
   useFloating — positions a floating element
   relative to an anchor, rendered via portal.
   Clamps to viewport edges and supports
   above/below flip when space is tight.
   ============================================ */

function useFloating(open: boolean, floatingWidth = 280) {
  const anchorRef = useRef<HTMLButtonElement>(null);
  const floatingRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ top: 0, left: 0, width: 0 });

  // Recompute position when open changes or after floating renders
  const reposition = useCallback(() => {
    if (!open || !anchorRef.current) return;
    const rect = anchorRef.current.getBoundingClientRect();
    const floatingHeight = floatingRef.current?.offsetHeight ?? 340;
    const spaceBelow = window.innerHeight - rect.bottom;
    const goAbove = spaceBelow < floatingHeight + 8 && rect.top > spaceBelow;
    // Clamp using the floating element's real width once mounted; a long-label
    // dropdown can be far wider than floatingWidth and would clip off-screen.
    const fw = floatingRef.current?.offsetWidth ?? floatingWidth;

    setPos({
      top: goAbove ? Math.max(8, rect.top - floatingHeight - 4) : rect.bottom + 4,
      left: Math.max(8, Math.min(rect.left, window.innerWidth - fw - 8)),
      width: rect.width,
    });
  }, [open, floatingWidth]);

  useLayoutEffect(reposition, [reposition]);

  // Re-measure after floating element mounts (first render has no ref)
  useEffect(() => {
    if (open && floatingRef.current) reposition();
  }, [open, reposition]);

  return { anchorRef, floatingRef, pos };
}

/* ============================================
   Shared: outside-click + Escape handler
   ============================================ */

function useOutsideClose(
  refs: React.RefObject<HTMLElement | null>[],
  onClose: () => void,
  active: boolean,
) {
  // Read both through a ref, as useModalA11y does with its own onClose. Every
  // caller builds the array inline and some pass an inline closure, so naming
  // them here would re-run the effect on every render the dropdown survives,
  // taking the document listeners off and putting them back each time.
  const latest = useRef({ refs, onClose });
  useEffect(() => {
    latest.current = { refs, onClose };
  });

  useEffect(() => {
    if (!active) return;
    const handlePointer = (e: MouseEvent | TouchEvent) => {
      const target = e.target as Node;
      const { refs: current, onClose: close } = latest.current;
      if (current.every((r) => r.current && !r.current.contains(target))) close();
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') latest.current.onClose();
    };
    document.addEventListener('mousedown', handlePointer);
    document.addEventListener('touchstart', handlePointer);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handlePointer);
      document.removeEventListener('touchstart', handlePointer);
      document.removeEventListener('keydown', handleKey);
    };
  }, [active]);
}

const POPUP_FOCUSABLE = 'button:not([disabled]):not([tabindex="-1"]), input:not([disabled])';

/**
 * Keys every floating panel owes the keyboard: Escape closes it without also
 * closing the dialog behind, and Tab stays inside it. The panel is portalled
 * out of the dialog, so the dialog's own trap cannot see it.
 */
function handlePopupKeys(e: React.KeyboardEvent<HTMLElement>, close: () => void) {
  if (e.key === 'Escape') {
    e.preventDefault();
    e.stopPropagation();
    close();
    return;
  }
  if (e.key !== 'Tab') return;
  e.stopPropagation();
  const stops = Array.from(e.currentTarget.querySelectorAll<HTMLElement>(POPUP_FOCUSABLE));
  const first = stops[0];
  const last = stops[stops.length - 1];
  if (!first || !last) {
    e.preventDefault();
    return;
  }
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}

/** Six weeks of seven days: every month fits, so the panel never changes height. */
const PICKER_GRID_CELLS = 42;

/** Below `sm`, pickers open as a bottom sheet with thumb-sized targets instead of a popover. */
function useSheetPicker(): boolean {
  const query = '(max-width: 639px)';
  const [sheet, setSheet] = useState(() => window.matchMedia?.(query).matches === true);
  useEffect(() => {
    const mql = window.matchMedia?.(query);
    if (!mql) return;
    const update = () => setSheet(mql.matches);
    mql.addEventListener('change', update);
    return () => mql.removeEventListener('change', update);
  }, []);
  return sheet;
}

/**
 * The surface every picker panel sits on: a popover anchored to its field, or
 * on a phone a bottom sheet over a dimmed backdrop. A tap on the backdrop is
 * outside `floatingRef`, so the panel's outside-click handler closes it.
 */
function PickerSurface({
  sheet,
  title,
  floatingRef,
  style,
  popoverClassName,
  onKeyDown,
  children,
}: {
  sheet: boolean;
  title?: string | undefined;
  floatingRef: React.RefObject<HTMLDivElement | null>;
  style: React.CSSProperties;
  popoverClassName: string;
  onKeyDown: (e: React.KeyboardEvent<HTMLDivElement>) => void;
  children: React.ReactNode;
}) {
  if (sheet) {
    return createPortal(
      <>
        <div
          aria-hidden
          className="modal-backdrop fixed inset-0 z-[9998] bg-[var(--color-overlay)]"
        />
        {/* biome-ignore lint/a11y/noStaticElementInteractions: the sheet only receives key events bubbling from its controls */}
        <div
          ref={floatingRef}
          onKeyDown={onKeyDown}
          className="glass-surface-heavy bottom-sheet fixed inset-x-0 bottom-0 z-[9999] flex max-h-[85dvh] flex-col rounded-t-[var(--radius-xl)] px-4 pt-2 pb-[max(env(safe-area-inset-bottom),16px)] ring-1 ring-[var(--color-border)]"
          style={{ boxShadow: 'var(--shadow-elevated)' }}
        >
          <div
            aria-hidden
            className="mx-auto mb-2 h-1 w-9 shrink-0 rounded-full bg-[var(--color-border-strong)]"
          />
          {title && (
            <p className="mb-1 px-1 text-footnote font-medium text-[var(--color-text-secondary)]">
              {title}
            </p>
          )}
          {children}
        </div>
      </>,
      document.body,
    );
  }
  return createPortal(
    // biome-ignore lint/a11y/noStaticElementInteractions: the panel only receives key events bubbling from its controls
    <div
      ref={floatingRef}
      onKeyDown={onKeyDown}
      className={`dropdown-panel fixed z-[9999] bg-[var(--color-surface-elevated)] ring-1 ring-[var(--color-border)] ${popoverClassName}`}
      style={{ ...style, boxShadow: 'var(--shadow-elevated)', backdropFilter: 'blur(20px)' }}
    >
      {children}
    </div>,
    document.body,
  );
}

function Chevron({ dir }: { dir: 'left' | 'right' | 'down' }) {
  const d = dir === 'left' ? 'M15 18l-6-6 6-6' : dir === 'right' ? 'M9 18l6-6-6-6' : 'M6 9l6 6 6-6';
  return (
    <svg
      width={dir === 'down' ? 14 : 16}
      height={dir === 'down' ? 14 : 16}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={d} />
    </svg>
  );
}

/**
 * The other end of a date range the field belongs to. The panel draws the
 * span between it and the day under the pointer, so picking one end shows the
 * period it produces before it is committed.
 */
export interface DateRangeLink {
  role: 'start' | 'end';
  other: DateTime;
}

interface DatePickerDropdownProps {
  value: DateTime;
  onChange: (date: DateTime) => void;
  onClose: () => void;
  minDate?: DateTime;
  range?: DateRangeLink;
  /** Heads the bottom sheet on a phone, where the field itself is covered. */
  title?: string;
  style: React.CSSProperties;
  floatingRef: React.RefObject<HTMLDivElement | null>;
  /** The field that opened the picker; takes focus back when the picker closes. */
  returnFocusRef: React.RefObject<HTMLElement | null>;
}

function DatePickerDropdown({
  value,
  onChange,
  onClose,
  minDate,
  range,
  title,
  style,
  floatingRef,
  returnFocusRef,
}: DatePickerDropdownProps) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const sheet = useSheetPicker();
  const [viewMonth, setViewMonth] = useState(value.startOf('month'));
  const [mode, setMode] = useState<'days' | 'months'>('days');
  // Which way the last month turn went, so the new grid slides in from that side.
  const [turn, setTurn] = useState(0);
  // The day the arrow keys are on; only that day is a Tab stop.
  const [focusDay, setFocusDay] = useState(value);
  const [hoverDay, setHoverDay] = useState<DateTime | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);

  useOutsideClose([floatingRef], onClose, true);
  useHolidayLoader(holidaysCountry, [viewMonth.year]);

  const closeToField = () => {
    onClose();
    returnFocusRef.current?.focus();
  };

  // Opening, and every arrow move, puts focus on the day it names.
  useEffect(() => {
    gridRef.current
      ?.querySelector<HTMLElement>(`[data-day="${focusDay.toISODate()}"]`)
      ?.focus({ preventScroll: true });
  }, [focusDay]);

  const minDay = minDate?.startOf('day');
  const minMonth = minDate?.setZone(value.zone).startOf('month');
  const todayDate = DateTime.now().setZone(value.zone).startOf('day');
  const isBeforeMin = (day: DateTime) => (minDay ? day < minDay : false);

  const showMonth = (month: DateTime) => {
    setTurn(month > viewMonth ? 1 : month < viewMonth ? -1 : 0);
    setViewMonth(month.startOf('month'));
  };

  const moveFocus = (e: React.KeyboardEvent, next: DateTime) => {
    e.preventDefault();
    if (isBeforeMin(next)) return;
    setFocusDay(next);
    if (!next.hasSame(viewMonth, 'month')) showMonth(next);
  };

  const handleGridKey = (e: React.KeyboardEvent) => {
    const next = (() => {
      switch (e.key) {
        case 'ArrowLeft':
          return focusDay.minus({ days: 1 });
        case 'ArrowRight':
          return focusDay.plus({ days: 1 });
        case 'ArrowUp':
          return focusDay.minus({ days: 7 });
        case 'ArrowDown':
          return focusDay.plus({ days: 7 });
        case 'PageUp':
          return focusDay.minus({ months: 1 });
        case 'PageDown':
          return focusDay.plus({ months: 1 });
        default:
          return null;
      }
    })();
    if (next) moveFocus(e, next);
  };

  const weekdays = [0, 1, 2, 3, 4, 5, 6].map((i) => {
    const label = getWeekdayLabel(i, locale);
    return locale === 'ja' ? label : label.slice(0, 2);
  });

  const { emptySlots, calendarDays, trailingSlots } = useMemo(() => {
    const first = viewMonth.startOf('month');
    const startOffset = first.weekday % 7;
    const daysInMonth = first.daysInMonth ?? 30;
    const days: DateTime[] = [];
    for (let d = 1; d <= daysInMonth; d++) days.push(first.set({ day: d }));
    // Always six rows, so the panel keeps its height from month to month.
    return {
      emptySlots: startOffset,
      calendarDays: days,
      trailingSlots: PICKER_GRID_CELLS - startOffset - daysInMonth,
    };
  }, [viewMonth]);

  const handleSelect = (day: DateTime) => {
    if (isBeforeMin(day)) return;
    onChange(day);
    closeToField();
  };

  const stepView = (step: 1 | -1) =>
    mode === 'days'
      ? showMonth(viewMonth.plus({ months: step }))
      : setViewMonth((m) => m.plus({ years: step }));
  const atFirstPage = minMonth
    ? mode === 'days'
      ? viewMonth <= minMonth
      : viewMonth.year <= minMonth.year
    : false;

  // The span the field would produce: its own end follows the pointer.
  const span = (() => {
    if (!range) return null;
    const own = (hoverDay ?? value).startOf('day');
    const other = range.other.startOf('day');
    const [start, end] = range.role === 'start' ? [own, other] : [other, own];
    return start <= end ? { start, end } : null;
  })();

  const shortcuts = [
    { label: t('calendar.today'), day: todayDate },
    { label: t('picker.tomorrow'), day: todayDate.plus({ days: 1 }) },
    { label: t('picker.inAWeek'), day: todayDate.plus({ weeks: 1 }) },
  ];

  const cellHeight = sheet ? 'h-11' : 'h-10';
  const dayButton = sheet ? 'h-11 w-11 text-callout' : 'h-9 w-9 text-body';
  const tabDay = focusDay.hasSame(viewMonth, 'month') ? focusDay : viewMonth.startOf('month');

  return (
    <PickerSurface
      sheet={sheet}
      title={title}
      floatingRef={floatingRef}
      style={style}
      popoverClassName="w-[296px] p-3"
      onKeyDown={(e) => handlePopupKeys(e, closeToField)}
    >
      <div className="mb-1 flex items-center justify-between">
        <button
          type="button"
          onClick={() => setMode((m) => (m === 'days' ? 'months' : 'days'))}
          aria-expanded={mode === 'months'}
          className="-ml-1 inline-flex h-8 items-center gap-1 rounded-[var(--radius-control)] px-2 text-default font-semibold tabular-nums text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-hover)]"
        >
          {mode === 'days' ? formatMonthYear(viewMonth, locale) : formatYear(viewMonth, locale)}
          <span
            className={`text-[var(--color-text-tertiary)] transition-transform ${mode === 'months' ? 'rotate-180' : ''}`}
          >
            <Chevron dir="down" />
          </span>
        </button>
        <div className="flex items-center">
          <button
            type="button"
            onClick={() => stepView(-1)}
            disabled={atFirstPage}
            className="icon-btn h-8 w-8 text-[var(--color-text-primary)] disabled:opacity-30"
            aria-label={mode === 'days' ? t('calendar.prevMonth') : t('picker.prevYear')}
          >
            <Chevron dir="left" />
          </button>
          <button
            type="button"
            onClick={() => stepView(1)}
            className="icon-btn h-8 w-8 text-[var(--color-text-primary)]"
            aria-label={mode === 'days' ? t('calendar.nextMonth') : t('picker.nextYear')}
          >
            <Chevron dir="right" />
          </button>
        </div>
      </div>

      {mode === 'months' ? (
        <div
          className={`grid grid-cols-3 grid-rows-4 gap-1.5 ${sheet ? 'h-[292px]' : 'h-[268px]'}`}
        >
          {Array.from({ length: 12 }, (_, i) => {
            const month = viewMonth.set({ month: i + 1 }).startOf('month');
            const disabled = minMonth ? month < minMonth : false;
            const selected = month.hasSame(value, 'month');
            const current = month.hasSame(todayDate, 'month');
            return (
              <button
                key={month.month}
                type="button"
                disabled={disabled}
                aria-pressed={selected}
                onClick={() => {
                  showMonth(month);
                  setMode('days');
                }}
                className={`rounded-[var(--radius-control)] text-body font-medium transition-colors disabled:opacity-30 ${
                  selected
                    ? 'bg-[var(--color-accent)] text-[var(--color-text-on-accent)]'
                    : current
                      ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)] enabled:hover:bg-[var(--color-accent-subtle)]'
                      : 'text-[var(--color-text-primary)] enabled:hover:bg-[var(--color-hover)]'
                }`}
              >
                {locale === 'en' ? month.setLocale('en').toFormat('MMM') : `${month.month}月`}
              </button>
            );
          })}
        </div>
      ) : (
        <>
          <div className="grid grid-cols-7">
            {weekdays.map((wd, i) => (
              <div
                key={wd}
                className="py-1 text-center text-caption font-medium"
                style={{
                  color:
                    i === 0
                      ? 'var(--color-sunday)'
                      : i === 6
                        ? 'var(--color-saturday)'
                        : 'var(--color-text-tertiary)',
                }}
              >
                {wd}
              </div>
            ))}
          </div>
          {/* biome-ignore lint/a11y/noStaticElementInteractions: the grid only receives key events bubbling from its buttons */}
          <div
            key={viewMonth.toISODate()}
            ref={gridRef}
            onKeyDown={handleGridKey}
            onMouseLeave={() => setHoverDay(null)}
            data-dir={turn}
            className="picker-turn grid grid-cols-7"
          >
            {Array.from({ length: emptySlots }, (_, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: placeholder cells have no identity
              <div key={`lead-${i}`} className={cellHeight} />
            ))}
            {calendarDays.map((day) => {
              const iso = day.toISODate() ?? '';
              const isSelected = day.hasSame(value, 'day');
              // The grid is built from `value`, so "today" has to be read in the
              // same zone that value carries rather than the browser's.
              const today = isToday(day, value.zoneName ?? undefined);
              const isDisabled = isBeforeMin(day);
              const holiday = getHoliday(holidaysCountry, iso);
              const dayOfWeek = day.weekday % 7;
              const inSpan = span && day >= span.start && day <= span.end;
              const spanStart = inSpan && day.hasSame(span.start, 'day');
              const spanEnd = inSpan && day.hasSame(span.end, 'day');
              const isOtherEnd = range ? day.hasSame(range.other, 'day') : false;
              const openLeft = !spanStart && (dayOfWeek === 0 || day.day === 1);
              const openRight = !spanEnd && (dayOfWeek === 6 || day.day === calendarDays.length);
              return (
                <div
                  key={iso}
                  className={`relative flex items-center justify-center ${cellHeight}`}
                >
                  {inSpan && !(spanStart && spanEnd) && (
                    <span
                      aria-hidden
                      className="absolute inset-y-1 bg-[var(--color-accent-bg)]"
                      style={{
                        left: spanStart ? '50%' : 0,
                        right: spanEnd ? '50%' : 0,
                        // The band breaks at a row or month edge; round it there.
                        borderTopLeftRadius: openLeft ? 999 : 0,
                        borderBottomLeftRadius: openLeft ? 999 : 0,
                        borderTopRightRadius: openRight ? 999 : 0,
                        borderBottomRightRadius: openRight ? 999 : 0,
                      }}
                    />
                  )}
                  <button
                    type="button"
                    onClick={() => handleSelect(day)}
                    onMouseEnter={() => !isDisabled && setHoverDay(day)}
                    disabled={isDisabled}
                    data-day={iso}
                    tabIndex={day.hasSame(tabDay, 'day') ? 0 : -1}
                    aria-label={
                      holiday
                        ? `${day.toFormat('yyyy-MM-dd')} ${holiday.name}`
                        : day.toFormat('yyyy-MM-dd')
                    }
                    aria-pressed={isSelected}
                    aria-current={today ? 'date' : undefined}
                    title={holiday?.name}
                    className={`relative flex items-center justify-center rounded-full tabular-nums transition-colors disabled:cursor-default ${dayButton} ${
                      isSelected
                        ? 'bg-[var(--color-accent)] shadow-[0_2px_8px_-2px_color-mix(in_srgb,var(--color-accent)_60%,transparent)]'
                        : isOtherEnd
                          ? 'ring-[1.5px] ring-[var(--color-accent)] ring-inset enabled:hover:bg-[var(--color-hover)]'
                          : 'enabled:hover:bg-[var(--color-hover)]'
                    }`}
                    style={{
                      color: isSelected
                        ? 'var(--color-text-on-accent)'
                        : dayOfWeek === 0 || holiday
                          ? 'var(--color-sunday)'
                          : dayOfWeek === 6
                            ? 'var(--color-saturday)'
                            : 'var(--color-text-primary)',
                      fontWeight: isSelected || today ? 600 : 400,
                      opacity: isDisabled ? 0.32 : 1,
                    }}
                  >
                    {day.day}
                    {today && (
                      <span
                        aria-hidden
                        className="absolute bottom-1 left-1/2 h-1 w-1 -translate-x-1/2 rounded-full"
                        style={{
                          backgroundColor: isSelected
                            ? 'var(--color-text-on-accent)'
                            : 'var(--color-accent)',
                        }}
                      />
                    )}
                  </button>
                </div>
              );
            })}
            {Array.from({ length: trailingSlots }, (_, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: placeholder cells have no identity
              <div key={`pad-${i}`} className={cellHeight} />
            ))}
          </div>
        </>
      )}

      <div className="mt-2 flex items-center gap-1.5 border-t border-[var(--color-separator)] pt-2.5">
        {shortcuts.map(({ label, day }) => (
          <button
            key={label}
            type="button"
            onClick={() => handleSelect(day)}
            disabled={isBeforeMin(day)}
            className={`rounded-full bg-[var(--color-surface-inset)] px-3 text-footnote font-medium text-[var(--color-text-secondary)] transition-colors enabled:hover:bg-[var(--color-accent-bg)] enabled:hover:text-[var(--color-accent-ink)] disabled:opacity-40 ${sheet ? 'h-9' : 'h-7'}`}
          >
            {label}
          </button>
        ))}
      </div>
    </PickerSurface>
  );
}

/* ============================================
   TimePicker — scrollable time slot dropdown
   ============================================ */

interface TimePickerDropdownProps {
  value: string;
  onChange: (time: string) => void;
  onClose: () => void;
  /** Heads the bottom sheet on a phone, where the field itself is covered. */
  title?: string;
  style: React.CSSProperties;
  floatingRef: React.RefObject<HTMLDivElement | null>;
  /** The field that opened the picker; takes focus back when the picker closes. */
  returnFocusRef: React.RefObject<HTMLElement | null>;
}

const TIME_SLOTS = (() => {
  const slots: string[] = [];
  for (let h = 0; h < 24; h++) {
    for (let m = 0; m < 60; m += 15) {
      slots.push(`${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`);
    }
  }
  return slots;
})();

/** The slot a time rounds to, as `HH:mm`. */
function nearestSlotOf(time: string): string {
  const [h, m] = time.split(':').map(Number);
  const totalMin = (h ?? 0) * 60 + (m ?? 0);
  const rounded = Math.min(Math.round(totalMin / 15) * 15, 23 * 60 + 45);
  const rh = Math.floor(rounded / 60);
  const rm = rounded % 60;
  return `${String(rh).padStart(2, '0')}:${String(rm).padStart(2, '0')}`;
}

/**
 * Reads a typed time as `HH:mm`, or null when it is not one.
 *
 * Accepts `H`, `HH`, `H:mm`, `HH:mm`, `Hmm` and `HHmm` with hour 0-23 and
 * minute 00-59; full-width digits and colons are folded first.
 */
export function parseTypedTime(input: string): string | null {
  const text = input.normalize('NFKC').trim();
  const match =
    text.match(/^(\d{1,2}):(\d{2})$/) ??
    text.match(/^(\d{1,2})()$/) ??
    text.match(/^(\d{1,2})(\d{2})$/);
  if (!match) return null;
  const hour = Number(match[1]);
  const minute = match[2] ? Number(match[2]) : 0;
  if (hour > 23 || minute > 59) return null;
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
}

function TimePickerDropdown({
  value,
  onChange,
  onClose,
  title,
  style,
  floatingRef,
  returnFocusRef,
}: TimePickerDropdownProps) {
  const t = useT();
  const selectedRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const [typed, setTyped] = useState('');
  const [typedInvalid, setTypedInvalid] = useState(false);

  useOutsideClose([floatingRef], onClose, true);

  const closeToField = () => {
    onClose();
    returnFocusRef.current?.focus();
  };

  // Focus starts on the slot, not the text box, so a tap does not raise the
  // on-screen keyboard.
  useEffect(() => {
    selectedRef.current?.focus({ preventScroll: true });
    selectedRef.current?.scrollIntoView({ block: 'center' });
  }, []);

  const handleTypedChange = (next: string) => {
    setTyped(next);
    setTypedInvalid(false);
    const time = parseTypedTime(next);
    if (!time) return;
    listRef.current
      ?.querySelector<HTMLElement>(`[data-slot="${nearestSlotOf(time)}"]`)
      ?.scrollIntoView({ block: 'nearest' });
  };

  const applyTyped = () => {
    const time = parseTypedTime(typed);
    if (!time) {
      setTypedInvalid(true);
      return;
    }
    onChange(time);
    closeToField();
  };

  const handleTypedKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!isSubmitEnter(e)) return;
    e.preventDefault();
    applyTyped();
  };

  // Slots are one Tab stop; the arrows walk the list.
  const handleListKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.target instanceof HTMLInputElement) return;
    const slots = Array.from(e.currentTarget.querySelectorAll<HTMLElement>('[data-slot]'));
    const at = slots.indexOf(document.activeElement as HTMLElement);
    const target =
      e.key === 'ArrowDown'
        ? slots[Math.min(at + 1, slots.length - 1)]
        : e.key === 'ArrowUp'
          ? slots[Math.max(at - 1, 0)]
          : e.key === 'Home'
            ? slots[0]
            : e.key === 'End'
              ? slots[slots.length - 1]
              : undefined;
    if (!target) return;
    e.preventDefault();
    target.focus();
  };

  const nearestSlot = useMemo(() => nearestSlotOf(value), [value]);
  const sheet = useSheetPicker();

  return (
    <PickerSurface
      sheet={sheet}
      title={title}
      floatingRef={floatingRef}
      style={style}
      popoverClassName="flex max-h-[300px] w-[120px] flex-col"
      onKeyDown={(e) => {
        handlePopupKeys(e, closeToField);
        handleListKey(e);
      }}
    >
      <div
        className={`flex items-center gap-1 ${sheet ? 'pb-2' : 'border-b border-[var(--color-separator)] p-1.5'}`}
      >
        <input
          data-1p-ignore
          type="text"
          inputMode="numeric"
          value={typed}
          onChange={(e) => handleTypedChange(e.target.value)}
          onKeyDown={handleTypedKey}
          aria-label={t('picker.typeTime')}
          aria-invalid={typedInvalid}
          placeholder="9:30"
          className={`input-modern h-9 min-w-0 flex-1 text-center text-default tabular-nums ${
            typedInvalid ? 'ring-1 ring-[var(--color-danger)]' : ''
          }`}
        />
        {/* A numeric keypad has no return key, so the typed time needs a button. */}
        <button
          type="button"
          aria-label={t('picker.applyTime')}
          onClick={applyTyped}
          disabled={!typed.trim()}
          className="icon-btn icon-btn-accent h-9 w-9 shrink-0 disabled:opacity-40"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
            aria-hidden="true"
          >
            <path d="M20 6L9 17l-5-5" />
          </svg>
        </button>
      </div>
      <div
        ref={listRef}
        className={`overflow-y-auto ${sheet ? '-mx-1 grid max-h-[46dvh] grid-cols-4 gap-1.5 p-1' : 'flex-1 p-1'}`}
      >
        {TIME_SLOTS.map((slot) => {
          const isSelected = slot === nearestSlot;
          // On the hour reads as a heading; the quarters under it recede.
          const onHour = slot.endsWith(':00');
          return (
            <button
              key={slot}
              ref={isSelected ? selectedRef : undefined}
              type="button"
              data-slot={slot}
              tabIndex={isSelected ? 0 : -1}
              aria-pressed={isSelected}
              onClick={() => {
                onChange(slot);
                closeToField();
              }}
              className={`flex w-full items-center justify-center rounded-[var(--radius-control)] tabular-nums transition-colors ${
                sheet ? 'h-11 text-callout' : 'h-8 text-default'
              } ${
                isSelected
                  ? 'bg-[var(--color-accent)] font-semibold text-[var(--color-text-on-accent)]'
                  : onHour
                    ? 'font-semibold text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]'
                    : 'text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)] hover:text-[var(--color-text-primary)]'
              } ${sheet && !isSelected ? 'bg-[var(--color-surface-inset)]' : ''}`}
            >
              {slot}
            </button>
          );
        })}
      </div>
    </PickerSurface>
  );
}

/* ============================================
   CustomSelect — styled dropdown replacement
   ============================================ */

interface SelectOption {
  value: string;
  label: string;
  /**
   * Swatch shown before the label, in the trigger as well as the list.
   *
   * A calendar is identified by its colour everywhere else it appears, and a
   * picker that drops it makes the one-calendar case — which shows the dot —
   * and the many-calendar case look like different things.
   */
  color?: string;
  /**
   * Marks the option as a person: their picture, or their initial on
   * `color`, takes the swatch's place in the trigger and the list.
   */
  person?: { name: string; avatarUrl?: string | undefined };
}

interface CustomSelectProps {
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  /** Class applied to the wrapper element (e.g. width constraints). */
  className?: string;
  /** Overrides the default filled trigger look (e.g. "input-modern" or a pill). */
  triggerClassName?: string;
  /**
   * Names the control for assistive tech. The visible caption sits outside
   * this component, so without it the only name is the current value.
   */
  ariaLabel: string;
  /** Shows the current value without letting the list open. */
  disabled?: boolean;
  /**
   * Adds a filter box to the dropdown.
   *
   * A list long enough that scrolling it is a chore — the two hundred
   * countries holiday data covers — is only usable if it can be narrowed.
   */
  searchable?: boolean;
  /** Shown in the trigger while no option matches `value`. */
  placeholder?: string;
}

/** Dot marking which calendar an option belongs to. */
function Swatch({ color }: { color: string }) {
  return (
    <span
      className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
      style={{ backgroundColor: color }}
    />
  );
}

/** What leads an option: a person's picture, else the swatch of its colour. */
function OptionMark({ option }: { option: SelectOption }) {
  if (option.person) {
    return (
      <Avatar
        src={option.person.avatarUrl}
        name={option.person.name}
        color={option.color}
        round
        size="h-5 w-5"
        className="text-micro"
      />
    );
  }
  return option.color ? <Swatch color={option.color} /> : null;
}

let selectSeq = 0;

export function CustomSelect({
  value,
  options,
  onChange,
  className,
  triggerClassName,
  ariaLabel,
  disabled = false,
  searchable,
  placeholder,
}: CustomSelectProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const { anchorRef, floatingRef, pos } = useFloating(open, 160);
  const activeRef = useRef<HTMLButtonElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const handleClose = useCallback(() => setOpen(false), []);
  // Ties the trigger to the list it opens.
  const idRef = useRef<string>('');
  if (!idRef.current) {
    selectSeq += 1;
    idRef.current = `select-${selectSeq}`;
  }
  const listID = `${idRef.current}-list`;

  useOutsideClose([floatingRef, anchorRef], handleClose, open);

  useEffect(() => {
    if (open) {
      // The list arrives through a portal, so it cannot be scrolled until it
      // has mounted.
      setTimeout(() => activeRef.current?.scrollIntoView({ block: 'center' }), 0);
      // Typing is the point of a filtered list; opening it should be enough.
      filterRef.current?.focus();
    } else {
      setQuery('');
    }
  }, [open]);

  const selected = options.find((o) => o.value === value);
  const needle = query.trim().toLowerCase();
  // The code is matched as well as the label: someone who knows "DE" should
  // not have to guess how the list spells Germany.
  const visible =
    searchable && needle
      ? options.filter(
          (o) => o.label.toLowerCase().includes(needle) || o.value.toLowerCase().includes(needle),
        )
      : options;

  // Which row the arrow keys are on. It follows the current value when the
  // list opens, and the filter resets it so a narrowed list starts at the top
  // rather than on a row that is no longer shown.
  const [activeIndex, setActiveIndex] = useState(0);
  useEffect(() => {
    if (!open) return;
    const at = options.findIndex((o) => o.value === value);
    setActiveIndex(at < 0 ? 0 : at);
  }, [open, options, value]);

  // Adjusted while rendering rather than in an effect: a narrowed list must
  // not paint for even one frame with the row highlighted at an index that
  // belonged to the wider one.
  const [narrowedBy, setNarrowedBy] = useState(needle);
  if (narrowedBy !== needle) {
    setNarrowedBy(needle);
    setActiveIndex(0);
  }

  const activeIndexInList = Math.min(activeIndex, Math.max(visible.length - 1, 0));
  const activeOption = visible[activeIndexInList];
  // Arrowing past the bottom of the scroll box has to bring the list along.
  // The row is found by index rather than held in a ref, so that this runs on
  // the move itself and not merely on whatever render followed it.
  useEffect(() => {
    if (!open) return;
    const rows = floatingRef.current?.querySelectorAll('[role="option"]');
    rows?.[activeIndexInList]?.scrollIntoView({ block: 'nearest' });
  }, [open, activeIndexInList, floatingRef]);

  // A control that replaces a native select has to answer the keys a native
  // select answers, or it is only a picture of one.
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      // Closed here rather than left to the document listener, because the
      // stop below would never let the event reach it: React dispatches from
      // its root, so stopping there stops the native event too. Escape with a
      // list open means close the list, not close the dialog behind it.
      if (open) {
        e.stopPropagation();
        setOpen(false);
      }
      return;
    }
    if (!open) {
      if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        setOpen(true);
      }
      return;
    }
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setActiveIndex(Math.min(activeIndexInList + 1, visible.length - 1));
        break;
      case 'ArrowUp':
        e.preventDefault();
        setActiveIndex(Math.max(activeIndexInList - 1, 0));
        break;
      case 'Home':
        e.preventDefault();
        setActiveIndex(0);
        break;
      case 'End':
        e.preventDefault();
        setActiveIndex(Math.max(visible.length - 1, 0));
        break;
      case 'Enter':
        e.preventDefault();
        if (activeOption) {
          onChange(activeOption.value);
          setOpen(false);
        }
        break;
      case 'Tab':
        setOpen(false);
        break;
      default:
        break;
    }
  };

  return (
    <div className={`relative ${className ?? ''}`}>
      {/* A button rather than role="combobox": that role takes its name only
          from the author, so a picker whose caption lives outside it would go
          nameless, while a button falls back to the choice it is showing. */}
      <button
        ref={anchorRef}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listID : undefined}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        onKeyDown={handleKeyDown}
        className={
          triggerClassName
            ? `flex w-full items-center justify-between gap-2 text-left transition-colors disabled:cursor-default disabled:opacity-60 ${triggerClassName}`
            : 'flex w-full items-center justify-between gap-2 bg-[var(--color-surface-inset)] py-1.5 pr-3 pl-3 text-default text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-hover)] disabled:cursor-default disabled:opacity-60 disabled:hover:bg-[var(--color-surface-inset)]'
        }
        style={triggerClassName ? undefined : { borderRadius: 'var(--radius-sm)' }}
      >
        <span className="flex min-w-0 items-center gap-2">
          {selected && <OptionMark option={selected} />}
          <span className="truncate">{selected?.label ?? placeholder ?? value}</span>
        </span>
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--color-text-tertiary)"
          strokeWidth="2"
          style={{
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.15s ease',
          }}
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>

      {open &&
        !disabled &&
        createPortal(
          <div
            ref={floatingRef}
            className={`dropdown-panel fixed z-[9999] bg-[var(--color-surface-elevated)] ring-1 ring-[var(--color-border)] ${
              searchable ? 'flex max-h-[280px] flex-col' : 'max-h-[240px] overflow-y-auto py-1'
            }`}
            style={{
              top: pos.top,
              left: pos.left,
              minWidth: Math.max(pos.width, 160),
              maxWidth: 'calc(100vw - 16px)',
              boxShadow: 'var(--shadow-elevated)',
              backdropFilter: 'blur(20px)',
            }}
          >
            {searchable && (
              <div className="border-b border-[var(--color-separator)] p-2">
                <input
                  data-1p-ignore
                  ref={filterRef}
                  type="text"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  aria-label={t('common.search')}
                  placeholder={t('common.search')}
                  className="input-modern w-full text-default"
                />
              </div>
            )}
            <div
              id={listID}
              role="listbox"
              aria-label={ariaLabel}
              className={searchable ? 'flex-1 overflow-y-auto py-1' : 'contents'}
            >
              {searchable && visible.length === 0 && (
                <p className="px-3 py-3 text-default text-[var(--color-text-tertiary)]">
                  {t('common.noMatches')}
                </p>
              )}
              {visible.map((opt, i) => {
                const isSelected = opt.value === value;
                const isActive = i === activeIndexInList;
                return (
                  <button
                    key={opt.value}
                    ref={isActive ? activeRef : undefined}
                    type="button"
                    role="option"
                    aria-selected={isSelected}
                    tabIndex={-1}
                    onClick={() => {
                      onChange(opt.value);
                      setOpen(false);
                    }}
                    onMouseEnter={() => setActiveIndex(i)}
                    className="flex w-full items-center gap-2 whitespace-nowrap px-3 py-2.5 text-left text-default transition-colors"
                    style={{
                      // The keyboard row and the chosen row are different
                      // things, so they cannot share one highlight: arrowing
                      // past the selection would otherwise look like the
                      // selection moving with it.
                      backgroundColor: isSelected
                        ? 'var(--color-accent-bg)'
                        : isActive
                          ? 'var(--color-hover)'
                          : 'transparent',
                      color: isSelected ? 'var(--color-accent-ink)' : 'var(--color-text-primary)',
                      fontWeight: isSelected ? 600 : 400,
                    }}
                  >
                    {/* Fixed lead so the labels line up whether or not a row
                        is the selected one. Its width plus the row gap is the
                        indent an unchecked row used to carry by hand. */}
                    <span className="flex w-[14px] shrink-0 items-center justify-center">
                      {isSelected && (
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="var(--color-accent)"
                          strokeWidth="2.5"
                        >
                          <path d="M20 6L9 17l-5-5" />
                        </svg>
                      )}
                    </span>
                    <OptionMark option={opt} />
                    <span>{opt.label}</span>
                  </button>
                );
              })}
            </div>
          </div>,
          document.body,
        )}
    </div>
  );
}

/* ============================================
   DateTimeField — tappable date/time display
   that opens DatePicker and TimePicker
   ============================================ */

/** Shared look for a form field that opens a picker panel, matching `input-modern input-compact`. */
const FORM_PICKER_TRIGGER =
  'input-modern input-compact flex w-full items-center justify-between gap-2 text-left tabular-nums text-[var(--color-text-primary)]';

function PickerFormLabel({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 text-footnote font-medium text-[var(--color-text-secondary)]">
      <span className="mb-1.5 block">{label}</span>
      {children}
    </div>
  );
}

function TriggerIcon({ path }: { path: string }) {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className="shrink-0 text-[var(--color-text-tertiary)]"
    >
      <path d={path} />
    </svg>
  );
}

const CALENDAR_ICON =
  'M7 3v3m10-3v3M4 9h16M5 5h14a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z';
const CLOCK_ICON = 'M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z';

/** A labelled form date field; the year is shown only when it is not the current one. */
export function DateField({
  label,
  value,
  onChange,
  minDate,
  range,
}: {
  label: string;
  value: DateTime;
  onChange: (date: DateTime) => void;
  minDate?: DateTime;
  range?: DateRangeLink;
}) {
  const locale = useUiStore((s) => s.locale);
  const [open, setOpen] = useState(false);
  const floating = useFloating(open, 280);
  const close = useCallback(() => setOpen(false), []);
  const dow = getWeekdayLabel(value.weekday % 7, locale);
  const sameYear = value.year === DateTime.now().setZone(value.zone).year;
  const text =
    locale === 'en'
      ? value.setLocale('en').toFormat(sameYear ? 'EEE, MMM d' : 'MMM d, yyyy')
      : `${sameYear ? '' : `${value.year}年`}${value.month}月${value.day}日(${dow})`;
  return (
    <PickerFormLabel label={label}>
      <button
        ref={floating.anchorRef}
        type="button"
        aria-label={`${label} ${value.toISODate()}`}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={FORM_PICKER_TRIGGER}
      >
        <span className="truncate">{text}</span>
        <TriggerIcon path={CALENDAR_ICON} />
      </button>
      {open && (
        <DatePickerDropdown
          value={value}
          onChange={onChange}
          onClose={close}
          {...(minDate ? { minDate } : {})}
          {...(range ? { range } : {})}
          title={label}
          style={{ top: floating.pos.top, left: floating.pos.left }}
          floatingRef={floating.floatingRef}
          returnFocusRef={floating.anchorRef}
        />
      )}
    </PickerFormLabel>
  );
}

/** A labelled form time field (`HH:mm`) that opens the 15-minute slot list. */
export function TimeField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (time: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const floating = useFloating(open, 100);
  const close = useCallback(() => setOpen(false), []);
  return (
    <PickerFormLabel label={label}>
      <button
        ref={floating.anchorRef}
        type="button"
        aria-label={`${label} ${value}`}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={FORM_PICKER_TRIGGER}
      >
        <span className="truncate">{value}</span>
        <TriggerIcon path={CLOCK_ICON} />
      </button>
      {open && (
        <TimePickerDropdown
          value={value}
          onChange={onChange}
          onClose={close}
          title={label}
          style={{
            top: floating.pos.top,
            left: floating.pos.left,
            width: Math.max(120, floating.pos.width),
          }}
          floatingRef={floating.floatingRef}
          returnFocusRef={floating.anchorRef}
        />
      )}
    </PickerFormLabel>
  );
}

interface DateTimeFieldProps {
  label: string;
  dateValue: DateTime;
  timeValue: string; // "HH:mm"
  showTime: boolean;
  onDateChange: (date: DateTime) => void;
  onTimeChange: (time: string) => void;
  minDate?: DateTime;
}

export function DateTimeField({
  label,
  dateValue,
  timeValue,
  showTime,
  onDateChange,
  onTimeChange,
  minDate,
}: DateTimeFieldProps) {
  const locale = useUiStore((s) => s.locale);
  const [showDatePicker, setShowDatePicker] = useState(false);
  const [showTimePicker, setShowTimePicker] = useState(false);
  const dateFloating = useFloating(showDatePicker, 280);
  const timeFloating = useFloating(showTimePicker, 100);

  const dateLabel = useMemo(() => {
    if (locale === 'en') return dateValue.setLocale('en').toFormat('EEE, MMM d, yyyy');
    const dow = getWeekdayLabel(dateValue.weekday % 7, locale);
    return `${dateValue.year}年${dateValue.month}月${dateValue.day}日(${dow})`;
  }, [dateValue, locale]);

  const handleCloseDatePicker = useCallback(() => setShowDatePicker(false), []);
  const handleCloseTimePicker = useCallback(() => setShowTimePicker(false), []);

  return (
    <div className="flex items-center justify-between py-2.5">
      {label && <span className="text-default text-[var(--color-text-primary)]">{label}</span>}
      <div className="flex items-center gap-2">
        <button
          ref={dateFloating.anchorRef}
          type="button"
          onClick={() => {
            setShowDatePicker((v) => !v);
            setShowTimePicker(false);
          }}
          className="pill-button bg-[var(--color-accent-bg)] px-3 py-1.5 text-default font-medium tabular-nums text-[var(--color-accent-ink)] hover:bg-[var(--color-accent-subtle)]"
        >
          {dateLabel}
        </button>
        {showDatePicker && (
          <DatePickerDropdown
            value={dateValue}
            onChange={onDateChange}
            onClose={handleCloseDatePicker}
            {...(minDate ? { minDate } : {})}
            {...(label ? { title: label } : {})}
            style={{ top: dateFloating.pos.top, left: dateFloating.pos.left }}
            floatingRef={dateFloating.floatingRef}
            returnFocusRef={dateFloating.anchorRef}
          />
        )}
        {showTime && (
          <>
            <button
              ref={timeFloating.anchorRef}
              type="button"
              onClick={() => {
                setShowTimePicker((v) => !v);
                setShowDatePicker(false);
              }}
              className="pill-button bg-[var(--color-accent-bg)] px-3 py-1.5 text-default font-medium tabular-nums text-[var(--color-accent-ink)] hover:bg-[var(--color-accent-subtle)]"
            >
              {timeValue}
            </button>
            {showTimePicker && (
              <TimePickerDropdown
                value={timeValue}
                onChange={onTimeChange}
                onClose={handleCloseTimePicker}
                {...(label ? { title: label } : {})}
                style={{ top: timeFloating.pos.top, left: timeFloating.pos.left }}
                floatingRef={timeFloating.floatingRef}
                returnFocusRef={timeFloating.anchorRef}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}
