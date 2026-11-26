import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DateTimeField, DateTimeRangeField } from './date-time-fields';

// Mock the ui-store to provide locale
vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string; weekStart: string }) => string) =>
    selector({ locale: 'en', weekStart: 'sun' }),
}));

afterEach(cleanup);

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

    const hours = screen.getByRole('spinbutton', { name: 'Hour' });
    expect(hours).toHaveAttribute('aria-valuenow', '9');
    expect(hours.children).toHaveLength(24);
    const minutes = screen.getByRole('spinbutton', { name: 'Minute' });
    expect(minutes).toHaveAttribute('aria-valuenow', '0');
    expect(minutes.children).toHaveLength(12);
  });

  it('keeps a minute off the five-minute steps on the wheel', async () => {
    const user = userEvent.setup();
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:07"
        showTime={true}
        onDateChange={() => {}}
        onTimeChange={() => {}}
      />,
    );

    await user.click(screen.getByText('09:07'));

    const minutes = screen.getByRole('spinbutton', { name: 'Minute' });
    expect(minutes.children).toHaveLength(13);
    expect(minutes).toHaveAttribute('aria-valuetext', '07');
  });

  it('applies an hour at once, keeping the minute and the picker open', async () => {
    const onTimeChange = vi.fn();
    const user = userEvent.setup();
    render(
      <DateTimeField
        label="Start"
        dateValue={baseDate}
        timeValue="09:30"
        showTime={true}
        onDateChange={() => {}}
        onTimeChange={onTimeChange}
      />,
    );

    await user.click(screen.getByText('09:30'));
    await user.click(within(screen.getByRole('spinbutton', { name: 'Hour' })).getByText('14'));

    expect(onTimeChange).toHaveBeenCalledWith('14:30');
    expect(screen.getByRole('spinbutton', { name: 'Hour' })).toBeInTheDocument();
  });

  it('closes when the picked row is tapped again', async () => {
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
    await user.click(within(screen.getByRole('spinbutton', { name: 'Minute' })).getByText('45'));
    expect(onTimeChange).toHaveBeenCalledWith('09:45');

    await user.click(within(screen.getByRole('spinbutton', { name: 'Minute' })).getByText('00'));
    expect(screen.queryByRole('spinbutton', { name: 'Hour' })).toBeNull();
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

    await user.click(screen.getByRole('button', { name: 'Next month' }));

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

    const panel = screen.getByRole('button', { name: '2026-04-19' }).closest('.dropdown-panel');
    expect(panel?.contains(document.activeElement)).toBe(true);
  });

  it('picks a time with the arrow keys and returns focus to the time button', async () => {
    const { onTimeChange } = renderField();
    const user = userEvent.setup();
    const timeButton = screen.getByRole('button', { name: '09:00' });

    timeButton.focus();
    await user.keyboard('{Enter}');
    expect(screen.getByRole('spinbutton', { name: 'Hour' })).toHaveFocus();

    await user.keyboard('{ArrowDown}');
    expect(onTimeChange).toHaveBeenLastCalledWith('10:00');
    await user.tab();
    await user.keyboard('{ArrowDown}');
    expect(onTimeChange).toHaveBeenLastCalledWith('09:05');

    await user.keyboard('{Enter}');
    expect(timeButton).toHaveFocus();
  });
});

describe('TimePicker typed entry', () => {
  function renderField(onTimeChange = vi.fn()) {
    render(
      <DateTimeField
        label="Start"
        dateValue={DateTime.fromISO('2026-04-19T09:00:00')}
        timeValue="09:00"
        showTime
        onDateChange={() => {}}
        onTimeChange={onTimeChange}
      />,
    );
    return onTimeChange;
  }

  async function openTyped() {
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '09:00' }));
    return { user, input: screen.getByRole('textbox', { name: 'Type a time' }) };
  }

  it('keeps focus on the hour wheel when it opens', async () => {
    renderField();
    await openTyped();

    expect(screen.getByRole('spinbutton', { name: 'Hour' })).toHaveFocus();
  });

  it('is reached from the wheels with Tab', async () => {
    renderField();
    const { user, input } = await openTyped();

    await user.tab();
    await user.tab();

    expect(input).toHaveFocus();
  });

  it('applies a typed time on Enter', async () => {
    const onTimeChange = renderField();
    const { user, input } = await openTyped();

    await user.click(input);
    await user.keyboard('1430{Enter}');

    expect(onTimeChange).toHaveBeenCalledWith('14:30');
  });

  it('applies a typed time from the button, which a numeric keypad needs', async () => {
    const onTimeChange = renderField();
    const { user, input } = await openTyped();
    const apply = screen.getByRole('button', { name: 'Use this time' });
    expect(apply).toBeDisabled();

    await user.click(input);
    await user.keyboard('1430');
    await user.click(apply);

    expect(onTimeChange).toHaveBeenCalledWith('14:30');
  });

  it('does not apply on the Safari IME Enter, reported only as keyCode 229', async () => {
    const onTimeChange = renderField();
    const { input } = await openTyped();

    fireEvent.change(input, { target: { value: '1430' } });
    fireEvent.keyDown(input, { key: 'Enter', keyCode: 229 });
    expect(onTimeChange).not.toHaveBeenCalled();
  });

  it('does not apply on the Enter that confirms an IME composition', async () => {
    const onTimeChange = renderField();
    const { input } = await openTyped();

    fireEvent.change(input, { target: { value: '1430' } });
    fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
    expect(onTimeChange).not.toHaveBeenCalled();

    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onTimeChange).toHaveBeenCalledWith('14:30');
  });

  it('marks an invalid entry and stays open', async () => {
    const onTimeChange = renderField();
    const { input } = await openTyped();

    fireEvent.change(input, { target: { value: '24:00' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(onTimeChange).not.toHaveBeenCalled();
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('textbox', { name: 'Type a time' })).toBeInTheDocument();
  });

  it('leaves the arrow keys to the text box while it has focus', async () => {
    renderField();
    const { user, input } = await openTyped();

    await user.click(input);
    await user.keyboard('{ArrowDown}{ArrowDown}');

    expect(input).toHaveFocus();
  });
});

describe('DateTimeRangeField', () => {
  function renderRange(props: { showTime?: boolean; endInvalid?: boolean; endDate?: string } = {}) {
    const onStartTime = vi.fn();
    const onEndDate = vi.fn();
    render(
      <DateTimeRangeField
        showTime={props.showTime ?? true}
        endInvalid={props.endInvalid ?? false}
        start={{
          label: 'Start',
          date: DateTime.fromISO('2026-10-07T14:00:00'),
          time: '14:00',
          onDateChange: () => {},
          onTimeChange: onStartTime,
        }}
        end={{
          label: 'End',
          date: DateTime.fromISO(props.endDate ?? '2026-10-07T19:00:00'),
          time: '19:00',
          onDateChange: onEndDate,
          onTimeChange: () => {},
        }}
      />,
    );
    return { onStartTime, onEndDate };
  }

  it('names each date and time after the end it belongs to', () => {
    renderRange();
    for (const name of [
      'Start Wed, Oct 7, 2026',
      'Start 14:00',
      'End Wed, Oct 7, 2026',
      'End 19:00',
    ]) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument();
    }
  });

  it('says "same day" instead of repeating the start date', () => {
    renderRange();
    const end = screen.getByRole('button', { name: 'End Wed, Oct 7, 2026' });
    expect(end).toHaveTextContent('Same day');
    expect(screen.getByRole('button', { name: 'Start Wed, Oct 7, 2026' })).toHaveTextContent(
      'Wed, Oct 7, 2026',
    );
  });

  it('spells out an end on another day', () => {
    renderRange({ endDate: '2026-10-08T10:00:00', showTime: false });
    expect(screen.getByRole('button', { name: 'End Thu, Oct 8, 2026' })).toHaveTextContent(
      '2026Thu, Oct 8',
    );
  });

  it('shows only the dates for an all-day span', () => {
    renderRange({ showTime: false });
    expect(screen.getByRole('button', { name: 'Start Wed, Oct 7, 2026' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Start 14:00' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'End 19:00' })).toBeNull();
  });

  it('routes a typed start time to the start', async () => {
    const { onStartTime } = renderRange();
    fireEvent.click(screen.getByRole('button', { name: 'Start 14:00' }));
    const input = await screen.findByRole('textbox', { name: 'Type a time' });
    fireEvent.change(input, { target: { value: '15:30' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onStartTime).toHaveBeenCalledWith('15:30');
  });

  it('refuses end dates before the start', () => {
    const { onEndDate } = renderRange();
    fireEvent.click(screen.getByRole('button', { name: 'End Wed, Oct 7, 2026' }));
    const before = screen.getByRole('button', { name: '2026-10-06' });
    expect(before).toBeDisabled();
    fireEvent.click(before);
    expect(onEndDate).not.toHaveBeenCalled();
  });

  it('draws an end the caller rejects as an error', () => {
    renderRange({ endInvalid: true });
    expect(screen.getByRole('button', { name: 'End 19:00' })).toHaveClass(
      'text-[var(--color-danger)]',
    );
  });
});
