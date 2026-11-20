import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ConfirmSheet } from './confirm-sheet';

afterEach(cleanup);

function setup(props: Partial<React.ComponentProps<typeof ConfirmSheet>> = {}) {
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  const view = render(
    <ConfirmSheet
      open
      title="Delete this?"
      hint="It cannot be undone"
      confirmLabel="Delete"
      cancelLabel="Keep"
      onConfirm={onConfirm}
      onCancel={onCancel}
      {...props}
    />,
  );
  return { onConfirm, onCancel, ...view };
}

describe('ConfirmSheet', () => {
  it('renders nothing while closed', () => {
    setup({ open: false });
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('is an alertdialog labelled by the title and described by the hint, portalled to body', () => {
    const { container } = setup();
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveAccessibleName('Delete this?');
    expect(dialog).toHaveAccessibleDescription('It cannot be undone');
    expect(dialog).toHaveClass('confirm-sheet');
    expect(container.contains(dialog)).toBe(false);
    expect(document.body.contains(dialog)).toBe(true);
  });

  it('puts initial focus on Cancel', async () => {
    setup();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Keep' })).toHaveFocus());
  });

  it('cancels on Escape and confirms on the action', () => {
    const { onConfirm, onCancel } = setup();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('disables both buttons while busy', () => {
    setup({ busy: true });
    const buttons = screen.getAllByRole('button').filter((b) => !b.hasAttribute('aria-hidden'));
    expect(buttons).toHaveLength(2);
    for (const b of buttons) expect(b).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  it('uses the given cancel label', () => {
    setup();
    expect(screen.getByRole('button', { name: 'Keep' })).toBeInTheDocument();
  });
});
