import type { DateTime } from 'luxon';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '@/i18n';
import { isToday } from '@/lib/date-utils';
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

const POPUP_FOCUSABLE = 'button:not([disabled]):not([tabindex="-1"])';

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

/* ============================================
   DatePicker — inline calendar for date selection
   ============================================ */

const WEEKDAYS_JA = ['日', '月', '火', '水', '木', '金', '土'];
const WEEKDAYS_EN = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

interface DatePickerDropdownProps {
  value: DateTime;
  onChange: (date: DateTime) => void;
  onClose: () => void;
  minDate?: DateTime;
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
  style,
  floatingRef,
  returnFocusRef,
}: DatePickerDropdownProps) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const [viewMonth, setViewMonth] = useState(value.startOf('month'));
  // The day the arrow keys are on; only that day is a Tab stop.
  const [focusDay, setFocusDay] = useState(value);
  const gridRef = useRef<HTMLDivElement>(null);

  useOutsideClose([floatingRef], onClose, true);

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

  const moveFocus = (e: React.KeyboardEvent, next: DateTime) => {
    e.preventDefault();
    if (minDate && next < minDate.startOf('day')) return;
    setFocusDay(next);
    setViewMonth(next.startOf('month'));
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

  const weekdays = locale === 'ja' ? WEEKDAYS_JA : WEEKDAYS_EN;

  const { emptySlots, calendarDays } = useMemo(() => {
    const first = viewMonth.startOf('month');
    const startOffset = first.weekday % 7;
    const daysInMonth = first.daysInMonth ?? 30;
    const days: DateTime[] = [];
    for (let d = 1; d <= daysInMonth; d++) days.push(first.set({ day: d }));
    return { emptySlots: startOffset, calendarDays: days };
  }, [viewMonth]);

  const monthLabel =
    locale === 'ja' ? `${viewMonth.year}年${viewMonth.month}月` : viewMonth.toFormat('MMMM yyyy');

  const handleSelect = (day: DateTime) => {
    if (minDate && day < minDate.startOf('day')) return;
    onChange(day);
    closeToField();
  };

  const tabDay = focusDay.hasSame(viewMonth, 'month') ? focusDay : viewMonth.startOf('month');

  return createPortal(
    // biome-ignore lint/a11y/noStaticElementInteractions: the panel only receives key events bubbling from its buttons
    <div
      ref={floatingRef}
      onKeyDown={(e) => handlePopupKeys(e, closeToField)}
      className="dropdown-panel fixed z-[9999] w-[280px] bg-[var(--color-surface-elevated)] p-3 ring-1 ring-[var(--color-border)]"
      style={{ ...style, boxShadow: 'var(--shadow-elevated)', backdropFilter: 'blur(20px)' }}
    >
      {/* Month navigation */}
      <div className="mb-2 flex items-center justify-between">
        <button
          type="button"
          onClick={() => setViewMonth((m) => m.minus({ months: 1 }))}
          className="flex h-8 w-8 items-center justify-center hover:bg-[var(--color-hover)]"
          style={{ borderRadius: 'var(--radius-sm)' }}
          aria-label={t('calendar.prevMonth')}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-text-primary)"
            strokeWidth="2"
          >
            <path d="M15 18l-6-6 6-6" />
          </svg>
        </button>
        <span className="text-default font-semibold tabular-nums text-[var(--color-text-primary)]">
          {monthLabel}
        </span>
        <button
          type="button"
          onClick={() => setViewMonth((m) => m.plus({ months: 1 }))}
          className="flex h-8 w-8 items-center justify-center hover:bg-[var(--color-hover)]"
          style={{ borderRadius: 'var(--radius-sm)' }}
          aria-label={t('calendar.nextMonth')}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-text-primary)"
            strokeWidth="2"
          >
            <path d="M9 18l6-6-6-6" />
          </svg>
        </button>
      </div>

      {/* Weekday header */}
      <div className="mb-1 grid grid-cols-7 gap-0">
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

      {/* Date grid */}
      {/* biome-ignore lint/a11y/noStaticElementInteractions: the panel only receives key events bubbling from its buttons */}
      <div ref={gridRef} onKeyDown={handleGridKey} className="grid grid-cols-7 gap-0">
        {emptySlots > 0 && <div style={{ gridColumn: `span ${emptySlots}` }} />}
        {calendarDays.map((day) => {
          const isSelected = day.hasSame(value, 'day');
          // The grid is built from `value`, so "today" has to be read in the
          // same zone that value carries rather than the browser's.
          const today = isToday(day, value.zoneName ?? undefined);
          const isDisabled = minDate ? day < minDate.startOf('day') : false;
          const dayOfWeek = day.weekday % 7;
          return (
            <button
              key={day.toISODate()}
              type="button"
              onClick={() => handleSelect(day)}
              disabled={isDisabled}
              data-day={day.toISODate()}
              tabIndex={day.hasSame(tabDay, 'day') ? 0 : -1}
              aria-label={day.toFormat('yyyy-MM-dd')}
              aria-pressed={isSelected}
              className="flex h-9 w-full items-center justify-center rounded-full text-body tabular-nums transition-colors"
              style={{
                backgroundColor: isSelected ? 'var(--color-accent)' : 'transparent',
                color: isSelected
                  ? '#ffffff'
                  : isDisabled
                    ? 'var(--color-text-tertiary)'
                    : dayOfWeek === 0
                      ? 'var(--color-sunday)'
                      : dayOfWeek === 6
                        ? 'var(--color-saturday)'
                        : 'var(--color-text-primary)',
                fontWeight: today ? 700 : isSelected ? 600 : 400,
                opacity: isDisabled ? 0.3 : 1,
                outline: today && !isSelected ? '2px solid var(--color-accent)' : 'none',
                outlineOffset: '-2px',
              }}
            >
              {day.day}
            </button>
          );
        })}
      </div>
    </div>,
    document.body,
  );
}

/* ============================================
   TimePicker — scrollable time slot dropdown
   ============================================ */

interface TimePickerDropdownProps {
  value: string;
  onChange: (time: string) => void;
  onClose: () => void;
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

function TimePickerDropdown({
  value,
  onChange,
  onClose,
  style,
  floatingRef,
  returnFocusRef,
}: TimePickerDropdownProps) {
  const selectedRef = useRef<HTMLButtonElement>(null);

  useOutsideClose([floatingRef], onClose, true);

  const closeToField = () => {
    onClose();
    returnFocusRef.current?.focus();
  };

  useEffect(() => {
    selectedRef.current?.focus({ preventScroll: true });
    selectedRef.current?.scrollIntoView({ block: 'center' });
  }, []);

  // Slots are one Tab stop; the arrows walk the list.
  const handleListKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const slots = Array.from(e.currentTarget.querySelectorAll<HTMLElement>('button'));
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

  const nearestSlot = useMemo(() => {
    const [h, m] = value.split(':').map(Number);
    const totalMin = (h ?? 0) * 60 + (m ?? 0);
    const rounded = Math.min(Math.round(totalMin / 15) * 15, 23 * 60 + 45);
    const rh = Math.floor(rounded / 60);
    const rm = rounded % 60;
    return `${String(rh).padStart(2, '0')}:${String(rm).padStart(2, '0')}`;
  }, [value]);

  return createPortal(
    // biome-ignore lint/a11y/noStaticElementInteractions: the panel only receives key events bubbling from its buttons
    <div
      ref={floatingRef}
      onKeyDown={(e) => {
        handlePopupKeys(e, closeToField);
        handleListKey(e);
      }}
      className="dropdown-panel fixed z-[9999] max-h-[240px] w-[100px] overflow-y-auto bg-[var(--color-surface-elevated)] py-1 ring-1 ring-[var(--color-border)]"
      style={{ ...style, boxShadow: 'var(--shadow-elevated)', backdropFilter: 'blur(20px)' }}
    >
      {TIME_SLOTS.map((slot) => {
        const isSelected = slot === nearestSlot;
        return (
          <button
            key={slot}
            ref={isSelected ? selectedRef : undefined}
            type="button"
            tabIndex={isSelected ? 0 : -1}
            aria-pressed={isSelected}
            onClick={() => {
              onChange(slot);
              closeToField();
            }}
            className="flex w-full items-center justify-center py-2 text-default tabular-nums transition-colors"
            style={{
              backgroundColor: isSelected ? 'var(--color-accent-bg)' : 'transparent',
              color: isSelected ? 'var(--color-accent)' : 'var(--color-text-primary)',
              fontWeight: isSelected ? 600 : 400,
            }}
          >
            {slot}
          </button>
        );
      })}
    </div>,
    document.body,
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
  ariaLabel?: string;
  /**
   * Adds a filter box to the dropdown.
   *
   * A list long enough that scrolling it is a chore — the two hundred
   * countries holiday data covers — is only usable if it can be narrowed.
   */
  searchable?: boolean;
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

let selectSeq = 0;

export function CustomSelect({
  value,
  options,
  onChange,
  className,
  triggerClassName,
  ariaLabel,
  searchable,
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
        onClick={() => setOpen((v) => !v)}
        onKeyDown={handleKeyDown}
        className={
          triggerClassName
            ? `flex w-full items-center justify-between gap-2 text-left transition-colors ${triggerClassName}`
            : 'flex w-full items-center justify-between gap-2 bg-[var(--color-surface-inset)] py-1.5 pr-3 pl-3 text-default text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-hover)]'
        }
        style={triggerClassName ? undefined : { borderRadius: 'var(--radius-sm)' }}
      >
        <span className="flex min-w-0 items-center gap-2">
          {selected?.color && <Swatch color={selected.color} />}
          <span className="truncate">{selected?.label ?? value}</span>
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
                      color: isSelected ? 'var(--color-accent)' : 'var(--color-text-primary)',
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
                    {opt.color && <Swatch color={opt.color} />}
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
    if (locale === 'en') return dateValue.toFormat('EEE, MMM d, yyyy');
    const dow = ['日', '月', '火', '水', '木', '金', '土'][dateValue.weekday % 7] ?? '';
    return `${dateValue.year}年${dateValue.month}月${dateValue.day}日(${dow})`;
  }, [dateValue, locale]);

  const handleCloseDatePicker = useCallback(() => setShowDatePicker(false), []);
  const handleCloseTimePicker = useCallback(() => setShowTimePicker(false), []);

  return (
    <div className="flex items-center justify-between py-2.5">
      {label && <span className="text-default text-[var(--color-text-secondary)]">{label}</span>}
      <div className="flex items-center gap-2">
        <button
          ref={dateFloating.anchorRef}
          type="button"
          onClick={() => {
            setShowDatePicker((v) => !v);
            setShowTimePicker(false);
          }}
          className="pill-button bg-[var(--color-accent-bg)] px-3 py-1.5 text-default font-medium tabular-nums text-[var(--color-accent)] hover:bg-[var(--color-accent-subtle)]"
        >
          {dateLabel}
        </button>
        {showDatePicker && (
          <DatePickerDropdown
            value={dateValue}
            onChange={onDateChange}
            onClose={handleCloseDatePicker}
            {...(minDate ? { minDate } : {})}
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
              className="pill-button bg-[var(--color-accent-bg)] px-3 py-1.5 text-default font-medium tabular-nums text-[var(--color-accent)] hover:bg-[var(--color-accent-subtle)]"
            >
              {timeValue}
            </button>
            {showTimePicker && (
              <TimePickerDropdown
                value={timeValue}
                onChange={onTimeChange}
                onClose={handleCloseTimePicker}
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
