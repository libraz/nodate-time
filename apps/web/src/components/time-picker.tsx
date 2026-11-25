import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
  handlePopupKeys,
  PickerSurface,
  useOutsideClose,
  useSheetPicker,
} from '@/components/picker-surface';
import { useT } from '@/i18n';
import { isSubmitEnter } from '@/lib/keys';

/* ============================================
   TimePicker — hour and minute wheels
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

const HOURS = Array.from({ length: 24 }, (_, h) => h);
const MINUTE_STEP = 5;
/** Rows a wheel shows: the selected one with two either side. */
const WHEEL_ROWS = 5;
/** How long a wheel must be still before the row under the band counts as picked. */
const WHEEL_SETTLE_MS = 120;

const pad2 = (n: number) => String(n).padStart(2, '0');

/** The minute wheel's rows: five-minute steps, plus the current minute when it is off them. */
function minuteRows(current: number): number[] {
  const rows = Array.from({ length: 60 / MINUTE_STEP }, (_, i) => i * MINUTE_STEP);
  if (!rows.includes(current)) {
    rows.push(current);
    rows.sort((a, b) => a - b);
  }
  return rows;
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
  return `${pad2(hour)}:${pad2(minute)}`;
}

/**
 * One scroll-snapped column of numbers. The row resting under the band is the
 * value once scrolling stops; a tap on a row picks it, a tap on the picked row
 * confirms. To assistive tech and the keyboard it is a spinbutton.
 */
function TimeWheel({
  label,
  rows,
  value,
  rowHeight,
  onPick,
  onConfirm,
  autoFocus,
}: {
  label: string;
  rows: number[];
  value: number;
  rowHeight: number;
  onPick: (v: number) => void;
  onConfirm: () => void;
  autoFocus?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const settleTimer = useRef<number | undefined>(undefined);
  const mounted = useRef(false);
  const [focused, setFocused] = useState(false);
  const index = Math.max(0, rows.indexOf(value));
  // The settle timer outlives renders, so it reads the latest props from here.
  const latest = useRef({ rows, value, onPick });
  latest.current = { rows, value, onPick };

  // Brings the picked row under the band: at once when the wheel appears,
  // gliding after that. A wheel already resting there is left alone.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const top = index * rowHeight;
    if (Math.abs(el.scrollTop - top) < 1) return;
    if (!mounted.current || !el.scrollTo) el.scrollTop = top;
    else el.scrollTo({ top, behavior: 'smooth' });
    mounted.current = true;
  }, [index, rowHeight]);

  useEffect(() => {
    mounted.current = true;
    if (autoFocus) ref.current?.focus({ preventScroll: true });
    return () => window.clearTimeout(settleTimer.current);
  }, [autoFocus]);

  const handleScroll = () => {
    window.clearTimeout(settleTimer.current);
    settleTimer.current = window.setTimeout(() => {
      const el = ref.current;
      if (!el) return;
      const { rows: r, value: v, onPick: pick } = latest.current;
      const at = Math.min(r.length - 1, Math.max(0, Math.round(el.scrollTop / rowHeight)));
      const next = r[at];
      if (next !== undefined && next !== v) pick(next);
    }, WHEEL_SETTLE_MS);
  };

  const stepTo = (i: number) => {
    const next = rows[Math.min(rows.length - 1, Math.max(0, i))];
    if (next !== undefined && next !== value) onPick(next);
  };

  const handleKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const moves = new Map<string, number>([
      ['ArrowUp', index - 1],
      ['ArrowDown', index + 1],
      ['PageUp', index - WHEEL_ROWS],
      ['PageDown', index + WHEEL_ROWS],
      ['Home', 0],
      ['End', rows.length - 1],
    ]);
    const target = moves.get(e.key);
    if (target !== undefined) {
      e.preventDefault();
      stepTo(target);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      onConfirm();
    }
  };

  return (
    <div
      ref={ref}
      role="spinbutton"
      tabIndex={0}
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={rows[0]}
      aria-valuemax={rows[rows.length - 1]}
      aria-valuetext={pad2(value)}
      onScroll={handleScroll}
      onKeyDown={handleKey}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
      className="time-wheel relative h-full overflow-y-auto overscroll-contain outline-none"
      style={{ paddingBlock: rowHeight * 2 }}
    >
      {rows.map((row) => {
        const picked = row === value;
        return (
          <div
            key={row}
            aria-hidden
            onClick={() => (picked ? onConfirm() : onPick(row))}
            className={`time-wheel-row flex cursor-pointer items-center justify-center tabular-nums transition-colors ${
              picked
                ? `font-semibold ${focused ? 'text-[var(--color-accent-ink)]' : 'text-[var(--color-text-primary)]'}`
                : 'text-[var(--color-text-tertiary)]'
            }`}
            style={{ height: rowHeight }}
          >
            {pad2(row)}
          </div>
        );
      })}
    </div>
  );
}

/**
 * Picks a time on an hour wheel and a minute wheel, the way a phone's own
 * clock does. Each wheel applies as it comes to rest, so the field behind
 * follows along; tapping the picked row, Enter or Done closes. A minute off
 * the five-minute steps is typed.
 */
export function TimePickerDropdown({
  value,
  onChange,
  onClose,
  title,
  style,
  floatingRef,
  returnFocusRef,
}: TimePickerDropdownProps) {
  const t = useT();
  const [typed, setTyped] = useState('');
  const [typedInvalid, setTypedInvalid] = useState(false);
  const sheet = useSheetPicker();
  const rowHeight = sheet ? 44 : 34;

  const [hourPart, minutePart] = value.split(':').map(Number);
  const hour = Number.isFinite(hourPart) ? (hourPart as number) : 9;
  const minute = Number.isFinite(minutePart) ? (minutePart as number) : 0;
  const minutes = useMemo(() => minuteRows(minute), [minute]);

  useOutsideClose([floatingRef], onClose, true);

  const closeToField = () => {
    onClose();
    returnFocusRef.current?.focus();
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

  return (
    <PickerSurface
      sheet={sheet}
      floatingRef={floatingRef}
      style={style}
      popoverClassName="w-[232px] p-3"
      onKeyDown={(e) => handlePopupKeys(e, closeToField)}
    >
      {sheet && (
        <div className="mb-1 flex items-center justify-between pl-1">
          <p className="text-footnote font-medium text-[var(--color-text-secondary)]">{title}</p>
          <button
            type="button"
            onClick={closeToField}
            className="-mr-2 h-10 px-3 text-callout font-semibold text-[var(--color-accent-ink)]"
          >
            {t('picker.done')}
          </button>
        </div>
      )}

      {/* The band marks the row each wheel rests on; the wheels scroll over it. */}
      <div
        className={`relative grid items-center justify-center ${
          sheet
            ? 'grid-cols-[88px_auto_88px] text-[23px]'
            : 'grid-cols-[64px_auto_64px] text-[17px]'
        }`}
        style={{ height: rowHeight * WHEEL_ROWS }}
      >
        <div
          aria-hidden
          className="pointer-events-none absolute inset-x-0 top-1/2 -translate-y-1/2 rounded-[var(--radius-control)] bg-[var(--color-surface-inset)]"
          style={{ height: rowHeight }}
        />
        <TimeWheel
          label={t('picker.hour')}
          rows={HOURS}
          value={hour}
          rowHeight={rowHeight}
          onPick={(h) => onChange(`${pad2(h)}:${pad2(minute)}`)}
          onConfirm={closeToField}
          autoFocus
        />
        <span aria-hidden className="relative px-1 font-semibold text-[var(--color-text-tertiary)]">
          :
        </span>
        <TimeWheel
          label={t('picker.minute')}
          rows={minutes}
          value={minute}
          rowHeight={rowHeight}
          onPick={(m) => onChange(`${pad2(hour)}:${pad2(m)}`)}
          onConfirm={closeToField}
        />
      </div>

      <div className="mt-3 flex items-center gap-1.5 border-t border-[var(--color-separator)] pt-3">
        <input
          data-1p-ignore
          type="text"
          inputMode="numeric"
          value={typed}
          onChange={(e) => {
            setTyped(e.target.value);
            setTypedInvalid(false);
          }}
          onKeyDown={handleTypedKey}
          aria-label={t('picker.typeTime')}
          aria-invalid={typedInvalid}
          placeholder={t('picker.typeTimePlaceholder')}
          className={`input-modern h-9 min-w-0 flex-1 text-default tabular-nums ${
            typedInvalid ? 'ring-1 ring-[var(--color-danger)]' : ''
          }`}
        />
        {/* A numeric keypad has no return key, so the typed time needs a button. */}
        <button
          type="button"
          aria-label={t('picker.applyTime')}
          onClick={applyTyped}
          disabled={!typed.trim()}
          className="icon-btn icon-btn-accent h-9 w-9 shrink-0 disabled:bg-[var(--color-surface-inset)] disabled:text-[var(--color-text-tertiary)]"
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
    </PickerSurface>
  );
}
