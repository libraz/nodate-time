import { LabelledButton } from '@/components/labelled-button';

interface SwitchProps {
  checked: boolean;
  onChange: (next: boolean) => void;
  /** The accessible name; the visible label beside the switch says the same. */
  label: string;
  disabled?: boolean;
  /** `sm` sits inline with body text; `md` ends a settings or form row. */
  size?: 'sm' | 'md';
}

const SIZES = {
  md: { track: 'h-7 w-12', knob: 'h-6 w-6', on: 'translate-x-5' },
  sm: { track: 'h-5 w-9', knob: 'h-4 w-4', on: 'translate-x-4' },
} as const;

/** The on/off switch every form row uses, so each one moves and reads the same. */
export function Switch({ checked, onChange, label, disabled, size = 'md' }: SwitchProps) {
  const s = SIZES[size];
  return (
    <LabelledButton
      label={label}
      pressed={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`toggle-track relative shrink-0 disabled:opacity-50 ${s.track}`}
      style={{
        backgroundColor: checked ? 'var(--color-accent)' : 'var(--color-surface-inset)',
      }}
    >
      <span
        aria-hidden
        className={`toggle-knob absolute left-0.5 top-0.5 ${s.knob} ${checked ? s.on : ''}`}
      />
    </LabelledButton>
  );
}
