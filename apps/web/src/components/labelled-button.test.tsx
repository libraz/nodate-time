import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LabelledButton } from './labelled-button';

afterEach(cleanup);

describe('LabelledButton', () => {
  it('is found by its label alone', () => {
    const onClick = vi.fn();
    render(
      <LabelledButton label="Close" onClick={onClick}>
        <svg aria-hidden="true" />
      </LabelledButton>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Close' }));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('reports a toggle’s pressed state and a disclosure’s expanded state', () => {
    render(
      <>
        <LabelledButton label="All day" pressed>
          <svg aria-hidden="true" />
        </LabelledButton>
        <LabelledButton label="Memo" expanded={false}>
          <svg aria-hidden="true" />
        </LabelledButton>
      </>,
    );

    expect(screen.getByRole('button', { name: 'All day' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Memo' })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByRole('button', { name: 'Memo' })).not.toHaveAttribute('aria-pressed');
  });

  it('does not compile without a name, or with both kinds of state', () => {
    // @ts-expect-error the accessible name is required
    const unnamed = <LabelledButton>x</LabelledButton>;
    // @ts-expect-error a control is a toggle or a disclosure, not both
    const both = <LabelledButton label="x" pressed expanded />;
    expect(unnamed).toBeTruthy();
    expect(both).toBeTruthy();
  });
});
