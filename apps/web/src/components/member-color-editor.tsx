import { type CSSProperties, useEffect, useId, useRef, useState } from 'react';
import { useT } from '@/i18n';
import { isSubmitEnter } from '@/lib/keys';
import { parseColorInput, readableInk, rgbOf } from '@/lib/member-color';
import { MEMBER_COLORS, type Member } from '@/types/calendar';

/**
 * A member's layer colour: one of the palette, or any colour given as RGB.
 * Colours other members already carry are marked but stay choosable, since a
 * repeat is sometimes what people want.
 */
export function MemberColorEditor({
  member,
  others,
  onCommit,
}: {
  member: Member;
  /** The rest of the calendar, to mark the colours already in use. */
  others: Member[];
  onCommit: (color: string) => void;
}) {
  const t = useT();
  const textId = useId();
  const current = member.color.toUpperCase();
  const [text, setText] = useState(current);
  const [invalid, setInvalid] = useState(false);
  const pickerRef = useRef<HTMLInputElement>(null);
  const commitRef = useRef(onCommit);
  useEffect(() => {
    commitRef.current = onCommit;
  }, [onCommit]);

  useEffect(() => {
    setText(current);
    setInvalid(false);
  }, [current]);

  // React's onChange is the DOM input event, fired on every step of a drag
  // through the picker; the native change event fires once, on the choice.
  useEffect(() => {
    const input = pickerRef.current;
    if (!input) return;
    const commit = () => commitRef.current(input.value.toUpperCase());
    input.addEventListener('change', commit);
    return () => input.removeEventListener('change', commit);
  }, []);

  const usedBy = (color: string) =>
    others.filter((o) => o.color.toUpperCase() === color).map((o) => o.name);

  const applyText = () => {
    const parsed = parseColorInput(text);
    if (!parsed) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    setText(parsed);
    if (parsed !== current) onCommit(parsed);
  };

  const preview = parseColorInput(text) ?? current;
  const rgb = rgbOf(preview);

  return (
    <div className="member-color-editor mt-3 space-y-3 sm:pl-12">
      <fieldset>
        <legend className="mb-2 text-caption font-medium text-[var(--color-text-secondary)]">
          {t('members.colorPalette')}
        </legend>
        <div className="grid grid-cols-10 gap-1.5">
          {MEMBER_COLORS.map((c) => {
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
                onClick={() => !selected && onCommit(c)}
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
        <p className="mt-1.5 flex items-center gap-1.5 text-micro text-[var(--color-text-tertiary)]">
          <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-[var(--color-text-tertiary)]" />
          {t('members.colorInUseHint')}
        </p>
      </fieldset>

      <div>
        <label
          htmlFor={textId}
          className="mb-2 block text-caption font-medium text-[var(--color-text-secondary)]"
        >
          {t('members.colorCustom')}
        </label>
        <div className="flex items-center gap-2">
          <span
            className="relative h-[34px] w-[34px] shrink-0 overflow-hidden rounded-full border border-[var(--color-border-strong)]"
            style={{ backgroundColor: preview }}
          >
            <input
              data-1p-ignore
              ref={pickerRef}
              type="color"
              value={preview.toLowerCase()}
              onChange={(e) => {
                setText(e.target.value.toUpperCase());
                setInvalid(false);
              }}
              className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
              aria-label={t('members.colorPicker')}
            />
          </span>
          <input
            id={textId}
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
            className="input-modern input-compact min-w-0 flex-1 font-mono tabular-nums"
          />
          <button
            type="button"
            onClick={applyText}
            className="h-[34px] shrink-0 rounded-full bg-[var(--color-surface-inset)] px-3.5 text-footnote font-medium text-[var(--color-text-primary)] transition hover:bg-[var(--color-hover)]"
          >
            {t('members.colorApply')}
          </button>
        </div>
        <p
          id={`${textId}-hint`}
          className={`mt-1.5 text-micro tabular-nums ${invalid ? 'text-[var(--color-danger)]' : 'text-[var(--color-text-tertiary)]'}`}
        >
          {invalid ? t('members.colorInvalid') : rgb ? `R ${rgb[0]}  G ${rgb[1]}  B ${rgb[2]}` : ''}
        </p>
      </div>
    </div>
  );
}
