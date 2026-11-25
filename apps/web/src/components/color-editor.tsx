import {
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react';
import { useT } from '@/i18n';
import { isSubmitEnter } from '@/lib/keys';
import {
  type Hsv,
  hexToHsv,
  hsvToHex,
  parseColorInput,
  readableInk,
  rgbOf,
} from '@/lib/member-color';
import { COLOR_SWATCHES } from '@/types/calendar';

/** A colour something else already carries, with the name of what carries it. */
export interface ColorUse {
  color: string;
  name: string;
}

/** How long keyboard steps on the picker wait before they count as a choice. */
const KEY_COMMIT_MS = 600;

const clamp01 = (n: number) => Math.min(1, Math.max(0, n));

/** Where a pointer sits across `el`, each axis 0–1. */
function pointerFraction(el: HTMLElement, e: PointerEvent): [number, number] {
  const r = el.getBoundingClientRect();
  return [
    r.width ? clamp01((e.clientX - r.left) / r.width) : 0,
    r.height ? clamp01((e.clientY - r.top) / r.height) : 0,
  ];
}

/** Arrow-key step on a slider as [x, y]: one unit, ten with Shift. */
const ARROWS = new Map<string, [number, number]>([
  ['ArrowLeft', [-1, 0]],
  ['ArrowRight', [1, 0]],
  ['ArrowUp', [0, 1]],
  ['ArrowDown', [0, -1]],
]);

function arrowStep(e: KeyboardEvent): [number, number] | null {
  const dir = ARROWS.get(e.key);
  if (!dir) return null;
  const n = e.shiftKey ? 10 : 1;
  return [dir[0] * n, dir[1] * n];
}

/**
 * A colour choice: one of the palette, or any colour from the picker or typed
 * as RGB. Colours in use elsewhere are marked but stay choosable, since a
 * repeat is sometimes what people want. Every change is committed once per
 * finished choice: a drag on the picker commits when it is let go.
 */
export function ColorEditor({
  value,
  onChange,
  inUse,
  legend,
  inUseHint,
}: {
  value: string;
  onChange: (color: string) => void;
  /** What already carries a colour, to mark those swatches and name their users. */
  inUse: ColorUse[];
  /** Caption of the palette. */
  legend: string;
  /** Explains the in-use mark. */
  inUseHint: string;
}) {
  const t = useT();
  const textId = useId();
  const pickerId = useId();
  const current = value.toUpperCase();
  const [text, setText] = useState(current);
  const [invalid, setInvalid] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  // The picker keeps its own hue, so dragging through grey or black does not
  // lose the hue the reader was on.
  const [hsv, setHsv] = useState<Hsv>(() => hexToHsv(current) ?? { h: 0, s: 0, v: 0 });
  const dragging = useRef<'area' | 'hue' | null>(null);
  const keyTimer = useRef<number | undefined>(undefined);
  const draft = useRef(current);
  const commitRef = useRef(onChange);
  useEffect(() => {
    commitRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    setText(current);
    setInvalid(false);
    if (draft.current === current) return;
    draft.current = current;
    const next = hexToHsv(current);
    if (next) setHsv((prev) => (next.s === 0 || next.v === 0 ? { ...next, h: prev.h } : next));
  }, [current]);

  useEffect(() => () => window.clearTimeout(keyTimer.current), []);

  const usedBy = (color: string) =>
    inUse.filter((u) => u.color.toUpperCase() === color).map((u) => u.name);

  // Moves the picker without committing: the field and preview follow along.
  const move = (next: Hsv) => {
    setHsv(next);
    const hex = hsvToHex(next);
    draft.current = hex;
    setText(hex);
    setInvalid(false);
  };

  const commitDraft = () => {
    window.clearTimeout(keyTimer.current);
    keyTimer.current = undefined;
    if (draft.current !== current) commitRef.current(draft.current);
  };

  const commitSoon = () => {
    window.clearTimeout(keyTimer.current);
    keyTimer.current = window.setTimeout(commitDraft, KEY_COMMIT_MS);
  };

  const flushKeys = () => {
    if (keyTimer.current !== undefined) commitDraft();
  };

  const fromArea = (el: HTMLElement, e: PointerEvent) => {
    const [x, y] = pointerFraction(el, e);
    move({ h: hsv.h, s: x, v: 1 - y });
  };

  const fromHue = (el: HTMLElement, e: PointerEvent) => {
    const [x] = pointerFraction(el, e);
    move({ ...hsv, h: Math.min(359, x * 360) });
  };

  const startDrag = (kind: 'area' | 'hue') => (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    e.currentTarget.focus();
    dragging.current = kind;
    (kind === 'area' ? fromArea : fromHue)(e.currentTarget, e);
  };

  const continueDrag = (e: PointerEvent<HTMLDivElement>) => {
    if (dragging.current === 'area') fromArea(e.currentTarget, e);
    else if (dragging.current === 'hue') fromHue(e.currentTarget, e);
  };

  const endDrag = () => {
    if (!dragging.current) return;
    dragging.current = null;
    commitDraft();
  };

  const areaKeys = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = arrowStep(e);
    if (!step) return;
    e.preventDefault();
    move({
      h: hsv.h,
      s: clamp01(hsv.s + step[0] / 100),
      v: clamp01(hsv.v + step[1] / 100),
    });
    commitSoon();
  };

  const hueKeys = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = arrowStep(e);
    if (!step) return;
    e.preventDefault();
    move({ ...hsv, h: (hsv.h + step[0] + step[1] + 360) % 360 });
    commitSoon();
  };

  const applyText = () => {
    const parsed = parseColorInput(text);
    if (!parsed) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    setText(parsed);
    if (parsed !== current) onChange(parsed);
  };

  const preview = parseColorInput(text) ?? current;
  const rgb = rgbOf(preview);
  const custom = !COLOR_SWATCHES.includes(current);
  const pct = (n: number) => Math.round(n * 100);

  return (
    <div className="space-y-3">
      <fieldset>
        <legend className="mb-2 text-caption font-medium text-[var(--color-text-secondary)]">
          {legend}
        </legend>
        <div className="grid grid-cols-10 gap-1.5">
          {COLOR_SWATCHES.map((c) => {
            const selected = c === current;
            const users = usedBy(c);
            const ink = readableInk(c);
            const label = users.length
              ? `${c} — ${t('members.colorInUse')}: ${users.join(', ')}`
              : c;
            return (
              <button
                key={c}
                type="button"
                onClick={() => !selected && onChange(c)}
                aria-label={label}
                aria-pressed={selected}
                title={label}
                className="member-swatch relative flex aspect-square w-full items-center justify-center rounded-full"
                style={{ backgroundColor: c, color: ink, '--swatch': c } as CSSProperties}
              >
                {selected ? (
                  <svg
                    aria-hidden
                    width="12"
                    height="12"
                    viewBox="0 0 12 12"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <path d="M2.5 6.25 5 8.5l4.5-5" />
                  </svg>
                ) : (
                  users.length > 0 && (
                    <span
                      aria-hidden
                      className="h-1.5 w-1.5 rounded-full"
                      style={{ backgroundColor: ink }}
                    />
                  )
                )}
              </button>
            );
          })}
        </div>
        {/* The legend explains the dot, so it shows only while a swatch carries one. */}
        {COLOR_SWATCHES.some((c) => c !== current && usedBy(c).length > 0) && (
          <p className="mt-1.5 flex items-center gap-1.5 text-micro text-[var(--color-text-tertiary)]">
            <span
              aria-hidden
              className="h-1.5 w-1.5 rounded-full bg-[var(--color-text-tertiary)]"
            />
            {inUseHint}
          </p>
        )}
      </fieldset>

      <div>
        <button
          type="button"
          onClick={() => setPickerOpen((open) => !open)}
          aria-expanded={pickerOpen}
          aria-controls={pickerOpen ? pickerId : undefined}
          className="color-option flex h-10 w-full items-center gap-2.5 rounded-[var(--radius-control)] px-1.5 text-left sm:h-9"
        >
          <span
            aria-hidden
            className={`color-ring relative h-7 w-7 shrink-0 rounded-full sm:h-6 sm:w-6 ${
              pickerOpen || custom ? 'is-active' : ''
            }`}
          >
            <span
              className="absolute inset-[3px] rounded-full"
              style={{ backgroundColor: custom ? preview : 'var(--color-surface-elevated)' }}
            />
          </span>
          <span className="flex-1 text-footnote font-medium text-[var(--color-text-secondary)]">
            {t('members.colorCustom')}
          </span>
          {custom && (
            <span className="font-mono text-footnote tabular-nums text-[var(--color-text-tertiary)]">
              {current}
            </span>
          )}
          <svg
            aria-hidden
            width="12"
            height="12"
            viewBox="0 0 12 12"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            className={`shrink-0 text-[var(--color-text-tertiary)] transition-transform duration-200 ${
              pickerOpen ? 'rotate-180' : ''
            }`}
          >
            <path d="M3 4.5 6 7.5l3-3" />
          </svg>
        </button>

        {pickerOpen && (
          <div id={pickerId} className="reveal-in mt-2 space-y-3">
            <div
              role="slider"
              tabIndex={0}
              aria-label={t('members.colorShade')}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={pct(hsv.s)}
              aria-valuetext={t('members.colorShadeValue', {
                s: String(pct(hsv.s)),
                v: String(pct(hsv.v)),
              })}
              onPointerDown={startDrag('area')}
              onPointerMove={continueDrag}
              onPointerUp={endDrag}
              onPointerCancel={endDrag}
              onKeyDown={areaKeys}
              onBlur={flushKeys}
              className="color-area relative h-36 w-full cursor-crosshair touch-none rounded-[var(--radius-control)] outline-none sm:h-32"
              style={{ '--hue': `hsl(${hsv.h} 100% 50%)` } as CSSProperties}
            >
              <span
                aria-hidden
                className="color-thumb"
                style={{
                  left: `${hsv.s * 100}%`,
                  top: `${(1 - hsv.v) * 100}%`,
                  backgroundColor: hsvToHex(hsv),
                }}
              />
            </div>
            <div
              role="slider"
              tabIndex={0}
              aria-label={t('members.colorHue')}
              aria-valuemin={0}
              aria-valuemax={359}
              aria-valuenow={Math.round(hsv.h)}
              onPointerDown={startDrag('hue')}
              onPointerMove={continueDrag}
              onPointerUp={endDrag}
              onPointerCancel={endDrag}
              onKeyDown={hueKeys}
              onBlur={flushKeys}
              className="color-hue relative mx-[11px] h-3.5 cursor-pointer touch-none rounded-full outline-none"
            >
              <span
                aria-hidden
                className="color-thumb"
                style={{
                  left: `${(hsv.h / 360) * 100}%`,
                  backgroundColor: `hsl(${hsv.h} 100% 50%)`,
                }}
              />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span
                  aria-hidden
                  className="color-preview h-10 w-10 shrink-0 rounded-full sm:h-[34px] sm:w-[34px]"
                  style={{ backgroundColor: preview }}
                />
                <input
                  id={textId}
                  aria-label={t('members.colorCode')}
                  data-1p-ignore
                  type="text"
                  inputMode="text"
                  autoComplete="off"
                  spellCheck={false}
                  value={text}
                  onChange={(e) => {
                    setText(e.target.value);
                    setInvalid(false);
                  }}
                  onKeyDown={(e) => {
                    if (!isSubmitEnter(e)) return;
                    e.preventDefault();
                    applyText();
                  }}
                  placeholder="#RRGGBB / 255, 128, 0"
                  aria-invalid={invalid || undefined}
                  aria-describedby={`${textId}-hint`}
                  className="input-modern input-compact h-10 min-w-0 flex-1 font-mono tabular-nums sm:h-[34px]"
                />
                <button
                  type="button"
                  onClick={applyText}
                  className="h-10 shrink-0 rounded-full bg-[var(--color-surface-inset)] px-3.5 text-footnote font-medium text-[var(--color-text-primary)] transition hover:bg-[var(--color-hover)] sm:h-[34px]"
                >
                  {t('members.colorApply')}
                </button>
              </div>
              <p
                id={`${textId}-hint`}
                className={`mt-1.5 text-micro tabular-nums ${invalid ? 'text-[var(--color-danger)]' : 'text-[var(--color-text-tertiary)]'}`}
              >
                {invalid
                  ? t('members.colorInvalid')
                  : rgb
                    ? `R ${rgb[0]}  G ${rgb[1]}  B ${rgb[2]}`
                    : ''}
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
