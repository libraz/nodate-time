import type { ButtonHTMLAttributes, ReactNode } from 'react';

type NativeButtonProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  'type' | 'aria-label' | 'aria-pressed' | 'aria-expanded' | 'children'
>;

/**
 * State a toggle announces: a two-state switch is `pressed`, a disclosure is
 * `expanded`, a plain action carries neither. Exactly one may be given.
 */
type ToggleState =
  | { pressed?: undefined; expanded?: undefined }
  | { pressed: boolean; expanded?: undefined }
  | { expanded: boolean; pressed?: undefined };

type LabelledButtonProps = NativeButtonProps &
  ToggleState & {
    /** The accessible name; required because the control has no text of its own. */
    label: string;
    children: ReactNode;
  };

/**
 * An icon-only button. It cannot be rendered without an accessible name, and a
 * toggle states its on/off or open/closed state alongside it.
 */
export function LabelledButton({
  label,
  pressed,
  expanded,
  children,
  ...rest
}: LabelledButtonProps) {
  return (
    <button
      type="button"
      {...rest}
      aria-label={label}
      aria-pressed={pressed}
      aria-expanded={expanded}
    >
      {children}
    </button>
  );
}
