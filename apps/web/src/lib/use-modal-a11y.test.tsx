import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useRef } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useModalA11y } from './use-modal-a11y';

function Dialog({ onClose }: { onClose: () => void }) {
  const ref = useModalA11y<HTMLDivElement>(true, onClose);
  return (
    <div ref={ref}>
      <button type="button">First</button>
      <button type="button">Last</button>
    </div>
  );
}

function DialogWithInitialFocus() {
  const inputRef = useRef<HTMLInputElement>(null);
  const ref = useModalA11y<HTMLDivElement>(true, () => {}, inputRef);
  return (
    <div ref={ref}>
      <button type="button">First</button>
      <input type="text" aria-label="Named" ref={inputRef} />
    </div>
  );
}

function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

afterEach(() => {
  document.body.style.overflow = '';
  vi.restoreAllMocks();
});

describe('useModalA11y', () => {
  it('locks body scroll, closes on Escape, and traps Tab inside the container', () => {
    const onClose = vi.fn();
    render(<Dialog onClose={onClose} />);
    for (const button of screen.getAllByRole('button')) {
      vi.spyOn(button, 'getClientRects').mockReturnValue(rectList());
    }

    expect(document.body.style.overflow).toBe('hidden');

    const [first, last] = screen.getAllByRole('button');
    expect(first).toBeDefined();
    expect(last).toBeDefined();
    if (!first || !last) return;
    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('focuses the control the caller names', async () => {
    render(<DialogWithInitialFocus />);

    await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText('Named')));
  });

  it('returns focus to the element that was focused before it opened', async () => {
    function Harness({ open }: { open: boolean }) {
      return (
        <>
          <button type="button">Trigger</button>
          {open && <Dialog onClose={() => {}} />}
        </>
      );
    }
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    const { rerender } = render(<Harness open={false} />);
    const trigger = screen.getByRole('button', { name: 'Trigger' });
    trigger.focus();

    rerender(<Harness open={true} />);
    await waitFor(() => expect(document.activeElement).not.toBe(trigger));

    rerender(<Harness open={false} />);
    expect(document.activeElement).toBe(trigger);
  });
});
