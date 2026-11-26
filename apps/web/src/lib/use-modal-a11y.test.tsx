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

function Panel({ onClose }: { onClose: () => void }) {
  const ref = useModalA11y<HTMLDivElement>(true, onClose, undefined, { modal: false });
  return (
    <div ref={ref}>
      <button type="button">First</button>
      <button type="button">Last</button>
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

  // A chooser opened over a dialog: Escape answers the chooser, and the
  // dialog underneath stays open.
  it('lets only the innermost open modal answer Escape', () => {
    const outerClose = vi.fn();
    const innerClose = vi.fn();
    const { unmount } = render(
      <>
        <Dialog onClose={outerClose} />
        <Dialog onClose={innerClose} />
      </>,
    );

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(innerClose).toHaveBeenCalledTimes(1);
    expect(outerClose).not.toHaveBeenCalled();
    unmount();
  });

  it('hands Escape back to the outer modal once the inner one closes', () => {
    const outerClose = vi.fn();
    const Pair = ({ inner }: { inner: boolean }) => (
      <>
        <Dialog onClose={outerClose} />
        {inner && <Dialog onClose={() => {}} />}
      </>
    );
    const { rerender } = render(<Pair inner />);
    rerender(<Pair inner={false} />);

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(outerClose).toHaveBeenCalledTimes(1);
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

// A panel beside the page, not over it: the page stays scrollable and
// reachable, and the panel answers the keyboard only while it holds focus.
describe('useModalA11y with modal: false', () => {
  it('neither locks the page nor traps Tab', () => {
    render(<Panel onClose={() => {}} />);
    for (const button of screen.getAllByRole('button')) {
      vi.spyOn(button, 'getClientRects').mockReturnValue(rectList());
    }

    expect(document.body.style.overflow).not.toBe('hidden');

    const last = screen.getByRole('button', { name: 'Last' });
    last.focus();
    const tab = fireEvent.keyDown(document, { key: 'Tab' });
    expect(tab).toBe(true);
    expect(document.activeElement).toBe(last);
  });

  it('answers Escape only while focus is inside it or nowhere', () => {
    const onClose = vi.fn();
    render(
      <>
        <input aria-label="Outside" />
        <Panel onClose={onClose} />
      </>,
    );

    screen.getByLabelText('Outside').focus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();

    screen.getByRole('button', { name: 'First' }).focus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);

    (document.activeElement as HTMLElement).blur();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('leaves an Escape another handler already took', () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} />);
    screen.getByRole('button', { name: 'First' }).focus();

    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
    event.preventDefault();
    document.dispatchEvent(event);

    expect(onClose).not.toHaveBeenCalled();
  });

  it('returns focus only when it held focus as it closed', async () => {
    function Harness({ open }: { open: boolean }) {
      return (
        <>
          <button type="button">Trigger</button>
          <input aria-label="Elsewhere" />
          {open && <Panel onClose={() => {}} />}
        </>
      );
    }
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    const { rerender } = render(<Harness open={false} />);
    const trigger = screen.getByRole('button', { name: 'Trigger' });

    trigger.focus();
    rerender(<Harness open={true} />);
    await waitFor(() => expect(document.activeElement).toBe(screen.getByText('First')));
    rerender(<Harness open={false} />);
    expect(document.activeElement).toBe(trigger);

    trigger.focus();
    rerender(<Harness open={true} />);
    await waitFor(() => expect(document.activeElement).toBe(screen.getByText('First')));
    const elsewhere = screen.getByLabelText('Elsewhere');
    fireEvent.pointerDown(elsewhere);
    elsewhere.focus();
    rerender(<Harness open={false} />);
    expect(document.activeElement).toBe(elsewhere);
  });
});
