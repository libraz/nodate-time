import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CustomSelect, DateTimeField } from './pickers';

// Mock the ui-store to provide locale
vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string }) => string) => selector({ locale: 'en' }),
}));

afterEach(cleanup);

describe('CustomSelect', () => {
  const options = [
    { value: 'a', label: 'Alpha' },
    { value: 'b', label: 'Beta' },
    { value: 'c', label: 'Gamma' },
  ];

  it('renders the selected label', () => {
    render(<CustomSelect value="b" options={options} onChange={() => {}} />);
    expect(screen.getByText('Beta')).toBeInTheDocument();
  });

  it('opens dropdown on click and shows all options', async () => {
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={() => {}} />);

    await user.click(screen.getByText('Alpha'));

    expect(screen.getByText('Beta')).toBeInTheDocument();
    expect(screen.getByText('Gamma')).toBeInTheDocument();
  });

  it('calls onChange when an option is selected', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={onChange} />);

    await user.click(screen.getByText('Alpha'));
    await user.click(screen.getByText('Gamma'));

    expect(onChange).toHaveBeenCalledWith('c');
  });

  it('closes dropdown after selection', async () => {
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={() => {}} />);

    await user.click(screen.getByText('Alpha'));
    expect(screen.getByText('Gamma')).toBeInTheDocument();

    await user.click(screen.getByText('Beta'));
    // After selection, portal dropdown should be gone
    const dropdowns = document.querySelectorAll('body > .fixed');
    expect(dropdowns).toHaveLength(0);
  });

  it('narrows a long list by label or by code', async () => {
    const user = userEvent.setup();
    const countries = [
      { value: 'DE', label: 'Germany' },
      { value: 'JP', label: 'Japan' },
      { value: 'US', label: 'United States' },
    ];
    render(<CustomSelect value="JP" options={countries} onChange={() => {}} searchable />);

    await user.click(screen.getByText('Japan'));
    const filter = screen.getByRole('textbox', { name: 'Search' });

    await user.type(filter, 'germ');
    expect(screen.getByText('Germany')).toBeInTheDocument();
    expect(screen.queryByText('United States')).toBeNull();

    await user.clear(filter);
    await user.type(filter, 'us');
    expect(screen.getByText('United States')).toBeInTheDocument();
    expect(screen.queryByText('Germany')).toBeNull();

    await user.clear(filter);
    await user.type(filter, 'zz');
    expect(screen.getByText('No matches')).toBeInTheDocument();
  });

  it('leaves an ordinary select without a filter box', async () => {
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={() => {}} />);

    await user.click(screen.getByText('Alpha'));
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it('shows checkmark on selected option', async () => {
    const user = userEvent.setup();
    render(<CustomSelect value="b" options={options} onChange={() => {}} />);

    await user.click(screen.getByText('Beta'));

    // The selected item "Beta" in the dropdown should have a checkmark svg
    const betaInDropdown = screen.getAllByText('Beta');
    // There should be 2 instances: trigger + dropdown
    expect(betaInDropdown.length).toBeGreaterThanOrEqual(2);
  });

  it('names itself and its list to assistive tech', async () => {
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={() => {}} ariaLabel="Calendar" />);

    const trigger = screen.getByRole('button', { name: 'Calendar' });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');

    await user.click(trigger);

    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('listbox', { name: 'Calendar' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Alpha' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('option', { name: 'Beta' })).toHaveAttribute('aria-selected', 'false');
  });

  /**
   * The keys a native select answers. Replacing one with a button that only
   * takes a pointer would read as the same control while being usable by
   * fewer people.
   */
  it('opens and picks from the keyboard', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={onChange} ariaLabel="Letters" />);

    const trigger = screen.getByRole('button', { name: 'Letters' });
    trigger.focus();

    await user.keyboard('{ArrowDown}');
    expect(screen.getByRole('listbox')).toBeInTheDocument();

    // Opening starts on the current value, so one step down lands on Beta.
    await user.keyboard('{ArrowDown}{Enter}');
    expect(onChange).toHaveBeenCalledWith('b');
  });

  /**
   * Escape closes the list and stops there. These pickers sit inside dialogs
   * that close on Escape themselves, and dismissing both at once loses the
   * form behind the list.
   */
  it('closes on Escape without letting the key travel further', async () => {
    const user = userEvent.setup();
    const onDialogKey = vi.fn();
    render(
      // biome-ignore lint/a11y/noStaticElementInteractions: stands in for the dialog listening behind the picker
      <div onKeyDown={onDialogKey}>
        <CustomSelect value="a" options={options} onChange={() => {}} ariaLabel="Letters" />
      </div>,
    );

    const trigger = screen.getByRole('button', { name: 'Letters' });
    trigger.focus();
    await user.keyboard('{ArrowDown}');
    expect(screen.getByRole('listbox')).toBeInTheDocument();

    await user.keyboard('{Escape}');

    expect(screen.queryByRole('listbox')).toBeNull();
    expect(onDialogKey).not.toHaveBeenCalledWith(
      expect.objectContaining({ key: 'Escape' }) as never,
    );
  });

  it('jumps to the ends of the list', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={onChange} ariaLabel="Letters" />);

    screen.getByRole('button', { name: 'Letters' }).focus();
    await user.keyboard('{Enter}{End}{Enter}');

    expect(onChange).toHaveBeenCalledWith('c');
  });

  /**
   * The row the arrows are on and the row that is chosen are two different
   * things. Sharing one highlight would make arrowing past the selection look
   * like the selection moving with it.
   */
  it('marks which row the keyboard is on without moving the selection', async () => {
    const user = userEvent.setup();
    render(<CustomSelect value="a" options={options} onChange={() => {}} ariaLabel="Letters" />);

    screen.getByRole('button', { name: 'Letters' }).focus();
    await user.keyboard('{ArrowDown}{ArrowDown}');

    const alpha = screen.getByRole('option', { name: 'Alpha' });
    const beta = screen.getByRole('option', { name: 'Beta' });

    expect(beta).toHaveStyle({ backgroundColor: 'var(--color-hover)' });
    expect(alpha).toHaveStyle({ backgroundColor: 'var(--color-accent-bg)' });
    expect(alpha).toHaveAttribute('aria-selected', 'true');
    expect(beta).toHaveAttribute('aria-selected', 'false');
  });

  it('shows the option colour on the trigger as well as in the list', async () => {
    const user = userEvent.setup();
    const swatch = (el: HTMLElement) => el.querySelector('span[style*="background-color"]');
    render(
      <CustomSelect
        value="a"
        options={[
          { value: 'a', label: 'Alpha', color: '#ff0000' },
          { value: 'b', label: 'Beta', color: '#00ff00' },
        ]}
        onChange={() => {}}
        ariaLabel="Calendar"
      />,
    );

    const trigger = screen.getByRole('button', { name: 'Calendar' });
    expect(swatch(trigger)).toHaveStyle({ backgroundColor: '#ff0000' });

    await user.click(trigger);
    expect(swatch(screen.getByRole('option', { name: 'Beta' }))).toHaveStyle({
      backgroundColor: '#00ff00',
    });
  });

  /**
   * The outside-click listeners are subscribed for as long as the dropdown is
   * open, not re-subscribed for every render it survives. The refs are handed
   * to the hook as a fresh array each render, so naming the array in the
   * effect's dependencies tore the document listeners down and put them back
   * on every keystroke that reached the picker's parent.
   */
  it('subscribes to the document once while open, not once per render', async () => {
    const user = userEvent.setup();
    const subscribe = vi.spyOn(document, 'addEventListener');
    const mousedowns = () => subscribe.mock.calls.filter(([type]) => type === 'mousedown').length;

    const { rerender } = render(<CustomSelect value="a" options={options} onChange={() => {}} />);
    await user.click(screen.getByText('Alpha'));

    const whileOpen = mousedowns();
    // A render the dropdown lives through: same open list, different props.
    rerender(<CustomSelect value="a" options={options} onChange={() => {}} className="w-40" />);

    expect(mousedowns()).toBe(whileOpen);
    subscribe.mockRestore();
  });
});

describe('DateTimeField', () => {
  const baseDate = DateTime.fromISO('2026-04-19T09:00:00');

  it('renders date label in English', () => {
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={false}
        onDateChange={() => {}}
        onTimeChange={() => {}}
      />,
    );

    expect(screen.getByText('Start')).toBeInTheDocument();
    // Should display English formatted date
    expect(screen.getByText(/Apr 19, 2026/)).toBeInTheDocument();
  });

  it('shows time button when showTime is true', () => {
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={true}
        onDateChange={() => {}}
        onTimeChange={() => {}}
      />,
    );

    expect(screen.getByText('09:00')).toBeInTheDocument();
  });

  it('hides time button when showTime is false', () => {
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={false}
        onDateChange={() => {}}
        onTimeChange={() => {}}
      />,
    );

    expect(screen.queryByText('09:00')).not.toBeInTheDocument();
  });

  it('opens date picker on date click', async () => {
    const user = userEvent.setup();
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={false}
        onDateChange={() => {}}
        onTimeChange={() => {}}
      />,
    );

    await user.click(screen.getByText(/Apr 19, 2026/));

    // Date picker should appear with month label and day numbers
    expect(screen.getByText('April 2026')).toBeInTheDocument();
    expect(screen.getByText('19')).toBeInTheDocument();
  });

  it('calls onDateChange when a date is picked', async () => {
    const onDateChange = vi.fn();
    const user = userEvent.setup();
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={false}
        onDateChange={onDateChange}
        onTimeChange={() => {}}
      />,
    );

    await user.click(screen.getByText(/Apr 19, 2026/));
    // Click day 25
    await user.click(screen.getByText('25'));

    expect(onDateChange).toHaveBeenCalledTimes(1);
    const picked = onDateChange.mock.calls[0]?.[0] as DateTime;
    expect(picked.day).toBe(25);
    expect(picked.month).toBe(4);
    expect(picked.year).toBe(2026);
  });

  it('opens time picker on time click', async () => {
    const user = userEvent.setup();
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={true}
        onDateChange={() => {}}
        onTimeChange={() => {}}
      />,
    );

    await user.click(screen.getByText('09:00'));

    // Time picker should show time slots
    expect(screen.getByText('09:15')).toBeInTheDocument();
    expect(screen.getByText('10:00')).toBeInTheDocument();
  });

  it('calls onTimeChange when a time slot is picked', async () => {
    const onTimeChange = vi.fn();
    const user = userEvent.setup();
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={true}
        onDateChange={() => {}}
        onTimeChange={onTimeChange}
      />,
    );

    await user.click(screen.getByText('09:00'));
    await user.click(screen.getByText('14:30'));

    expect(onTimeChange).toHaveBeenCalledWith('14:30');
  });

  it('navigates months in date picker', async () => {
    const user = userEvent.setup();
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime={false}
        onDateChange={() => {}}
        onTimeChange={() => {}}
      />,
    );

    await user.click(screen.getByText(/Apr 19, 2026/));
    expect(screen.getByText('April 2026')).toBeInTheDocument();

    // Click next month arrow (the second arrow button)
    const picker = document.querySelector('.fixed.z-\\[9999\\]');
    expect(picker).not.toBeNull();
    const navButtons = picker?.querySelectorAll('button') ?? [];
    // First button is prev, second is next (in the month nav row)
    const nextBtn = navButtons[1];
    expect(nextBtn).toBeDefined();
    await user.click(nextBtn as HTMLButtonElement);

    expect(screen.getByText('May 2026')).toBeInTheDocument();
  });

  it('respects minDate constraint', async () => {
    const onDateChange = vi.fn();
    const user = userEvent.setup();
    const minDate = DateTime.fromISO('2026-04-15');

    render(
      <DateTimeField
        label="End"
        dateValue={DateTime.fromISO('2026-04-20')}
        timeValue="10:00"
        showTime={false}
        onDateChange={onDateChange}
        onTimeChange={() => {}}
        minDate={minDate}
      />,
    );

    await user.click(screen.getByText(/Apr 20, 2026/));

    // Day 10 should be disabled (before minDate)
    const day10 = screen.getByText('10');
    expect(day10).toBeDisabled();

    // Click disabled day — should not trigger onChange
    await user.click(day10);
    expect(onDateChange).not.toHaveBeenCalled();

    // Day 20 should work
    await user.click(screen.getByText('20'));
    expect(onDateChange).toHaveBeenCalledTimes(1);
  });
});

describe('DateTimeField from the keyboard', () => {
  const baseDate = DateTime.fromISO('2026-04-19T09:00:00');

  function renderField(onDateChange = vi.fn(), onTimeChange = vi.fn()) {
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:00"
        showTime
        onDateChange={onDateChange}
        onTimeChange={onTimeChange}
      />,
    );
    return { onDateChange, onTimeChange };
  }

  it('picks another day with the arrow keys and returns focus to the date button', async () => {
    const { onDateChange } = renderField();
    const user = userEvent.setup();
    const dateButton = screen.getByRole('button', { name: /Apr 19, 2026/ });

    dateButton.focus();
    await user.keyboard('{Enter}');
    expect(screen.getByRole('button', { name: '2026-04-19' })).toHaveFocus();

    await user.keyboard('{ArrowRight}{ArrowDown}{Enter}');

    expect(onDateChange).toHaveBeenCalledTimes(1);
    const picked = onDateChange.mock.calls[0]?.[0] as DateTime;
    expect(picked.toISODate()).toBe('2026-04-27');
    expect(dateButton).toHaveFocus();
  });

  it('closes the picker on Escape without letting the key reach the dialog behind', async () => {
    renderField();
    const user = userEvent.setup();
    const behind = vi.fn();
    document.addEventListener('keydown', behind);
    const dateButton = screen.getByRole('button', { name: /Apr 19, 2026/ });

    await user.click(dateButton);
    await user.keyboard('{Escape}');

    document.removeEventListener('keydown', behind);
    expect(screen.queryByRole('button', { name: '2026-04-19' })).toBeNull();
    expect(behind).not.toHaveBeenCalled();
    expect(dateButton).toHaveFocus();
  });

  it('keeps Tab inside the open date picker', async () => {
    renderField();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /Apr 19, 2026/ }));

    for (let i = 0; i < 6; i++) await user.tab();

    const panel = screen.getByRole('button', { name: '2026-04-19' }).parentElement?.parentElement;
    expect(panel?.contains(document.activeElement)).toBe(true);
  });

  it('picks a time with the arrow keys and returns focus to the time button', async () => {
    const { onTimeChange } = renderField();
    const user = userEvent.setup();
    const timeButton = screen.getByRole('button', { name: '09:00' });

    timeButton.focus();
    await user.keyboard('{Enter}');
    expect(screen.getByRole('button', { name: '09:00', pressed: true })).toHaveFocus();

    await user.keyboard('{ArrowDown}{Enter}');

    expect(onTimeChange).toHaveBeenCalledWith('09:15');
    expect(timeButton).toHaveFocus();
  });
});
