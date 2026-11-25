import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Avatar } from '@/components/avatar';
import { useFloating, useOutsideClose } from '@/components/picker-surface';
import { useT } from '@/i18n';

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
