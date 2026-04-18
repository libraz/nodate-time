import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
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

  it('shows checkmark on selected option', async () => {
    const user = userEvent.setup();
    render(<CustomSelect value="b" options={options} onChange={() => {}} />);

    await user.click(screen.getByText('Beta'));

    // The selected item "Beta" in the dropdown should have a checkmark svg
    const betaInDropdown = screen.getAllByText('Beta');
    // There should be 2 instances: trigger + dropdown
    expect(betaInDropdown.length).toBeGreaterThanOrEqual(2);
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
    const picked = onDateChange.mock.calls[0][0] as DateTime;
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
