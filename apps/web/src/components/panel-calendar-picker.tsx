import { CustomSelect } from '@/components/pickers';
import type { Calendar } from '@/types/calendar';

interface PanelCalendarPickerProps {
  label: string;
  options: Calendar[];
  target: Calendar;
  onChange: (id: string) => void;
}

/** Names the calendar a side panel acts on, and offers the others when there are any. */
export function PanelCalendarPicker({
  label,
  options,
  target,
  onChange,
}: PanelCalendarPickerProps) {
  return (
    <div className="space-y-1.5">
      <span className="block text-caption font-medium text-[var(--color-text-secondary)]">
        {label}
      </span>
      {options.length > 1 ? (
        <CustomSelect
          value={target.id}
          onChange={onChange}
          ariaLabel={label}
          triggerClassName="input-modern"
          options={options.map((c) => ({ value: c.id, label: c.name, color: c.color }))}
        />
      ) : (
        <div className="flex items-center gap-2">
          <span
            className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
            style={{ backgroundColor: target.color }}
          />
          <span className="truncate text-callout font-medium text-[var(--color-text-primary)]">
            {target.name}
          </span>
        </div>
      )}
    </div>
  );
}
